package app

import (
	"context"
	"errors"
	"log"
	"strings"
	"unicode/utf8"

	"github.com/lunitide/lunitide/internal/domain/agentrun"
	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/domain/token"
	"github.com/lunitide/lunitide/internal/llmadapter"
)

const maxWindowRetries = 8

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

// isContextOverflowError is a request that does not fit. Those retry by
// compacting history. A truncated reply is not a window overflow: the
// partial stays, and the turn continues so the file can still be written.
func isContextOverflowError(err error) bool {
	if errors.Is(err, agentrun.ErrContextWindow) {
		return true
	}
	var upstream *llmadapter.Error
	if !errors.As(err, &upstream) {
		return false
	}
	switch upstream.Code {
	case "CONTEXT_WINDOW_EXCEEDED", "REQUEST_TOO_LARGE":
		return true
	}
	return upstream.HTTPStatus == 413
}

func isWindowOverflowError(err error) bool {
	return isContextOverflowError(err)
}

func isReplyTruncatedError(err error) bool {
	var upstream *llmadapter.Error
	return errors.As(err, &upstream) && upstream.Code == "RESPONSE_TRUNCATED"
}

func shrinkMessagesForWindowRetry(req *llmadapter.Request) {
	applyWindowRetryMessages(req, "")
}

func windowRetryKeep(attempt int) int {
	switch attempt {
	case 1:
		return 8
	case 2:
		return 4
	default:
		return 2
	}
}

func applyWindowRetryMessages(req *llmadapter.Request, checkpointSummary string) {
	applyWindowRetryMessagesKeep(req, checkpointSummary, 8)
}

func applyWindowRetryMessagesKeep(req *llmadapter.Request, checkpointSummary string, keep int) {
	if req == nil || len(req.Messages) == 0 {
		return
	}
	if keep < 1 {
		keep = 1
	}
	var systems, rest []llmadapter.Message
	for _, m := range req.Messages {
		if m.Role == llmadapter.RoleSystem {
			systems = append(systems, m)
			continue
		}
		rest = append(rest, m)
	}
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

func clipWindowRetryPayloads(req *llmadapter.Request, attempt int) {
	if req == nil || attempt < 2 {
		return
	}
	limit := 4000
	if attempt >= 3 {
		limit = 1500
	}
	for i := range req.Messages {
		if req.Messages[i].Role != llmadapter.RoleTool && req.Messages[i].Role != llmadapter.RoleAssistant {
			continue
		}
		if utf8.RuneCountInString(req.Messages[i].Content) <= limit {
			continue
		}
		req.Messages[i].Content = string([]rune(req.Messages[i].Content)[:limit]) + "\n…（已压缩）"
	}
}

func dropWindowRetryImages(req *llmadapter.Request) {
	if req == nil {
		return
	}
	req.Images = nil
}

func windowOverflowUserMessage() string {
	return "较早的对话已经收起，这一步会接着做。"
}

func windowRetryThinkingNotice() string {
	return "较早的对话已经收起，正在接着做当前这一步。"
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
