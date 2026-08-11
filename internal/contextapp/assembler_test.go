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
		ContextWindow:    128000,
		SafetyCeiling:    100000,
		ReservedOutput:   4096,
		SystemTokens:     2048,
		ToolSchemaTokens: 1024,
		SafetyMargin:     512,
	}
	budget := p.EffectiveInputBudget()
	expected := int64(100000 - 4096 - 2048 - 1024 - 512) // 92320
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

// TestAssembleTurnBoundaryProtection verifies that an assistant message is
// never included without its corresponding user message (ADR-005 §3).
func TestAssembleTurnBoundaryProtection(t *testing.T) {
	// Backward order: m5(user), m4(assistant), m3(user), m2(assistant), m1(user)
	// Turns (forward): [m1,m2], [m3,m4], [m5]
	reader := &mockReader{
		messages: []Message{
			{ID: "m5", Role: "user", Content: "latest user", Sequence: 5, TokenCount: 10},
			{ID: "m4", Role: "assistant", Content: "reply to m3", Sequence: 4, TokenCount: 80},
			{ID: "m3", Role: "user", Content: "third user", Sequence: 3, TokenCount: 10},
			{ID: "m2", Role: "assistant", Content: "reply to m1", Sequence: 2, TokenCount: 80},
			{ID: "m1", Role: "user", Content: "first user", Sequence: 1, TokenCount: 10},
		},
	}
	info := ProviderInfo{
		ContextWindow:  200,
		ReservedOutput: 0,
	}
	// Budget = 200. RecentUserReserve = max(512, 20) = 512.
	// But m5 only needs 10, so reserve = 512, remaining = 200 - 512 = negative.
	// Actually reserve = max(512, 20) = 512, but budget is 200.
	// So remaining = 200 - 512 = -312 < 0 → return only m5.
	// Use explicit reserve to control the test.
	msgs, err := Assemble(context.Background(), reader, "s1", info, AssembleOptions{RecentUserReserve: 20})
	if err != nil {
		t.Fatal(err)
	}
	// m5(10) reserved, remaining = 200 - 20 = 180.
	// m4(assistant, 80) + m3(user, 10) = 90 ≤ 180 → include both.
	// m2(assistant, 80) + m1(user, 10) = 90 ≤ 90 → include both.
	// Total selected: m5, m4, m3, m2, m1 (all 5)
	if len(msgs) != 5 {
		t.Fatalf("expected 5 messages, got %d: %+v", len(msgs), msgs)
	}
	// Verify forward chronological order.
	if msgs[0].ID != "m1" || msgs[4].ID != "m5" {
		t.Fatalf("expected m1..m5 order, got %s..%s", msgs[0].ID, msgs[4].ID)
	}
}

// TestAssembleTurnBoundarySkipsIncompleteTurn verifies that when an assistant
// message fits but its user pair doesn't, both are skipped.
func TestAssembleTurnBoundarySkipsIncompleteTurn(t *testing.T) {
	// Backward order: m5(user, 10), m4(assistant, 80), m3(user, 80), m2(assistant, 10), m1(user, 10)
	// Turns (forward): [m1,m2], [m3,m4], [m5]
	reader := &mockReader{
		messages: []Message{
			{ID: "m5", Role: "user", Content: "latest", Sequence: 5, TokenCount: 10},
			{ID: "m4", Role: "assistant", Content: "big reply", Sequence: 4, TokenCount: 80},
			{ID: "m3", Role: "user", Content: "big user", Sequence: 3, TokenCount: 80},
			{ID: "m2", Role: "assistant", Content: "small reply", Sequence: 2, TokenCount: 10},
			{ID: "m1", Role: "user", Content: "first", Sequence: 1, TokenCount: 10},
		},
	}
	info := ProviderInfo{
		ContextWindow:  120,
		ReservedOutput: 0,
	}
	// Budget = 120. Reserve = 20, remaining = 100.
	// m4(80) + m3(80) = 160 > 100 → skip both (turn boundary).
	// m2(10) + m1(10) = 20 ≤ 100 → include both.
	// Selected (backward): m5, m2, m1 → forward: m1, m2, m5
	msgs, err := Assemble(context.Background(), reader, "s1", info, AssembleOptions{RecentUserReserve: 20})
	if err != nil {
		t.Fatal(err)
	}
	// Verify m4 (assistant) is NOT included without m3 (user).
	ids := make(map[string]bool, len(msgs))
	for _, m := range msgs {
		ids[m.ID] = true
	}
	if ids["m4"] && !ids["m3"] {
		t.Fatal("turn boundary violation: m4 (assistant) included without m3 (user)")
	}
	if ids["m3"] && !ids["m4"] {
		t.Fatal("turn boundary violation: m3 (user) included without m4 (assistant)")
	}
	// m5 should always be included (latest user).
	if !ids["m5"] {
		t.Fatal("latest user message m5 not included")
	}
	// m2+m1 turn should be included.
	if !ids["m2"] || !ids["m1"] {
		t.Fatal("expected m2+m1 turn to be included")
	}
}

