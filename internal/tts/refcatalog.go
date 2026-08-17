// refcatalog.go defines the built-in GPT-SoVITS character-voice pack:
// 18 preset role voices resolved from a local reference-audio collection
// (逗哥音色整理合集\角色扮演) through the local GPT-SoVITS api_v2
// service. Preset voice IDs are "refpack:<file>" so the settings page
// needs no file picker — picking a voice from the dropdown is enough.
package tts

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// DefaultRefEndpoint is the GPT-SoVITS api_v2 default port. The webui
// itself serves port 9874 and exposes no stable programmatic TTS route,
// so the dedicated api_v2 service on 9880 is the integration target.
const DefaultRefEndpoint = "http://127.0.0.1:9880"

// DefaultRefPackDir points at the shipped character-voice collection.
// A var (not const) so tests can point it at fixtures.
var DefaultRefPackDir = `E:\AI电影漫剧\800+音色合集\逗哥音色整理合集\角色扮演`

// RefPresetVoiceIDPrefix marks catalog voice IDs ("refpack:甜心少女.wav").
const RefPresetVoiceIDPrefix = "refpack:"

type refPreset struct {
	File   string
	Gender string
	Group  string
}

// refPresets picks 18 distinct role timbres across four style groups so
// the Moon Companion can switch persona without touching files. Every
// reference WAV must stay inside GPT-SoVITS's 3–10s clone window (all
// entries verified: 3.1s–6s at 24kHz mono).
var refPresets = []refPreset{
	{"甜心少女.wav", "female", "甜美女声 · 萝莉 / 萌妹"},
	{"甜美萌妹.wav", "female", "甜美女声 · 萝莉 / 萌妹"},
	{"超嗲萌妹.wav", "female", "甜美女声 · 萝莉 / 萌妹"},
	{"软糯萝莉.wav", "female", "甜美女声 · 萝莉 / 萌妹"},
	{"娇媚萝莉.wav", "female", "甜美女声 · 萝莉 / 萌妹"},
	{"阳光甜心.wav", "female", "甜美女声 · 萝莉 / 萌妹"},
	{"开朗妹妹.wav", "female", "温柔女声 · 妹妹 / 御姐 / 闺蜜"},
	{"温暖御姐.wav", "female", "温柔女声 · 妹妹 / 御姐 / 闺蜜"},
	{"知性御姐.wav", "female", "温柔女声 · 妹妹 / 御姐 / 闺蜜"},
	{"俏皮姐姐.wav", "female", "温柔女声 · 妹妹 / 御姐 / 闺蜜"},
	{"稚嫩少女.wav", "female", "温柔女声 · 妹妹 / 御姐 / 闺蜜"},
	{"傲娇女王.wav", "female", "气场女声 · 女王 / 女神"},
	{"冰山女王.wav", "female", "气场女声 · 女王 / 女神"},
	{"娇媚女神.wav", "female", "气场女声 · 女王 / 女神"},
	{"青春女神.wav", "female", "气场女声 · 女王 / 女神"},
	{"阳光少年.wav", "male", "个性男声 · 少年 / 男神 / 大爷"},
	{"冷面霸总.wav", "male", "个性男声 · 少年 / 男神 / 大爷"},
	{"唠嗑大爷.wav", "male", "个性男声 · 少年 / 男神 / 大爷"},
}

// RefVoices returns the built-in preset catalogue for tts.voices.
func RefVoices() []Voice {
	out := make([]Voice, 0, len(refPresets))
	for _, p := range refPresets {
		out = append(out, Voice{
			VoiceID:     RefPresetVoiceIDPrefix + p.File,
			DisplayName: strings.TrimSuffix(p.File, ".wav"),
			Gender:      p.Gender,
			Lang:        "zh-CN",
			Group:       p.Group,
		})
	}
	return out
}

// RefDefaultVoiceID is applied when engine=ref carries no voiceId.
func RefDefaultVoiceID() string { return RefPresetVoiceIDPrefix + refPresets[0].File }

// IsRefPresetVoiceID reports whether voiceID addresses a catalog entry.
func IsRefPresetVoiceID(voiceID string) bool {
	return strings.HasPrefix(voiceID, RefPresetVoiceIDPrefix)
}

// RefResolveVoice maps a preset voice ID onto the reference WAV under
// packDir (DefaultRefPackDir when empty). The second result is false for
// non-preset IDs or unknown files.
func RefResolveVoice(voiceID, packDir string) (string, bool) {
	if !IsRefPresetVoiceID(voiceID) {
		return "", false
	}
	file := strings.TrimPrefix(voiceID, RefPresetVoiceIDPrefix)
	known := false
	for _, p := range refPresets {
		if p.File == file {
			known = true
			break
		}
	}
	if !known {
		return "", false
	}
	if packDir == "" {
		packDir = DefaultRefPackDir
	}
	return filepath.Join(packDir, file), true
}

// RefMeta is the tts.voices(engine=ref) health block for the settings
// page: which endpoint and pack folder are in use and whether they are
// reachable.
type RefMeta struct {
	Endpoint     string   `json:"endpoint"`
	PackDir      string   `json:"pack_dir"`
	ServerOnline bool     `json:"server_online"`
	PackExists   bool     `json:"pack_exists"`
	MissingFiles []string `json:"missing_files"`
}

// RefPackMeta probes the api_v2 service (any HTTP answer counts as
// online — the api_v2 root answers 200 with its landing page) and
// checks the preset files on disk. MissingFiles stays empty when the
// pack folder itself is absent (pack_exists=false says it already).
func RefPackMeta(endpoint string) RefMeta {
	if endpoint == "" {
		endpoint = DefaultRefEndpoint
	}
	meta := RefMeta{Endpoint: endpoint, PackDir: DefaultRefPackDir, MissingFiles: []string{}}
	client := &http.Client{Timeout: 800 * time.Millisecond}
	if resp, err := client.Get(strings.TrimRight(endpoint, "/") + "/"); err == nil {
		_ = resp.Body.Close()
		meta.ServerOnline = true
	}
	if info, err := os.Stat(DefaultRefPackDir); err == nil && info.IsDir() {
		meta.PackExists = true
		for _, p := range refPresets {
			if _, err := os.Stat(filepath.Join(DefaultRefPackDir, p.File)); err != nil {
				meta.MissingFiles = append(meta.MissingFiles, p.File)
			}
		}
	}
	return meta
}

// refSpeedFactor maps the SAPI rate [-10,10] onto the api_v2
// speed_factor range [0.5,2.0]: rate 0 → 1.0, +10 → 1.6, -10 → 0.5.
func refSpeedFactor(rate int) float64 {
	speed := 1 + float64(rate)*0.06
	if speed < 0.5 {
		speed = 0.5
	}
	if speed > 2 {
		speed = 2
	}
	return speed
}
