package contextapp

import (
	"context"
	"strings"
	"testing"
)

type mockReader struct {
	messages  []Message
	sumTokens int64
	err       error
}

func (m *mockReader) ListMessages(_ context.Context, _ string, _ string, _ int) ([]Message, error) {
	return m.messages, m.err
}

func (m *mockReader) SumTokens(_ context.Context, _, _, _, _ string) (int64, error) {
	return m.sumTokens, m.err
}

func TestEffectiveInputBudget(t *testing.T) {
	p := ProviderInfo{
		ContextWindow:  128000,
		SafetyCeiling:  100000,
		ReservedOutput: 4096,
		SystemTokens:   2048,
		SafetyMargin:   1024,
	}
	budget := p.EffectiveInputBudget()
	expected := int64(100000 - 4096 - 2048 - 1024) // 92832
	if budget != expected {
		t.Fatalf("budget = %d, want %d", budget, expected)
	}
}

func TestEffectiveInputBudgetDefaultsToContextWindow(t *testing.T) {
	p := ProviderInfo{
		ContextWindow:  128000,
		ReservedOutput: 4096,
	}
	budget := p.EffectiveInputBudget()
	expected := int64(128000 - 4096)
	if budget != expected {
		t.Fatalf("budget = %d, want %d", budget, expected)
	}
}

func TestAssembleReturnsLatestUserMessageWhenBudgetTight(t *testing.T) {
	reader := &mockReader{
		messages: []Message{
			{ID: "m3", Role: "user", Content: strings.Repeat("a", 100), Sequence: 3, TokenCount: 25},
			{ID: "m2", Role: "user", Content: strings.Repeat("b", 100), Sequence: 2, TokenCount: 25},
			{ID: "m1", Role: "user", Content: strings.Repeat("c", 100), Sequence: 1, TokenCount: 25},
		},
	}
	info := ProviderInfo{
		ContextWindow:  1000,
		ReservedOutput: 500,
		SystemTokens:   400,
	}
	// Budget = 100. RecentUserReserve defaults to max(512, 10) = 512.
	// But budget is 100, so even the latest user won't fit.
	// We still return it.
	msgs, err := Assemble(context.Background(), reader, "s1", info, AssembleOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 || msgs[0].ID != "m3" {
		t.Fatalf("expected only latest user message, got %d messages", len(msgs))
	}
}

func TestAssembleSelectsMessagesWithinBudget(t *testing.T) {
	reader := &mockReader{
		messages: []Message{
			{ID: "m5", Role: "user", Content: "latest", Sequence: 5, TokenCount: 10},
			{ID: "m4", Role: "user", Content: "fourth", Sequence: 4, TokenCount: 10},
			{ID: "m3", Role: "user", Content: "third", Sequence: 3, TokenCount: 10},
			{ID: "m2", Role: "user", Content: "second", Sequence: 2, TokenCount: 10},
			{ID: "m1", Role: "user", Content: "first", Sequence: 1, TokenCount: 10},
		},
	}
	info := ProviderInfo{
		ContextWindow:  1000,
		ReservedOutput: 100,
	}
	// Budget = 900. RecentUserReserve = max(512, 90) = 512.
	// Latest user = 10 tokens, remaining = 890.
	// Can fit m4(10), m3(10), m2(10), m1(10) = 40 more.
	msgs, err := Assemble(context.Background(), reader, "s1", info, AssembleOptions{RecentUserReserve: 512})
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) < 2 {
		t.Fatalf("expected at least 2 messages, got %d", len(msgs))
	}
	// Latest user should be first in output (chronological order).
	// Since we reverse, m1 should be first.
	if msgs[0].ID != "m1" {
		t.Fatalf("expected first message to be m1, got %s", msgs[0].ID)
	}
}

func TestAssembleEmptySessionReturnsError(t *testing.T) {
	reader := &mockReader{messages: nil}
	info := ProviderInfo{ContextWindow: 1000}
	_, err := Assemble(context.Background(), reader, "s1", info, AssembleOptions{})
	if err != ErrNoMessages {
		t.Fatalf("expected ErrNoMessages, got %v", err)
	}
}

func TestAssembleZeroBudgetReturnsError(t *testing.T) {
	reader := &mockReader{
		messages: []Message{{ID: "m1", Role: "user", Content: "test", Sequence: 1}},
	}
	info := ProviderInfo{
		ContextWindow:  100,
		ReservedOutput: 100, // Budget = 0
	}
	_, err := Assemble(context.Background(), reader, "s1", info, AssembleOptions{})
	if err != ErrBudgetTooSmall {
		t.Fatalf("expected ErrBudgetTooSmall, got %v", err)
	}
}

func TestReverseMessages(t *testing.T) {
	msgs := []Message{
		{ID: "m3", Sequence: 3},
		{ID: "m2", Sequence: 2},
		{ID: "m1", Sequence: 1},
	}
	reverseMessages(msgs)
	if msgs[0].ID != "m1" || msgs[1].ID != "m2" || msgs[2].ID != "m3" {
		t.Fatalf("reverse failed: %v", msgs)
	}
}