package compactionapp

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/lunitide/lunitide/internal/domain/compaction"
	"github.com/lunitide/lunitide/internal/domain/token"
)

// Summarizer generates a structured summary from a range of session messages.
// Implementations typically call an LLM via the gateway.
type Summarizer interface {
	// Summarize produces a summary for the given message range.
	// providerID and modelID identify which LLM to use for summarization.
	// priorSummary is the JSON summary of the previous succeeded checkpoint
	// (empty for the first compaction), enabling rolling/incremental summaries.
	// Returns summaryJSON (structured), humanSummary (plain text), and error.
	Summarize(ctx context.Context, sessionID, providerID, modelID string, sourceStartSeq, sourceEndSeq int64, messages []SummaryMessage, priorSummary string) (summaryJSON, humanSummary string, err error)
}

// SummaryMessage is a minimal message representation passed to the Summarizer.
type SummaryMessage struct {
	ID       string `json:"id"`
	Role     string `json:"role"`
	Content  string `json:"content"`
	Sequence int64  `json:"sequence"`
}

// ErrCheckpointNotPending is returned when attempting to execute a checkpoint that is not in pending state.
var ErrCheckpointNotPending = errors.New("checkpoint is not pending")

// ErrCheckpointNotFound is returned when the checkpoint does not exist.
var ErrCheckpointNotFound = errors.New("checkpoint not found")

// ErrNoMessagesToSummarize is returned when the source message range contains no messages.
var ErrNoMessagesToSummarize = errors.New("no messages in source range to summarize")

// ErrConcurrentModification is returned when the CAS (compare-and-swap) on
// checkpoint status fails because the expected status does not match the
// current status in storage (ADR-005 §4.2).
var ErrConcurrentModification = errors.New("concurrent modification: checkpoint status changed")

// ErrCheckpointNotSucceeded is returned when attempting to commit a checkpoint that is not in succeeded state.
var ErrCheckpointNotSucceeded = errors.New("checkpoint is not succeeded")

// ErrVersionConflict is returned when the baseVersion does not match the checkpoint's version (CAS failure).
var ErrVersionConflict = errors.New("baseVersion does not match checkpoint version")

// ExecutorStore provides the storage operations needed by the compaction executor.
type ExecutorStore interface {
	// GetCheckpoint returns the checkpoint by ID.
	GetCheckpoint(ctx context.Context, id string) (*compaction.Checkpoint, error)
	// UpdateCheckpointStatus atomically updates status, summary, and failure code
	// using CAS (compare-and-swap) on expectedStatus. If the current status in
	// storage does not match expectedStatus, the update affects 0 rows and the
	// implementation returns ErrConcurrentModification (ADR-005 §4.2).
	UpdateCheckpointStatus(ctx context.Context, id string, expectedStatus, status compaction.Status, summaryJSON, humanSummary string, failureCode *string) error
	ActivateCheckpoint(ctx context.Context, id string, baseVersion int64) error
	// ListCheckpointsByStatus returns checkpoints matching the given status,
	// ordered by created_at ascending. Used by restart recovery to find
	// orphaned pending/running checkpoints (ADR-005 §5).
	ListCheckpointsByStatus(ctx context.Context, status compaction.Status, limit int) ([]compaction.Checkpoint, error)
	// MarkPreviousSucceededAsSuperseded marks all succeeded checkpoints for the
	// session (except the one with the given currentCheckpointID) as superseded.
	// This is called when a new checkpoint is activated to ensure only one
	// active succeeded checkpoint exists per session (ADR-005 §4.2).
	MarkPreviousSucceededAsSuperseded(ctx context.Context, sessionID, currentCheckpointID string) (int64, error)
	// GetLatestSucceededCheckpoint returns the latest succeeded checkpoint for
	// the session, or nil if none. Used for low-watermark verification and
	// rolling summary loading.
	GetLatestSucceededCheckpoint(ctx context.Context, sessionID string) (*compaction.Checkpoint, error)
	// SumTokenLedgerAfterSeq returns the total token count for messages in the
	// session with sequence > afterSeq. Used for low-watermark verification
	// (remaining uncompacted messages).
	SumTokenLedgerAfterSeq(ctx context.Context, sessionID, provider, model, tokenizerRevision string, afterSeq int64) (int64, error)
}

