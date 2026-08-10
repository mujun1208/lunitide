package handoff

import (
	"testing"
	"time"
)

func TestCapsuleValidate(t *testing.T) {
	valid := Capsule{
		ID:               "01ARZ3NDEKTSV4RRFFQ69G5FAV",
		SourceSessionID:  "01ARZ3NDEKTSV4RRFFQ69G5FAV",
		CheckpointID:     "01ARZ3NDEKTSV4RRFFQ69G5FAV",
		ActiveTasksJSON:  "[]",
		RecentMessageIDs: "[]",
		Digest:           "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789",
		Status:           StatusActive,
		CreatedAt:        time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid capsule rejected: %v", err)
	}

	invalid := valid
	invalid.ID = "bad"
	if err := invalid.Validate(); err == nil {
		t.Fatal("invalid ID accepted")
	}
	invalid = valid
	invalid.Digest = "short"
	if err := invalid.Validate(); err == nil {
		t.Fatal("short digest accepted")
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

func TestCapsuleStatusTransitions(t *testing.T) {
	c := Capsule{Status: StatusActive}
	if _, err := c.TransitionTo(StatusActivated); err != nil {
		t.Fatalf("active->activated: %v", err)
	}
	if _, err := c.TransitionTo(StatusExpired); err != nil {
		t.Fatalf("active->expired: %v", err)
	}
	if _, err := c.TransitionTo(StatusRevoked); err != nil {
		t.Fatalf("active->revoked: %v", err)
	}
	c.Status = StatusActivated
	if _, err := c.TransitionTo(StatusActive); err == nil {
		t.Fatal("activated->active should fail")
	}
	c.Status = StatusExpired
	if _, err := c.TransitionTo(StatusActive); err == nil {
		t.Fatal("expired->active should fail")
	}
	c.Status = StatusRevoked
	if _, err := c.TransitionTo(StatusActive); err == nil {
		t.Fatal("revoked->active should fail")
	}
}

func TestCapsuleActivatedRequiresActivatedAt(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	c := Capsule{
		ID:               "01ARZ3NDEKTSV4RRFFQ69G5FAV",
		SourceSessionID:  "01ARZ3NDEKTSV4RRFFQ69G5FAV",
		CheckpointID:     "01ARZ3NDEKTSV4RRFFQ69G5FAV",
		ActiveTasksJSON:  "[]",
		RecentMessageIDs: "[]",
		Digest:           "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789",
		Status:           StatusActivated,
		CreatedAt:        now,
		ActivatedAt:      &now,
	}
	if err := c.Validate(); err != nil {
		t.Fatalf("valid activated capsule rejected: %v", err)
	}
	c.Status = StatusActivated
	c.ActivatedAt = nil
	if err := c.Validate(); err == nil {
		t.Fatal("activated without activated_at accepted")
	}
	c.Status = StatusActive
	c.ActivatedAt = &now
	if err := c.Validate(); err == nil {
		t.Fatal("active with activated_at accepted")
	}
}