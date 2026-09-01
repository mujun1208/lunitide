// refaudio.go implements the "ref" Moon Companion engine: zero-shot
// reference-timbre synthesis through a local GPT-SoVITS api_v2
// compatible service (POST {endpoint}/tts with ref_audio_path +
// prompt_text). The reference audio stays on the same machine as the
// service, so the server-local path is passed through untouched. It
// also owns the reference-directory browser for tts.refAudios. The
// built-in 18-voice character catalogue (refpack: IDs, default
// endpoint and pack folder) lives in refcatalog.go.
package tts

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"time"
)

// refStreamingOff is set after a streaming_mode=true probe returns non-RIFF
// or HTTP failure. Companion may TryStreaming; production Synthesize stays off.
var refStreamingOff atomic.Bool

func refStreamingBanned() bool { return refStreamingOff.Load() }

func banRefStreaming() { refStreamingOff.Store(true) }

func resetRefStreamingBanForTest() { refStreamingOff.Store(false) }

// refTTSBody is the GPT-SoVITS api_v2 JSON. Both streaming modes must parse;
// the bat and port stay untouched.
func refTTSBody(text, refWav, prompt string, rate int, streaming bool) ([]byte, error) {
	return json.Marshal(map[string]any{
		"text":              text,
		"text_lang":         "zh",
		"ref_audio_path":    refWav,
		"prompt_text":       prompt,
		"prompt_lang":       "zh",
		"text_split_method": "cut0",
		"media_type":        "wav",
		"streaming_mode":    streaming,
		"speed_factor":      refSpeedFactor(rate),
	})
}

// RefAudioEntry is one row of the tts.refAudios directory listing.
type RefAudioEntry struct {
	Name      string `json:"name"`
	Path      string `json:"path"`
	IsDir     bool   `json:"is_dir"`
	SizeBytes int64  `json:"size_bytes"`
}

var refAudioExts = map[string]bool{
	".wav": true, ".mp3": true, ".flac": true, ".ogg": true,
	".m4a": true, ".aac": true, ".opus": true, ".wma": true,
}

const refMaxAudio = 32 << 20 // 32 MiB response cap

// First connection-refused wait. Cold loads take 30-90s; if /docs is
// still silent after this, return ErrRefEngineStarting so the player
// retries instead of occupying the bridge for a full minute.
const refHostFirstWait = 8 * time.Second

type refEngine struct{ client *http.Client }

// NewRefEngine returns the reference-timbre engine. The 120s budget matters:
// GPT-SoVITS on CPU serializes concurrent segment syntheses (the companion
// double-prefetch fires two POSTs), so a queued segment can legitimately wait
// 30s+ before its own ~30s inference — a 30s client timeout cut requests off
// exactly at that boundary and surfaced as synthesis failures.
func NewRefEngine() Engine {
	return &refEngine{client: &http.Client{Timeout: 120 * time.Second}}
}

// wrapRefHostErr keeps the starting window in the M95-001 family. Wrapping
// it as ErrSynthesisFailed used to turn a 30-90s cold load into a hard
// "该段语音合成失败" on the settings preview and the companion player.
func refServiceErrorDetail(status int, body []byte) string {
	text := strings.TrimSpace(string(body))
	if text == "" || strings.Contains(text, "<") {
		return fmt.Sprintf("HTTP %d", status)
	}
	var payload map[string]any
	if json.Unmarshal(body, &payload) == nil {
		for _, key := range []string{"message", "detail", "error", "msg"} {
			if s, ok := payload[key].(string); ok && strings.TrimSpace(s) != "" {
				text = strings.TrimSpace(s)
				break
			}
		}
	}
	runes := []rune(text)
	if len(runes) > 120 {
		text = string(runes[:120]) + "…"
	}
	return fmt.Sprintf("HTTP %d：%s", status, text)
}

func wrapRefHostErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrRefEngineStarting) {
		return err
	}
	return fmt.Errorf("%w: %v", ErrSynthesisFailed, err)
}

func (r *refEngine) Voices() ([]Voice, error) { return RefVoices(), nil }

