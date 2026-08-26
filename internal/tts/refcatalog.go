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

// DefaultRefPackDir points at the shipped character-voice collection
// (role-play voices). A var (not const) so tests can point it at fixtures.
var DefaultRefPackDir = `E:\AI电影漫剧\800+音色合集\逗哥音色整理合集\角色扮演`

// DefaultRefPackDirHot is the second voice pack directory (popular timbres
// across age groups). Bundled presets from this directory use hotPackDir
// as their implicit base.
var DefaultRefPackDirHot = `E:\AI电影漫剧\800+音色合集\不同年龄人群音色\热门音色`

// RefPresetVoiceIDPrefix marks catalog voice IDs ("refpack:甜心少女.wav").
const RefPresetVoiceIDPrefix = "refpack:"

type refPreset struct {
	File    string
	Gender  string
	Group   string
	PackDir string // empty → DefaultRefPackDir; "hot" → DefaultRefPackDirHot
}

// refPresets is the role-timbre catalogue the settings dropdown offers.
//
// Every reference WAV must sit inside GPT-SoVITS's 3-10s clone window. That
// used to be asserted in this comment and nowhere else, and it stopped being
// true the moment the catalogue grew past the original eighteen: ten of the
// additions were between 2.4s and 84s, and each of them was a voice the user
// could pick from the dropdown and then find simply did not work.
// TestRefPresetsResolveToRealFiles now checks it, so the claim and the code
// cannot drift apart again.
var refPresets = []refPreset{
	// 台湾腔, one of each. First because they are what a user asking for a
	// natural companion voice usually means by it.
	{"优质台湾腔.wav", "female", "台湾腔 · 推荐", "hot"},
	{"台湾男青年音故事讲述.wav", "male", "台湾腔 · 推荐", "hot"},

	{"甜心少女.wav", "female", "甜美女声 · 萝莉 / 萌妹", ""},
	{"甜美萌妹.wav", "female", "甜美女声 · 萝莉 / 萌妹", ""},
	{"超嗲萌妹.wav", "female", "甜美女声 · 萝莉 / 萌妹", ""},
	{"软糯萝莉.wav", "female", "甜美女声 · 萝莉 / 萌妹", ""},
	{"娇媚萝莉.wav", "female", "甜美女声 · 萝莉 / 萌妹", ""},
	{"阳光甜心.wav", "female", "甜美女声 · 萝莉 / 萌妹", ""},
	{"开朗妹妹.wav", "female", "温柔女声 · 妹妹 / 御姐 / 闺蜜", ""},
	{"温暖御姐.wav", "female", "温柔女声 · 妹妹 / 御姐 / 闺蜜", ""},
	{"知性御姐.wav", "female", "温柔女声 · 妹妹 / 御姐 / 闺蜜", ""},
	{"俏皮姐姐.wav", "female", "温柔女声 · 妹妹 / 御姐 / 闺蜜", ""},
	{"稚嫩少女.wav", "female", "温柔女声 · 妹妹 / 御姐 / 闺蜜", ""},
	{"傲娇女王.wav", "female", "气场女声 · 女王 / 女神", ""},
	{"冰山女王.wav", "female", "气场女声 · 女王 / 女神", ""},
	{"娇媚女神.wav", "female", "气场女声 · 女王 / 女神", ""},
	{"青春女神.wav", "female", "气场女声 · 女王 / 女神", ""},
	{"阳光少年.wav", "male", "个性男声 · 少年 / 男神 / 大爷", ""},
	{"冷面霸总.wav", "male", "个性男声 · 少年 / 男神 / 大爷", ""},
	{"唠嗑大爷.wav", "male", "个性男声 · 少年 / 男神 / 大爷", ""},

	// --- 32 popular timbres from 热门音色 (hot pack) ---
	// 甜美 / 少女 (female, 7)
	{"云甜甜.wav", "female", "热门音色 · 甜美少女", "hot"},
	{"偏萝莉的少女音.wav", "female", "热门音色 · 甜美少女", "hot"},
	{"可爱小萝莉.wav", "female", "热门音色 · 甜美少女", "hot"},
	{"甜美少女音.wav", "female", "热门音色 · 甜美少女", "hot"},
	{"撒娇小师妹.wav", "female", "热门音色 · 甜美少女", "hot"},
	{"萌小音（11岁 女）.wav", "female", "热门音色 · 甜美少女", "hot"},
	{"蛋黄（8岁 女孩）.wav", "female", "热门音色 · 甜美少女", "hot"},

	// 温柔 / 御姐 (female, 7)
	{"叶子温柔师姐-中文.wav", "female", "热门音色 · 温柔御姐", "hot"},
	{"御姐.wav", "female", "热门音色 · 温柔御姐", "hot"},
	{"温柔御妈.wav", "female", "热门音色 · 温柔御姐", "hot"},
	{"温软姐姐 柔和质感.wav", "female", "热门音色 · 温柔御姐", "hot"},
	{"女-温柔、冷酷、亦正亦邪.wav", "female", "热门音色 · 温柔御姐", "hot"},
	{"中音磁性女声旁白.wav", "female", "热门音色 · 温柔御姐", "hot"},

	// 气场 / 古风 (female, 4)
	{"病娇反派女声.wav", "female", "热门音色 · 气场古风", "hot"},
	{"青灯古佛皇后.wav", "female", "热门音色 · 气场古风", "hot"},
	{"邻家婶子.wav", "female", "热门音色 · 气场古风", "hot"},
	{"奶奶中文.wav", "female", "热门音色 · 气场古风", "hot"},

	// 磁性 / 叔音 (male, 6)
	{"中年男声（45岁±）.wav", "male", "热门音色 · 磁性男声", "hot"},
	{"大叔.wav", "male", "热门音色 · 磁性男声", "hot"},
	{"大羊磁性舒适音.wav", "male", "热门音色 · 磁性男声", "hot"},
	{"磁性男声.wav", "male", "热门音色 · 磁性男声", "hot"},
	{"质感叔音.wav", "male", "热门音色 · 磁性男声", "hot"},
	{"纪录片宣传片高质男音.wav", "male", "热门音色 · 磁性男声", "hot"},

	// 少年 / 青年 (male, 5)
	{"少年侠客.wav", "male", "热门音色 · 少年青年", "hot"},
	{"温柔青年音.wav", "male", "热门音色 · 少年青年", "hot"},
	{"阳光有趣的学霸小哥.wav", "male", "热门音色 · 少年青年", "hot"},
	{"奶爸中文.wav", "male", "热门音色 · 少年青年", "hot"},
	{"若三夜（北京口音.wav", "male", "热门音色 · 少年青年", "hot"},

	// 权威 / 霸总 (male, 3)
	{"掌门师叔、帝王高管.wav", "male", "热门音色 · 权威霸总", "hot"},
	{"老年人旁白（男）.wav", "male", "热门音色 · 权威霸总", "hot"},
}

