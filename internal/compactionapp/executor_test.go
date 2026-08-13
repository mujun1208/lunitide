package compactionapp

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/lunitide/lunitide/internal/domain/compaction"
	"github.com/lunitide/lunitide/internal/domain/token"
)

type mockExecutorStore struct {
	checkpoint     *compaction.Checkpoint
	prevCheckpoint *compaction.Checkpoint
	getErr         error
	updates        []mockUpdate
	updateErr      error
	// byStatus simulates checkpoints stored by status for recovery tests.
	byStatus             map[compaction.Status][]compaction.Checkpoint
	sumTokenizerRevision string
}

type mockUpdate struct {
	id           string
	status       compaction.Status
	summaryJSON  string
	humanSummary string
	failureCode  *string
}

func (m *mockExecutorStore) GetCheckpoint(_ context.Context, id string) (*compaction.Checkpoint, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	if m.checkpoint != nil && m.checkpoint.ID == id {
		return m.checkpoint, nil
	}
	if m.prevCheckpoint != nil && m.prevCheckpoint.ID == id {
		return m.prevCheckpoint, nil
	}
	// Also check byStatus for recovery tests.
	for _, cps := range m.byStatus {
		for i := range cps {
			if cps[i].ID == id {
				return &cps[i], nil
			}
		}
	}
	return nil, nil
}
func (m *mockExecutorStore) UpdateCheckpointStatus(_ context.Context, id string, expectedStatus, status compaction.Status, summaryJSON, humanSummary string, failureCode *string) error {
	if m.updateErr != nil {
		return m.updateErr
	}
	// CAS check: verify the checkpoint's current status matches expectedStatus.
	// Check the single checkpoint first.
	if m.checkpoint != nil && m.checkpoint.ID == id {
		if m.checkpoint.Status != expectedStatus {
			return ErrConcurrentModification
		}
		m.updates = append(m.updates, mockUpdate{id, status, summaryJSON, humanSummary, failureCode})
		m.checkpoint.Status = status
		return nil
	}
	if m.prevCheckpoint != nil && m.prevCheckpoint.ID == id {
		if m.prevCheckpoint.Status != expectedStatus {
			return ErrConcurrentModification
		}
		m.updates = append(m.updates, mockUpdate{id, status, summaryJSON, humanSummary, failureCode})
		m.prevCheckpoint.Status = status
		return nil
	}
	// Check byStatus for recovery tests.
	for oldStatus, cps := range m.byStatus {
		for i := range cps {
			if cps[i].ID == id {
				if cps[i].Status != expectedStatus {
					return ErrConcurrentModification
				}
				m.updates = append(m.updates, mockUpdate{id, status, summaryJSON, humanSummary, failureCode})
				cps[i].Status = status
				// Move to new status bucket.
				m.byStatus[status] = append(m.byStatus[status], cps[i])
				m.byStatus[oldStatus] = append(cps[:i], cps[i+1:]...)
				return nil
			}
		}
	}
	return nil
}

func (m *mockExecutorStore) ActivateCheckpoint(_ context.Context, id string, baseVersion int64) error {
	if m.checkpoint == nil || m.checkpoint.ID != id || m.checkpoint.Version != baseVersion {
		return ErrVersionConflict
	}
	_, _ = m.MarkPreviousSucceededAsSuperseded(context.Background(), m.checkpoint.SessionID, id)
	return nil
}

// ListCheckpointsByStatus returns checkpoints matching the given status.
func (m *mockExecutorStore) ListCheckpointsByStatus(_ context.Context, status compaction.Status, limit int) ([]compaction.Checkpoint, error) {
	if limit <= 0 || limit > 1000 {
		limit = 1000
	}
	cps := m.byStatus[status]
	if len(cps) > limit {
		cps = cps[:limit]
	}
	result := make([]compaction.Checkpoint, len(cps))
	copy(result, cps)
	return result, nil
}

// MarkPreviousSucceededAsSuperseded marks all succeeded checkpoints (except the
// current one) as superseded. In the mock, this updates the checkpoint's status
// in-place.
func (m *mockExecutorStore) MarkPreviousSucceededAsSuperseded(_ context.Context, sessionID, currentCheckpointID string) (int64, error) {
	var count int64
	if m.checkpoint != nil && m.checkpoint.SessionID == sessionID && m.checkpoint.ID != currentCheckpointID && m.checkpoint.Status == compaction.StatusSucceeded {
		m.checkpoint.Status = compaction.StatusSuperseded
		count++
	}
	if m.prevCheckpoint != nil && m.prevCheckpoint.SessionID == sessionID && m.prevCheckpoint.ID != currentCheckpointID && m.prevCheckpoint.Status == compaction.StatusSucceeded {
		m.prevCheckpoint.Status = compaction.StatusSuperseded
		count++
	}
	for _, cps := range m.byStatus {
		for i := range cps {
			if cps[i].SessionID == sessionID && cps[i].ID != currentCheckpointID && cps[i].Status == compaction.StatusSucceeded {
				cps[i].Status = compaction.StatusSuperseded
				count++
			}
		}
	}
	return count, nil
}

