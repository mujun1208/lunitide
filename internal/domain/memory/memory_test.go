package memory

import (
	"testing"
	"time"
)

func TestMemoryValidate(t *testing.T) {
	valid := Memory{
		ID:         "01ARZ3NDEKTSV4RRFFQ69G5FAV",
		ProjectID:  "01ARZ3NDEKTSV4RRFFQ69G5FAV",
		Layer:      LayerWorking,
		Scope:      ScopeSession,
		Key:        "test-key",
		Content:    "test content",
		Confidence: 0.9,
		CreatedAt:  time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt:  time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid memory rejected: %v", err)
	}

	invalid := valid
	invalid.ID = "bad"
	if err := invalid.Validate(); err == nil {
		t.Fatal("invalid ID accepted")
	}
	invalid = valid
	invalid.Layer = "unknown"
	if err := invalid.Validate(); err == nil {
		t.Fatal("unknown layer accepted")
	}
	invalid = valid
	invalid.Scope = "unknown"
	if err := invalid.Validate(); err == nil {
		t.Fatal("unknown scope accepted")
	}
	invalid = valid
	invalid.Confidence = 1.5
	if err := invalid.Validate(); err == nil {
		t.Fatal("confidence > 1.0 accepted")
	}
	invalid = valid
	invalid.Confidence = -0.1
	if err := invalid.Validate(); err == nil {
		t.Fatal("negative confidence accepted")
	}
	invalid = valid
	invalid.CreatedAt = time.Time{}
	if err := invalid.Validate(); err == nil {
		t.Fatal("zero created_at accepted")
	}
}

func TestMemoryIsExpired(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	m := Memory{}
	if m.IsExpired(now) {
		t.Fatal("memory without expiration should not be expired")
	}
	expires := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	m.ExpiresAt = &expires
	if !m.IsExpired(now) {
		t.Fatal("expired memory should be expired")
	}
	expires = time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	m.ExpiresAt = &expires
	if m.IsExpired(now) {
		t.Fatal("future expiration should not be expired")
	}
}

func TestConfidenceValidate(t *testing.T) {
	if err := Confidence(0.0).Validate(); err != nil {
		t.Fatal("confidence 0.0 rejected")
	}
	if err := Confidence(0.5).Validate(); err != nil {
		t.Fatal("confidence 0.5 rejected")
	}
	if err := Confidence(1.0).Validate(); err != nil {
		t.Fatal("confidence 1.0 rejected")
	}
	if err := Confidence(1.1).Validate(); err == nil {
		t.Fatal("confidence 1.1 accepted")
	}
	if err := Confidence(-0.1).Validate(); err == nil {
		t.Fatal("confidence -0.1 accepted")
	}
}