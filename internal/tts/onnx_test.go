package tts

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// chineseProbeText is a mixed Mandarin sentence used by the env-gated real
// synthesis test to prove UTF-8 argv survives the Windows exec path.
const chineseProbeText = "你好，我是月汐，很高兴见到你。"

func TestOnnxVoicesCatalogue(t *testing.T) {
	voices := OnnxVoices()
	if len(voices) != len(onnxVoiceCatalog) || len(voices) == 0 {
		t.Fatalf("OnnxVoices len = %d, want %d", len(voices), len(onnxVoiceCatalog))
	}
	var sawDefault bool
	seen := map[string]bool{}
	for _, v := range voices {
		if v.VoiceID == "" || v.DisplayName == "" {
			t.Errorf("voice missing id/name: %+v", v)
		}
		if v.Group != onnxVoiceGroup {
			t.Errorf("voice %s group = %q, want %q", v.VoiceID, v.Group, onnxVoiceGroup)
		}
		if seen[v.VoiceID] {
			t.Errorf("duplicate voice id %s", v.VoiceID)
		}
		seen[v.VoiceID] = true
		if v.VoiceID == OnnxDefaultVoiceID {
			sawDefault = true
		}
	}
	if !sawDefault {
		t.Fatalf("default voice %s not in catalogue", OnnxDefaultVoiceID)
	}
}

func TestOnnxSIDFor(t *testing.T) {
	if sid, fb := onnxSIDFor("onnx-zf-xiaoxiao"); sid != 47 || fb {
		t.Errorf("xiaoxiao => (%d,%v), want (47,false)", sid, fb)
	}
	if sid, fb := onnxSIDFor("onnx-am-michael"); sid != 16 || fb {
		t.Errorf("michael => (%d,%v), want (16,false)", sid, fb)
	}
	// Empty is the default by design, not a fallback notice.
	if sid, fb := onnxSIDFor(""); sid != 47 || fb {
		t.Errorf("empty => (%d,%v), want (47,false)", sid, fb)
	}
	// Unknown non-empty falls back and flags it.
	if sid, fb := onnxSIDFor("onnx-nope"); sid != 47 || !fb {
		t.Errorf("unknown => (%d,%v), want (47,true)", sid, fb)
	}
}

func TestOnnxLengthScale(t *testing.T) {
	for _, tc := range []struct {
		rate int
		want string
	}{{0, "1.00"}, {10, "0.70"}, {-10, "1.30"}, {100, "0.70"}, {-100, "1.30"}} {
		if got := onnxLengthScale(tc.rate); got != tc.want {
			t.Errorf("onnxLengthScale(%d) = %q, want %q", tc.rate, got, tc.want)
		}
	}
}

func TestBuildOnnxArgs(t *testing.T) {
	cfg := kokoroConfig()
	args := buildOnnxArgs(cfg, filepath.FromSlash("/m"), 47, filepath.FromSlash("/tmp/o.wav"), 0, "text-body")
	joined := strings.Join(args, "\x00")
	for _, want := range []string{
		"--kokoro-model=", "--kokoro-voices=", "--kokoro-tokens=",
		"--kokoro-data-dir=", "--kokoro-lexicon=", "--tts-rule-fsts=",
		"--kokoro-length-scale=1.00", "--num-threads=2", "--sid=47",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("args missing %q: %v", want, args)
		}
	}
	// The text must be the final positional argument.
	if args[len(args)-1] != "text-body" {
		t.Errorf("last arg = %q, want text", args[len(args)-1])
	}
	// dict-dir is intentionally omitted for Kokoro v1.0.
	if strings.Contains(joined, "--kokoro-dict-dir") {
		t.Errorf("unexpected --kokoro-dict-dir in %v", args)
	}
	// Lexicons joined by comma.
	var lex string
	for _, a := range args {
		if strings.HasPrefix(a, "--kokoro-lexicon=") {
			lex = a
		}
	}
	if strings.Count(lex, ",") != 1 {
		t.Errorf("lexicon flag = %q, want two comma-joined paths", lex)
	}
}

