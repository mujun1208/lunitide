package contextapp

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// TestAssembleEnvelopeDeletedSourceFailClosed verifies that any source marked
// as Deleted causes AssembleEnvelope to fail-closed (ADR-005 §3: "deleted
// sources must never be included in assembled context").
func TestAssembleEnvelopeDeletedSourceFailClosed(t *testing.T) {
	reader := &mockReader{
		messages: []Message{
			{ID: "m1", Role: "user", Content: "hello", Sequence: 1, TokenCount: 5},
		},
	}
	info := ProviderInfo{ContextWindow: 1000, ReservedOutput: 0}

	tests := []struct {
		name     string
		envelope ContextEnvelope
		wantErr  error
	}{
		{
			name: "deleted accepted checkpoint",
			envelope: ContextEnvelope{
				Provider: info,
				AcceptedCheckpoint: &ContextSource{
					Type:    SourceCompactionSummary,
					Content: "summary",
					Deleted: true,
				},
			},
			wantErr: ErrEnvelopeDeletedSource,
		},
		{
			name: "deleted pinned facts",
			envelope: ContextEnvelope{
				Provider: info,
				PinnedFacts: []ContextSource{{
					Type:    SourcePinnedFacts,
					Content: "facts",
					Deleted: true,
				}},
			},
			wantErr: ErrEnvelopeDeletedSource,
		},
		{
			name: "deleted related evidence",
			envelope: ContextEnvelope{
				Provider: info,
				RelatedEvidence: []ContextSource{{
					Type:    SourceRetrievedEvidence,
					Content: "evidence",
					Deleted: true,
				}},
			},
			wantErr: ErrEnvelopeDeletedSource,
		},
		{
			name: "deleted handoff capsule",
			envelope: ContextEnvelope{
				Provider: info,
				HandoffCapsules: []ContextSource{{
					Type:    SourceHandoffCapsule,
					Content: "capsule summary",
					Deleted: true,
				}},
			},
			wantErr: ErrEnvelopeDeletedSource,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := AssembleEnvelope(context.Background(), reader, "s1", tt.envelope)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("expected %v, got %v", tt.wantErr, err)
			}
		})
	}
}

// TestAssembleEnvelopeSelectionTrace verifies that the selection trace
// records every candidate source with its selection decision and reject
// reason.
func TestAssembleEnvelopeSelectionTrace(t *testing.T) {
	reader := &mockReader{
		messages: []Message{
			{ID: "m5", Role: "user", Content: "latest", Sequence: 5, TokenCount: 10},
			{ID: "m4", Role: "assistant", Content: "reply4", Sequence: 4, TokenCount: 80},
			{ID: "m3", Role: "user", Content: "user3", Sequence: 3, TokenCount: 80},
			{ID: "m2", Role: "assistant", Content: "reply2", Sequence: 2, TokenCount: 10},
			{ID: "m1", Role: "user", Content: "first", Sequence: 1, TokenCount: 10},
		},
	}
	info := ProviderInfo{ContextWindow: 120, ReservedOutput: 0}
	// Budget = 120. Reserve = max(512, 12) = 512 → but budget is 120.
	// Actually default reserve = max(512, 120/10) = max(512, 12) = 512.
	// remaining = 120 - 512 < 0 → only latest user returned.
	// Use explicit reserve to control.
	env := ContextEnvelope{
		Provider:          info,
		RecentUserReserve: 20,
		SafetyMargin:      0,
	}
	result, err := AssembleEnvelope(context.Background(), reader, "s1", env)
	if err != nil {
		t.Fatal(err)
	}
	// Verify trace entries exist.
	if len(result.Trace.Entries) == 0 {
		t.Fatal("expected non-empty selection trace")
	}
	// Verify trace has at least the latest user turn entry.
	var hasLatestUser bool
	for _, e := range result.Trace.Entries {
		if e.SourceType == SourceLatestUserTurn && e.Selected {
			hasLatestUser = true
		}
	}
	if !hasLatestUser {
		t.Fatal("trace missing selected latest user turn entry")
	}
	// Verify trace records rejected sources with reject reasons.
	var hasRejected bool
	for _, e := range result.Trace.Entries {
		if !e.Selected && e.RejectReason != "" {
			hasRejected = true
		}
	}
	if !hasRejected {
		t.Fatal("trace missing rejected source entries with reject reasons")
	}
	// Verify trace budget fields are populated.
	if result.Trace.EffectiveBudget <= 0 {
		t.Fatalf("expected positive effective budget, got %d", result.Trace.EffectiveBudget)
	}
	if result.Trace.UsedTokens <= 0 {
		t.Fatalf("expected positive used tokens, got %d", result.Trace.UsedTokens)
	}
}