// SourceReader provides access to the messages in the source range.
type SourceReader interface {
	// ListMessagesByRange returns messages in the session within [startSeq, endSeq] inclusive,
	// ordered by sequence ascending.
	ListMessagesByRange(ctx context.Context, sessionID string, startSeq, endSeq int64) ([]SummaryMessage, error)
}

// Executor processes pending compaction checkpoints by generating summaries.
type Executor struct {
	store        ExecutorStore
	sourceReader SourceReader
	summarizer   Summarizer
	// trigger is used by Preview to create checkpoints via the same watermark
	// logic as automatic compaction. Optional; only needed for Preview.
	trigger *Trigger
	// maxMessages limits the number of messages passed to the summarizer to bound cost.
	maxMessages int
	// timeout bounds the entire execution.
	timeout time.Duration
}

// NewExecutor creates a new compaction executor.
func NewExecutor(store ExecutorStore, sourceReader SourceReader, summarizer Summarizer) *Executor {
	return &Executor{
		store:        store,
		sourceReader: sourceReader,
		summarizer:   summarizer,
		maxMessages:  500,
		timeout:      60 * time.Second,
	}
}

// SetTrigger wires the compaction trigger into the executor, enabling Preview
// to create checkpoints using the same watermark logic as automatic compaction.
func (e *Executor) SetTrigger(t *Trigger) { e.trigger = t }

// ExecuteResult describes the outcome of executing a checkpoint.
type ExecuteResult struct {
	CheckpointID string
	Version      int64
	Status       compaction.Status
	SummaryJSON  string
	HumanSummary string
	FailureCode  *string
	DurationMs   int64
	// LowWatermarkVerified is true when the post-compaction reusable context
	// was verified to be below the low watermark (ADR-005 §5). False when
	// verification was not performed (e.g., context window unknown).
	LowWatermarkVerified bool
	// LowWatermarkUsageFraction is the remaining reusable context as a fraction
	// of the context window (0.0–1.0). Only meaningful when LowWatermarkVerified is true.
	LowWatermarkUsageFraction float64
}

