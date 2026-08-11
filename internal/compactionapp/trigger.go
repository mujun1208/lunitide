package compactionapp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/lunitide/lunitide/internal/domain/compaction"
	"github.com/lunitide/lunitide/internal/domain/token"
)

// WatermarkConfig defines the high/low watermark thresholds for automatic compaction.
// The default policy targets compaction at 80% and reduces reusable conversational
// context below 60% (see ADR-005).
type WatermarkConfig struct {
	// HighWatermark is the fraction of the context window at which automatic
	// compaction is triggered. Default: 0.80.
	HighWatermark float64
	// LowWatermark is the fraction of the context window below which compaction
	// is considered sufficient. Default: 0.60.
	LowWatermark float64
	// MinMessagesBeforeCompaction is the minimum number of messages in a session
	// before compaction is considered. Default: 10.
	MinMessagesBeforeCompaction int
	// CheckCooldown is the minimum duration between automatic compaction triggers
	// for the same session. Default: 30 seconds.
	CheckCooldown time.Duration
}

// DefaultWatermarkConfig returns the recommended watermark configuration.
func DefaultWatermarkConfig() WatermarkConfig {
	return WatermarkConfig{
		HighWatermark:               0.80,
		LowWatermark:                0.60,
		MinMessagesBeforeCompaction: 10,
		CheckCooldown:               30 * time.Second,
	}
}

// CheckpointStore defines the storage operations needed by the compaction trigger.
type CheckpointStore interface {
	// CreateCheckpoint inserts a new compaction checkpoint.
	CreateCheckpoint(ctx context.Context, cp compaction.Checkpoint) (compaction.Checkpoint, error)
	// GetLatestCheckpoint returns the latest checkpoint (by version) for a session.
	GetLatestCheckpoint(ctx context.Context, sessionID string) (*compaction.Checkpoint, error)
	// CountCheckpointsBySession returns the number of checkpoints for a session.
	CountCheckpointsBySession(ctx context.Context, sessionID string) (int64, error)
	// ListCheckpointsByStatus returns checkpoints matching the given status,
	// ordered by created_at ascending. Used by restart recovery.
	ListCheckpointsByStatus(ctx context.Context, status compaction.Status, limit int) ([]compaction.Checkpoint, error)
}

// MessageReader provides access to messages for source range computation.
type MessageReader interface {
	// ListMessages returns messages in a session ordered by sequence.
	ListMessages(ctx context.Context, sessionID string, direction string, limit int) ([]MessageInfo, error)
}

// MessageInfo is a minimal message representation for the trigger.
type MessageInfo struct {
	ID       string
	Sequence int64
}

// TriggerResult describes the outcome of a compaction trigger check.
type TriggerResult struct {
	// Triggered indicates whether compaction was triggered.
	Triggered bool
	// CheckpointID is the ID of the created checkpoint, if triggered.
	CheckpointID string
	// CurrentUsage is the current token usage in the session.
	CurrentUsage int64
	// Budget is the effective context budget.
	Budget int64
	// UsageFraction is the current usage as a fraction of the budget.
	UsageFraction float64
	// Reason describes why compaction was or was not triggered.
	Reason string
}

// Trigger checks token usage and creates compaction checkpoints automatically.
type Trigger struct {
	config          WatermarkConfig
	tokenRepo       token.Repository
	checkpointStore CheckpointStore
	messageReader   MessageReader
	// lastTrigger tracks the last trigger time per session to enforce cooldown.
	lastTrigger map[string]time.Time
}

// NewTrigger creates a new compaction trigger.
func NewTrigger(config WatermarkConfig, tokenRepo token.Repository, checkpointStore CheckpointStore, messageReader MessageReader) *Trigger {
	if config.HighWatermark <= 0 || config.HighWatermark > 1 {
		config.HighWatermark = 0.80
	}
	if config.LowWatermark <= 0 || config.LowWatermark > 1 {
		config.LowWatermark = 0.60
	}
	if config.MinMessagesBeforeCompaction <= 0 {
		config.MinMessagesBeforeCompaction = 10
	}
	if config.CheckCooldown <= 0 {
		config.CheckCooldown = 30 * time.Second
	}
	return &Trigger{
		config:          config,
		tokenRepo:       tokenRepo,
		checkpointStore: checkpointStore,
		messageReader:   messageReader,
		lastTrigger:     make(map[string]time.Time),
	}
}