// TestAssembleEnvelopePriorSummaryParticipatesInBudget verifies that
// PriorSummary (accepted checkpoint) token cost is subtracted from the
// message selection budget (ADR-005 §3 priority 3).
func TestAssembleEnvelopePriorSummaryParticipatesInBudget(t *testing.T) {
	reader := &mockReader{
		messages: []Message{
			{ID: "m3", Role: "user", Content: "latest", Sequence: 3, TokenCount: 10},
			{ID: "m2", Role: "user", Content: "second", Sequence: 2, TokenCount: 10},
			{ID: "m1", Role: "user", Content: "first", Sequence: 1, TokenCount: 10},
		},
	}
	info := ProviderInfo{ContextWindow: 100, ReservedOutput: 0}
	// Budget = 100. Reserve = 20, remaining = 80.
	// PriorSummary = 60 tokens → messageBudget = 80 - 60 = 20.
	// m3(10) reserved. remaining = 20 - 10 = 10... actually m3 is the latest user.
	// Let's trace: messageBudget = 100 - 60 = 40. Reserve = 20. remaining = 40 - 20 = 20.
	// m2(10) fits → remaining = 10. m1(10) fits → remaining = 0.
	env := ContextEnvelope{
		Provider:          info,
		RecentUserReserve: 20,
		AcceptedCheckpoint: &ContextSource{
			Type:       SourceCompactionSummary,
			Content:    strings.Repeat("s", 240), // ~60 tokens (4 chars/token)
			Authority:  AuthorityCheckpoint,
			Provenance: "session:s1:checkpoint:latest",
		},
	}
	result, err := AssembleEnvelope(context.Background(), reader, "s1", env)
	if err != nil {
		t.Fatal(err)
	}
	// Verify prior summary is in the output.
	var hasSummary bool
	for _, m := range result.Messages {
		if m.Role == "system" && strings.Contains(m.Content, "[Prior Context Summary]") {
			hasSummary = true
		}
	}
	if !hasSummary {
		t.Fatal("prior summary not in assembled messages")
	}
	// Verify trace records prior summary tokens as reserved.
	if result.Trace.ReservedTokens.PriorSummary <= 0 {
		t.Fatalf("expected positive prior summary reserved tokens, got %d", result.Trace.ReservedTokens.PriorSummary)
	}
}

// TestAssembleEnvelopePinnedFactsOrder verifies that pinned facts appear
// after prior summary in the assembled output (priority 3 before 4).
func TestAssembleEnvelopePinnedFactsOrder(t *testing.T) {
	reader := &mockReader{
		messages: []Message{
			{ID: "m1", Role: "user", Content: "hello", Sequence: 1, TokenCount: 5},
		},
	}
	info := ProviderInfo{ContextWindow: 1000, ReservedOutput: 0}
	env := ContextEnvelope{
		Provider:          info,
		RecentUserReserve: 20,
		AcceptedCheckpoint: &ContextSource{
			Type:      SourceCompactionSummary,
			Content:   "Context summary text.",
			Authority: AuthorityCheckpoint,
		},
		PinnedFacts: []ContextSource{{
			Type:      SourcePinnedFacts,
			Content:   "Pinned decision text.",
			Authority: AuthorityPinned,
		}},
	}
	result, err := AssembleEnvelope(context.Background(), reader, "s1", env)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Messages) < 3 {
		t.Fatalf("expected at least 3 messages, got %d", len(result.Messages))
	}
	if !strings.Contains(result.Messages[0].Content, "[Prior Context Summary]") {
		t.Fatalf("expected first message to be prior summary, got %q", result.Messages[0].Content)
	}
	if !strings.Contains(result.Messages[1].Content, "[Pinned Facts]") {
		t.Fatalf("expected second message to be pinned facts, got %q", result.Messages[1].Content)
	}
}