// GetLatestSucceededCheckpoint returns the latest succeeded checkpoint for the session.
func (m *mockExecutorStore) GetLatestSucceededCheckpoint(_ context.Context, sessionID string) (*compaction.Checkpoint, error) {
	var latest *compaction.Checkpoint
	if m.checkpoint != nil && m.checkpoint.SessionID == sessionID && m.checkpoint.Status == compaction.StatusSucceeded {
		latest = m.checkpoint
	}
	if m.prevCheckpoint != nil && m.prevCheckpoint.SessionID == sessionID && m.prevCheckpoint.Status == compaction.StatusSucceeded {
		if latest == nil || m.prevCheckpoint.Version > latest.Version {
			latest = m.prevCheckpoint
		}
	}
	return latest, nil
}

// SumTokenLedgerAfterSeq returns a mock value for remaining tokens after a sequence.
func (m *mockExecutorStore) SumTokenLedgerAfterSeq(_ context.Context, _, _, _, tokenizerRevision string, _ int64) (int64, error) {
	m.sumTokenizerRevision = tokenizerRevision
	return 0, nil
}

type mockSourceReader struct {
	messages []SummaryMessage
	err      error
}

func (m *mockSourceReader) ListMessagesByRange(_ context.Context, _ string, start, end int64) ([]SummaryMessage, error) {
	if m.err != nil {
		return nil, m.err
	}
	var result []SummaryMessage
	for _, message := range m.messages {
		if message.Sequence >= start && message.Sequence <= end {
			result = append(result, message)
		}
	}
	return result, nil
}

type mockSummarizer struct {
	summaryJSON  string
	humanSummary string
	err          error
	called       bool
	providerID   string
	modelID      string
	priorSummary string
	calls        []summarizerCall
	responses    []string
}

type summarizerCall struct {
	start, end   int64
	priorSummary string
}

func (m *mockSummarizer) Summarize(_ context.Context, _, providerID, modelID string, start, end int64, _ []SummaryMessage, priorSummary string) (string, string, error) {
	m.called = true
	m.providerID = providerID
	m.modelID = modelID
	m.priorSummary = priorSummary
	m.calls = append(m.calls, summarizerCall{start: start, end: end, priorSummary: priorSummary})
	if len(m.responses) >= len(m.calls) {
		return m.responses[len(m.calls)-1], m.humanSummary, m.err
	}
	return m.summaryJSON, m.humanSummary, m.err
}

func makePendingCheckpoint() *compaction.Checkpoint {
	return &compaction.Checkpoint{
		ID:                   "01ARZ3NDEKTSV4RRFFQ69G5FA0",
		SessionID:            "01ARZ3NDEKTSV4RRFFQ69G5FA1",
		Version:              1,
		SourceStartID:        "01ARZ3NDEKTSV4RRFFQ69G5FA2",
		SourceEndID:          "01ARZ3NDEKTSV4RRFFQ69G5FA3",
		SourceStartSeq:       1,
		SourceEndSeq:         1,
		SourceDigest:         "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789",
		Status:               compaction.StatusPending,
		SummaryJSON:          "{}",
		SummarySchemaVersion: "1.0",
		Provider:             "test",
		Model:                "test",
	}
}

func TestExecute_Success(t *testing.T) {
	cp := makePendingCheckpoint()
	cp.SourceEndSeq = 2
	store := &mockExecutorStore{checkpoint: cp}
	reader := &mockSourceReader{messages: []SummaryMessage{
		{ID: "m1", Role: "user", Content: "hello", Sequence: 1},
		{ID: "m2", Role: "assistant", Content: "hi there", Sequence: 2},
	}}
	summarizer := &mockSummarizer{summaryJSON: `{"topics":["greeting"]}`, humanSummary: "用户打招呼"}

	exec := NewExecutor(store, reader, summarizer)
	result, err := exec.Execute(context.Background(), "01ARZ3NDEKTSV4RRFFQ69G5FA0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != compaction.StatusSucceeded {
		t.Fatalf("expected succeeded, got %s", result.Status)
	}
	if result.SummaryJSON != `{"topics":["greeting"]}` {
		t.Fatalf("unexpected summaryJSON: %s", result.SummaryJSON)
	}
	if !summarizer.called {
		t.Fatal("summarizer not called")
	}
	// Verify transitions: pending→running→succeeded
	if len(store.updates) != 2 {
		t.Fatalf("expected 2 updates, got %d", len(store.updates))
	}
	if store.updates[0].status != compaction.StatusRunning {
		t.Fatalf("expected first update to running, got %s", store.updates[0].status)
	}
	if store.updates[1].status != compaction.StatusSucceeded {
		t.Fatalf("expected second update to succeeded, got %s", store.updates[1].status)
	}
}

func TestExecuteAutomaticCheckpointPassesProviderIDToSummarizer(t *testing.T) {
	cp := makePendingCheckpoint()
	cp.Trigger = compaction.TriggerAutomatic
	cp.Provider = "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	store := &mockExecutorStore{checkpoint: cp}
	summarizer := &mockSummarizer{summaryJSON: `{}`, humanSummary: "summary"}
	exec := NewExecutor(store, &mockSourceReader{messages: []SummaryMessage{{ID: "m1", Role: "user", Content: "hello", Sequence: 1}}}, summarizer)

	if _, err := exec.Execute(context.Background(), cp.ID); err != nil {
		t.Fatal(err)
	}
	if summarizer.providerID != cp.Provider {
		t.Fatalf("automatic summarizer provider = %q, want real provider ID %q", summarizer.providerID, cp.Provider)
	}
}

