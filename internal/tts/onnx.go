// onnx.go implements the "onnx" Moon Companion engine: a fully offline,
// install-and-use local voice built on the sherpa-onnx offline-tts binary
// driving the Kokoro multi-lang model. Unlike the "ref" (GPT-SoVITS) path it
// needs no Python, no server process and no reference audio — a verified
// download of a ~21 MB runtime and a ~333 MB model into
// %LOCALAPPDATA%\Lunitide is all it takes, after which every synthesis is a
// single short-lived subprocess that writes one WAV.
//
// The engine is deliberately config-driven: kokoroConfig() names the binary,
// the model files and the speaker table, and onnxEngine turns a request into
// argv through the pure buildOnnxArgs. That split is what lets the whole
// render path be unit-tested with a fake runner and a synthetic config, with
// the real binary exercised only by the env-gated integration test.
package tts

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Bundle directory names under RefEngineDataRoot() (…\Lunitide). They are the
// install targets for the two downloads and the lookup keys the engine uses
// to find the binary and model on disk, so they are part of the contract.
const (
	// OnnxRuntimeBundleID holds the sherpa-onnx offline-tts binary and its
	// co-located onnxruntime DLLs (bin/…). Kept separate from the ASR
	// runtime bundle because that one is the -no-tts build.
	OnnxRuntimeBundleID = "onnx-tts-runtime"
	// OnnxModelBundleID holds the extracted Kokoro multi-lang pack
	// (model.onnx, voices.bin, tokens.txt, lexicons, espeak-ng-data, …).
	OnnxModelBundleID = "kokoro-multi-lang-v1_0"
)

// onnxVoice maps a stable, engine-scoped voice_id onto a Kokoro speaker id.
// The Chinese speakers (sid 45-52) are distilled from the Azure zh-CN voices
// and keep those familiar names; two English speakers round out the list for
// mixed replies.
type onnxVoice struct {
	ID     string
	Name   string
	Gender string
	Lang   string
	SID    int
}

// onnxVoiceCatalog is the fixed Kokoro speaker subset exposed to the picker.
// Order is display order: Mandarin female, Mandarin male, then English.
var onnxVoiceCatalog = []onnxVoice{
	{ID: "onnx-zf-xiaoxiao", Name: "晓晓 · 温柔女声", Gender: "female", Lang: "zh-CN", SID: 47},
	{ID: "onnx-zf-xiaoyi", Name: "晓伊 · 清亮女声", Gender: "female", Lang: "zh-CN", SID: 48},
	{ID: "onnx-zf-xiaobei", Name: "晓北 · 亲切女声", Gender: "female", Lang: "zh-CN", SID: 45},
	{ID: "onnx-zf-xiaoni", Name: "晓妮 · 甜美女声", Gender: "female", Lang: "zh-CN", SID: 46},
	{ID: "onnx-zm-yunxi", Name: "云希 · 沉稳男声", Gender: "male", Lang: "zh-CN", SID: 50},
	{ID: "onnx-zm-yunyang", Name: "云扬 · 播报男声", Gender: "male", Lang: "zh-CN", SID: 52},
	{ID: "onnx-zm-yunjian", Name: "云健 · 磁性男声", Gender: "male", Lang: "zh-CN", SID: 49},
	{ID: "onnx-zm-yunxia", Name: "云夏 · 少年男声", Gender: "male", Lang: "zh-CN", SID: 51},
	{ID: "onnx-af-heart", Name: "Heart · 英文女声", Gender: "female", Lang: "en-US", SID: 3},
	{ID: "onnx-am-michael", Name: "Michael · 英文男声", Gender: "male", Lang: "en-US", SID: 16},
}

// OnnxDefaultVoiceID is the out-of-the-box voice: a natural Mandarin female.
const OnnxDefaultVoiceID = "onnx-zf-xiaoxiao"

// onnxVoiceGroup groups the catalogue under one settings header.
const onnxVoiceGroup = "本地 Kokoro"

// OnnxVoices returns the picker catalogue. It does not require the model to
// be installed: the list is compiled in, and Synthesize is what guards on
// the download being present.
func OnnxVoices() []Voice {
	out := make([]Voice, 0, len(onnxVoiceCatalog))
	for _, v := range onnxVoiceCatalog {
		out = append(out, Voice{
			VoiceID:     v.ID,
			DisplayName: v.Name,
			Gender:      v.Gender,
			Lang:        v.Lang,
			Group:       onnxVoiceGroup,
		})
	}
	return out
}

