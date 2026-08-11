package compactionapp

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/lunitide/lunitide/internal/domain/compaction"
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
	ID       string
	Role     string
	Content  string
	Sequence int64
}

// ErrCheckpointNotPending is returned when attempting to execute a checkpoint that is not in pending state.
var ErrCheckpointNotPending = errors.New("checkpoint is not pending")

// ErrCheckpointNotFound is returned when the checkpoint does not exist.
var ErrCheckpointNotFound = errors.New("checkpoint not found")

// ErrNoMessagesToSummarize is returned when the source message range contains no messages.
var ErrNoMessagesToSummarize = errors.New("no messages in source range to summarize")

// ExecutorStore provides the storage operations needed by the compaction executor.
type ExecutorStore interface {
	// GetCheckpoint returns the checkpoint by ID.
	GetCheckpoint(ctx context.Context, id string) (*compaction.Checkpoint, error)
	// UpdateCheckpointStatus updates status, summary, and failure code.
	UpdateCheckpointStatus(ctx context.Context, id string, status compaction.Status, summaryJSON, humanSummary string, failureCode *string) error
	// ListCheckpointsByStatus returns checkpoints matching the given status,
	// ordered by created_at ascending. Used by restart recovery to find
	// orphaned pending/running checkpoints (ADR-005 §5).
	ListCheckpointsByStatus(ctx context.Context, status compaction.Status, limit int) ([]compaction.Checkpoint, error)
}

// SourceReader provides access to the messages in the source range.
type SourceReader interface {
	// ListMessagesByRange returns messages in the session within [startSeq, endSeq] inclusive,
	// ordered by sequence ascending.
	ListMessagesByRange(ctx context.Context, sessionID string, startSeq, endSeq int64) ([]SummaryMessage, error)
}

// Executor processes pending compaction checkpoints by generating summaries.
type Executor struct {
	store       ExecutorStore
	sourceReader SourceReader
	summarizer  Summarizer
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

// ExecuteResult describes the outcome of executing a checkpoint.
type ExecuteResult struct {
	CheckpointID  string
	Status        compaction.Status
	SummaryJSON   string
	HumanSummary  string
	FailureCode   *string
	DurationMs    int64
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

	// 2. Transition pending → running.
	if err := e.store.UpdateCheckpointStatus(execCtx, checkpointID, compaction.StatusRunning, "{}", "", nil); err != nil {
		return result, fmt.Errorf("transition to running: %w", err)
	}

	// 3. Fetch source messages.
	messages, err := e.sourceReader.ListMessagesByRange(execCtx, cp.SessionID, cp.SourceStartSeq, cp.SourceEndSeq)
	if err != nil {
		e.markFailed(execCtx, checkpointID, "SOURCE_READ_FAILED", err)
		result.Status = compaction.StatusFailed
		result.DurationMs = time.Since(start).Milliseconds()
		return result, nil
	}
	if len(messages) == 0 {
		e.markFailed(execCtx, checkpointID, "EMPTY_SOURCE_RANGE", ErrNoMessagesToSummarize)
		result.Status = compaction.StatusFailed
		result.DurationMs = time.Since(start).Milliseconds()
		return result, nil
	}

	// Limit messages to bound cost.
	if len(messages) > e.maxMessages {
		messages = messages[:e.maxMessages]
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
		e.markFailed(execCtx, checkpointID, "SUMMARY_FAILED", err)
		result.Status = compaction.StatusFailed
		result.DurationMs = time.Since(start).Milliseconds()
		return result, nil
	}

	// 4.5. Protected facts validation: verify the summary preserves exact
	// identifiers, values, and quotations from the source range (ADR-005 §4).
	facts := ExtractProtectedFacts(messages)
	if err := ValidateProtectedFacts(summaryJSON, facts); err != nil {
		e.markFailed(execCtx, checkpointID, "PROTECTED_FACTS_VIOLATION", err)
		result.Status = compaction.StatusFailed
		result.DurationMs = time.Since(start).Milliseconds()
		return result, nil
	}

	// 5. Transition running → succeeded.
	if err := e.store.UpdateCheckpointStatus(execCtx, checkpointID, compaction.StatusSucceeded, summaryJSON, humanSummary, nil); err != nil {
		return result, fmt.Errorf("transition to succeeded: %w", err)
	}

	result.Status = compaction.StatusSucceeded
	result.SummaryJSON = summaryJSON
	result.HumanSummary = humanSummary
	result.DurationMs = time.Since(start).Milliseconds()
	return result, nil
}

// markFailed transitions a checkpoint to failed status with a failure code.
func (e *Executor) markFailed(ctx context.Context, checkpointID, code string, cause error) {
	failureCode := code
	_ = e.store.UpdateCheckpointStatus(ctx, checkpointID, compaction.StatusFailed, "{}", "", &failureCode)
	_ = cause // cause is logged via failure code; full error not stored to avoid leaking details
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
		if err := e.store.UpdateCheckpointStatus(ctx, cp.ID, compaction.StatusFailed, "{}", "", &failureCode); err != nil {
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