// Execute processes a pending checkpoint: transitions to running, generates summary, transitions to succeeded/failed.
func (e *Executor) Execute(ctx context.Context, checkpointID string) (ExecuteResult, error) {
	start := time.Now()
	result := ExecuteResult{CheckpointID: checkpointID}

	execCtx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()

	// 1. Load checkpoint.
	cp, err := e.store.GetCheckpoint(execCtx, checkpointID)
	if err != nil {
		return result, fmt.Errorf("get checkpoint: %w", err)
	}
	if cp == nil {
		return result, ErrCheckpointNotFound
	}
	if cp.Status != compaction.StatusPending {
		return result, fmt.Errorf("%w: current status %s", ErrCheckpointNotPending, cp.Status)
	}

	// 2. Transition pending → running (CAS: expectedStatus=pending).
	if err := e.store.UpdateCheckpointStatus(execCtx, checkpointID, compaction.StatusPending, compaction.StatusRunning, "{}", "", nil); err != nil {
		return result, fmt.Errorf("transition to running: %w", err)
	}

	// 3. Fetch source messages.
	messages, err := e.sourceReader.ListMessagesByRange(execCtx, cp.SessionID, cp.SourceStartSeq, cp.SourceEndSeq)
	if err != nil {
		if cleanupErr := e.markFailed(execCtx, checkpointID, "SOURCE_READ_FAILED", err); cleanupErr != nil {
			return result, fmt.Errorf("source read failed (%v); persist failed status: %w", err, cleanupErr)
		}
		result.Status = compaction.StatusFailed
		result.DurationMs = time.Since(start).Milliseconds()
		return result, nil
	}
	if len(messages) == 0 {
		if cleanupErr := e.markFailed(execCtx, checkpointID, "EMPTY_SOURCE_RANGE", ErrNoMessagesToSummarize); cleanupErr != nil {
			return result, fmt.Errorf("empty source range; persist failed status: %w", cleanupErr)
		}
		result.Status = compaction.StatusFailed
		result.DurationMs = time.Since(start).Milliseconds()
		return result, nil
	}

	// Reject oversized source ranges rather than summarizing only a prefix while
	// recording the full checkpoint range as covered.
	if len(messages) > e.maxMessages {
		if cleanupErr := e.markFailed(execCtx, checkpointID, "SOURCE_RANGE_TOO_LARGE", nil); cleanupErr != nil {
			return result, fmt.Errorf("source range exceeds maximum of %d messages; persist failed status: %w", e.maxMessages, cleanupErr)
		}
		result.Status = compaction.StatusFailed
		result.DurationMs = time.Since(start).Milliseconds()
		return result, nil
	}

	// Load the previous succeeded checkpoint's summary for rolling/incremental
	// compaction (ADR-005 §3). The checkpoint's PrevCheckpointID links to the
	// prior version; if that checkpoint succeeded, its SummaryJSON becomes the
	// priorSummary passed to the Summarizer.
	var priorSummary string
	if cp.PrevCheckpointID != nil {
		prevCp, prevErr := e.store.GetCheckpoint(execCtx, *cp.PrevCheckpointID)
		if prevErr == nil && prevCp != nil && prevCp.Status == compaction.StatusSucceeded && prevCp.SummaryJSON != "" && prevCp.SummaryJSON != "{}" {
			priorSummary = prevCp.SummaryJSON
		}
	}

	// 4. Generate summary.
	summaryJSON, humanSummary, err := e.summarizer.Summarize(execCtx, cp.SessionID, cp.Provider, cp.Model, cp.SourceStartSeq, cp.SourceEndSeq, messages, priorSummary)
	if err != nil {
		if cleanupErr := e.markFailed(execCtx, checkpointID, "SUMMARY_FAILED", err); cleanupErr != nil {
			return result, fmt.Errorf("summary failed (%v); persist failed status: %w", err, cleanupErr)
		}
		result.Status = compaction.StatusFailed
		result.DurationMs = time.Since(start).Milliseconds()
		return result, nil
	}

	// 4.5. Protected facts validation: verify the summary preserves exact
	// identifiers, values, and quotations from the source range (ADR-005 §4).
	facts := ExtractProtectedFacts(messages)
	if err := ValidateProtectedFacts(summaryJSON, facts); err != nil {
		if cleanupErr := e.markFailed(execCtx, checkpointID, "PROTECTED_FACTS_VIOLATION", err); cleanupErr != nil {
			return result, fmt.Errorf("protected facts validation failed (%v); persist failed status: %w", err, cleanupErr)
		}
		result.Status = compaction.StatusFailed
		result.DurationMs = time.Since(start).Milliseconds()
		return result, nil
	}

	// 5. Transition running → succeeded (CAS: expectedStatus=running).
	if err := e.store.UpdateCheckpointStatus(execCtx, checkpointID, compaction.StatusRunning, compaction.StatusSucceeded, summaryJSON, humanSummary, nil); err != nil {
		return result, fmt.Errorf("transition to succeeded: %w", err)
	}

	result.Status = compaction.StatusSucceeded
	result.SummaryJSON = summaryJSON
	result.HumanSummary = humanSummary
	result.Version = cp.Version
	result.DurationMs = time.Since(start).Milliseconds()
	return result, nil
}

// markFailed transitions a checkpoint to failed status with a failure code.
// CAS: expectedStatus=running (markFailed is only called after pending→running).
func (e *Executor) markFailed(ctx context.Context, checkpointID, code string, cause error) error {
	failureCode := code
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	err := e.store.UpdateCheckpointStatus(cleanupCtx, checkpointID, compaction.StatusRunning, compaction.StatusFailed, "{}", "", &failureCode)
	_ = cause // cause is logged via failure code; full error not stored to avoid leaking details
	return err
}

// SetMaxMessages adjusts the maximum messages passed to the summarizer.
func (e *Executor) SetMaxMessages(n int) {
	if n > 0 {
		e.maxMessages = n
	}
}

// SetTimeout adjusts the execution timeout.
func (e *Executor) SetTimeout(d time.Duration) {
	if d > 0 {
		e.timeout = d
	}
}

// RecoveryResult describes the outcome of a single checkpoint recovery.
type RecoveryResult struct {
	CheckpointID string
	SessionID    string
	Version      int64
	// Action taken: "reexecuted", "marked_failed", "skipped".
	Action string
	// Status is the final status after recovery.
	Status compaction.Status
	// Err is non-nil if recovery encountered an error.
	Err error
}