// onnxSIDFor resolves a voice_id to a Kokoro speaker id. An empty or unknown
// id falls back to the default voice and reports true so the bridge can send
// the M95-004 fallback notice.
func onnxSIDFor(voiceID string) (int, bool) {
	if voiceID == "" {
		// Empty means "use the default voice" by design (the picker always
		// sends one); it is not a missing-voice fallback.
		return onnxSIDByID(OnnxDefaultVoiceID), false
	}
	for _, v := range onnxVoiceCatalog {
		if v.ID == voiceID {
			return v.SID, false
		}
	}
	return onnxSIDByID(OnnxDefaultVoiceID), true
}

func onnxSIDByID(voiceID string) int {
	for _, v := range onnxVoiceCatalog {
		if v.ID == voiceID {
			return v.SID
		}
	}
	return 0
}

// onnxConfig names everything a Kokoro-style sherpa pack needs. It is a value
// so a test can supply a synthetic one whose files it created, driving the
// whole argv/read/decode path without the 350 MB model.
type onnxConfig struct {
	BinaryRel  string   // relative to the runtime bundle dir
	ModelFile  string   // relative to the model bundle dir
	VoicesFile string   // "
	TokensFile string   // "
	DataDir    string   // espeak-ng-data, relative to the model bundle dir
	Lexicons   []string // relative to the model bundle dir, joined with ","
	RuleFSTs   []string // relative to the model bundle dir, joined with ","
	Threads    int
}

// kokoroConfig is the built-in Kokoro multi-lang v1.0 wiring, matching the
// extracted pack layout. dict_dir is intentionally omitted: sherpa-onnx
// ≥1.12.15 ignores it for this model and warns when it is passed.
func kokoroConfig() onnxConfig {
	return onnxConfig{
		BinaryRel:  filepath.Join("bin", "sherpa-onnx-offline-tts.exe"),
		ModelFile:  "model.onnx",
		VoicesFile: "voices.bin",
		TokensFile: "tokens.txt",
		DataDir:    "espeak-ng-data",
		Lexicons:   []string{"lexicon-us-en.txt", "lexicon-zh.txt"},
		RuleFSTs:   []string{"phone-zh.fst", "date-zh.fst", "number-zh.fst"},
		Threads:    2,
	}
}

// onnxEngine renders one segment per subprocess. root/rootFn locate the
// install; run is the process runner (execOnnx by default, a stub in tests).
type onnxEngine struct {
	cfg     onnxConfig
	root    string        // explicit data root; empty means resolve via rootFn
	rootFn  func() string // default: RefEngineDataRoot
	run     func(ctx context.Context, bin string, args []string, workdir string) error
	timeout time.Duration
}

// NewOnnxEngine wires the built-in Kokoro config against the per-user data
// root and the real subprocess runner. The 90s budget covers a cold CPU
// synthesis of a full 500-char segment with room to spare (measured RTF is
// well above 1 on a warm run, but the first call pays model load).
func NewOnnxEngine() Engine {
	return &onnxEngine{
		cfg:     kokoroConfig(),
		rootFn:  RefEngineDataRoot,
		run:     execOnnx,
		timeout: 90 * time.Second,
	}
}

func (o *onnxEngine) dataRoot() string {
	if o.root != "" {
		return o.root
	}
	if o.rootFn != nil {
		return o.rootFn()
	}
	return ""
}

// dirs resolves the runtime and model directories. Explicit env overrides win
// so the integration test (and a power user with a pre-placed pack) can point
// at any location; otherwise they are subdirectories of the data root.
func (o *onnxEngine) dirs() (runtimeDir, modelDir string, ok bool) {
	if rt := strings.TrimSpace(os.Getenv("LUNITIDE_ONNX_TTS_RUNTIME_DIR")); rt != "" {
		if md := strings.TrimSpace(os.Getenv("LUNITIDE_ONNX_TTS_MODEL_DIR")); md != "" {
			return rt, md, true
		}
	}
	root := o.dataRoot()
	if root == "" {
		return "", "", false
	}
	return filepath.Join(root, OnnxRuntimeBundleID), filepath.Join(root, OnnxModelBundleID), true
}

// ready reports the resolved binary path and model dir, or ErrEngineUnavailable
// with a message the bridge maps to a retryable M95-001 (prompting install).
func (o *onnxEngine) ready() (bin, modelDir string, err error) {
	rt, md, ok := o.dirs()
	if !ok {
		return "", "", fmt.Errorf("%w: 无法定位本地语音目录", ErrEngineUnavailable)
	}
	bin = filepath.Join(rt, o.cfg.BinaryRel)
	if _, statErr := os.Stat(bin); statErr != nil {
		return "", "", fmt.Errorf("%w: 本地语音引擎未安装（请在设置中下载）", ErrEngineUnavailable)
	}
	if _, statErr := os.Stat(filepath.Join(md, o.cfg.ModelFile)); statErr != nil {
		return "", "", fmt.Errorf("%w: 本地语音模型未安装（请在设置中下载）", ErrEngineUnavailable)
	}
	return bin, md, nil
}

