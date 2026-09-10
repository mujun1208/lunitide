package scheduler

import (
	"strings"
	"unicode"
)

// UserMessage maps persisted run errors to Chinese fail-closed copy.
// Known Chinese headless messages are kept; unknown English is not shown.
func UserMessage(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	switch {
	case strings.Contains(text, "context deadline exceeded"):
		return "执行超时，任务已停止。可查看已生成内容，核对后再决定是否重新运行。"
	case strings.Contains(text, "context canceled"):
		return "执行已中断，任务已停止。可查看已生成内容。"
	}
	for _, r := range text {
		if unicode.Is(unicode.Han, r) {
			return text
		}
	}
	return "任务未完成，请查看执行对话了解详情。"
}