// RecoverOrphanedCheckpoints scans for checkpoints left in pending or running
// state by a previous process crash and reconciles them (ADR-005 §5: "restart
// recovery"). This must be called once at engine startup before serving traffic.
//
// Recovery policy:
//   - Running checkpoints: The previous process crashed mid-execution. The
//     summary is likely incomplete or missing. Mark as failed with code
//     "INTERRUPTED_BY_RESTART" so the trigger can re-compact on the next chat
//     turn. Re-executing would risk duplicate LLM calls.
//   - Pending checkpoints: The checkpoint was created but never started. Re-
//     execute synchronously to preserve the compaction intent.
//
// The method returns one RecoveryResult per orphaned checkpoint found.
func (e *Executor) RecoverOrphanedCheckpoints(ctx context.Context) ([]RecoveryResult, error) {
	var results []RecoveryResult

	// Phase 1: Find and mark running checkpoints as failed.
	running, err := e.store.ListCheckpointsByStatus(ctx, compaction.StatusRunning, 1000)
	if err != nil {
		return nil, fmt.Errorf("list running checkpoints: %w", err)
	}
	for _, cp := range running {
		failureCode := "INTERRUPTED_BY_RESTART"
		if err := e.store.UpdateCheckpointStatus(ctx, cp.ID, compaction.StatusRunning, compaction.StatusFailed, "{}", "", &failureCode); err != nil {
			results = append(results, RecoveryResult{
				CheckpointID: cp.ID,
				SessionID:    cp.SessionID,
				Version:      cp.Version,
				Action:       "marked_failed",
				Status:       compaction.StatusFailed,
				Err:          fmt.Errorf("update status: %w", err),
			})
			continue
		}
		results = append(results, RecoveryResult{
			CheckpointID: cp.ID,
			SessionID:    cp.SessionID,
			Version:      cp.Version,
			Action:       "marked_failed",
			Status:       compaction.StatusFailed,
		})
	}

	// Phase 2: Find and re-execute pending checkpoints.
	pending, err := e.store.ListCheckpointsByStatus(ctx, compaction.StatusPending, 1000)
	if err != nil {
		return results, fmt.Errorf("list pending checkpoints: %w", err)
	}
	for _, cp := range pending {
		execResult, err := e.Execute(ctx, cp.ID)
		if err != nil {
			results = append(results, RecoveryResult{
				CheckpointID: cp.ID,
				SessionID:    cp.SessionID,
				Version:      cp.Version,
				Action:       "reexecuted",
				Status:       compaction.StatusFailed,
				Err:          err,
			})
			continue
		}
		results = append(results, RecoveryResult{
			CheckpointID: cp.ID,
			SessionID:    cp.SessionID,
			Version:      cp.Version,
			Action:       "reexecuted",
			Status:       execResult.Status,
		})
	}

	return results, nil
}

// PreviewResult describes the outcome of a preview compaction.
type PreviewResult struct {
	CheckpointID   string
	Version        int64
	SourceStartSeq int64
	SourceEndSeq   int64
	SourceDigest   string
	SummaryPreview string
	HumanSummary   string
	Status         string
}

