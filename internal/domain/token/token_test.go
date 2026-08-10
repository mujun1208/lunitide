package token

import (
	"strings"
	"testing"
	"time"
)

func TestEstimateTokens(t *testing.T) {
	tests := []struct {
		name string
		text string
		min  int64
		max  int64
	}{
		{"empty", "", 0, 0},
		{"single ascii", "a", 1, 1},
		{"four ascii", "abcd", 1, 1},
		{"five ascii", "abcde", 1, 5},
		{"english sentence", "Hello world, this is a test sentence.", 1, 100},
		{"chinese single", "中", 1, 3},
		{"chinese sentence", "这是一段中文测试文本。", 1, 50},
		{"mixed", "Hello 世界 test", 1, 50},
		{"emoji", "😀😀😀", 1, 10},
		{"long english", strings.Repeat("a", 2048), 1, 2048},
		{"long chinese", strings.Repeat("中", 2048), 1, 2048},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EstimateTokens(tt.text)
			if got < tt.min || got > tt.max {
				t.Errorf("EstimateTokens(%q) = %d, want between %d and %d", tt.text, got, tt.min, tt.max)
			}
		})
	}
}

func TestLedgerEntryValidate(t *testing.T) {
	valid := LedgerEntry{
		ID:                "01ARZ3NDEKTSV4RRFFQ69G5FAV",
		MessageID:         "01ARZ3NDEKTSV4RRFFQ69G5FAV",
		Provider:          "openai",
		Model:             "gpt-4",
		TokenizerRevision: "cl100k_base",
		TokenCount:        100,
		EstimationMethod:  CharRatio,
		UTF8Bytes:         400,
		ComputedAt:        time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid entry rejected: %v", err)
	}
	invalid := valid
	invalid.ID = "bad"
	if err := invalid.Validate(); err == nil {
		t.Fatal("invalid ID accepted")
	}
	invalid = valid
	invalid.MessageID = ""
	if err := invalid.Validate(); err == nil {
		t.Fatal("empty message ID accepted")
	}
	invalid = valid
	invalid.TokenCount = -1
	if err := invalid.Validate(); err == nil {
		t.Fatal("negative token count accepted")
	}
	invalid = valid
	invalid.EstimationMethod = "unknown"
	if err := invalid.Validate(); err == nil {
		t.Fatal("unknown estimation method accepted")
	}
	invalid = valid
	invalid.ComputedAt = time.Time{}
	if err := invalid.Validate(); err == nil {
		t.Fatal("zero computed_at accepted")
	}
}