// refaudio.go implements the "ref" Moon Companion engine: zero-shot
// reference-timbre synthesis through a local GPT-SoVITS api_v2
// compatible service (works with api_v2, api.py descendants and most
// forks: POST {endpoint}/tts with ref_audio_path + prompt_text). The
// reference audio stays on the same machine as the service, so the
// server-local path is passed through untouched. It also owns the
// reference-directory browser for tts.refAudios.
package tts

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

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

type refEngine struct{ client *http.Client }

// NewRefEngine returns the reference-timbre engine.
func NewRefEngine() Engine {
	return &refEngine{client: &http.Client{Timeout: 30 * time.Second}}
}

// RefVoices is the pseudo voice list for tts.voices(engine=ref).
func RefVoices() []Voice {
	return []Voice{{
		VoiceID:     "ref",
		DisplayName: "参考音色（跟随所选参考音频）",
		Gender:      "neutral",
		Lang:        "zh-CN",
	}}
}

func (r *refEngine) Voices() ([]Voice, error) { return RefVoices(), nil }

func (r *refEngine) Synthesize(in SynthesizeInput) (SynthesizeResult, bool, error) {
	if !strings.HasPrefix(in.RefEndpoint, "http://") && !strings.HasPrefix(in.RefEndpoint, "https://") {
		return SynthesizeResult{}, false, fmt.Errorf("%w: 参考音色服务地址无效", ErrSynthesisFailed)
	}
	if in.RefWavPath == "" {
		return SynthesizeResult{}, false, fmt.Errorf("%w: 未选择参考音频", ErrSynthesisFailed)
	}
	if info, err := os.Stat(in.RefWavPath); err != nil || info.IsDir() {
		return SynthesizeResult{}, false, fmt.Errorf("%w: 参考音频文件不存在", ErrSynthesisFailed)
	}
	body, _ := json.Marshal(map[string]any{
		"text":             in.Text,
		"text_lang":        "zh",
		"ref_audio_path":   in.RefWavPath,
		"prompt_text":      in.RefPromptText,
		"prompt_lang":      "zh",
		"text_split_method": "cut0", // segments are already split upstream
		"media_type":       "wav",
		"streaming_mode":   false,
	})
	endpoint := strings.TrimRight(in.RefEndpoint, "/") + "/tts"
	resp, err := r.client.Post(endpoint, "application/json", bytes.NewReader(body))
	if err != nil {
		return SynthesizeResult{}, false, fmt.Errorf("%w: 无法连接参考音色服务（%v）", ErrSynthesisFailed, err)
	}
	defer resp.Body.Close()
	wav, err := io.ReadAll(io.LimitReader(resp.Body, refMaxAudio))
	if err != nil {
		return SynthesizeResult{}, false, fmt.Errorf("%w: 读取合成结果失败", ErrSynthesisFailed)
	}
	if resp.StatusCode != http.StatusOK || len(wav) < 44 || string(wav[:4]) != "RIFF" {
		return SynthesizeResult{}, false, fmt.Errorf("%w: 参考音色服务返回异常（HTTP %d）", ErrSynthesisFailed, resp.StatusCode)
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
