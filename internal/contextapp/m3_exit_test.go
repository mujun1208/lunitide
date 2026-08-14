package contextapp

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestM3ExitWindowMatrixWithMillionTokenLogicalHistory(t *testing.T) {
	const messageTokens int64 = 1000
	messages := make([]Message, 1001)
	for i := range messages {
		role := "assistant"
		if i%2 == 0 {
			role = "user"
		}
		messages[i] = Message{
			ID: fmt.Sprintf("m-%04d", i+1), Role: role,
			Content:  strings.Repeat("context ", 500),
			Sequence: int64(i + 1), TokenCount: messageTokens,
		}
	}
	for left, right := 0, len(messages)-1; left < right; left, right = left+1, right-1 {
		messages[left], messages[right] = messages[right], messages[left]
	}
	if int64(len(messages))*messageTokens < 1_000_000 {
		t.Fatal("fixture does not reach one million logical tokens")
	}
	for _, window := range []int64{32 * 1024, 128 * 1024, 200 * 1024} {
		t.Run(fmt.Sprintf("window-%d", window), func(t *testing.T) {
			result, err := AssembleEnvelope(context.Background(), &mockReader{messages: messages}, "million-token-session", ContextEnvelope{
				Provider: ProviderInfo{
					ContextWindow: window, SafetyCeiling: window,
					ReservedOutput: 4096, SafetyMargin: 512,
				},
				MaxMessages: len(messages), RecentUserReserve: 1024,
			})
			if err != nil {
				t.Fatal(err)
			}
			if result.Trace.UsedTokens > result.Trace.EffectiveBudget {
				t.Fatalf("used %d exceeds effective budget %d", result.Trace.UsedTokens, result.Trace.EffectiveBudget)
			}
			if len(result.Messages) == 0 || result.Messages[len(result.Messages)-1].ID != "m-1001" {
				t.Fatal("latest user turn was not preserved")
			}
		})
	}
}

// TestM3FixedCorpusSourceAndSummaryFidelity is deliberately deterministic and
// reviewable: the stable facts below model the M3 corpus categories while the
// repeated messages raise logical history above one million tokens. Original
// messages remain in the reader; assembly is selection, never deletion.
func TestM3FixedCorpusSourceAndSummaryFidelity(t *testing.T) {
	const (
		fact       = "FACT invoice-id=INV-2048"
		constraint = "CONSTRAINT never delete original messages"
		unfinished = "TODO reconcile failed upload"
		citation   = "SOURCE message:m-0001 attachment:att-0001"
	)
	messages := make([]Message, 1001)
	for i := range messages {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		content := strings.Repeat("fixed-corpus-message ", 100)
		if i == 0 {
			content = fact + "\n" + constraint + "\n" + unfinished + "\n" + citation + "\n" + content
		}
		if i == 500 {
			role = "tool"
			content = "TOOL LARGE OUTPUT\n" + strings.Repeat("tool-row=stable ", 100)
		}
		messages[i] = Message{ID: fmt.Sprintf("m-%04d", i+1), Role: role, Content: content, Sequence: int64(i + 1), TokenCount: 1000}
	}
	summary := strings.Join([]string{fact, constraint, unfinished, citation}, "\n")
	for _, window := range []int64{32 * 1024, 128 * 1024, 200 * 1024} {
		t.Run(fmt.Sprintf("window-%d", window), func(t *testing.T) {
			envelope := ContextEnvelope{
				Provider:           ProviderInfo{ContextWindow: window, SafetyCeiling: window, ReservedOutput: 2048, SafetyMargin: 512},
				AcceptedCheckpoint: &ContextSource{Type: SourceCompactionSummary, ID: "cp-fixed", Provenance: "messages:m-0001..m-0900", Authority: AuthorityCheckpoint, Content: summary},
				HandoffCapsules:    []ContextSource{{Type: SourceHandoffCapsule, ID: "handoff-fixed", Provenance: "session:source", Content: unfinished + "\n" + citation}},
				AttachmentExcerpts: []ContextSource{{Type: SourceAttachmentExcerpt, ID: "att-0001", Provenance: "attachment:att-0001", Content: fact}},
				RelatedEvidence:    []ContextSource{{Type: SourceRetrievedEvidence, ID: "source-fixed", Provenance: citation, Content: citation}},
				MaxMessages:        len(messages), RecentUserReserve: 1024,
			}
			result, err := AssembleEnvelope(context.Background(), &mockReader{messages: messages}, "fixed-million", envelope)
			if err != nil {
				t.Fatal(err)
			}
			if result.Trace.UsedTokens > result.Trace.EffectiveBudget {
				t.Fatalf("used=%d budget=%d", result.Trace.UsedTokens, result.Trace.EffectiveBudget)
			}
			joined := ""
			for _, m := range result.Messages {
				joined += m.Content + "\n"
			}
			for _, required := range []string{fact, constraint, unfinished, citation} {
				if !strings.Contains(joined, required) {
					t.Fatalf("lost summary/source fidelity: %q", required)
				}
			}
			if len(messages) != 1001 || messages[0].ID != "m-0001" {
				t.Fatal("assembly mutated/deleted original corpus")
			}
		})
	}
}

func TestM3FixedCorpusTombstoneFailsClosed(t *testing.T) {
	_, err := AssembleEnvelope(context.Background(), &mockReader{messages: []Message{{ID: "m-live", Role: "user", Content: "live", Sequence: 1, TokenCount: 1}}}, "fixed-million", ContextEnvelope{
		Provider:    ProviderInfo{ContextWindow: 32 * 1024, SafetyCeiling: 32 * 1024},
		PinnedFacts: []ContextSource{{Type: SourcePinnedFacts, ID: "tombstoned-source", Provenance: "tombstone:message:m-deleted", Content: "DELETED SECRET MUST NOT APPEAR", Deleted: true}},
	})
	if err == nil || !strings.Contains(err.Error(), "deleted source") {
		t.Fatalf("tombstone did not fail closed: %v", err)
	}
}
