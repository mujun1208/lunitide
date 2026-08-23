package tts

import (
	"sort"
	"strings"
)

const edgeStyleVoiceSep = "::"

type edgeMandarinPreset struct {
	BaseVoice   string
	Style       string
	DisplayName string
	Gender      string
	Group       string
	Rank        int
}

// edgeMandarinPresets lists 50 distinct Mandarin (zh-CN) cloud voices:
// six Microsoft Neural bases expanded with unique speaking styles so every
// entry sounds different yet maps to a live Edge voice ID.
var edgeMandarinPresets = []edgeMandarinPreset{
	// --- 女声 · 晓晓 (13) ---
	{"zh-CN-XiaoxiaoNeural", "chat", "晓晓 · 温柔日常（推荐）", "female", "云端普通话 · 女声 · 晓晓", 0},
	{"zh-CN-XiaoxiaoNeural", "cheerful", "晓晓 · 活泼开朗", "female", "云端普通话 · 女声 · 晓晓", 1},
	{"zh-CN-XiaoxiaoNeural", "affectionate", "晓晓 · 亲昵陪伴", "female", "云端普通话 · 女声 · 晓晓", 2},
	{"zh-CN-XiaoxiaoNeural", "gentle", "晓晓 · 轻柔耳语", "female", "云端普通话 · 女声 · 晓晓", 3},
	{"zh-CN-XiaoxiaoNeural", "lyrical", "晓晓 · 抒情朗读", "female", "云端普通话 · 女声 · 晓晓", 4},
	{"zh-CN-XiaoxiaoNeural", "calm", "晓晓 · 平静舒缓", "female", "云端普通话 · 女声 · 晓晓", 5},
	{"zh-CN-XiaoxiaoNeural", "empathetic", "晓晓 · 共情倾听", "female", "云端普通话 · 女声 · 晓晓", 6},
	{"zh-CN-XiaoxiaoNeural", "sad", "晓晓 · 低语伤感", "female", "云端普通话 · 女声 · 晓晓", 7},
	{"zh-CN-XiaoxiaoNeural", "serious", "晓晓 · 认真严肃", "female", "云端普通话 · 女声 · 晓晓", 8},
	{"zh-CN-XiaoxiaoNeural", "newscast", "晓晓 · 新闻播报", "female", "云端普通话 · 女声 · 晓晓", 9},
	{"zh-CN-XiaoxiaoNeural", "customerservice", "晓晓 · 客服应答", "female", "云端普通话 · 女声 · 晓晓", 10},
	{"zh-CN-XiaoxiaoNeural", "assistant", "晓晓 · 智能助手", "female", "云端普通话 · 女声 · 晓晓", 11},
	{"zh-CN-XiaoxiaoNeural", "poetry-reading", "晓晓 · 诗歌朗诵", "female", "云端普通话 · 女声 · 晓晓", 12},
	// --- 女声 · 晓伊 (12) ---
	{"zh-CN-XiaoyiNeural", "chat", "晓伊 · 日常聊天", "female", "云端普通话 · 女声 · 晓伊", 13},
	{"zh-CN-XiaoyiNeural", "cheerful", "晓伊 · 元气少女", "female", "云端普通话 · 女声 · 晓伊", 14},
	{"zh-CN-XiaoyiNeural", "affectionate", "晓伊 · 撒娇亲昵", "female", "云端普通话 · 女声 · 晓伊", 15},
	{"zh-CN-XiaoyiNeural", "gentle", "晓伊 · 温柔安抚", "female", "云端普通话 · 女声 · 晓伊", 16},
	{"zh-CN-XiaoyiNeural", "lyrical", "晓伊 · 抒情女声", "female", "云端普通话 · 女声 · 晓伊", 17},
	{"zh-CN-XiaoyiNeural", "calm", "晓伊 · 静夜细语", "female", "云端普通话 · 女声 · 晓伊", 18},
	{"zh-CN-XiaoyiNeural", "empathetic", "晓伊 · 暖心共情", "female", "云端普通话 · 女声 · 晓伊", 19},
	{"zh-CN-XiaoyiNeural", "sad", "晓伊 · 细腻伤感", "female", "云端普通话 · 女声 · 晓伊", 20},
	{"zh-CN-XiaoyiNeural", "serious", "晓伊 · 正经播报", "female", "云端普通话 · 女声 · 晓伊", 21},
	{"zh-CN-XiaoyiNeural", "newscast", "晓伊 · 时事速报", "female", "云端普通话 · 女声 · 晓伊", 22},
	{"zh-CN-XiaoyiNeural", "customerservice", "晓伊 · 亲切客服", "female", "云端普通话 · 女声 · 晓伊", 23},
	{"zh-CN-XiaoyiNeural", "assistant", "晓伊 · 语音助理", "female", "云端普通话 · 女声 · 晓伊", 24},
	// --- 男声 · 云希 (7) ---
	{"zh-CN-YunxiNeural", "chat", "云希 · 阳光少年（推荐）", "male", "云端普通话 · 男声 · 云希", 25},
	{"zh-CN-YunxiNeural", "cheerful", "云希 · 开朗男孩", "male", "云端普通话 · 男声 · 云希", 26},
	{"zh-CN-YunxiNeural", "affectionate", "云希 · 暖男陪伴", "male", "云端普通话 · 男声 · 云希", 27},
	{"zh-CN-YunxiNeural", "gentle", "云希 · 温和低沉", "male", "云端普通话 · 男声 · 云希", 28},
	{"zh-CN-YunxiNeural", "newscast", "云希 · 新闻男声", "male", "云端普通话 · 男声 · 云希", 29},
	{"zh-CN-YunxiNeural", "assistant", "云希 · 智能助手", "male", "云端普通话 · 男声 · 云希", 30},
	{"zh-CN-YunxiNeural", "narration-relaxed", "云希 · 轻松旁白", "male", "云端普通话 · 男声 · 云希", 31},
	// --- 男声 · 云健 (6) ---
	{"zh-CN-YunjianNeural", "sports-commentary", "云健 · 体育解说", "male", "云端普通话 · 男声 · 云健", 32},
	{"zh-CN-YunjianNeural", "chat", "云健 · 解说日常", "male", "云端普通话 · 男声 · 云健", 33},
	{"zh-CN-YunjianNeural", "cheerful", "云健 · 激情解说", "male", "云端普通话 · 男声 · 云健", 34},
	{"zh-CN-YunjianNeural", "serious", "云健 · 赛事评述", "male", "云端普通话 · 男声 · 云健", 35},
	{"zh-CN-YunjianNeural", "newscast", "云健 · 快讯播报", "male", "云端普通话 · 男声 · 云健", 36},
	{"zh-CN-YunjianNeural", "assistant", "云健 · 语音助手", "male", "云端普通话 · 男声 · 云健", 37},
	// --- 男声 · 云夏 (6) ---
	{"zh-CN-YunxiaNeural", "chat", "云夏 · 少年日常", "male", "云端普通话 · 男声 · 云夏", 38},
	{"zh-CN-YunxiaNeural", "cheerful", "云夏 · 活力少年", "male", "云端普通话 · 男声 · 云夏", 39},
	{"zh-CN-YunxiaNeural", "affectionate", "云夏 · 邻家男孩", "male", "云端普通话 · 男声 · 云夏", 40},
	{"zh-CN-YunxiaNeural", "gentle", "云夏 · 温柔少年", "male", "云端普通话 · 男声 · 云夏", 41},
	{"zh-CN-YunxiaNeural", "calm", "云夏 · 安静陪伴", "male", "云端普通话 · 男声 · 云夏", 42},
	{"zh-CN-YunxiaNeural", "assistant", "云夏 · 少年助理", "male", "云端普通话 · 男声 · 云夏", 43},
	// --- 男声 · 云扬 (6) ---
	{"zh-CN-YunyangNeural", "newscast", "云扬 · 新闻主播（推荐）", "male", "云端普通话 · 男声 · 云扬", 44},
	{"zh-CN-YunyangNeural", "chat", "云扬 · 沉稳播报", "male", "云端普通话 · 男声 · 云扬", 45},
	{"zh-CN-YunyangNeural", "serious", "云扬 · 正式通告", "male", "云端普通话 · 男声 · 云扬", 46},
	{"zh-CN-YunyangNeural", "customerservice", "云扬 · 官方客服", "male", "云端普通话 · 男声 · 云扬", 47},
	{"zh-CN-YunyangNeural", "assistant", "云扬 · 播报助手", "male", "云端普通话 · 男声 · 云扬", 48},
	{"zh-CN-YunyangNeural", "narration-professional", "云扬 · 专业旁白", "male", "云端普通话 · 男声 · 云扬", 49},
}

