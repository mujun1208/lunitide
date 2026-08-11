package compactionapp

import (
	"context"
	"errors"
	"testing"

	"github.com/lunitide/lunitide/internal/domain/compaction"
)

func TestExtractProtectedFacts(t *testing.T) {
	messages := []SummaryMessage{
		{ID: "m1", Role: "user", Content: "Project ID is 01ARZ3NDEKTSV4RRFFQ69G5FAV and file is e:\\src\\main.go", Sequence: 1},
		{ID: "m2", Role: "assistant", Content: "Found `func main()` and value \"hello world\"", Sequence: 2},
	}
	facts := ExtractProtectedFacts(messages)
	if len(facts) == 0 {
		t.Fatal("expected non-empty facts")
	}
	// Verify ULID extraction.
	foundULID := false
	foundPath := false
	foundCode := false
	foundQuote := false
	for _, f := range facts {
		switch f.Kind {
		case "ulid":
			if f.Value == "01ARZ3NDEKTSV4RRFFQ69G5FAV" {
				foundULID = true
			}
		case "path":
			if f.Value == "e:\\src\\main.go" {
				foundPath = true
			}
		case "code":
			if f.Value == "func main()" {
				foundCode = true
			}
		case "quote":
			if f.Value == "hello world" {
				foundQuote = true
			}
		}
	}
	if !foundULID {
		t.Error("ULID not extracted")
	}
	if !foundPath {
		t.Error("path not extracted")
	}
	if !foundCode {
		t.Error("code not extracted")
	}
	if !foundQuote {
		t.Error("quote not extracted")
	}
}

func TestExtractProtectedFactsDeduplicates(t *testing.T) {
	messages := []SummaryMessage{
		{ID: "m1", Role: "user", Content: "ID 01ARZ3NDEKTSV4RRFFQ69G5FAV mentioned", Sequence: 1},
		{ID: "m2", Role: "assistant", Content: "ID 01ARZ3NDEKTSV4RRFFQ69G5FAV again", Sequence: 2},
	}
	facts := ExtractProtectedFacts(messages)
	count := 0
	for _, f := range facts {
		if f.Value == "01ARZ3NDEKTSV4RRFFQ69G5FAV" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected 1 deduplicated ULID, got %d", count)
	}
}

func TestExtractProtectedFactsEmptyMessages(t *testing.T) {
	facts := ExtractProtectedFacts(nil)
	if len(facts) != 0 {
		t.Fatalf("expected 0 facts for empty messages, got %d", len(facts))
	}
}

func TestValidateProtectedFactsAllPresent(t *testing.T) {
	facts := []ProtectedFact{
		{Value: "01ARZ3NDEKTSV4RRFFQ69G5FAV", Kind: "ulid"},
		{Value: "main.go", Kind: "path"},
	}
	summary := `{"identifiers":["01ARZ3NDEKTSV4RRFFQ69G5FAV"],"files":["main.go"]}`
	if err := ValidateProtectedFacts(summary, facts); err != nil {
		t.Fatalf("expected pass, got %v", err)
	}
}

func TestValidateProtectedFactsMissing(t *testing.T) {
	facts := []ProtectedFact{
		{Value: "01ARZ3NDEKTSV4RRFFQ69G5FAV", Kind: "ulid"},
		{Value: "01ARZ3NDEKTSV4RRFFQ69G5FAW", Kind: "ulid"},
	}
	summary := `{"identifiers":["01ARZ3NDEKTSV4RRFFQ69G5FAV"]}`
	err := ValidateProtectedFacts(summary, facts)
	if !errors.Is(err, ErrProtectedFactsViolation) {
		t.Fatalf("expected ErrProtectedFactsViolation, got %v", err)
	}
}

func TestValidateProtectedFactsNoFacts(t *testing.T) {
	summary := `{"topics":["greeting"]}`
	if err := ValidateProtectedFacts(summary, nil); err != nil {
		t.Fatalf("expected pass with no facts, got %v", err)
	}
}

func TestValidateProtectedFactsInvalidJSON(t *testing.T) {
	facts := []ProtectedFact{{Value: "abc", Kind: "quote"}}
	err := ValidateProtectedFacts("not-json", facts)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestExecute_ProtectedFactsViolation(t *testing.T) {
	store := &mockExecutorStore{checkpoint: makePendingCheckpoint()}
	reader := &mockSourceReader{messages: []SummaryMessage{
		{ID: "m1", Role: "user", Content: "The ID is 01ARZ3NDEKTSV4RRFFQ69G5FAV", Sequence: 1},
	}}
	// Summary omits the ULID → protected facts violation.
	summarizer := &mockSummarizer{summaryJSON: `{"topics":["greeting"]}`, humanSummary: "打招呼"}
	exec := NewExecutor(store, reader, summarizer)
	result, _ := exec.Execute(context.Background(), "01ARZ3NDEKTSV4RRFFQ69G5FA0")
	if result.Status != compaction.StatusFailed {
		t.Fatalf("expected failed, got %s", result.Status)
	}
	if len(store.updates) != 2 {
		t.Fatalf("expected 2 updates (running+failed), got %d", len(store.updates))
	}
	if store.updates[1].failureCode == nil || *store.updates[1].failureCode != "PROTECTED_FACTS_VIOLATION" {
		t.Fatalf("expected failure code PROTECTED_FACTS_VIOLATION, got %v", store.updates[1].failureCode)
	}
}

func TestExecute_ProtectedFactsPreservedSucceeds(t *testing.T) {
	store := &mockExecutorStore{checkpoint: makePendingCheckpoint()}
	reader := &mockSourceReader{messages: []SummaryMessage{
		{ID: "m1", Role: "user", Content: "The ID is 01ARZ3NDEKTSV4RRFFQ69G5FAV", Sequence: 1},
	}}
	// Summary includes the ULID → validation passes.
	summarizer := &mockSummarizer{
		summaryJSON:  `{"identifiers":["01ARZ3NDEKTSV4RRFFQ69G5FAV"],"topics":["project setup"]}`,
		humanSummary: "项目设置",
	}
	exec := NewExecutor(store, reader, summarizer)
	result, _ := exec.Execute(context.Background(), "01ARZ3NDEKTSV4RRFFQ69G5FA0")
	if result.Status != compaction.StatusSucceeded {
		t.Fatalf("expected succeeded, got %s", result.Status)
	}
}