func (o *onnxEngine) Voices() ([]Voice, error) { return OnnxVoices(), nil }

func (o *onnxEngine) Synthesize(in SynthesizeInput) (SynthesizeResult, bool, error) {
	bin, modelDir, err := o.ready()
	if err != nil {
		return SynthesizeResult{}, false, err
	}
	sid, fallback := onnxSIDFor(in.VoiceID)

	out, err := os.CreateTemp("", "lunitide-onnx-*.wav")
	if err != nil {
		return SynthesizeResult{}, false, fmt.Errorf("%w: 无法创建临时音频文件", ErrSynthesisFailed)
	}
	outPath := out.Name()
	_ = out.Close()
	defer func() { _ = os.Remove(outPath) }()

	args := buildOnnxArgs(o.cfg, modelDir, sid, outPath, in.Rate, in.Text)
	ctx, cancel := context.WithTimeout(context.Background(), o.timeout)
	defer cancel()
	// The binary loads its onnxruntime DLLs from its own directory, so it is
	// launched with that as the working directory.
	if runErr := o.run(ctx, bin, args, filepath.Dir(bin)); runErr != nil {
		return SynthesizeResult{}, false, fmt.Errorf("%w: 本地语音合成失败（%v）", ErrSynthesisFailed, runErr)
	}
	wav, readErr := os.ReadFile(outPath)
	if readErr != nil || len(wav) < 44 || string(wav[:4]) != "RIFF" {
		return SynthesizeResult{}, false, fmt.Errorf("%w: 本地语音未生成有效音频", ErrSynthesisFailed)
	}
	return SynthesizeResult{
		WavBase64:    base64.StdEncoding.EncodeToString(wav),
		DurationHint: wavDurationSeconds(wav),
	}, fallback, nil
}

// buildOnnxArgs turns a request into the sherpa-onnx-offline-tts argv. Pure
// (no I/O), so a test pins the exact flags without a binary. The text is the
// final positional argument, exactly as the binary expects.
func buildOnnxArgs(cfg onnxConfig, modelDir string, sid int, outPath string, rate int, text string) []string {
	joinRel := func(names []string) string {
		parts := make([]string, 0, len(names))
		for _, n := range names {
			parts = append(parts, filepath.Join(modelDir, n))
		}
		return strings.Join(parts, ",")
	}
	threads := cfg.Threads
	if threads <= 0 {
		threads = 2
	}
	args := []string{
		"--kokoro-model=" + filepath.Join(modelDir, cfg.ModelFile),
		"--kokoro-voices=" + filepath.Join(modelDir, cfg.VoicesFile),
		"--kokoro-tokens=" + filepath.Join(modelDir, cfg.TokensFile),
		"--kokoro-data-dir=" + filepath.Join(modelDir, cfg.DataDir),
	}
	if len(cfg.Lexicons) > 0 {
		args = append(args, "--kokoro-lexicon="+joinRel(cfg.Lexicons))
	}
	if len(cfg.RuleFSTs) > 0 {
		args = append(args, "--tts-rule-fsts="+joinRel(cfg.RuleFSTs))
	}
	args = append(args,
		"--kokoro-length-scale="+onnxLengthScale(rate),
		"--num-threads="+strconv.Itoa(threads),
		"--sid="+strconv.Itoa(sid),
		"--output-filename="+outPath,
		text,
	)
	return args
}

// onnxLengthScale maps the companion rate [-10,10] onto Kokoro's length
// scale, where larger is slower. Rate 0 is 1.0; the ends land at a natural
// 0.7×–1.4× so a fast or slow preference is audible but never garbled.
func onnxLengthScale(rate int) string {
	if rate < -10 {
		rate = -10
	}
	if rate > 10 {
		rate = 10
	}
	scale := 1.0 - float64(rate)*0.03
	return strconv.FormatFloat(scale, 'f', 2, 64)
}

// execOnnx runs one synthesis subprocess, surfacing a trimmed stderr tail so
// a failure carries a clue without dumping the argv echo the binary prints.
func execOnnx(ctx context.Context, bin string, args []string, workdir string) error {
	cmd := exec.CommandContext(ctx, bin, args...)
	if workdir != "" {
		cmd.Dir = workdir
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if tail := onnxErrTail(stderr.String()); tail != "" {
			return fmt.Errorf("%v: %s", err, tail)
		}
		return err
	}
	return nil
}

// onnxErrTail extracts the most useful stderr line: sherpa prints its whole
// argv on start, so the plain tail would be noise. A real error line is kept
// and clipped.
func onnxErrTail(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	lines := strings.Split(s, "\n")
	last := strings.TrimSpace(lines[len(lines)-1])
	if len(last) > 200 {
		last = last[:200] + "…"
	}
	return last
}