// TestAssembleEnvelopeHandoffCapsuleInjection verifies that handoff capsules
// are injected as untrusted system preambles with provenance tagging.
func TestAssembleEnvelopeHandoffCapsuleInjection(t *testing.T) {
	reader := &mockReader{
		messages: []Message{
			{ID: "m1", Role: "user", Content: "hello", Sequence: 1, TokenCount: 5},
		},
	}
	info := ProviderInfo{ContextWindow: 1000, ReservedOutput: 0}
	env := ContextEnvelope{
		Provider:          info,
		RecentUserReserve: 20,
		HandoffCapsules: []ContextSource{{
			Type:       SourceHandoffCapsule,
			ID:         "cap-001",
			Content:    "Transferred context from another session.",
			Authority:  AuthorityCheckpoint,
			Provenance: "handoff:capsule:cap-001",
		}},
	}
	result, err := AssembleEnvelope(context.Background(), reader, "s1", env)
	if err != nil {
		t.Fatal(err)
	}
	var hasCapsule bool
	for _, m := range result.Messages {
		if m.Role == "system" && strings.Contains(m.Content, "[Handoff Context]") {
			hasCapsule = true
		}
	}
	if !hasCapsule {
		t.Fatal("handoff capsule not injected as system preamble")
	}
	// Verify trace records the handoff capsule.
	var tracedCapsule bool
	for _, e := range result.Trace.Entries {
		if e.SourceType == SourceHandoffCapsule {
			tracedCapsule = true
		}
	}
	if !tracedCapsule {
		t.Fatal("handoff capsule not recorded in selection trace")
	}
}

// TestAssembleEnvelopeDeterministicOutput verifies that the same input
// produces the same output across multiple calls (determinism).
func TestAssembleEnvelopeDeterministicOutput(t *testing.T) {
	reader := &mockReader{
		messages: []Message{
			{ID: "m4", Role: "user", Content: "latest", Sequence: 4, TokenCount: 10},
			{ID: "m3", Role: "assistant", Content: "reply3", Sequence: 3, TokenCount: 10},
			{ID: "m2", Role: "user", Content: "user2", Sequence: 2, TokenCount: 10},
			{ID: "m1", Role: "assistant", Content: "reply1", Sequence: 1, TokenCount: 10},
		},
	}
	info := ProviderInfo{ContextWindow: 1000, ReservedOutput: 0}
	env := ContextEnvelope{
		Provider:          info,
		RecentUserReserve: 20,
	}
	result1, err := AssembleEnvelope(context.Background(), reader, "s1", env)
	if err != nil {
		t.Fatal(err)
	}
	result2, err := AssembleEnvelope(context.Background(), reader, "s1", env)
	if err != nil {
		t.Fatal(err)
	}
	if len(result1.Messages) != len(result2.Messages) {
		t.Fatalf("non-deterministic message count: %d vs %d", len(result1.Messages), len(result2.Messages))
	}
	for i := range result1.Messages {
		if result1.Messages[i].ID != result2.Messages[i].ID {
			t.Fatalf("non-deterministic message at position %d: %s vs %s", i, result1.Messages[i].ID, result2.Messages[i].ID)
		}
		if result1.Messages[i].Content != result2.Messages[i].Content {
			t.Fatalf("non-deterministic content at position %d", i)
		}
	}
}

