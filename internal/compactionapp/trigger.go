package compactionapp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
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
	// ListMessages returns one stable sequence page. snapshot and boundary are
	// zero for the first page; subsequent pages reuse the returned snapshot and
	// use the last message sequence as boundary. A page limit is never a session
	// capacity limit.
	ListMessages(ctx context.Context, sessionID string, direction string, snapshot, boundary int64, limit int) ([]MessageInfo, int64, bool, error)
}

// MessageInfo is a minimal message representation for the trigger.
type MessageInfo struct {
	ID       string
	Sequence int64
}

const compactionMessagePageSize = 256

var errStopMessageScan = fmt.Errorf("stop message scan")

func scanMessagePages(ctx context.Context, reader MessageReader, sessionID, direction string, visit func(MessageInfo) error) error {
	var snapshot, boundary int64
	for {
		page, stableSnapshot, hasMore, err := reader.ListMessages(ctx, sessionID, direction, snapshot, boundary, compactionMessagePageSize)
		if err != nil {
			return err
		}
		if snapshot == 0 {
			snapshot = stableSnapshot
		} else if stableSnapshot != snapshot {
			return fmt.Errorf("message snapshot changed during compaction scan")
		}
		for _, message := range page {
			if err := visit(message); err != nil {
				if err == errStopMessageScan {
					return nil
				}
				return err
			}
		}
		if !hasMore {
			return nil
		}
		if len(page) == 0 {
			return fmt.Errorf("message reader returned empty non-terminal page")
		}
		boundary = page[len(page)-1].Sequence
	}
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
	// lastMu protects lastTrigger from concurrent access.
	lastMu sync.Mutex
	// sessionLocks provides per-session mutexes to prevent concurrent
	// compaction triggers and executions on the same session (ADR-005 §5:
	// "automatic and manual compaction must not run concurrently on the same
	// session").
	sessionLocks sync.Map // map[string]*sync.Mutex
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

// sessionLock returns the mutex for the given session, creating it if needed.
// This mutex serializes all compaction operations (trigger + execute) for a
// single session, preventing TOCTOU races and concurrent checkpoint creation.
func (t *Trigger) sessionLock(sessionID string) chan struct{} {
	candidate := make(chan struct{}, 1)
	candidate <- struct{}{}
	v, _ := t.sessionLocks.LoadOrStore(sessionID, candidate)
	return v.(chan struct{})
}

// LockSession acquires the per-session compaction lock. The returned unlock
// function must be called when done. This allows the Executor to coordinate
// with the Trigger to prevent concurrent compactions on the same session.
func (t *Trigger) LockSession(ctx context.Context, sessionID string) (func(), error) {
	lock := t.sessionLock(sessionID)
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-lock:
		return func() { lock <- struct{}{} }, nil
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
//
// This method acquires a per-session lock to prevent concurrent compaction
// triggers on the same session (ADR-005 §5).
func (t *Trigger) CheckAndTrigger(ctx context.Context, sessionID, provider, model, tokenizerRevision string, contextWindow int64) (TriggerResult, error) {
	result := TriggerResult{
		Budget: contextWindow,
	}

	// Acquire per-session lock to serialize compaction operations.
	unlock, err := t.LockSession(ctx, sessionID)
	if err != nil {
		return result, err
	}
	defer unlock()

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

	// Check cooldown (thread-safe).
	t.lastMu.Lock()
	if last, ok := t.lastTrigger[sessionID]; ok {
		if time.Since(last) < t.config.CheckCooldown {
			t.lastMu.Unlock()
			result.Reason = fmt.Sprintf("cooldown period not elapsed (last trigger: %s)", last.Format(time.RFC3339))
			return result, nil
		}
	}
	t.lastMu.Unlock()

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
	messages, _, _, err := t.messageReader.ListMessages(ctx, sessionID, "backward", 0, 0, t.config.MinMessagesBeforeCompaction)
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

	// Scan the stable snapshot in backward pages. Retain only source boundaries
	// and count so long sessions do not require a second full in-memory index.
	var keepTokens int64
	var sourceStart, sourceEnd MessageInfo
	compactCount := 0
	err = scanMessagePages(ctx, t.messageReader, sessionID, "backward", func(msg MessageInfo) error {
		entry, err := t.tokenRepo.GetTokenLedger(ctx, msg.ID, provider, model, tokenizerRevision)
		if err != nil {
			return fmt.Errorf("get token ledger for message %s: %w", msg.ID, err)
		}
		msgTokens := int64(0)
		if entry != nil {
			msgTokens = entry.TokenCount
		}
		if compactCount == 0 && keepTokens+msgTokens <= lowWatermarkTokens {
			keepTokens += msgTokens
			return nil
		}
		if compactCount == 0 {
			sourceEnd = msg
		}
		sourceStart = msg
		compactCount++
		return nil
	})
	if err != nil {
		return result, fmt.Errorf("scan messages: %w", err)
	}
	if compactCount == 0 {
		result.Reason = "all messages fit within low watermark"
		return result, nil
	}
	if compactCount < t.config.MinMessagesBeforeCompaction {
		result.Reason = fmt.Sprintf("compaction range too small (%d < %d)", compactCount, t.config.MinMessagesBeforeCompaction)
		return result, nil
	}

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

	t.lastMu.Lock()
	t.lastTrigger[sessionID] = time.Now()
	t.lastMu.Unlock()

	result.Triggered = true
	result.CheckpointID = created.ID
	result.Reason = fmt.Sprintf("compaction triggered: checkpoint %s, compacting messages %d-%d (%d messages), usage %.1f%%",
		created.ID, sourceStart.Sequence, sourceEnd.Sequence, compactCount, result.UsageFraction*100)
	return result, nil
}

// ResetCooldown clears the cooldown for a session, allowing immediate re-trigger.
func (t *Trigger) ResetCooldown(sessionID string) {
	t.lastMu.Lock()
	delete(t.lastTrigger, sessionID)
	t.lastMu.Unlock()
}

// GetLatestCheckpoint returns the latest checkpoint (by version) for a session,
// or nil if no checkpoint exists. Delegates to the checkpoint store.
func (t *Trigger) GetLatestCheckpoint(ctx context.Context, sessionID string) (*compaction.Checkpoint, error) {
	return t.checkpointStore.GetLatestCheckpoint(ctx, sessionID)
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

	// Acquire per-session lock to serialize compaction operations.
	unlock, err := t.LockSession(ctx, sessionID)
	if err != nil {
		return result, err
	}
	defer unlock()

	// Check if a compaction is already in progress.
	latest, err := t.checkpointStore.GetLatestCheckpoint(ctx, sessionID)
	if err != nil {
		return result, fmt.Errorf("get latest checkpoint: %w", err)
	}
	if latest != nil && (latest.Status == compaction.StatusPending || latest.Status == compaction.StatusRunning) {
		result.Reason = fmt.Sprintf("compaction already in progress (checkpoint %s, status %s)", latest.ID, latest.Status)
		return result, nil
	}

	// Scan the stable snapshot page by page and retain only selected boundaries.
	var sourceStart, sourceEnd MessageInfo
	messageCount := 0
	err = scanMessagePages(ctx, t.messageReader, sessionID, "forward", func(msg MessageInfo) error {
		if msg.Sequence < startSeq || msg.Sequence > endSeq {
			return nil
		}
		if messageCount == 0 {
			sourceStart = msg
		}
		sourceEnd = msg
		messageCount++
		if msg.Sequence == endSeq {
			return errStopMessageScan
		}
		return nil
	})
	if err != nil {
		return result, fmt.Errorf("scan messages: %w", err)
	}
	if messageCount < t.config.MinMessagesBeforeCompaction {
		result.Reason = fmt.Sprintf("insufficient messages in range (%d < %d)", messageCount, t.config.MinMessagesBeforeCompaction)
		return result, nil
	}
	if sourceStart.Sequence != startSeq || sourceEnd.Sequence != endSeq || int64(messageCount) != endSeq-startSeq+1 {
		return result, fmt.Errorf("source range does not exist exactly: requested=%d-%d actual=%d-%d count=%d", startSeq, endSeq, sourceStart.Sequence, sourceEnd.Sequence, messageCount)
	}

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
	result.MessageCount = messageCount
	result.Reason = fmt.Sprintf("manual checkpoint %s created for messages %d-%d (%d messages)",
		created.ID, sourceStart.Sequence, sourceEnd.Sequence, messageCount)
	return result, nil
}