// Preview generates a draft checkpoint without activating it. The checkpoint
// is created with status "pending" and the summary is generated via Execute
// (pending→running→succeeded). The succeeded checkpoint becomes the latest
// checkpoint but does not change the active prompt assembly until Commit is
// called (ADR-005 §4.2: preview must not change active checkpoint).
//
// Preview uses the Trigger's CheckAndTrigger logic to determine the source
// range and create the checkpoint. If the token usage is below the high
// watermark, no checkpoint is created and an error is returned.
func (e *Executor) Preview(ctx context.Context, sessionID, provider, model, tokenizerRevision string, contextWindow int64) (*PreviewResult, error) {
	if e.trigger == nil {
		return nil, errors.New("trigger not configured on executor")
	}

	// 1. Create a pending checkpoint via the trigger's watermark logic.
	triggerResult, err := e.trigger.CheckAndTrigger(ctx, sessionID, provider, model, tokenizerRevision, contextWindow)
	if err != nil {
		return nil, fmt.Errorf("trigger preview checkpoint: %w", err)
	}
	if !triggerResult.Triggered {
		return nil, fmt.Errorf("compaction not triggered: %s", triggerResult.Reason)
	}

	// 2. Execute the checkpoint to generate the summary (pending→running→succeeded).
	execResult, err := e.Execute(ctx, triggerResult.CheckpointID)
	if err != nil {
		return nil, fmt.Errorf("execute preview checkpoint: %w", err)
	}
	if execResult.Status != compaction.StatusSucceeded {
		return nil, fmt.Errorf("preview execution failed: status %s", execResult.Status)
	}

	// 3. Load the checkpoint to get full details for the preview result.
	cp, err := e.store.GetCheckpoint(ctx, triggerResult.CheckpointID)
	if err != nil {
		return nil, fmt.Errorf("load preview checkpoint: %w", err)
	}
	if cp == nil {
		return nil, ErrCheckpointNotFound
	}

	return &PreviewResult{
		CheckpointID:   cp.ID,
		Version:        cp.Version,
		SourceStartSeq: cp.SourceStartSeq,
		SourceEndSeq:   cp.SourceEndSeq,
		SourceDigest:   cp.SourceDigest,
		SummaryPreview: cp.SummaryJSON,
		HumanSummary:   cp.HumanSummary,
		Status:         string(cp.Status),
	}, nil
}

// CommitResult describes the outcome of committing a previewed checkpoint.
type CommitResult struct {
	CheckpointID string
	Version      int64
	Status       string
	Activated    bool
}

// Commit activates a previewed checkpoint using CAS on baseVersion.
// It re-validates that the checkpoint exists, is in "succeeded" status,
// and that baseVersion matches the current version (ADR-005 §4.2).
//
// On successful commit, all previous succeeded checkpoints for the same
// session are marked as superseded, ensuring only one active succeeded
// checkpoint exists per session.
func (e *Executor) Commit(ctx context.Context, checkpointID string, baseVersion int64) (*CommitResult, error) {
	cp, err := e.store.GetCheckpoint(ctx, checkpointID)
	if err != nil {
		return nil, fmt.Errorf("get checkpoint: %w", err)
	}
	if cp == nil {
		return nil, ErrCheckpointNotFound
	}
	if cp.Status != compaction.StatusSucceeded {
		return nil, fmt.Errorf("%w: current status %s", ErrCheckpointNotSucceeded, cp.Status)
	}
	if cp.Version != baseVersion {
		return nil, fmt.Errorf("%w: expected %d, got %d", ErrVersionConflict, baseVersion, cp.Version)
	}

	if err := e.store.ActivateCheckpoint(ctx, checkpointID, baseVersion); err != nil {
		return nil, fmt.Errorf("activate checkpoint: %w", err)
	}

	return &CommitResult{
		CheckpointID: cp.ID,
		Version:      cp.Version,
		Status:       string(cp.Status),
		Activated:    true,
	}, nil
}

// CancelResult describes the outcome of cancelling a pending checkpoint.
type CancelResult struct {
	CheckpointID string
	Status       string
	Cancelled    bool
}

// Cancel cancels a pending checkpoint by marking it as failed with code
// "CANCELLED" using CAS (expectedStatus=pending). Only pending checkpoints
// can be cancelled; checkpoints in running or terminal states return an error.
func (e *Executor) Cancel(ctx context.Context, checkpointID string) (*CancelResult, error) {
	cp, err := e.store.GetCheckpoint(ctx, checkpointID)
	if err != nil {
		return nil, fmt.Errorf("get checkpoint: %w", err)
	}
	if cp == nil {
		return nil, ErrCheckpointNotFound
	}
	if cp.Status != compaction.StatusPending {
		return nil, fmt.Errorf("%w: current status %s", ErrCheckpointNotPending, cp.Status)
	}

	failureCode := "CANCELLED"
	if err := e.store.UpdateCheckpointStatus(ctx, checkpointID, compaction.StatusPending, compaction.StatusFailed, "{}", "", &failureCode); err != nil {
		return nil, fmt.Errorf("cancel checkpoint: %w", err)
	}

	return &CancelResult{
		CheckpointID: checkpointID,
		Status:       string(compaction.StatusFailed),
		Cancelled:    true,
	}, nil
}

// RetryResult describes the outcome of retrying a failed checkpoint.
type RetryResult struct {
	CheckpointID string
	Status       compaction.Status
	Retried      bool
}