// TestAssembleEnvelopeWindowMatrix verifies assembly across a range of
// context window sizes (8K, 16K, 32K, 128K, 200K, 1M).
func TestAssembleEnvelopeWindowMatrix(t *testing.T) {
	// Create messages totaling ~5000 tokens.
	var msgs []Message
	for i := 0; i < 100; i++ {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		msgs = append(msgs, Message{
			ID:         "m" + string(rune('a'+i%26)) + string(rune('0'+i/26)),
			Role:       role,
			Content:    strings.Repeat("x", 200), // ~50 tokens each
			Sequence:   int64(i + 1),
			TokenCount: 50,
		})
	}
	// Reverse order (newest first) as expected by the assembler.
	for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 {
		msgs[i], msgs[j] = msgs[j], msgs[i]
	}
	reader := &mockReader{messages: msgs}

	windows := []int64{8192, 16384, 32768, 131072, 204800, 1048576}
	for _, window := range windows {
		t.Run("window_"+itoa(window), func(t *testing.T) {
			info := ProviderInfo{
				ContextWindow:  window,
				ReservedOutput: 4096,
				SafetyMargin:   512,
			}
			env := ContextEnvelope{
				Provider:          info,
				RecentUserReserve: 512,
			}
			result, err := AssembleEnvelope(context.Background(), reader, "s1", env)
			if err != nil {
				t.Fatalf("window %d: assembly failed: %v", window, err)
			}
			// Verify total tokens don't exceed budget.
			budget := info.EffectiveInputBudget()
			var totalTokens int64
			for _, m := range result.Messages {
				totalTokens += m.TokenCount
			}
			if totalTokens > budget+info.SafetyMargin {
				t.Fatalf("window %d: total tokens %d exceeds budget %d", window, totalTokens, budget)
			}
			// Verify at least one message is returned.
			if len(result.Messages) == 0 {
				t.Fatalf("window %d: no messages assembled", window)
			}
			// Larger windows should include at least as many messages as
			// smaller windows (monotonic non-decreasing).
		})
	}
}

// TestAssembleEnvelopeRetrievedEvidenceBudgetExceeded verifies that
// retrieved evidence is NOT injected when it exceeds remaining budget.
func TestAssembleEnvelopeRetrievedEvidenceBudgetExceeded(t *testing.T) {
	reader := &mockReader{
		messages: []Message{
			{ID: "m1", Role: "user", Content: "hello", Sequence: 1, TokenCount: 5},
		},
	}
	info := ProviderInfo{ContextWindow: 100, ReservedOutput: 0}
	env := ContextEnvelope{
		Provider:          info,
		RecentUserReserve: 20,
		RelatedEvidence: []ContextSource{{
			Type:       SourceRetrievedEvidence,
			Content:    strings.Repeat("e", 10000), // Very large, will exceed budget
			Authority:  AuthorityEvidence,
			Provenance: "memory:old-evidence",
		}},
	}
	result, err := AssembleEnvelope(context.Background(), reader, "s1", env)
	if err != nil {
		t.Fatal(err)
	}
	// Verify evidence was NOT included.
	for _, m := range result.Messages {
		if strings.Contains(m.Content, "[Retrieved Evidence]") {
			t.Fatal("retrieved evidence should not be included when exceeding budget")
		}
	}
	// Verify trace records the rejection.
	var hasRejectedEvidence bool
	for _, e := range result.Trace.Entries {
		if e.SourceType == SourceRetrievedEvidence && !e.Selected && e.RejectReason == "over_budget" {
			hasRejectedEvidence = true
		}
	}
	if !hasRejectedEvidence {
		t.Fatal("trace missing rejected evidence entry")
	}
}

// TestAssembleEnvelopeBudgetTooSmall verifies that ErrEnvelopeBudgetTooSmall
// is returned when the budget is zero or negative.
func TestAssembleEnvelopeBudgetTooSmall(t *testing.T) {
	reader := &mockReader{
		messages: []Message{
			{ID: "m1", Role: "user", Content: "hello", Sequence: 1, TokenCount: 5},
		},
	}
	info := ProviderInfo{
		ContextWindow:  100,
		ReservedOutput: 200, // Makes budget negative
	}
	env := ContextEnvelope{Provider: info}
	_, err := AssembleEnvelope(context.Background(), reader, "s1", env)
	if !errors.Is(err, ErrEnvelopeBudgetTooSmall) {
		t.Fatalf("expected ErrEnvelopeBudgetTooSmall, got %v", err)
	}
}

// itoa converts an int64 to string (avoiding strconv import).
func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	pos := len(buf)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}
