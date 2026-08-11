package compactionapp

import (
	"context"
	"errors"
	"testing"

	"github.com/lunitide/lunitide/internal/domain/compaction"
)

type mockExecutorStore struct {
	checkpoint *compaction.Checkpoint
	getErr     error
	updates    []mockUpdate
	updateErr  error
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
	if m.checkpoint == nil || m.checkpoint.ID != id {
		return nil, nil
	}
	return m.checkpoint, nil
}
func (m *mockExecutorStore) UpdateCheckpointStatus(_ context.Context, id string, status compaction.Status, summaryJSON, humanSummary string, failureCode *string) error {
	if m.updateErr != nil {
		return m.updateErr
	}
	m.updates = append(m.updates, mockUpdate{id, status, summaryJSON, humanSummary, failureCode})
	return nil
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
}

func (m *mockSummarizer) Summarize(_ context.Context, _ string, _, _ int64, _ []SummaryMessage) (string, string, error) {
	m.called = true
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