func (r *refEngine) Synthesize(in SynthesizeInput) (SynthesizeResult, bool, error) {
	endpoint := CanonicalRefEndpoint(in.RefEndpoint)
	if !strings.HasPrefix(endpoint, "http://") && !strings.HasPrefix(endpoint, "https://") {
		return SynthesizeResult{}, false, fmt.Errorf("%w: 参考音色服务地址无效", ErrSynthesisFailed)
	}
	refWav := in.RefWavPath
	if path, ok := RefResolveVoice(in.VoiceID, ""); ok {
		refWav = path // refpack:<file> beats an explicit path
	}
	if refWav == "" {
		return SynthesizeResult{}, false, fmt.Errorf("%w: 未选择参考音频", ErrSynthesisFailed)
	}
	if info, err := os.Stat(refWav); err != nil || info.IsDir() {
		if IsRefPresetVoiceID(in.VoiceID) {
			return SynthesizeResult{}, false, fmt.Errorf("%w: 音色包文件不存在（%s）", ErrSynthesisFailed, DefaultRefPackDir)
		}
		return SynthesizeResult{}, false, fmt.Errorf("%w: 参考音频文件不存在", ErrSynthesisFailed)
	}
	// A narration sample running to a minute is a perfectly good timbre and
	// an impossible prompt; trimmed to the window rather than refused.
	refWav = refPromptClip(refWav)
	// L-2: both JSON modes must parse. Default stays false until a companion
	// probe proves streaming clips stay ≤200ms — untested default-on jams CPU.
	body, err := refTTSBody(in.Text, refWav, in.RefPromptText, in.Rate, in.TryStreaming && !refStreamingBanned())
	if err != nil {
		return SynthesizeResult{}, false, fmt.Errorf("%w: 参考音色请求无效", ErrSynthesisFailed)
	}
	resp, err := r.client.Post(strings.TrimRight(endpoint, "/")+"/tts", "application/json", bytes.NewReader(body))
	if err != nil {
		// Connection refused on the default local endpoint → auto-host
		// the GPT-SoVITS service (spawn + wait for the model to load),
		// then retry once. Custom endpoints (LAN/remote services) and
		// test stubs never trigger a spawn. This is what makes the
		// 50-preset catalogue work without the user ever launching the
		// model server by hand.
		if IsDefaultRefEndpoint(endpoint) {
			if DefaultRefHost.IsLaunching(endpoint) {
				return SynthesizeResult{}, false, fmt.Errorf("%w: 语音引擎仍在启动", ErrRefEngineStarting)
			}
			if hostErr := DefaultRefHost.EnsureRunning(endpoint, refHostFirstWait); hostErr == nil {
				resp, err = r.client.Post(strings.TrimRight(endpoint, "/")+"/tts", "application/json", bytes.NewReader(body))
			} else {
				return SynthesizeResult{}, false, wrapRefHostErr(hostErr)
			}
			if err != nil {
				return SynthesizeResult{}, false, fmt.Errorf("%w: 无法连接参考音色服务（%v）", ErrSynthesisFailed, err)
			}
		} else {
			return SynthesizeResult{}, false, fmt.Errorf("%w: 无法连接参考音色服务（%v）", ErrSynthesisFailed, err)
		}
	}
	defer resp.Body.Close()
	wav, err := io.ReadAll(io.LimitReader(resp.Body, refMaxAudio))
	if err != nil {
		return SynthesizeResult{}, false, fmt.Errorf("%w: 读取合成结果失败", ErrSynthesisFailed)
	}
	if resp.StatusCode != http.StatusOK || len(wav) < 44 || string(wav[:4]) != "RIFF" {
		if in.TryStreaming && !refStreamingBanned() {
			banRefStreaming()
			retry := in
			retry.TryStreaming = false
			return r.Synthesize(retry)
		}
		return SynthesizeResult{}, false, fmt.Errorf("%w: 参考音色服务返回异常（%s）", ErrSynthesisFailed, refServiceErrorDetail(resp.StatusCode, wav))
	}
	return SynthesizeResult{
		WavBase64:    base64.StdEncoding.EncodeToString(wav),
		DurationHint: wavDurationSeconds(wav),
	}, false, nil
}

// wavDurationSeconds reads the byte rate from the fmt chunk and the data
// chunk size (chunk-walk, so LIST/INFO extras do not break it).
func wavDurationSeconds(wav []byte) float64 {
	if len(wav) < 44 || string(wav[:4]) != "RIFF" {
		return 0
	}
	byteRate := int(binary.LittleEndian.Uint32(wav[28:32]))
	if byteRate <= 0 {
		return 0
	}
	for pos := 12; pos+8 <= len(wav); {
		id := string(wav[pos : pos+4])
		size := int(binary.LittleEndian.Uint32(wav[pos+4 : pos+8]))
		if id == "data" {
			if size > len(wav)-pos-8 {
				size = len(wav) - pos - 8
			}
			return float64(size) / float64(byteRate)
		}
		pos += 8 + size + size%2
	}
	return 0
}

// ListRefAudioEntries browses dir for reference audio: audio files plus
// subdirectories (and ".." for walking up) so the settings page can
// navigate to folders like E:\AI电影漫剧\800+音色合集.
func ListRefAudioEntries(dir string) (string, []RefAudioEntry, error) {
	clean := filepath.Clean(dir)
	entries, err := os.ReadDir(clean)
	if err != nil {
		return clean, nil, err
	}
	out := make([]RefAudioEntry, 0, len(entries)+1)
	if parent := filepath.Dir(clean); parent != clean {
		out = append(out, RefAudioEntry{Name: "..", Path: parent, IsDir: true})
	}
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if entry.IsDir() {
			out = append(out, RefAudioEntry{Name: entry.Name() + "/", Path: filepath.Join(clean, entry.Name()), IsDir: true})
			continue
		}
		if !refAudioExts[strings.ToLower(filepath.Ext(entry.Name()))] {
			continue
		}
		out = append(out, RefAudioEntry{Name: entry.Name(), Path: filepath.Join(clean, entry.Name()), IsDir: false, SizeBytes: info.Size()})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].IsDir != out[j].IsDir {
			return out[i].IsDir // directories first, ".." stays on top
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return clean, out, nil
}
