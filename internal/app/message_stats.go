package app

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"

	"github.com/lunitide/lunitide/internal/atomicfile"
	"github.com/lunitide/lunitide/internal/domain/message"
	"github.com/lunitide/lunitide/internal/llmadapter"
)

const messageStatsMaxBytes = 2 * 1024

// messageTurnStats is the tiny per-turn footer (elapsed time plus provider
// token usage) rendered under an assistant reply once the turn ends. It is
// stored beside .message-process, outside every SessionFolder workspace
// digest, so history pages can render it without a database migration.
type messageTurnStats struct {
	MessageID             string `json:"messageId"`
	DurationMs            int64  `json:"durationMs"`
	InputTokens           int64  `json:"inputTokens"`
	OutputTokens          int64  `json:"outputTokens"`
	TotalTokens           int64  `json:"totalTokens"`
	CachedInputTokens     int64  `json:"cachedInputTokens,omitempty"`
	CacheWriteInputTokens int64  `json:"cacheWriteInputTokens,omitempty"`
	CacheUsageReported    bool   `json:"cacheUsageReported,omitempty"`
	Model                 string `json:"model,omitempty"`
}

// messageTurnStatsDTO mirrors MessageTurnStatsDTO from the bridge schema:
// the messageId stays in the file, never in the wire payload.
type messageTurnStatsDTO struct {
	DurationMs            int64  `json:"durationMs"`
	InputTokens           int64  `json:"inputTokens"`
	OutputTokens          int64  `json:"outputTokens"`
	TotalTokens           int64  `json:"totalTokens"`
	CachedInputTokens     int64  `json:"cachedInputTokens,omitempty"`
	CacheWriteInputTokens int64  `json:"cacheWriteInputTokens,omitempty"`
	CacheUsageReported    bool   `json:"cacheUsageReported,omitempty"`
	Model                 string `json:"model,omitempty"`
}

func (s messageTurnStats) valid() bool {
	if !message.CanonicalULID(s.MessageID) || s.DurationMs < 0 {
		return false
	}
	return s.InputTokens >= 0 && s.OutputTokens >= 0 && s.TotalTokens >= 0
}

func (s messageTurnStats) dto() messageTurnStatsDTO {
	return messageTurnStatsDTO{
		DurationMs:            s.DurationMs,
		InputTokens:           s.InputTokens,
		OutputTokens:          s.OutputTokens,
		TotalTokens:           s.TotalTokens,
		CachedInputTokens:     s.CachedInputTokens,
		CacheWriteInputTokens: s.CacheWriteInputTokens,
		CacheUsageReported:    s.CacheUsageReported,
		Model:                 s.Model,
	}
}

// messageTurnStatsFromUsage projects the same aggregate the usage event
// publishes, so the live footer and the history footer never disagree.
func messageTurnStatsFromUsage(messageID string, durationMs int64, usage llmadapter.Usage, model string) messageTurnStats {
	return messageTurnStats{
		MessageID:             messageID,
		DurationMs:            durationMs,
		InputTokens:           int64(usage.InputTokens),
		OutputTokens:          int64(usage.OutputTokens),
		TotalTokens:           int64(usage.TotalTokens),
		CachedInputTokens:     int64(usage.CachedInputTokens),
		CacheWriteInputTokens: int64(usage.CacheWriteInputTokens),
		CacheUsageReported:    usage.CacheUsageReported,
		Model:                 model,
	}
}

func (e *Engine) messageStatsPath(sessionID, messageID string) string {
	if e == nil || e.tools == nil || !message.CanonicalULID(sessionID) || !message.CanonicalULID(messageID) {
		return ""
	}
	return filepath.Join(e.tools.WorkspaceRoot(), ".message-stats", sessionID, messageID+".json")
}

func (e *Engine) saveMessageTurnStats(sessionID, messageID string, stats messageTurnStats) {
	stats.MessageID = messageID
	if !stats.valid() {
		return
	}
	path := e.messageStatsPath(sessionID, messageID)
	if path == "" {
		return
	}
	raw, err := json.Marshal(stats)
	if err != nil {
		return
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err == nil {
		err = atomicfile.Write(path, raw, 0600)
	}
	if err != nil {
		log.Printf("chat turn stats save failed: %v", err)
	}
}

// enrichMessageTurnStatsPage attaches per-message turnStats rows to an
// assistant history page. Corrupt or oversized files are skipped, never
// fatal: the footer is a display extra, not a data dependency.
func (e *Engine) enrichMessageTurnStatsPage(sessionID string, payload map[string]any) {
	items, _ := payload["items"].([]any)
	for _, item := range items {
		row, ok := item.(map[string]any)
		if !ok || row["role"] != "assistant" {
			continue
		}
		id, _ := row["id"].(string)
		path := e.messageStatsPath(sessionID, id)
		if path == "" {
			continue
		}
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() || info.Size() > messageStatsMaxBytes {
			continue
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var stats messageTurnStats
		if json.Unmarshal(raw, &stats) != nil || !stats.valid() || stats.MessageID != id {
			continue
		}
		row["turnStats"] = stats.dto()
	}
}