// onnxWavFixture builds a minimal but structurally valid 24kHz mono 16-bit
// WAV with a real byte rate and data chunk so wavDurationSeconds reads > 0.
func onnxWavFixture(samples int) []byte {
	const sampleRate = 24000
	byteRate := sampleRate * 2
	dataLen := samples * 2
	buf := make([]byte, 44+dataLen)
	copy(buf[0:4], "RIFF")
	binary.LittleEndian.PutUint32(buf[4:8], uint32(36+dataLen))
	copy(buf[8:12], "WAVE")
	copy(buf[12:16], "fmt ")
	binary.LittleEndian.PutUint32(buf[16:20], 16)
	binary.LittleEndian.PutUint16(buf[20:22], 1)
	binary.LittleEndian.PutUint16(buf[22:24], 1)
	binary.LittleEndian.PutUint32(buf[24:28], sampleRate)
	binary.LittleEndian.PutUint32(buf[28:32], uint32(byteRate))
	binary.LittleEndian.PutUint16(buf[32:34], 2)
	binary.LittleEndian.PutUint16(buf[34:36], 16)
	copy(buf[36:40], "data")
	binary.LittleEndian.PutUint32(buf[40:44], uint32(dataLen))
	return buf
}

// newFakeOnnxEngine sets up an engine whose runner writes a canned WAV to the
// requested output path, with the binary and model files present on disk.
func newFakeOnnxEngine(t *testing.T, run func(ctx context.Context, bin string, args []string, workdir string) error) *onnxEngine {
	t.Helper()
	root := t.TempDir()
	cfg := kokoroConfig()
	bin := filepath.Join(root, OnnxRuntimeBundleID, cfg.BinaryRel)
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin, []byte("stub"), 0o755); err != nil {
		t.Fatal(err)
	}
	modelDir := filepath.Join(root, OnnxModelBundleID)
	if err := os.MkdirAll(modelDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(modelDir, cfg.ModelFile), []byte("onnx"), 0o644); err != nil {
		t.Fatal(err)
	}
	return &onnxEngine{cfg: cfg, root: root, run: run, timeout: 5 * time.Second}
}

// outPathFromArgs extracts the --output-filename value.
func outPathFromArgs(args []string) string {
	for _, a := range args {
		if strings.HasPrefix(a, "--output-filename=") {
			return strings.TrimPrefix(a, "--output-filename=")
		}
	}
	return ""
}

func TestOnnxSynthesizeWritesWav(t *testing.T) {
	var gotSID, gotText string
	eng := newFakeOnnxEngine(t, func(_ context.Context, _ string, args []string, _ string) error {
		for _, a := range args {
			if strings.HasPrefix(a, "--sid=") {
				gotSID = a
			}
		}
		gotText = args[len(args)-1]
		return os.WriteFile(outPathFromArgs(args), onnxWavFixture(2400), 0o644)
	})
	res, fb, err := eng.Synthesize(SynthesizeInput{Text: "hello-body", VoiceID: "onnx-zm-yunxi"})
	if err != nil {
		t.Fatalf("Synthesize err = %v", err)
	}
	if fb {
		t.Errorf("known voice should not fall back")
	}
	if res.WavBase64 == "" || res.DurationHint <= 0 {
		t.Fatalf("empty result: %+v", res)
	}
	raw, decErr := base64.StdEncoding.DecodeString(res.WavBase64)
	if decErr != nil || string(raw[:4]) != "RIFF" {
		t.Fatalf("result is not a WAV: err=%v", decErr)
	}
	if gotSID != "--sid=50" {
		t.Errorf("yunxi sid arg = %q, want --sid=50", gotSID)
	}
	if gotText != "hello-body" {
		t.Errorf("text arg = %q", gotText)
	}
}

