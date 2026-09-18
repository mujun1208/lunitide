package m8core

import (
	"github.com/oklog/ulid/v2"
	"strings"
	"unicode/utf8"
)

// RejectTransientMemory reports greetings, thanks, one-shot weather/price
// queries, and other utterances that must never become long-term memory.
// MediaObservationRequiresReview is true for derived playback observations.
// Those facts may enter a review queue, never silent long-term authority.
func MediaObservationRequiresReview(raw string) bool {
	t := strings.ToLower(strings.TrimSpace(raw))
	if t == "" {
		return false
	}
	for _, marker := range []string{"正在播放", "now playing", "听过这首", "played this", "播放了"} {
		if strings.Contains(t, marker) {
			return true
		}
	}
	return false
}

func RejectTransientMemory(raw string) bool {
	t := strings.ToLower(strings.TrimSpace(raw))
	if t == "" {
		return true
	}
	for _, marker := range []string{
		"你好", "您好", "哈喽", "谢谢", "感谢", "hello", "thanks", "thank you",
		"天气如何", "天气怎么样", "今天天气", "气温", "weather",
		"股价", "行情", "验证码", "密码是", "password is", "otp",
		"播放",
	} {
		if strings.Contains(t, marker) {
			return true
		}
	}
	return false
}

func memoryClauses(raw string) []string {
	t := strings.TrimSpace(raw)
	if t == "" {
		return nil
	}
	runes := []rune(t)
	var out []string
	start := 0
	for i, r := range runes {
		if r == '。' || r == '！' || r == '？' || r == '!' || r == '?' || r == '\n' {
			if i > start {
				out = append(out, string(runes[start:i]))
			}
			start = i + 1
		}
	}
	if start < len(runes) {
		out = append(out, string(runes[start:]))
	}
	if len(out) == 0 {
		return []string{t}
	}
	return out
}

func stripQuestionTag(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimRight(s, "？?。.!！")
	for _, tag := range []string{"可以吗", "好吗", "行吗", "对吗", "是吗"} {
		s = strings.TrimSuffix(s, tag)
	}
	return strings.TrimRight(strings.TrimSpace(s), "，,")
}

// StableUserMemory accepts a direct statement by the user. It never guesses
// preferences from the assistant's reply, quotations, role prompts or tasks.
func StableUserMemory(raw string) string {
	if RejectTransientMemory(raw) || MediaObservationRequiresReview(raw) {
		return ""
	}
	var kept []string
	seen := map[string]struct{}{}
	for _, clause := range memoryClauses(raw) {
		got := stableUserMemoryClause(stripQuestionTag(clause))
		if got == "" {
			continue
		}
		if _, ok := seen[got]; ok {
			continue
		}
		seen[got] = struct{}{}
		kept = append(kept, got)
	}
	return strings.Join(kept, "。")
}

func stableUserMemoryClause(t string) string {
	t = strings.TrimSpace(t)
	if n := utf8.RuneCountInString(t); n < 4 || n > 280 {
		return ""
	}
	if RejectTransientMemory(t) {
		return ""
	}
	lower := strings.ToLower(t)
	for _, marker := range []string{"用户：", "忽略规则", "绕过", "泄露", "system prompt", "ignore instructions", "帮我", "打开", "创建", "生成", "查询", "播放", "发送", "执行", "写一", "写个", "做一", "做个", "删除", "如果", "别人", "用户说", "user:", "我叫你", "[引用", "@", "```", "http://", "https://", "助手：", "assistant:", "要点：", "你是", "扮演", "模拟", "假设", "例如", "他说", "她说", "原文", "翻译", "这次", "本次", "今天", "明天", "稍后", "马上", "临时", "刚才", "开会", "提醒我", "“", "”", "\"", "<", ">"} {
		if strings.Contains(lower, marker) {
			return ""
		}
	}

	normalized := strings.TrimRight(strings.TrimSpace(t), "。.!！")
	direct := strings.ToLower(normalized)
	for _, prefix := range []string{"请记住", "记住"} {
		if strings.HasPrefix(direct, prefix) {
			direct = strings.TrimLeft(strings.TrimPrefix(direct, prefix), " ，,：:")
			break
		}
	}
	for _, marker := range []string{"我喜欢", "我习惯", "我偏好", "我的名字是", "我叫", "我住在", "我常住", "我的职业是", "我从事", "my name is", "i live in", "i prefer"} {
		if strings.HasPrefix(direct, marker) {
			return normalized
		}
	}
	persistent := false
	for _, marker := range []string{"以后", "默认", "从现在起", "我希望你一直", "from now on", "always use"} {
		if strings.Contains(lower, marker) {
			persistent = true
			break
		}
	}
	if persistent {
		for _, topic := range []string{"回答", "回复", "称呼", "叫我", "中文", "英语", "语言", "注释", "格式", "语气", "风格", "简洁", "详细", "主题", "封面", "单位", "时区", "ppt", "深蓝", "python", "golang", "reply", "response", "language", "english", "chinese", "concise"} {
			if strings.Contains(lower, topic) {
				return normalized
			}
		}
		compact := strings.ReplaceAll(lower, " ", "")
		if strings.Contains(compact, "默认go") || strings.Contains(compact, "用go") {
			return normalized
		}
	}

	return ""
}

// PersonalMemoryContent filters legacy global summaries without changing or
// deleting their stored evidence. Expert work remains in its expert/session.
func PersonalMemoryContent(doc PayloadDoc) string {
	for _, leaf := range doc.Leaves {
		if strings.HasPrefix(leaf.EvidenceRef, "expert://") {
			return ""
		}
	}
	t := strings.TrimSpace(doc.Content)
	if strings.HasPrefix(t, "用户：") || strings.Contains(t, "\n要点：") {
		if !strings.HasPrefix(t, "用户：") {
			return ""
		}
		user := strings.TrimPrefix(t, "用户：")
		if i := strings.Index(user, "\n要点："); i >= 0 {
			user = user[:i]
		}
		return StableUserMemory(user)
	}
	for _, marker := range []string{"[引用专家", "@PPT专家", "助手：", "assistant:", "你是", "我是你的", "作为你的", "```"} {
		if strings.Contains(t, marker) {
			return ""
		}
	}
	return t
}

func MemorySourceSession(doc PayloadDoc) string {
	for _, leaf := range doc.Leaves {
		for _, prefix := range []string{"chat-user://", "chat://"} {
			if strings.HasPrefix(leaf.EvidenceRef, prefix) {
				parts := strings.Split(strings.TrimPrefix(leaf.EvidenceRef, prefix), "/")
				if len(parts) >= 2 {
					if id, err := ulid.ParseStrict(parts[0]); err == nil && id.String() == parts[0] {
						return parts[0]
					}
				}
			}
		}
	}
	return ""
}