// TestAssembleToolSchemaTokensReserved verifies that tool schema tokens are
// subtracted from the effective budget before message selection.
func TestAssembleToolSchemaTokensReserved(t *testing.T) {
	reader := &mockReader{
		messages: []Message{
			{ID: "m3", Role: "user", Content: "latest", Sequence: 3, TokenCount: 10},
			{ID: "m2", Role: "user", Content: "second", Sequence: 2, TokenCount: 10},
			{ID: "m1", Role: "user", Content: "first", Sequence: 1, TokenCount: 10},
		},
	}
	info := ProviderInfo{
		ContextWindow:    200,
		ReservedOutput:   0,
		ToolSchemaTokens: 150, // Leaves only 50 for messages.
	}
	// Budget = 200 - 150 = 50. Reserve = max(512, 5) = 512.
	// remaining = 50 - 512 < 0 → return only latest user.
	msgs, err := Assemble(context.Background(), reader, "s1", info, AssembleOptions{RecentUserReserve: 20})
	if err != nil {
		t.Fatal(err)
	}
	// Budget = 50. Reserve = 20, remaining = 30.
	// m3(10) reserved. m2(10) fits, m1(10) fits.
	// All 3 should be included.
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(msgs))
	}
}

// TestAssemblePriorSummaryInjection verifies that PriorSummary is prepended
// as a system-level preamble.
func TestAssemblePriorSummaryInjection(t *testing.T) {
	reader := &mockReader{
		messages: []Message{
			{ID: "m1", Role: "user", Content: "hello", Sequence: 1, TokenCount: 5},
		},
	}
	info := ProviderInfo{ContextWindow: 1000, ReservedOutput: 0}
	msgs, err := Assemble(context.Background(), reader, "s1", info, AssembleOptions{
		RecentUserReserve: 20,
		PriorSummary:      "Previous conversation context summary.",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) < 2 {
		t.Fatalf("expected at least 2 messages (summary + user), got %d", len(msgs))
	}
	if msgs[0].Role != "system" {
		t.Fatalf("expected first message to be system summary, got role %s", msgs[0].Role)
	}
	if !strings.Contains(msgs[0].Content, "[Prior Context Summary]") {
		t.Fatalf("expected summary preamble, got %q", msgs[0].Content)
	}
}

// TestAssembleFindsLatestUserNotFirst verifies that Assemble correctly finds
// the latest user message even when the most recent message is from assistant.
func TestAssembleFindsLatestUserNotFirst(t *testing.T) {
	// Backward order: m3(assistant), m2(user), m1(user)
	// The latest user is m2 (not m3 which is assistant).
	reader := &mockReader{
		messages: []Message{
			{ID: "m3", Role: "assistant", Content: "reply", Sequence: 3, TokenCount: 10},
			{ID: "m2", Role: "user", Content: "latest user", Sequence: 2, TokenCount: 10},
			{ID: "m1", Role: "user", Content: "first user", Sequence: 1, TokenCount: 10},
		},
	}
	info := ProviderInfo{ContextWindow: 1000, ReservedOutput: 0}
	msgs, err := Assemble(context.Background(), reader, "s1", info, AssembleOptions{RecentUserReserve: 20})
	if err != nil {
		t.Fatal(err)
	}
	// m2 should be the reserved latest user.
	// m3(assistant) pairs with m2(user) — but m2 is already selected as latest.
	// m3 is orphan (its user pair is the latest user, already selected).
	// m3 should still be included if it fits.
	// m1 should be included if it fits.
	ids := make(map[string]bool, len(msgs))
	for _, m := range msgs {
		ids[m.ID] = true
	}
	if !ids["m2"] {
		t.Fatal("latest user m2 not included")
	}
}

// TestAssembleNoUserMessagesReturnsError verifies that a session with only
// assistant messages returns ErrNoMessages.
func TestAssembleNoUserMessagesReturnsError(t *testing.T) {
	reader := &mockReader{
		messages: []Message{
			{ID: "m2", Role: "assistant", Content: "reply", Sequence: 2, TokenCount: 10},
			{ID: "m1", Role: "assistant", Content: "first reply", Sequence: 1, TokenCount: 10},
		},
	}
	info := ProviderInfo{ContextWindow: 1000, ReservedOutput: 0}
	_, err := Assemble(context.Background(), reader, "s1", info, AssembleOptions{})
	if err != ErrNoMessages {
		t.Fatalf("expected ErrNoMessages, got %v", err)
	}
}

// TestValidateProviderSequenceValid verifies that a well-formed alternating
// sequence passes validation.
func TestValidateProviderSequenceValid(t *testing.T) {
	tests := []struct {
		name string
		msgs []Message
	}{
		{"simple user-assistant", []Message{
			{Role: "user", Content: "hi"},
			{Role: "assistant", Content: "hello"},
		}},
		{"system at start", []Message{
			{Role: "system", Content: "instructions"},
			{Role: "user", Content: "hi"},
			{Role: "assistant", Content: "hello"},
		}},
		{"multiple system at start", []Message{
			{Role: "system", Content: "instructions"},
			{Role: "system", Content: "more instructions"},
			{Role: "user", Content: "hi"},
		}},
		{"consecutive user messages", []Message{
			{Role: "user", Content: "first"},
			{Role: "user", Content: "second"},
			{Role: "assistant", Content: "reply"},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidateProviderSequence(tt.msgs); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

// TestValidateProviderSequenceInvalid verifies that invalid sequences are
// rejected.
func TestValidateProviderSequenceInvalid(t *testing.T) {
	tests := []struct {
		name string
		msgs []Message
	}{
		{"empty", []Message{}},
		{"system after non-system", []Message{
			{Role: "user", Content: "hi"},
			{Role: "system", Content: "late system"},
		}},
		{"consecutive assistant", []Message{
			{Role: "user", Content: "hi"},
			{Role: "assistant", Content: "reply1"},
			{Role: "assistant", Content: "reply2"},
		}},
		{"no user message", []Message{
			{Role: "system", Content: "instructions"},
			{Role: "assistant", Content: "reply"},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidateProviderSequence(tt.msgs); err == nil {
				t.Fatal("expected error, got nil")
			}
		})
	}
}

// TestAssemblePinnedFactsInjection verifies that PinnedFacts is injected as a
// system-level preamble (ADR-005 §3 priority 4).
func TestAssemblePinnedFactsInjection(t *testing.T) {
	reader := &mockReader{
		messages: []Message{
			{ID: "m1", Role: "user", Content: "hello", Sequence: 1, TokenCount: 5},
		},
	}
	info := ProviderInfo{ContextWindow: 1000, ReservedOutput: 0}
	msgs, err := Assemble(context.Background(), reader, "s1", info, AssembleOptions{
		RecentUserReserve: 20,
		PinnedFacts:       "Decision: use PostgreSQL for persistence.",
	})
	if err != nil {
		t.Fatal(err)
	}
	// Expect: [Pinned Facts system msg] + [user message]
	if len(msgs) < 2 {
		t.Fatalf("expected at least 2 messages, got %d", len(msgs))
	}
	if msgs[0].Role != "system" {
		t.Fatalf("expected first message to be system, got %s", msgs[0].Role)
	}
	if !strings.Contains(msgs[0].Content, "[Pinned Facts]") {
		t.Fatalf("expected pinned facts preamble, got %q", msgs[0].Content)
	}
}

// TestAssemblePinnedFactsWithPriorSummary verifies that both PriorSummary and
// PinnedFacts are injected in the correct order (summary first, then facts).
func TestAssemblePinnedFactsWithPriorSummary(t *testing.T) {
	reader := &mockReader{
		messages: []Message{
			{ID: "m1", Role: "user", Content: "hello", Sequence: 1, TokenCount: 5},
		},
	}
	info := ProviderInfo{ContextWindow: 1000, ReservedOutput: 0}
	msgs, err := Assemble(context.Background(), reader, "s1", info, AssembleOptions{
		RecentUserReserve: 20,
		PriorSummary:      "Context summary text.",
		PinnedFacts:       "Pinned decision text.",
	})
	if err != nil {
		t.Fatal(err)
	}
	// Expect: [PriorSummary] + [PinnedFacts] + [user message]
	if len(msgs) < 3 {
		t.Fatalf("expected at least 3 messages, got %d", len(msgs))
	}
	if !strings.Contains(msgs[0].Content, "[Prior Context Summary]") {
		t.Fatalf("expected first message to be prior summary, got %q", msgs[0].Content)
	}
	if !strings.Contains(msgs[1].Content, "[Pinned Facts]") {
		t.Fatalf("expected second message to be pinned facts, got %q", msgs[1].Content)
	}
}

// TestAssembleRetrievedEvidenceWithinBudget verifies that RetrievedEvidence is
// appended when within the remaining budget.
func TestAssembleRetrievedEvidenceWithinBudget(t *testing.T) {
	reader := &mockReader{
		messages: []Message{
			{ID: "m1", Role: "user", Content: "hello", Sequence: 1, TokenCount: 5},
		},
	}
	info := ProviderInfo{ContextWindow: 1000, ReservedOutput: 0}
	msgs, err := Assemble(context.Background(), reader, "s1", info, AssembleOptions{
		RecentUserReserve: 20,
		RetrievedEvidence: "Older evidence from memory.",
	})
	if err != nil {
		t.Fatal(err)
	}
	// Last message should be the retrieved evidence system message.
	last := msgs[len(msgs)-1]
	if last.Role != "system" {
		t.Fatalf("expected last message to be system, got %s", last.Role)
	}
	if !strings.Contains(last.Content, "[Retrieved Evidence]") {
		t.Fatalf("expected retrieved evidence preamble, got %q", last.Content)
	}
}

// TestAssembleRetrievedEvidenceExceedingBudget verifies that RetrievedEvidence
// is NOT appended when it exceeds the remaining budget.
func TestAssembleRetrievedEvidenceExceedingBudget(t *testing.T) {
	reader := &mockReader{
		messages: []Message{
			{ID: "m1", Role: "user", Content: "hello", Sequence: 1, TokenCount: 5},
		},
	}
	info := ProviderInfo{ContextWindow: 100, ReservedOutput: 0}
	// Budget = 100. Reserve = 20, remaining after m1 = 75.
	// RetrievedEvidence is very large, should exceed remaining.
	largeEvidence := strings.Repeat("evidence ", 1000)
	msgs, err := Assemble(context.Background(), reader, "s1", info, AssembleOptions{
		RecentUserReserve: 20,
		RetrievedEvidence: largeEvidence,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Last message should NOT be retrieved evidence (exceeds budget).
	last := msgs[len(msgs)-1]
	if strings.Contains(last.Content, "[Retrieved Evidence]") {
		t.Fatal("retrieved evidence should not be included when exceeding budget")
	}
}

// TestAssembleToolCallToolResultAtomicity verifies that a tool result and its
// corresponding assistant tool_call are always included together (ADR-005 §3:
// "never splits an assistant tool call from its tool result").
func TestAssembleToolCallToolResultAtomicity(t *testing.T) {
	// Forward order: m1(user), m2(assistant tool_call), m3(tool result), m4(assistant final), m5(user)
	// Backward order: m5, m4, m3, m2, m1
	reader := &mockReader{
		messages: []Message{
			{ID: "m5", Role: "user", Content: "latest user", Sequence: 5, TokenCount: 10},
			{ID: "m4", Role: "assistant", Content: "final reply", Sequence: 4, TokenCount: 10},
			{ID: "m3", Role: "tool", Content: `{"result":"sunny"}`, Sequence: 3, TokenCount: 10},
			{ID: "m2", Role: "assistant", Content: `{"tool":"get_weather"}`, Sequence: 2, TokenCount: 10},
			{ID: "m1", Role: "user", Content: "first user", Sequence: 1, TokenCount: 10},
		},
	}
	info := ProviderInfo{ContextWindow: 1000, ReservedOutput: 0}
	msgs, err := Assemble(context.Background(), reader, "s1", info, AssembleOptions{RecentUserReserve: 20})
	if err != nil {
		t.Fatal(err)
	}
	ids := make(map[string]bool, len(msgs))
	for _, m := range msgs {
		ids[m.ID] = true
	}
	// m3 (tool) and m2 (assistant tool_call) must both be present or both absent.
	if ids["m3"] != ids["m2"] {
		t.Fatalf("tool_call/tool_result atomicity violation: m3(tool)=%v m2(assistant)=%v", ids["m3"], ids["m2"])
	}
}

// TestAssembleToolCallToolResultSkippedTogether verifies that when the
// tool+assistant pair exceeds budget, both are skipped (not just one).
func TestAssembleToolCallToolResultSkippedTogether(t *testing.T) {
	// Forward: m1(user), m2(assistant tool_call, 80), m3(tool result, 80), m4(assistant, 10), m5(user, 10)
	// Backward: m5(10), m4(10), m3(80), m2(80), m1(10)
	reader := &mockReader{
		messages: []Message{
			{ID: "m5", Role: "user", Content: "latest", Sequence: 5, TokenCount: 10},
			{ID: "m4", Role: "assistant", Content: "reply", Sequence: 4, TokenCount: 10},
			{ID: "m3", Role: "tool", Content: strings.Repeat("r", 80), Sequence: 3, TokenCount: 80},
			{ID: "m2", Role: "assistant", Content: strings.Repeat("c", 80), Sequence: 2, TokenCount: 80},
			{ID: "m1", Role: "user", Content: "first", Sequence: 1, TokenCount: 10},
		},
	}
	info := ProviderInfo{ContextWindow: 120, ReservedOutput: 0}
	// Budget = 120. Reserve = 20, remaining = 100.
	// m4(10) fits → remaining = 90.
	// m3(tool,80) + m2(assistant,80) = 160 > 90 → skip BOTH.
	// m1(10) fits → remaining = 80.
	msgs, err := Assemble(context.Background(), reader, "s1", info, AssembleOptions{RecentUserReserve: 20})
	if err != nil {
		t.Fatal(err)
	}
	ids := make(map[string]bool, len(msgs))
	for _, m := range msgs {
		ids[m.ID] = true
	}
	// Both m3 and m2 must be absent (skipped together).
	if ids["m3"] || ids["m2"] {
		t.Fatalf("expected m3 and m2 to be skipped together, got m3=%v m2=%v", ids["m3"], ids["m2"])
	}
	// m5 (latest user) and m1 should be present.
	if !ids["m5"] {
		t.Fatal("latest user m5 not included")
	}
	if !ids["m1"] {
		t.Fatal("m1 should be included within budget")
	}
}

// TestValidateProviderSequenceToolValid verifies that sequences with tool
// messages in valid positions pass validation.
func TestValidateProviderSequenceToolValid(t *testing.T) {
	tests := []struct {
		name string
		msgs []Message
	}{
		{"assistant then tool", []Message{
			{Role: "user", Content: "hi"},
			{Role: "assistant", Content: "call tool"},
			{Role: "tool", Content: "result"},
			{Role: "assistant", Content: "final reply"},
		}},
		{"assistant then multiple tools", []Message{
			{Role: "user", Content: "hi"},
			{Role: "assistant", Content: "call tools"},
			{Role: "tool", Content: "result1"},
			{Role: "tool", Content: "result2"},
			{Role: "assistant", Content: "final reply"},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidateProviderSequence(tt.msgs); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

// TestValidateProviderSequenceToolInvalid verifies that invalid tool message
// positions are rejected.
func TestValidateProviderSequenceToolInvalid(t *testing.T) {
	tests := []struct {
		name string
		msgs []Message
	}{
		{"tool without preceding assistant", []Message{
			{Role: "user", Content: "hi"},
			{Role: "tool", Content: "result"},
		}},
		{"tool at start", []Message{
			{Role: "tool", Content: "result"},
			{Role: "user", Content: "hi"},
		}},
		{"tool after user", []Message{
			{Role: "user", Content: "hi"},
			{Role: "tool", Content: "result"},
			{Role: "assistant", Content: "reply"},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidateProviderSequence(tt.msgs); err == nil {
				t.Fatal("expected error, got nil")
			}
		})
	}
}
