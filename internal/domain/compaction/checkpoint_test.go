package compaction

import (
	"testing"
	"time"
)

func TestCheckpointValidate(t *testing.T) {
	valid := Checkpoint{
		ID:                  "01ARZ3NDEKTSV4RRFFQ69G5FAV",
		SessionID:           "01ARZ3NDEKTSV4RRFFQ69G5FAV",
		Version:             1,
		SourceStartID:       "01ARZ3NDEKTSV4RRFFQ69G5FAV",
		SourceEndID:         "01ARZ3NDEKTSV4RRFFQ69G5FAV",
		SourceStartSeq:      1,
		SourceEndSeq:        10,
		SourceDigest:        "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789",
		SummarySchemaVersion: "1.0",
		Trigger:             TriggerManual,
		TriggerReason:       "test",
		Status:              StatusPending,
		Provider:            "openai",
		Model:               "gpt-4",
		SummaryJSON:         "{}",
		HumanSummary:        "",
		CreatedAt:           time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid checkpoint rejected: %v", err)
	}

	invalid := valid
	invalid.ID = "bad"
	if err := invalid.Validate(); err == nil {
		t.Fatal("invalid ID accepted")
	}
	invalid = valid
	invalid.Version = 0
	if err := invalid.Validate(); err == nil {
		t.Fatal("zero version accepted")
	}
	invalid = valid
	invalid.SourceStartSeq = 5
	invalid.SourceEndSeq = 3
	if err := invalid.Validate(); err == nil {
		t.Fatal("reversed source range accepted")
	}
	invalid = valid
	invalid.SourceDigest = "short"
	if err := invalid.Validate(); err == nil {
		t.Fatal("short digest accepted")
	}
	invalid = valid
	invalid.Trigger = "unknown"
	if err := invalid.Validate(); err == nil {
		t.Fatal("unknown trigger accepted")
	}
	invalid = valid
	invalid.Status = "unknown"
	if err := invalid.Validate(); err == nil {
		t.Fatal("unknown status accepted")
	}
	invalid = valid
	invalid.CreatedAt = time.Time{}
	if err := invalid.Validate(); err == nil {
		t.Fatal("zero created_at accepted")
	}
}

func TestCheckpointStatusTransitions(t *testing.T) {
	cp := Checkpoint{Status: StatusPending}
	if _, err := cp.TransitionTo(StatusRunning); err != nil {
		t.Fatalf("pending->running: %v", err)
	}
	if _, err := cp.TransitionTo(StatusSucceeded); err == nil {
		t.Fatal("pending->succeeded should fail")
	}
	cp.Status = StatusRunning
	if _, err := cp.TransitionTo(StatusSucceeded); err != nil {
		t.Fatalf("running->succeeded: %v", err)
	}
	if _, err := cp.TransitionTo(StatusFailed); err != nil {
		t.Fatalf("running->failed: %v", err)
	}
	cp.Status = StatusSucceeded
	if _, err := cp.TransitionTo(StatusSuperseded); err != nil {
		t.Fatalf("succeeded->superseded: %v", err)
	}
	if _, err := cp.TransitionTo(StatusRunning); err == nil {
		t.Fatal("succeeded->running should fail")
	}
	cp.Status = StatusFailed
	if _, err := cp.TransitionTo(StatusPending); err != nil {
		t.Fatalf("failed->pending: %v", err)
	}
	cp.Status = StatusSuperseded
	if _, err := cp.TransitionTo(StatusPending); err == nil {
		t.Fatal("superseded->pending should fail")
	}
}

func TestCheckpointTerminalRequiresCompletedAt(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	cp := Checkpoint{
		ID:                  "01ARZ3NDEKTSV4RRFFQ69G5FAV",
		SessionID:           "01ARZ3NDEKTSV4RRFFQ69G5FAV",
		Version:             1,
		SourceStartID:       "01ARZ3NDEKTSV4RRFFQ69G5FAV",
		SourceEndID:         "01ARZ3NDEKTSV4RRFFQ69G5FAV",
		SourceStartSeq:      1,
		SourceEndSeq:        10,
		SourceDigest:        "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789",
		SummarySchemaVersion: "1.0",
		Trigger:             TriggerManual,
		TriggerReason:       "test",
		Status:              StatusSucceeded,
		Provider:            "",
		Model:               "",
		SummaryJSON:         "{}",
		HumanSummary:        "",
		CreatedAt:           now,
		CompletedAt:         &now,
	}
	if err := cp.Validate(); err != nil {
		t.Fatalf("valid terminal checkpoint rejected: %v", err)
	}
	cp.Status = StatusPending
	cp.CompletedAt = nil
	if err := cp.Validate(); err != nil {
		t.Fatalf("valid pending checkpoint rejected: %v", err)
	}
	cp.Status = StatusSucceeded
	cp.CompletedAt = nil
	if err := cp.Validate(); err == nil {
		t.Fatal("terminal without completed_at accepted")
	}
	cp.Status = StatusPending
	cp.CompletedAt = &now
	if err := cp.Validate(); err == nil {
		t.Fatal("pending with completed_at accepted")
	}
}