func TestExecuteManualCheckpointPassesProviderIDToSummarizer(t *testing.T) {
	cp := makePendingCheckpoint()
	cp.Trigger = compaction.TriggerManual
	cp.Provider = "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	store := &mockExecutorStore{checkpoint: cp}
	summarizer := &mockSummarizer{summaryJSON: `{}`, humanSummary: "summary"}
	exec := NewExecutor(store, &mockSourceReader{messages: []SummaryMessage{{ID: "m1", Role: "user", Content: "hello", Sequence: 1}}}, summarizer)

	if _, err := exec.Execute(context.Background(), cp.ID); err != nil {
		t.Fatal(err)
	}
	if summarizer.providerID != cp.Provider {
		t.Fatalf("manual summarizer provider = %q, want real provider ID %q", summarizer.providerID, cp.Provider)
	}
}

func TestExecute_NotPending(t *testing.T) {
	cp := makePendingCheckpoint()
	cp.Status = compaction.StatusSucceeded
	store := &mockExecutorStore{checkpoint: cp}
	exec := NewExecutor(store, &mockSourceReader{}, &mockSummarizer{})
	_, err := exec.Execute(context.Background(), "01ARZ3NDEKTSV4RRFFQ69G5FA0")
	if !errors.Is(err, ErrCheckpointNotPending) {
		t.Fatalf("expected ErrCheckpointNotPending, got %v", err)
	}
}

func TestExecute_NotFound(t *testing.T) {
	store := &mockExecutorStore{checkpoint: nil}
	exec := NewExecutor(store, &mockSourceReader{}, &mockSummarizer{})
	_, err := exec.Execute(context.Background(), "nonexistent")
	if !errors.Is(err, ErrCheckpointNotFound) {
		t.Fatalf("expected ErrCheckpointNotFound, got %v", err)
	}
}

func TestExecute_EmptySourceRange(t *testing.T) {
	store := &mockExecutorStore{checkpoint: makePendingCheckpoint()}
	reader := &mockSourceReader{messages: []SummaryMessage{}}
	summarizer := &mockSummarizer{}
	exec := NewExecutor(store, reader, summarizer)
	result, _ := exec.Execute(context.Background(), "01ARZ3NDEKTSV4RRFFQ69G5FA0")
	if result.Status != compaction.StatusFailed {
		t.Fatalf("expected failed, got %s", result.Status)
	}
	if summarizer.called {
		t.Fatal("summarizer should not be called for empty range")
	}
	if len(store.updates) != 2 {
		t.Fatalf("expected 2 updates (running+failed), got %d", len(store.updates))
	}
	if store.updates[1].status != compaction.StatusFailed {
		t.Fatalf("expected second update to failed")
	}
	if store.updates[1].failureCode == nil || *store.updates[1].failureCode != "EMPTY_SOURCE_RANGE" {
		t.Fatalf("expected failure code EMPTY_SOURCE_RANGE")
	}
}

