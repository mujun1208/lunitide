package app

import (
	"errors"
	"strings"

	"github.com/lunitide/lunitide/internal/llmadapter"
)

func isWindowOverflowError(err error) bool {
	var upstream *llmadapter.Error
	if !errors.As(err, &upstream) {
		return false
	}
	if upstream.Code == "CONTEXT_WINDOW_EXCEEDED" || upstream.Code == "REQUEST_TOO_LARGE" {
		return true
	}
	return upstream.HTTPStatus == 413
}

func shrinkMessagesForWindowRetry(req *llmadapter.Request) {
	if req == nil || len(req.Messages) == 0 {
		return
	}
	var systems, rest []llmadapter.Message
	for _, m := range req.Messages {
		if m.Role == llmadapter.RoleSystem {
			systems = append(systems, m)
			continue
		}
		rest = append(rest, m)
	}
	const keep = 8
	if len(rest) > keep {
		rest = rest[len(rest)-keep:]
	}
	note := llmadapter.Message{Role: llmadapter.RoleSystem, Content: "较早的对话已写入检查点摘要。不要声称记得未出现的细节。继续完成当前用户请求。"}
	req.Messages = append(append([]llmadapter.Message{}, systems...), note)
	req.Messages = append(req.Messages, rest...)
}

func windowOverflowUserMessage() string {
	return "上下文已满，请开新话题或先压缩"
}

func windowRetryThinkingNotice() string {
	return "上下文已满，已按检查点收面后重试当前步。"
}

func companionColdMaxMessages(hasCheckpoint bool) int {
	if hasCheckpoint {
		return companionMaxMessages
	}
	return companionMaxMessages / 2
}

func discardStepText(builder *strings.Builder, stepStart int) {
	if builder == nil || stepStart < 0 || stepStart > builder.Len() {
		return
	}
	kept := builder.String()[:stepStart]
	builder.Reset()
	builder.WriteString(kept)
}