// Two clips from the packs are left out rather than trimmed: 纯欲狐狸精 at
// 2.4s and 霸总 at 2.9s. Trimming shortens, and both are already under the
// three seconds GPT-SoVITS needs to hear a timbre.

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
// its pack directory (DefaultRefPackDir for role-play, DefaultRefPackDirHot
// for hot-pack presets). The second result is false for non-preset IDs or
// unknown files.
func RefResolveVoice(voiceID, packDir string) (string, bool) {
	if !IsRefPresetVoiceID(voiceID) {
		return "", false
	}
	file := strings.TrimPrefix(voiceID, RefPresetVoiceIDPrefix)
	for _, p := range refPresets {
		if p.File == file {
			dir := packDir
			if dir == "" {
				switch p.PackDir {
				case "hot":
					dir = DefaultRefPackDirHot
				default:
					dir = DefaultRefPackDir
				}
			}
			return filepath.Join(dir, file), true
		}
	}
	return "", false
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
	// HostState reports the auto-host lifecycle (online / launching /
	// offline / not_configured) and HostScript the detected launcher.
	HostState   string `json:"host_state"`
	HostScript  string `json:"host_script"`
	HostLastErr string `json:"host_last_err,omitempty"`
}

// RefPackMeta probes the api_v2 service (any HTTP answer counts as
// online — the api_v2 /docs answers 200 when the service is alive) and
// checks the preset files on disk across both pack directories.
// MissingFiles stays empty when a pack folder itself is absent.
func RefPackMeta(endpoint string) RefMeta {
	if endpoint == "" {
		endpoint = DefaultRefEndpoint
	}
	meta := RefMeta{Endpoint: endpoint, PackDir: DefaultRefPackDir + " + " + DefaultRefPackDirHot, MissingFiles: []string{}}
	client := &http.Client{Timeout: 800 * time.Millisecond}
	// FastAPI api_v2 has no root handler; /docs is the reliable liveness probe.
	if resp, err := client.Get(strings.TrimRight(endpoint, "/") + "/docs"); err == nil {
		_ = resp.Body.Close()
		meta.ServerOnline = resp.StatusCode == http.StatusOK
	}
	// Auto-host state feeds the settings-page badge ("引擎在线 / 启动中 /
	// 未检测到 GPT-SoVITS") — the 50-preset list stays selectable in
	// every state.
	meta.HostState, meta.HostScript = DefaultRefHost.Status(endpoint)
	if meta.HostState == RefHostOffline || meta.HostState == RefHostNotConfigured {
		meta.HostLastErr = DefaultRefHost.LastErr()
	}
	meta.PackExists = true
	for _, p := range refPresets {
		dir := DefaultRefPackDir
		if p.PackDir == "hot" {
			dir = DefaultRefPackDirHot
		}
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			meta.PackExists = false
			continue
		}
		if _, err := os.Stat(filepath.Join(dir, p.File)); err != nil {
			meta.MissingFiles = append(meta.MissingFiles, p.File)
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
