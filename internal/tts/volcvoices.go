package tts

// VolcDefaultVoiceID is 小何 2.0, the documented general-purpose Chinese
// female speaker for seed-tts-2.0.
func VolcDefaultVoiceID() string { return volcDefaultSpeaker }

const volcDefaultSpeaker = "zh_female_xiaohe_uranus_bigtts"

type volcPreset struct {
	ID     string
	Name   string
	Gender string
	Lang   string
	Group  string
}

// volcPresets is the Agent Plan seed-tts-2.0 official speaker list we
// ship. It is not the local GPT-SoVITS 50-life pack, and it is not the
// Doubao App 角色库 (温柔桃子). IDs are the documented *_uranus_bigtts
// speakers; we do not invent extras to pad the count to 50.
var volcPresets = []volcPreset{
	{"zh_female_xiaohe_uranus_bigtts", "小何", "female", "zh-CN", "通用女声"},
	{"zh_female_vv_uranus_bigtts", "Vivi", "female", "zh-CN", "通用女声"},
	{"zh_female_gaolengyujie_uranus_bigtts", "高冷御姐", "female", "zh-CN", "通用女声"},
	{"zh_female_qingxinnvsheng_uranus_bigtts", "清新女声", "female", "zh-CN", "通用女声"},
	{"zh_female_tianmeitaozi_uranus_bigtts", "甜美桃子", "female", "zh-CN", "通用女声"},
	{"zh_female_tianmeixiaoyuan_uranus_bigtts", "甜美小源", "female", "zh-CN", "通用女声"},
	{"zh_female_shuangkuaisisi_uranus_bigtts", "爽快思思", "female", "zh-CN", "通用女声"},
	{"zh_female_linjianvhai_uranus_bigtts", "邻家女孩", "female", "zh-CN", "通用女声"},
	{"zh_female_meilinvyou_uranus_bigtts", "魅力女友", "female", "zh-CN", "通用女声"},
	{"zh_female_liuchangnv_uranus_bigtts", "流畅女声", "female", "zh-CN", "通用女声"},

	{"zh_male_m191_uranus_bigtts", "云舟", "male", "zh-CN", "通用男声"},
	{"zh_male_taocheng_uranus_bigtts", "小天", "male", "zh-CN", "通用男声"},
	{"zh_male_liufei_uranus_bigtts", "刘飞", "male", "zh-CN", "通用男声"},
	{"zh_male_shaonianzixin_uranus_bigtts", "少年梓辛", "male", "zh-CN", "通用男声"},
	{"zh_male_ruyayichen_uranus_bigtts", "儒雅逸辰", "male", "zh-CN", "通用男声"},

	{"zh_female_cancan_uranus_bigtts", "知性灿灿", "female", "zh-CN", "角色配音"},
	{"zh_female_sajiaoxuemei_uranus_bigtts", "撒娇学妹", "female", "zh-CN", "角色配音"},
	{"zh_male_sunwukong_uranus_bigtts", "猴哥", "male", "zh-CN", "角色配音"},
	{"zh_female_peiqi_uranus_bigtts", "佩奇猪", "female", "zh-CN", "角色配音"},
	{"zh_male_dayi_uranus_bigtts", "大壹", "male", "zh-CN", "角色配音"},
	{"zh_female_mizai_uranus_bigtts", "咪仔", "female", "zh-CN", "角色配音"},
	{"zh_female_jitangnv_uranus_bigtts", "鸡汤女", "female", "zh-CN", "角色配音"},
	{"zh_male_sophie_uranus_bigtts", "魅力苏菲", "male", "zh-CN", "角色配音"},

	{"zh_female_yingyujiaoxue_uranus_bigtts", "Tina老师", "female", "zh-CN", "教育客服"},
	{"zh_female_kefunvsheng_uranus_bigtts", "暖阳女声", "female", "zh-CN", "教育客服"},
	{"zh_female_xiaoxue_uranus_bigtts", "儿童绘本", "female", "zh-CN", "教育客服"},

	{"en_male_tim_uranus_bigtts", "Tim", "male", "en-US", "多语种"},
	{"en_female_dacey_uranus_bigtts", "Dacey", "female", "en-US", "多语种"},
	{"en_female_stokie_uranus_bigtts", "Stokie", "female", "en-US", "多语种"},
}

// VolcVoices is the static official catalogue. Settings can list it
// without a network hop or a stored key.
func VolcVoices() []Voice {
	out := make([]Voice, 0, len(volcPresets))
	for _, p := range volcPresets {
		out = append(out, Voice{
			VoiceID:     p.ID,
			DisplayName: p.Name,
			Gender:      p.Gender,
			Lang:        p.Lang,
			Group:       p.Group,
		})
	}
	return out
}

// IsVolcSpeakerID reports a documented seed-tts 2.0 speaker token.
func IsVolcSpeakerID(id string) bool {
	for _, p := range volcPresets {
		if p.ID == id {
			return true
		}
	}
	return false
}

func volcResolveSpeaker(voiceID string) (speaker string, fallback bool) {
	if voiceID == "" || IsVolcTTSResourceID(voiceID) {
		return volcDefaultSpeaker, false
	}
	if IsVolcSpeakerID(voiceID) {
		return voiceID, false
	}
	return volcDefaultSpeaker, true
}