func TestExecute_LargeSourceRangeUsesRollingBatches(t *testing.T) {
	cp := makePendingCheckpoint()
	cp.SourceEndSeq = 3
	store := &mockExecutorStore{checkpoint: cp}
	reader := &mockSourceReader{messages: []SummaryMessage{
		{ID: "m1", Sequence: 1},
		{ID: "m2", Sequence: 2},
		{ID: "m3", Sequence: 3},
	}}
	summarizer := &mockSummarizer{summaryJSON: `{"summary":"batch"}`, humanSummary: "batch"}
	exec := NewExecutor(store, reader, summarizer)
	exec.SetMaxMessages(2)

	result, err := exec.Execute(context.Background(), store.checkpoint.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != compaction.StatusSucceeded {
		t.Fatalf("expected succeeded, got %s", result.Status)
	}
	if len(summarizer.calls) != 2 {
		t.Fatalf("expected 2 bounded summarizer calls, got %#v", summarizer.calls)
	}
	if summarizer.calls[0].start != 1 || summarizer.calls[0].end != 2 || summarizer.calls[1].start != 3 || summarizer.calls[1].end != 3 {
		t.Fatalf("unexpected batch ranges: %#v", summarizer.calls)
	}
	if summarizer.calls[1].priorSummary != `{"summary":"batch"}` {
		t.Fatalf("second batch did not receive rolling summary: %#v", summarizer.calls[1])
	}
}

func TestExecuteRollingBatchRejectsLossOfEarlierProtectedFact(t *testing.T) {
	cp := makePendingCheckpoint()
	cp.SourceEndSeq = 2
	store := &mockExecutorStore{checkpoint: cp}
	reader := &mockSourceReader{messages: []SummaryMessage{
		{ID: "m1", Role: "user", Content: "keep `criticalSymbol`", Sequence: 1},
		{ID: "m2", Role: "assistant", Content: "continue", Sequence: 2},
	}}
	summarizer := &mockSummarizer{responses: []string{
		`{"summary":"criticalSymbol"}`,
		`{"summary":"dropped prior fact"}`,
	}}
	exec := NewExecutor(store, reader, summarizer)
	exec.SetMaxMessages(1)

	result, err := exec.Execute(context.Background(), cp.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != compaction.StatusFailed || len(store.updates) != 2 || store.updates[1].failureCode == nil || *store.updates[1].failureCode != "PROTECTED_FACTS_VIOLATION" {
		t.Fatalf("earlier protected fact loss was accepted: result=%#v updates=%#v", result, store.updates)
	}
}

func TestExecute_SummarizerFails(t *testing.T) {
	store := &mockExecutorStore{checkpoint: makePendingCheckpoint()}
	reader := &mockSourceReader{messages: []SummaryMessage{{ID: "m1", Role: "user", Content: "hi", Sequence: 1}}}
	summarizer := &mockSummarizer{err: errors.New("LLM unavailable")}
	exec := NewExecutor(store, reader, summarizer)
	result, _ := exec.Execute(context.Background(), "01ARZ3NDEKTSV4RRFFQ69G5FA0")
	if result.Status != compaction.StatusFailed {
		t.Fatalf("expected failed, got %s", result.Status)
	}
	if store.updates[1].failureCode == nil || *store.updates[1].failureCode != "SUMMARY_FAILED" {
		t.Fatalf("expected failure code SUMMARY_FAILED")
	}
}

func TestExecute_SourceReadFails(t *testing.T) {
	store := &mockExecutorStore{checkpoint: makePendingCheckpoint()}
	reader := &mockSourceReader{err: errors.New("disk error")}
	exec := NewExecutor(store, reader, &mockSummarizer{})
	result, _ := exec.Execute(context.Background(), "01ARZ3NDEKTSV4RRFFQ69G5FA0")
	if result.Status != compaction.StatusFailed {
		t.Fatalf("expected failed, got %s", result.Status)
	}
	if store.updates[1].failureCode == nil || *store.updates[1].failureCode != "SOURCE_READ_FAILED" {
		t.Fatalf("expected failure code SOURCE_READ_FAILED")
	}
}

func TestExecute_RollingSummary_PassesPriorSummary(t *testing.T) {
	// Setup: current pending checkpoint links to a previous succeeded checkpoint.
	cp := makePendingCheckpoint()
	cp.Version = 2
	cp.SourceStartSeq = 11
	cp.SourceEndSeq = 11
	prevID := "01ARZ3NDEKTSV4RRFFQ69G5FA9"
	cp.PrevCheckpointID = &prevID

	prevCp := &compaction.Checkpoint{
		ID:          prevID,
		SessionID:   cp.SessionID,
		Version:     1,
		Status:      compaction.StatusSucceeded,
		SummaryJSON: `{"summary":"prior context","keyPoints":["old decision"]}`,
	}

	store := &mockExecutorStore{
		checkpoint:     cp,
		prevCheckpoint: prevCp,
	}
	reader := &mockSourceReader{messages: []SummaryMessage{
		{ID: "m1", Role: "user", Content: "new topic", Sequence: 11},
	}}
	summarizer := &mockSummarizer{
		summaryJSON:  `{"summary":"updated"}`,
		humanSummary: "updated summary",
	}

	exec := NewExecutor(store, reader, summarizer)
	result, err := exec.Execute(context.Background(), cp.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != compaction.StatusSucceeded {
		t.Fatalf("expected succeeded, got %s", result.Status)
	}
	if !summarizer.called {
		t.Fatal("summarizer not called")
	}
	if summarizer.priorSummary != prevCp.SummaryJSON {
		t.Fatalf("expected priorSummary %q, got %q", prevCp.SummaryJSON, summarizer.priorSummary)
	}
}

func TestExecute_RollingSummary_NoPrevCheckpoint(t *testing.T) {
	// First compaction: no PrevCheckpointID, priorSummary should be empty.
	store := &mockExecutorStore{checkpoint: makePendingCheckpoint()}
	reader := &mockSourceReader{messages: []SummaryMessage{
		{ID: "m1", Role: "user", Content: "hello", Sequence: 1},
	}}
	summarizer := &mockSummarizer{summaryJSON: `{"summary":"first"}`, humanSummary: "首次"}

	exec := NewExecutor(store, reader, summarizer)
	_, err := exec.Execute(context.Background(), "01ARZ3NDEKTSV4RRFFQ69G5FA0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if summarizer.priorSummary != "" {
		t.Fatalf("expected empty priorSummary for first compaction, got %q", summarizer.priorSummary)
	}
}

// TestRecoverOrphanedCheckpoints_RunningMarkedFailed verifies that running
// checkpoints left by a crashed process are marked failed with code
// INTERRUPTED_BY_RESTART (ADR-005 §5).
func TestRecoverOrphanedCheckpoints_RunningMarkedFailed(t *testing.T) {
	runningCP := compaction.Checkpoint{
		ID:        "cp-running-1",
		SessionID: "s1",
		Version:   1,
		Status:    compaction.StatusRunning,
	}
	store := &mockExecutorStore{
		byStatus: map[compaction.Status][]compaction.Checkpoint{
			compaction.StatusRunning: {runningCP},
		},
	}
	exec := NewExecutor(store, &mockSourceReader{}, &mockSummarizer{})

	results, err := exec.RecoverOrphanedCheckpoints(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 recovery result, got %d", len(results))
	}
	r := results[0]
	if r.CheckpointID != "cp-running-1" {
		t.Fatalf("expected checkpoint cp-running-1, got %s", r.CheckpointID)
	}
	if r.Action != "marked_failed" {
		t.Fatalf("expected action marked_failed, got %s", r.Action)
	}
	if r.Status != compaction.StatusFailed {
		t.Fatalf("expected status failed, got %s", r.Status)
	}
	// Verify the failure code was set.
	var failureCode *string
	for _, u := range store.updates {
		if u.id == "cp-running-1" && u.failureCode != nil {
			failureCode = u.failureCode
		}
	}
	if failureCode == nil || *failureCode != "INTERRUPTED_BY_RESTART" {
		t.Fatalf("expected failure code INTERRUPTED_BY_RESTART, got %v", failureCode)
	}
}

// TestRecoverOrphanedCheckpoints_PendingReexecuted verifies that pending
// checkpoints are re-executed synchronously during recovery (ADR-005 §5).
func TestRecoverOrphanedCheckpoints_PendingReexecuted(t *testing.T) {
	pendingCP := compaction.Checkpoint{
		ID:             "cp-pending-1",
		SessionID:      "s1",
		Version:        1,
		SourceStartSeq: 1,
		SourceEndSeq:   2,
		Status:         compaction.StatusPending,
	}
	store := &mockExecutorStore{
		byStatus: map[compaction.Status][]compaction.Checkpoint{
			compaction.StatusPending: {pendingCP},
		},
	}
	reader := &mockSourceReader{messages: []SummaryMessage{
		{ID: "m1", Role: "user", Content: "hello", Sequence: 1},
		{ID: "m2", Role: "assistant", Content: "hi", Sequence: 2},
	}}
	summarizer := &mockSummarizer{summaryJSON: `{"summary":"recovered"}`, humanSummary: "恢复"}
	exec := NewExecutor(store, reader, summarizer)

	results, err := exec.RecoverOrphanedCheckpoints(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 recovery result, got %d", len(results))
	}
	r := results[0]
	if r.Action != "reexecuted" {
		t.Fatalf("expected action reexecuted, got %s", r.Action)
	}
	if r.Status != compaction.StatusSucceeded {
		t.Fatalf("expected status succeeded, got %s", r.Status)
	}
}

// TestRecoverOrphanedCheckpoints_NoOrphans verifies that recovery is a no-op
// when there are no orphaned checkpoints.
func TestRecoverOrphanedCheckpoints_NoOrphans(t *testing.T) {
	store := &mockExecutorStore{
		byStatus: map[compaction.Status][]compaction.Checkpoint{},
	}
	exec := NewExecutor(store, &mockSourceReader{}, &mockSummarizer{})

	results, err := exec.RecoverOrphanedCheckpoints(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected 0 results for no orphans, got %d", len(results))
	}
}

func TestRecoverOrphanedCheckpointsProcessesMoreThanOneBatch(t *testing.T) {
	running := make([]compaction.Checkpoint, 1001)
	for i := range running {
		running[i] = compaction.Checkpoint{ID: fmt.Sprintf("cp-running-%04d", i), SessionID: "s", Version: 1, Status: compaction.StatusRunning}
	}
	store := &mockExecutorStore{byStatus: map[compaction.Status][]compaction.Checkpoint{compaction.StatusRunning: running}}
	exec := NewExecutor(store, &mockSourceReader{}, &mockSummarizer{})

	results, err := exec.RecoverOrphanedCheckpoints(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1001 || len(store.byStatus[compaction.StatusRunning]) != 0 {
		t.Fatalf("recovered=%d remaining=%d", len(results), len(store.byStatus[compaction.StatusRunning]))
	}
}

// TestRecoverOrphanedCheckpoints_Mixed verifies recovery handles both running
// and pending orphans in a single pass.
func TestRecoverOrphanedCheckpoints_Mixed(t *testing.T) {
	runningCP := compaction.Checkpoint{
		ID:        "cp-running",
		SessionID: "s1",
		Version:   1,
		Status:    compaction.StatusRunning,
	}
	pendingCP := compaction.Checkpoint{
		ID:             "cp-pending",
		SessionID:      "s2",
		Version:        1,
		SourceStartSeq: 1,
		SourceEndSeq:   1,
		Status:         compaction.StatusPending,
	}
	store := &mockExecutorStore{
		byStatus: map[compaction.Status][]compaction.Checkpoint{
			compaction.StatusRunning: {runningCP},
			compaction.StatusPending: {pendingCP},
		},
	}
	reader := &mockSourceReader{messages: []SummaryMessage{
		{ID: "m1", Role: "user", Content: "msg", Sequence: 1},
	}}
	summarizer := &mockSummarizer{summaryJSON: `{"summary":"ok"}`, humanSummary: "ok"}
	exec := NewExecutor(store, reader, summarizer)

	results, err := exec.RecoverOrphanedCheckpoints(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 recovery results, got %d", len(results))
	}
	// First should be the running one (marked failed), second the pending (reexecuted).
	if results[0].Action != "marked_failed" {
		t.Fatalf("expected first result marked_failed, got %s", results[0].Action)
	}
	if results[1].Action != "reexecuted" {
		t.Fatalf("expected second result reexecuted, got %s", results[1].Action)
	}
}

// GetCheckpoint and UpdateCheckpointStatus extend fakeCheckpointStore (defined
// in trigger_test.go) to also implement ExecutorStore, enabling Preview tests
// that use a single store for both trigger and executor.

func (f *fakeCheckpointStore) GetCheckpoint(_ context.Context, id string) (*compaction.Checkpoint, error) {
	return f.checkpoints[id], nil
}

func (f *fakeCheckpointStore) UpdateCheckpointStatus(_ context.Context, id string, expectedStatus, status compaction.Status, summaryJSON, humanSummary string, failureCode *string) error {
	cp, ok := f.checkpoints[id]
	if !ok {
		return ErrCheckpointNotFound
	}
	if cp.Status != expectedStatus {
		return ErrConcurrentModification
	}
	cp.Status = status
	cp.SummaryJSON = summaryJSON
	cp.HumanSummary = humanSummary
	cp.FailureCode = failureCode
	return nil
}

func (f *fakeCheckpointStore) ActivateCheckpoint(_ context.Context, id string, baseVersion int64) error {
	cp := f.checkpoints[id]
	if cp == nil || cp.Version != baseVersion {
		return ErrVersionConflict
	}
	return nil
}

// MarkPreviousSucceededAsSuperseded marks all succeeded checkpoints for the
// session (except currentCheckpointID) as superseded.
func (f *fakeCheckpointStore) MarkPreviousSucceededAsSuperseded(_ context.Context, sessionID, currentCheckpointID string) (int64, error) {
	var count int64
	for _, cp := range f.bySession[sessionID] {
		if cp.ID != currentCheckpointID && cp.Status == compaction.StatusSucceeded {
			cp.Status = compaction.StatusSuperseded
			count++
		}
	}
	return count, nil
}

// GetLatestSucceededCheckpoint returns the latest succeeded checkpoint for the session.
func (f *fakeCheckpointStore) GetLatestSucceededCheckpoint(_ context.Context, sessionID string) (*compaction.Checkpoint, error) {
	var latest *compaction.Checkpoint
	for _, cp := range f.bySession[sessionID] {
		if cp.Status == compaction.StatusSucceeded {
			if latest == nil || cp.Version > latest.Version {
				latest = cp
			}
		}
	}
	return latest, nil
}

// SumTokenLedgerAfterSeq returns a mock value for remaining tokens after a sequence.
func (f *fakeCheckpointStore) SumTokenLedgerAfterSeq(_ context.Context, _, _, _, _ string, _ int64) (int64, error) {
	return 0, nil
}

// TestPreviewCreatesCheckpointAndReturnsSummary verifies that Preview creates
// a checkpoint, executes it to succeeded, and returns the summary preview
// (ADR-005 §4.2).
func TestPreviewCreatesCheckpointAndReturnsSummary(t *testing.T) {
	tokenRepo := newFakeTokenRepo()
	tokenRepo.usageBySession["s1"] = 100000 // above 80% of 100000 context window
	messages := makeMessages(50)
	for i := range messages {
		tokenRepo.entries[messages[i].ID] = &token.LedgerEntry{
			MessageID:  messages[i].ID,
			TokenCount: 2000,
		}
	}
	checkpointStore := newFakeCheckpointStore()
	msgReader := &fakeMessageReader{messages: messages}

	trigger := NewTrigger(DefaultWatermarkConfig(), tokenRepo, checkpointStore, msgReader)

	summaryMessages := make([]SummaryMessage, 20)
	for i := range summaryMessages {
		summaryMessages[i] = SummaryMessage{ID: fmt.Sprintf("msg-%d", i+1), Role: "user", Content: fmt.Sprintf("message %d", i+1), Sequence: int64(i + 1)}
	}
	sourceReader := &mockSourceReader{messages: summaryMessages}
	summarizer := &mockSummarizer{
		summaryJSON:  `{"topics":["greeting"]}`,
		humanSummary: "用户打招呼",
	}

	exec := NewExecutor(checkpointStore, sourceReader, summarizer)
	exec.SetTrigger(trigger)

	result, err := exec.Preview(context.Background(), "s1", "p1", "m1", "v1", 100000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.CheckpointID == "" {
		t.Fatal("expected non-empty checkpoint ID")
	}
	if result.Version < 1 {
		t.Fatalf("expected version >= 1, got %d", result.Version)
	}
	if result.SourceStartSeq < 1 || result.SourceEndSeq < result.SourceStartSeq {
		t.Fatalf("invalid source range: %d-%d", result.SourceStartSeq, result.SourceEndSeq)
	}
	if result.SourceDigest == "" {
		t.Fatal("expected non-empty source digest")
	}
	if result.SummaryPreview == "" {
		t.Fatal("expected non-empty summary preview")
	}
	if result.HumanSummary == "" {
		t.Fatal("expected non-empty human summary")
	}
	if result.Status != string(compaction.StatusSucceeded) {
		t.Fatalf("expected succeeded status, got %s", result.Status)
	}
}

// TestCommitWithCorrectBaseVersionActivates verifies that Commit succeeds when
// baseVersion matches the checkpoint's version (ADR-005 §4.2).
func TestCommitWithCorrectBaseVersionActivates(t *testing.T) {
	cp := makePendingCheckpoint()
	cp.Status = compaction.StatusSucceeded
	cp.Version = 3
	store := &mockExecutorStore{checkpoint: cp}
	exec := NewExecutor(store, &mockSourceReader{}, &mockSummarizer{})

	result, err := exec.Commit(context.Background(), cp.ID, 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Activated {
		t.Fatal("expected activated=true")
	}
	if result.Version != 3 {
		t.Fatalf("expected version 3, got %d", result.Version)
	}
	if result.Status != string(compaction.StatusSucceeded) {
		t.Fatalf("expected succeeded, got %s", result.Status)
	}
}

// TestCommitWithWrongBaseVersionReturnsConflict verifies that Commit returns
// ErrVersionConflict when baseVersion does not match (ADR-005 §4.2 CAS).
func TestCommitWithWrongBaseVersionReturnsConflict(t *testing.T) {
	cp := makePendingCheckpoint()
	cp.Status = compaction.StatusSucceeded
	cp.Version = 3
	store := &mockExecutorStore{checkpoint: cp}
	exec := NewExecutor(store, &mockSourceReader{}, &mockSummarizer{})

	_, err := exec.Commit(context.Background(), cp.ID, 2)
	if !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("expected ErrVersionConflict, got %v", err)
	}
}

// TestCancelPendingCheckpoint verifies that Cancel transitions a pending
// checkpoint to failed with code CANCELLED (ADR-005 §4.2).
func TestCancelPendingCheckpoint(t *testing.T) {
	cp := makePendingCheckpoint()
	store := &mockExecutorStore{checkpoint: cp}
	exec := NewExecutor(store, &mockSourceReader{}, &mockSummarizer{})

	result, err := exec.Cancel(context.Background(), cp.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Cancelled {
		t.Fatal("expected cancelled=true")
	}
	if result.Status != string(compaction.StatusFailed) {
		t.Fatalf("expected failed status, got %s", result.Status)
	}
	// Verify CAS update was recorded with CANCELLED failure code.
	if len(store.updates) != 1 {
		t.Fatalf("expected 1 update, got %d", len(store.updates))
	}
	if store.updates[0].failureCode == nil || *store.updates[0].failureCode != "CANCELLED" {
		t.Fatalf("expected failure code CANCELLED, got %v", store.updates[0].failureCode)
	}
	if store.checkpoint.Status != compaction.StatusFailed {
		t.Fatalf("expected checkpoint status failed, got %s", store.checkpoint.Status)
	}
}

// TestCancelNonPendingReturnsError verifies that Cancel rejects checkpoints
// not in pending state (ADR-005 §4.2).
func TestCancelNonPendingReturnsError(t *testing.T) {
	cp := makePendingCheckpoint()
	cp.Status = compaction.StatusSucceeded
	store := &mockExecutorStore{checkpoint: cp}
	exec := NewExecutor(store, &mockSourceReader{}, &mockSummarizer{})

	_, err := exec.Cancel(context.Background(), cp.ID)
	if !errors.Is(err, ErrCheckpointNotPending) {
		t.Fatalf("expected ErrCheckpointNotPending, got %v", err)
	}
}

// TestExecuteCASConflict verifies that Execute returns ErrConcurrentModification
// when the CAS on pending→running fails (ADR-005 §4.2).
func TestExecuteCASConflict(t *testing.T) {
	store := &mockExecutorStore{
		checkpoint: makePendingCheckpoint(),
		updateErr:  ErrConcurrentModification,
	}
	exec := NewExecutor(store, &mockSourceReader{}, &mockSummarizer{})

	_, err := exec.Execute(context.Background(), "01ARZ3NDEKTSV4RRFFQ69G5FA0")
	if !errors.Is(err, ErrConcurrentModification) {
		t.Fatalf("expected ErrConcurrentModification, got %v", err)
	}
}

// TestCommitMarksPreviousSucceededAsSuperseded verifies that Commit marks all
// previous succeeded checkpoints as superseded (ADR-005 §4.2).
func TestCommitMarksPreviousSucceededAsSuperseded(t *testing.T) {
	cp := makePendingCheckpoint()
	cp.Status = compaction.StatusSucceeded
	cp.Version = 2
	cp.ID = "01ARZ3NDEKTSV4RRFFQ69G5FAA"

	// Previous succeeded checkpoint (should be superseded).
	prevCp := &compaction.Checkpoint{
		ID:                   "01ARZ3NDEKTSV4RRFFQ69G5FAB",
		SessionID:            cp.SessionID,
		Version:              1,
		SourceStartID:        "01ARZ3NDEKTSV4RRFFQ69G5FA2",
		SourceEndID:          "01ARZ3NDEKTSV4RRFFQ69G5FA3",
		SourceStartSeq:       1,
		SourceEndSeq:         5,
		SourceDigest:         "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789",
		Status:               compaction.StatusSucceeded,
		SummaryJSON:          "{}",
		SummarySchemaVersion: "1.0",
		Provider:             "test",
		Model:                "test",
	}

	store := &mockExecutorStore{
		checkpoint:     cp,
		prevCheckpoint: prevCp,
	}
	exec := NewExecutor(store, &mockSourceReader{}, &mockSummarizer{})

	result, err := exec.Commit(context.Background(), cp.ID, cp.Version)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Activated {
		t.Fatal("expected activated=true")
	}
	// Previous checkpoint should be superseded.
	if prevCp.Status != compaction.StatusSuperseded {
		t.Fatalf("expected previous checkpoint superseded, got %s", prevCp.Status)
	}
	// Current checkpoint should still be succeeded (not superseded).
	if cp.Status != compaction.StatusSucceeded {
		t.Fatalf("expected current checkpoint still succeeded, got %s", cp.Status)
	}
}

// TestRetryFailedCheckpoint verifies that Retry transitions a failed checkpoint
// back to pending and re-executes it (ADR-005 §5).
func TestRetryFailedCheckpoint(t *testing.T) {
	cp := makePendingCheckpoint()
	cp.Status = compaction.StatusFailed
	cp.SummaryJSON = "{}"
	failureCode := "SUMMARY_FAILED"
	cp.FailureCode = &failureCode

	store := &mockExecutorStore{checkpoint: cp}
	reader := &mockSourceReader{messages: []SummaryMessage{
		{ID: "m1", Role: "user", Content: "hello", Sequence: 1},
		{ID: "m2", Role: "assistant", Content: "hi", Sequence: 2},
	}}
	summarizer := &mockSummarizer{summaryJSON: `{"summary":"test"}`, humanSummary: "Test summary"}
	exec := NewExecutor(store, reader, summarizer)

	retryResult, execResult, err := exec.Retry(context.Background(), cp.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !retryResult.Retried {
		t.Fatal("expected retried=true")
	}
	if execResult.Status != compaction.StatusSucceeded {
		t.Fatalf("expected succeeded after retry, got %s", execResult.Status)
	}
	// Checkpoint should now be succeeded.
	if cp.Status != compaction.StatusSucceeded {
		t.Fatalf("expected checkpoint succeeded, got %s", cp.Status)
	}
}

// TestRetryNonFailedReturnsError verifies that Retry rejects checkpoints not
// in failed state.
func TestRetryNonFailedReturnsError(t *testing.T) {
	cp := makePendingCheckpoint()
	cp.Status = compaction.StatusSucceeded
	store := &mockExecutorStore{checkpoint: cp}
	exec := NewExecutor(store, &mockSourceReader{}, &mockSummarizer{})

	_, _, err := exec.Retry(context.Background(), cp.ID)
	if err == nil {
		t.Fatal("expected error for non-failed checkpoint")
	}
}

// TestVerifyLowWatermarkPasses verifies that low-watermark verification passes
// when remaining tokens are below 60% of context window.
func TestVerifyLowWatermarkPasses(t *testing.T) {
	cp := makePendingCheckpoint()
	cp.Status = compaction.StatusSucceeded
	cp.SummaryJSON = `{"summary":"short"}`
	cp.SourceEndSeq = 50

	store := &mockExecutorStore{checkpoint: cp}
	exec := NewExecutor(store, &mockSourceReader{}, &mockSummarizer{})

	// contextWindow=100000, lowWatermark=0.60 → threshold=60000
	// SumTokenLedgerAfterSeq returns 0 (mock) → totalReusable = 0 + ~6 = 6
	verified, fraction, err := exec.VerifyLowWatermark(context.Background(), cp.ID, 100000, 0.60)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !verified {
		t.Fatal("expected low-watermark verification to pass")
	}
	if fraction > 0.60 {
		t.Fatalf("expected fraction <= 0.60, got %f", fraction)
	}
	if store.sumTokenizerRevision != token.CanonicalTokenizerRevision {
		t.Fatalf("tokenizer revision = %q, want canonical %q", store.sumTokenizerRevision, token.CanonicalTokenizerRevision)
	}
}

// TestVerifyLowWatermarkZeroContextWindow verifies that verification returns
// false when contextWindow is 0 (unknown).
func TestVerifyLowWatermarkZeroContextWindow(t *testing.T) {
	cp := makePendingCheckpoint()
	cp.Status = compaction.StatusSucceeded
	store := &mockExecutorStore{checkpoint: cp}
	exec := NewExecutor(store, &mockSourceReader{}, &mockSummarizer{})

	verified, _, err := exec.VerifyLowWatermark(context.Background(), cp.ID, 0, 0.60)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if verified {
		t.Fatal("expected verified=false when contextWindow=0")
	}
}

// TestConcurrentCheckAndTriggerSameSession verifies that concurrent
// CheckAndTrigger calls on the same session are serialized by the per-session
// lock, preventing duplicate checkpoint creation (ADR-005 §5).
func TestConcurrentCheckAndTriggerSameSession(t *testing.T) {
	tokenRepo := newFakeTokenRepo()
	tokenRepo.usageBySession["s1"] = 100000 // above 80% of 100000
	messages := makeMessages(50)
	for i := range messages {
		tokenRepo.entries[messages[i].ID] = &token.LedgerEntry{
			MessageID:  messages[i].ID,
			TokenCount: 2000,
		}
	}
	checkpointStore := newFakeCheckpointStore()
	msgReader := &fakeMessageReader{messages: messages}
	trigger := NewTrigger(DefaultWatermarkConfig(), tokenRepo, checkpointStore, msgReader)

	// Launch 3 concurrent CheckAndTrigger calls.
	var wg sync.WaitGroup
	results := make([]TriggerResult, 3)
	errs := make([]error, 3)
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			results[idx], errs[idx] = trigger.CheckAndTrigger(context.Background(), "s1", "test", "test", "", 100000)
		}(i)
	}
	wg.Wait()

	// At most one should have triggered (created a checkpoint).
	triggeredCount := 0
	for i := range results {
		if errs[i] != nil {
			continue
		}
		if results[i].Triggered {
			triggeredCount++
		}
	}
	if triggeredCount > 1 {
		t.Fatalf("expected at most 1 triggered compaction, got %d", triggeredCount)
	}
}
