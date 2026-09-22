package app

import (
	"context"
	"errors"
	"log"
	"strings"

	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/domain/token"
	"github.com/lunitide/lunitide/internal/llmadapter"
)

// providerModelContextWindow reads the configured window for this model.
// Missing configuration keeps the historical 128000 fallback. The bool is
// true only when the provider row itself supplied a positive window.
func providerModelContextWindow(p provider.Provider, modelID string) (int64, bool) {
	for _, m := range p.Models {
		if m.ModelID == modelID && m.ContextWindow > 0 {
			return m.ContextWindow, true
		}
	}
	return 128000, false
}

func (e *Engine) compactSession(ctx context.Context, sessionID string, p provider.Provider, modelID, tokenizerRevision string) {
	if e == nil || e.compactionTrigger == nil || e.compactionExecutor == nil {
		return
	}
	window, _ := providerModelContextWindow(p, modelID)
	if strings.TrimSpace(tokenizerRevision) == "" {
		tokenizerRevision = token.CanonicalTokenizerRevision
	}
	result := e.TriggerPreTurnCompaction(ctx, sessionID, p.ID, modelID, tokenizerRevision, window)
	if result.Err != nil {
		log.Printf("session compaction: session=%s model=%s window=%d: %s", sessionID, modelID, window, result.Reason)
	}
}

func (e *Engine) latestCheckpointSummary(ctx context.Context, sessionID string) string {
	if e == nil || e.summaryReader == nil || sessionID == "" {
		return ""
	}
	if cr, ok := e.summaryReader.(compactionCoverageReader); ok {
		summary, _, err := cr.GetLatestCompactionCheckpoint(ctx, sessionID)
		if err == nil && strings.TrimSpace(summary) != "" {
			return summary
		}
	}
	summary, err := e.summaryReader.GetLatestCompactionSummary(ctx, sessionID)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(summary)
}

func isWindowOverflowError(err error) bool {
	var upstream *llmadapter.Error
	if !errors.As(err, &upstream) {
		return false
	}
	switch upstream.Code {
	case "CONTEXT_WINDOW_EXCEEDED", "REQUEST_TOO_LARGE", "RESPONSE_TRUNCATED":
		return true
	}
	return upstream.HTTPStatus == 413
}

func shrinkMessagesForWindowRetry(req *llmadapter.Request) {
	applyWindowRetryMessages(req, "")
}

func applyWindowRetryMessages(req *llmadapter.Request, checkpointSummary string) {
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
	note := "较早的对话已写入检查点摘要。不要声称记得未出现的细节。继续完成当前用户请求。"
	if summary := strings.TrimSpace(checkpointSummary); summary != "" {
		note = "较早的对话已写入检查点摘要：\n" + summary + "\n不要声称记得未出现的细节。继续完成当前用户请求。"
	}
	req.Messages = append(append([]llmadapter.Message{}, systems...), llmadapter.Message{Role: llmadapter.RoleSystem, Content: note})
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
