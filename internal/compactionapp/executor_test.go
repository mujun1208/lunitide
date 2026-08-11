package compactionapp

import (
	"context"
	"errors"
	"testing"

	"github.com/lunitide/lunitide/internal/domain/compaction"
)

type mockExecutorStore struct {
	checkpoint     *compaction.Checkpoint
	prevCheckpoint *compaction.Checkpoint
	getErr         error
	updates        []mockUpdate
	updateErr      error
	// byStatus simulates checkpoints stored by status for recovery tests.
	byStatus map[compaction.Status][]compaction.Checkpoint
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
func (m *mockExecutorStore) UpdateCheckpointStatus(_ context.Context, id string, status compaction.Status, summaryJSON, humanSummary string, failureCode *string) error {
	if m.updateErr != nil {
		return m.updateErr
	}
	m.updates = append(m.updates, mockUpdate{id, status, summaryJSON, humanSummary, failureCode})
	// Update status in byStatus if present.
	for oldStatus, cps := range m.byStatus {
		for i := range cps {
			if cps[i].ID == id {
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

type mockSourceReader struct {
	messages []SummaryMessage
	err      error
}

func (m *mockSourceReader) ListMessagesByRange(_ context.Context, _ string, _, _ int64) ([]SummaryMessage, error) {
	return m.messages, m.err
}

type mockSummarizer struct {
	summaryJSON  string
	humanSummary string
	err          error
	called       bool
	priorSummary string
}

func (m *mockSummarizer) Summarize(_ context.Context, _, _, _ string, _, _ int64, _ []SummaryMessage, priorSummary string) (string, string, error) {
	m.called = true
	m.priorSummary = priorSummary
	return m.summaryJSON, m.humanSummary, m.err
}

func makePendingCheckpoint() *compaction.Checkpoint {
	return &compaction.Checkpoint{
		ID:               "01ARZ3NDEKTSV4RRFFQ69G5FA0",
		SessionID:        "01ARZ3NDEKTSV4RRFFQ69G5FA1",
		Version:          1,
		SourceStartID:    "01ARZ3NDEKTSV4RRFFQ69G5FA2",
		SourceEndID:      "01ARZ3NDEKTSV4RRFFQ69G5FA3",
		SourceStartSeq:   1,
		SourceEndSeq:     10,
		SourceDigest:     "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789",
		Status:           compaction.StatusPending,
		SummaryJSON:      "{}",
		SummarySchemaVersion: "1.0",
		Provider:         "test",
		Model:            "test",
	}
}

func TestExecute_Success(t *testing.T) {
	store := &mockExecutorStore{checkpoint: makePendingCheckpoint()}
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
		ID:            "cp-pending-1",
		SessionID:     "s1",
		Version:       1,
		SourceStartSeq: 1,
		SourceEndSeq:   5,
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
		SourceEndSeq:   3,
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