// CheckAndTrigger evaluates whether automatic compaction should be triggered
// for the given session. It returns a TriggerResult describing the outcome.
//
// Compaction is triggered when:
//  1. Token usage exceeds the high watermark.
//  2. The session has enough messages to justify compaction.
//  3. The cooldown period has elapsed since the last trigger.
//  4. The latest checkpoint is not already in a pending/running state.
func (t *Trigger) CheckAndTrigger(ctx context.Context, sessionID, provider, model, tokenizerRevision string, contextWindow int64) (TriggerResult, error) {
	result := TriggerResult{
		Budget: contextWindow,
	}

	// Calculate effective budget using the high watermark.
	effectiveBudget := int64(float64(contextWindow) * t.config.HighWatermark)

	// Get current token usage.
	usage, err := t.tokenRepo.SumTokenLedgerBySession(ctx, sessionID, provider, model, tokenizerRevision)
	if err != nil {
		return result, fmt.Errorf("sum token ledger: %w", err)
	}
	result.CurrentUsage = usage
	if effectiveBudget > 0 {
		result.UsageFraction = float64(usage) / float64(effectiveBudget)
	}

	// Check if usage is below the high watermark.
	if usage < effectiveBudget {
		result.Reason = fmt.Sprintf("token usage (%d) below high watermark (%d)", usage, effectiveBudget)
		return result, nil
	}

	// Check cooldown.
	if last, ok := t.lastTrigger[sessionID]; ok {
		if time.Since(last) < t.config.CheckCooldown {
			result.Reason = fmt.Sprintf("cooldown period not elapsed (last trigger: %s)", last.Format(time.RFC3339))
			return result, nil
		}
	}

	// Check if a compaction is already in progress.
	latest, err := t.checkpointStore.GetLatestCheckpoint(ctx, sessionID)
	if err != nil {
		return result, fmt.Errorf("get latest checkpoint: %w", err)
	}
	if latest != nil && (latest.Status == compaction.StatusPending || latest.Status == compaction.StatusRunning) {
		result.Reason = fmt.Sprintf("compaction already in progress (checkpoint %s, status %s)", latest.ID, latest.Status)
		return result, nil
	}

	// We need at least MinMessagesBeforeCompaction messages in the session.
	// Use the message reader to check the actual message count.
	messages, err := t.messageReader.ListMessages(ctx, sessionID, "backward", t.config.MinMessagesBeforeCompaction)
	if err != nil {
		return result, fmt.Errorf("list messages: %w", err)
	}
	if len(messages) < t.config.MinMessagesBeforeCompaction {
		result.Reason = fmt.Sprintf("insufficient messages for compaction (%d < %d)", len(messages), t.config.MinMessagesBeforeCompaction)
		return result, nil
	}

	// Determine the source range for compaction.
	// Compact from the beginning of the session up to (but not including) the
	// most recent messages that should remain uncompacted.
	// The low watermark defines how much context should remain after compaction.
	lowWatermarkTokens := int64(float64(contextWindow) * t.config.LowWatermark)

	// Find the cutoff point: messages from the end that fit within the low watermark.
	// We need to find the source range [startSeq, endSeq] to compact.
	// The endSeq is the point where remaining messages fit within the low watermark.
	allMessages, err := t.messageReader.ListMessages(ctx, sessionID, "backward", 1000)
	if err != nil {
		return result, fmt.Errorf("list all messages: %w", err)
	}

	if len(allMessages) == 0 {
		result.Reason = "no messages in session"
		return result, nil
	}

	// Find the compaction boundary: compact all messages that are older than
	// what fits within the low watermark.
	var keepTokens int64
	cutoffIdx := 0
	for i, msg := range allMessages {
		// allMessages is in backward order (newest first).
		// We keep the newest messages that fit within the low watermark.
		entry, err := t.tokenRepo.GetTokenLedger(ctx, msg.ID, provider, model, tokenizerRevision)
		if err != nil {
			return result, fmt.Errorf("get token ledger for message %s: %w", msg.ID, err)
		}
		msgTokens := int64(0)
		if entry != nil {
			msgTokens = entry.TokenCount
		}
		if keepTokens+msgTokens > lowWatermarkTokens {
			cutoffIdx = i
			break
		}
		keepTokens += msgTokens
		if i == len(allMessages)-1 {
			// All messages fit within the low watermark; no compaction needed.
			result.Reason = fmt.Sprintf("all messages (%d) fit within low watermark", len(allMessages))
			return result, nil
		}
	}

	// Messages from cutoffIdx to end are the ones to compact.
	// In backward order, these are the oldest messages.
	compactMessages := allMessages[cutoffIdx:]
	if len(compactMessages) < t.config.MinMessagesBeforeCompaction {
		result.Reason = fmt.Sprintf("compaction range too small (%d < %d)", len(compactMessages), t.config.MinMessagesBeforeCompaction)
		return result, nil
	}

	// The source range is the oldest compacted messages.
	// In backward order, the last element is the oldest (sequence 1).
	sourceStart := compactMessages[len(compactMessages)-1]
	sourceEnd := compactMessages[0]

	// Compute source digest.
	digestInput := fmt.Sprintf("%s:%d:%d:%s:%s", sessionID, sourceStart.Sequence, sourceEnd.Sequence, sourceStart.ID, sourceEnd.ID)
	digest := sha256.Sum256([]byte(digestInput))

	// Determine the next version.
	nextVersion := int64(1)
	if latest != nil {
		nextVersion = latest.Version + 1
	}

	// Create the checkpoint.
	prevCheckpointID := (*string)(nil)
	prevCheckpointDigest := (*string)(nil)
	if latest != nil && latest.Status == compaction.StatusSucceeded {
		prevCheckpointID = &latest.ID
		prevCheckpointDigest = &latest.SourceDigest
	}

	checkpoint := compaction.Checkpoint{
		SessionID:            sessionID,
		Version:              nextVersion,
		SourceStartID:        sourceStart.ID,
		SourceEndID:          sourceEnd.ID,
		SourceStartSeq:       sourceStart.Sequence,
		SourceEndSeq:         sourceEnd.Sequence,
		SourceDigest:         hex.EncodeToString(digest[:]),
		PrevCheckpointID:     prevCheckpointID,
		PrevCheckpointDigest: prevCheckpointDigest,
		SummarySchemaVersion: compaction.SummarySchemaVersion,
		Trigger:              compaction.TriggerAutomatic,
		TriggerReason:        fmt.Sprintf("token usage %.1f%% exceeds high watermark %.1f%%", result.UsageFraction*100, t.config.HighWatermark*100),
		Status:               compaction.StatusPending,
		Provider:             provider,
		Model:                model,
		SummaryJSON:          "{}",
	}

	created, err := t.checkpointStore.CreateCheckpoint(ctx, checkpoint)
	if err != nil {
		return result, fmt.Errorf("create checkpoint: %w", err)
	}

	t.lastTrigger[sessionID] = time.Now()

	result.Triggered = true
	result.CheckpointID = created.ID
	result.Reason = fmt.Sprintf("compaction triggered: checkpoint %s, compacting messages %d-%d (%d messages), usage %.1f%%",
		created.ID, sourceStart.Sequence, sourceEnd.Sequence, len(compactMessages), result.UsageFraction*100)
	return result, nil
}