func TestOnnxSynthesizeUnknownVoiceFallsBack(t *testing.T) {
	eng := newFakeOnnxEngine(t, func(_ context.Context, _ string, args []string, _ string) error {
		return os.WriteFile(outPathFromArgs(args), onnxWavFixture(1200), 0o644)
	})
	_, fb, err := eng.Synthesize(SynthesizeInput{Text: "hi", VoiceID: "onnx-does-not-exist"})
	if err != nil {
		t.Fatalf("Synthesize err = %v", err)
	}
	if !fb {
		t.Errorf("unknown voice should report fallback")
	}
}

func TestOnnxSynthesizeMissingModelUnavailable(t *testing.T) {
	eng := newFakeOnnxEngine(t, func(_ context.Context, _ string, args []string, _ string) error {
		return os.WriteFile(outPathFromArgs(args), onnxWavFixture(10), 0o644)
	})
	// Remove the model file to simulate a runtime-only (half-installed) state.
	if err := os.Remove(filepath.Join(eng.root, OnnxModelBundleID, eng.cfg.ModelFile)); err != nil {
		t.Fatal(err)
	}
	_, _, err := eng.Synthesize(SynthesizeInput{Text: "hi", VoiceID: OnnxDefaultVoiceID})
	if !errors.Is(err, ErrEngineUnavailable) {
		t.Fatalf("missing model err = %v, want ErrEngineUnavailable", err)
	}
}

func TestOnnxSynthesizeRunnerErrorIsSynthesisFailed(t *testing.T) {
	eng := newFakeOnnxEngine(t, func(_ context.Context, _ string, _ []string, _ string) error {
		return errors.New("boom")
	})
	_, _, err := eng.Synthesize(SynthesizeInput{Text: "hi", VoiceID: OnnxDefaultVoiceID})
	if !errors.Is(err, ErrSynthesisFailed) {
		t.Fatalf("runner error => %v, want ErrSynthesisFailed", err)
	}
}

func TestOnnxBundlesPinned(t *testing.T) {
	rt := OnnxRuntimeBundle()
	if rt.ID != OnnxRuntimeBundleID || len(rt.Downloads) != 1 {
		t.Fatalf("runtime bundle shape: %+v", rt)
	}
	if rt.Downloads[0].SHA256 == "" || rt.Downloads[0].Bytes <= 0 {
		t.Errorf("runtime download not pinned: %+v", rt.Downloads[0])
	}
	md := OnnxModelBundle()
	if md.Downloads[0].SHA256 == "" || md.Downloads[0].Bytes <= 0 {
		t.Errorf("model download not pinned: %+v", md.Downloads[0])
	}
	if len(OnnxBundles()) != 2 {
		t.Errorf("OnnxBundles should list runtime + model")
	}
}

// TestOnnxSynthesizeReal exercises the actual sherpa-onnx binary and Kokoro
// model when both are present on disk (env-gated so CI stays hermetic). It is
// the authoritative check that Chinese text survives the Windows argv path.
func TestOnnxSynthesizeReal(t *testing.T) {
	rt := strings.TrimSpace(os.Getenv("LUNITIDE_ONNX_TTS_RUNTIME_DIR"))
	md := strings.TrimSpace(os.Getenv("LUNITIDE_ONNX_TTS_MODEL_DIR"))
	if rt == "" || md == "" {
		t.Skip("set LUNITIDE_ONNX_TTS_RUNTIME_DIR and _MODEL_DIR to run the real synthesis")
	}
	eng := NewOnnxEngine()
	res, _, err := eng.Synthesize(SynthesizeInput{Text: chineseProbeText, VoiceID: OnnxDefaultVoiceID})
	if err != nil {
		t.Fatalf("real Synthesize err = %v", err)
	}
	raw, err := base64.StdEncoding.DecodeString(res.WavBase64)
	if err != nil || len(raw) < 1000 || string(raw[:4]) != "RIFF" {
		t.Fatalf("real synthesis produced no audio: len=%d err=%v", len(raw), err)
	}
	if res.DurationHint <= 0 {
		t.Errorf("duration hint = %v", res.DurationHint)
	}
	t.Logf("real synthesis ok: %d wav bytes, %.2fs", len(raw), res.DurationHint)
}