// Retry transitions a failed checkpoint back to pending and re-executes it
// (ADR-005 §5: "failed checkpoints can be retried"). The state machine allows
// failed→pending→running→succeeded/failed. If the checkpoint is not in failed
// state, an error is returned.
//
// The retry is idempotent in the sense that re-executing a failed checkpoint
// creates a new summary attempt without duplicating the checkpoint record.
// The source range and digest are preserved; only the summary is regenerated.
func (e *Executor) Retry(ctx context.Context, checkpointID string) (RetryResult, ExecuteResult, error) {
	cp, err := e.store.GetCheckpoint(ctx, checkpointID)
	if err != nil {
		return RetryResult{}, ExecuteResult{}, fmt.Errorf("get checkpoint: %w", err)
	}
	if cp == nil {
		return RetryResult{}, ExecuteResult{}, ErrCheckpointNotFound
	}
	if cp.Status != compaction.StatusFailed {
		return RetryResult{}, ExecuteResult{}, fmt.Errorf("checkpoint not in failed state (current: %s)", cp.Status)
	}

	// Transition failed → pending (CAS: expectedStatus=failed).
	// Clear the failure code and reset summary to empty JSON.
	if err := e.store.UpdateCheckpointStatus(ctx, checkpointID, compaction.StatusFailed, compaction.StatusPending, "{}", "", nil); err != nil {
		return RetryResult{}, ExecuteResult{}, fmt.Errorf("transition to pending: %w", err)
	}

	// Re-execute the checkpoint (pending → running → succeeded/failed).
	execResult, err := e.Execute(ctx, checkpointID)
	retryResult := RetryResult{
		CheckpointID: checkpointID,
		Status:       execResult.Status,
		Retried:      true,
	}
	return retryResult, execResult, err
}

// VerifyLowWatermark checks that the reusable conversational input after
// compaction is below the low watermark (default 60% of the context window).
// This is a post-compaction verification: the remaining uncompacted messages
// (sequence > sourceEndSeq) plus the summary tokens should be below
// LowWatermark * contextWindow (ADR-005 §5).
//
// Returns (verified, usageFraction, error). When verified is false, the
// compaction was insufficient but cannot be undone — the caller should log a
// warning and potentially trigger another compaction on the next turn.
func (e *Executor) VerifyLowWatermark(ctx context.Context, checkpointID string, contextWindow int64, lowWatermark float64) (bool, float64, error) {
	if contextWindow <= 0 {
		return false, 0, nil
	}
	if lowWatermark <= 0 || lowWatermark > 1 {
		lowWatermark = 0.60
	}

	cp, err := e.store.GetCheckpoint(ctx, checkpointID)
	if err != nil {
		return false, 0, fmt.Errorf("get checkpoint: %w", err)
	}
	if cp == nil {
		return false, 0, ErrCheckpointNotFound
	}

	// Sum tokens for messages after the compaction range (remaining context).
	remaining, err := e.store.SumTokenLedgerAfterSeq(ctx, cp.SessionID, cp.Provider, cp.Model, token.CanonicalTokenizerRevision, cp.SourceEndSeq)
	if err != nil {
		return false, 0, fmt.Errorf("sum remaining tokens: %w", err)
	}

	// Estimate summary token cost (rough: 4 bytes per token, JSON content).
	summaryTokens := int64(len(cp.SummaryJSON) / 4)
	totalReusable := remaining + summaryTokens

	threshold := int64(float64(contextWindow) * lowWatermark)
	fraction := float64(totalReusable) / float64(contextWindow)
	verified := totalReusable < threshold

	return verified, fraction, nil
}

// MarkPreviousSucceededSuperseded marks all succeeded checkpoints for the
// session (except the one with currentCheckpointID) as superseded. This is
// the activation step for automatic compaction (ADR-005 §4.2). Returns the
// number of checkpoints superseded.
func (e *Executor) MarkPreviousSucceededSuperseded(ctx context.Context, sessionID, currentCheckpointID string) (int64, error) {
	return e.store.MarkPreviousSucceededAsSuperseded(ctx, sessionID, currentCheckpointID)
}

func (e *Executor) ActivateAutomatic(ctx context.Context, checkpointID string, version int64) error {
	return e.store.ActivateCheckpoint(ctx, checkpointID, version)
}