// ResetCooldown clears the cooldown for a session, allowing immediate re-trigger.
func (t *Trigger) ResetCooldown(sessionID string) {
	delete(t.lastTrigger, sessionID)
}

// ManualTriggerResult describes the outcome of a manual compaction trigger.
type ManualTriggerResult struct {
	// Triggered indicates whether a checkpoint was created.
	Triggered bool
	// CheckpointID is the ID of the created pending checkpoint.
	CheckpointID string
	// SourceStartSeq is the first sequence number in the compaction range.
	SourceStartSeq int64
	// SourceEndSeq is the last sequence number in the compaction range.
	SourceEndSeq int64
	// MessageCount is the number of messages in the compaction range.
	MessageCount int
	// Reason describes the outcome.
	Reason string
}

// TriggerManual creates a manual compaction checkpoint for the specified source
// range. Unlike CheckAndTrigger, it bypasses watermark checks and cooldowns,
// but still respects:
//   - No compaction in progress (pending/running checkpoint exists).
//   - The source range must contain at least MinMessagesBeforeCompaction messages.
//   - The source range must be valid (startSeq <= endSeq, both >= 1).
//
// The caller is responsible for executing the checkpoint via the Executor after
// creation. Manual compaction never deletes source messages (ADR-005 §1).
func (t *Trigger) TriggerManual(ctx context.Context, sessionID, provider, model string, startSeq, endSeq int64) (ManualTriggerResult, error) {
	result := ManualTriggerResult{}

	if startSeq < 1 || endSeq < 1 || startSeq > endSeq {
		return result, fmt.Errorf("invalid source range: start=%d end=%d", startSeq, endSeq)
	}

	// Check if a compaction is already in progress.
	latest, err := t.checkpointStore.GetLatestCheckpoint(ctx, sessionID)
	if err != nil {
		return result, fmt.Errorf("get latest checkpoint: %w", err)
	}
	if latest != nil && (latest.Status == compaction.StatusPending || latest.Status == compaction.StatusRunning) {
		result.Reason = fmt.Sprintf("compaction already in progress (checkpoint %s, status %s)", latest.ID, latest.Status)
		return result, nil
	}

	// Fetch all messages to find the ones in the source range.
	allMessages, err := t.messageReader.ListMessages(ctx, sessionID, "forward", 10000)
	if err != nil {
		return result, fmt.Errorf("list messages: %w", err)
	}

	// Filter messages to the specified range.
	var rangeMessages []MessageInfo
	for _, msg := range allMessages {
		if msg.Sequence >= startSeq && msg.Sequence <= endSeq {
			rangeMessages = append(rangeMessages, msg)
		}
	}

	if len(rangeMessages) < t.config.MinMessagesBeforeCompaction {
		result.Reason = fmt.Sprintf("insufficient messages in range (%d < %d)", len(rangeMessages), t.config.MinMessagesBeforeCompaction)
		return result, nil
	}

	sourceStart := rangeMessages[0]
	sourceEnd := rangeMessages[len(rangeMessages)-1]

	// Compute source digest (deterministic, covers the range boundaries).
	digestInput := fmt.Sprintf("%s:%d:%d:%s:%s", sessionID, sourceStart.Sequence, sourceEnd.Sequence, sourceStart.ID, sourceEnd.ID)
	digest := sha256.Sum256([]byte(digestInput))

	// Determine the next version.
	nextVersion := int64(1)
	if latest != nil {
		nextVersion = latest.Version + 1
	}

	// Link to the previous succeeded checkpoint for rolling compaction.
	prevCheckpointID := (*string)(nil)
	prevCheckpointDigest := (*string)(nil)
	if latest != nil && latest.Status == compaction.StatusSucceeded {
		prevCheckpointID = &latest.ID
		prevCheckpointDigest = &latest.SourceDigest
	}

	checkpoint := compaction.Checkpoint{
		SessionID:            sessionID,
		Version:              nextVersion,
		SourceStartID:        sourceStart.ID,
		SourceEndID:          sourceEnd.ID,
		SourceStartSeq:       sourceStart.Sequence,
		SourceEndSeq:         sourceEnd.Sequence,
		SourceDigest:         hex.EncodeToString(digest[:]),
		PrevCheckpointID:     prevCheckpointID,
		PrevCheckpointDigest: prevCheckpointDigest,
		SummarySchemaVersion: compaction.SummarySchemaVersion,
		Trigger:              compaction.TriggerManual,
		TriggerReason:        fmt.Sprintf("manual compaction requested for messages %d-%d", sourceStart.Sequence, sourceEnd.Sequence),
		Status:               compaction.StatusPending,
		Provider:             provider,
		Model:                model,
		SummaryJSON:          "{}",
	}

	created, err := t.checkpointStore.CreateCheckpoint(ctx, checkpoint)
	if err != nil {
		return result, fmt.Errorf("create checkpoint: %w", err)
	}

	result.Triggered = true
	result.CheckpointID = created.ID
	result.SourceStartSeq = sourceStart.Sequence
	result.SourceEndSeq = sourceEnd.Sequence
	result.MessageCount = len(rangeMessages)
	result.Reason = fmt.Sprintf("manual checkpoint %s created for messages %d-%d (%d messages)",
		created.ID, sourceStart.Sequence, sourceEnd.Sequence, len(rangeMessages))
	return result, nil
}