func edgeVoiceStyleID(baseVoice, style string) string {
	if style == "" {
		return baseVoice
	}
	return baseVoice + edgeStyleVoiceSep + style
}

func edgeParseStyleVoiceID(voiceID string) (baseVoice, style string) {
	voiceID = strings.TrimSpace(voiceID)
	if voiceID == "" {
		return "", ""
	}
	if i := strings.Index(voiceID, edgeStyleVoiceSep); i > 0 {
		return voiceID[:i], voiceID[i+len(edgeStyleVoiceSep):]
	}
	return voiceID, ""
}

func edgePresetMeta(voiceID string) (edgeMandarinPreset, bool) {
	for _, row := range edgeMandarinPresets {
		if edgeVoiceStyleID(row.BaseVoice, row.Style) == voiceID {
			return row, true
		}
	}
	return edgeMandarinPreset{}, false
}

func expandEdgeMandarinVoices(apiVoices []Voice) []Voice {
	available := map[string]bool{}
	for _, v := range apiVoices {
		if v.Lang == "zh-CN" && strings.Contains(v.VoiceID, "Neural") {
			available[v.VoiceID] = true
		}
	}
	out := make([]Voice, 0, len(edgeMandarinPresets))
	for _, preset := range edgeMandarinPresets {
		if len(available) > 0 && !available[preset.BaseVoice] {
			continue
		}
		out = append(out, Voice{
			VoiceID:     edgeVoiceStyleID(preset.BaseVoice, preset.Style),
			DisplayName: preset.DisplayName,
			Gender:      preset.Gender,
			Lang:        "zh-CN",
			Group:       preset.Group,
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		return edgeVoiceRank(out[i]) < edgeVoiceRank(out[j])
	})
	return out
}

func edgeApplyStyleVoice(in *SynthesizeInput) {
	voice := strings.TrimSpace(in.VoiceID)
	if voice == "" || strings.HasPrefix(voice, "HKEY_") || strings.HasPrefix(voice, "refpack:") {
		in.VoiceID = edgeDefaultVoice
		if strings.TrimSpace(in.Style) == "" {
			in.Style = "chat"
		}
		return
	}
	base, style := edgeParseStyleVoiceID(voice)
	if base != "" {
		in.VoiceID = base
	}
	if strings.TrimSpace(in.Style) == "" {
		if style != "" {
			in.Style = style
		} else if preset, ok := edgePresetMeta(voice); ok {
			in.Style = preset.Style
		} else if edgeVoiceSupportsChatStyle(in.VoiceID) {
			in.Style = "chat"
		}
	}
}
