// P2-2 hierarchical context window coverage: messages inside the accepted
// checkpoint's coverage range are represented by the Prior Summary and
// must NOT also be projected verbatim; post-checkpoint messages and the
// latest user turn (priority 6) always stay. Storage is never touched —
// this is pure projection behavior.
package contextapp

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type hierarchicalReader struct {
	msgs []Message
}

func (r *hierarchicalReader) ListMessages(_ context.Context, _ string, _ string, _ int) ([]Message, error) {
	// Reader contract: newest first (backward).
	out := make([]Message, len(r.msgs))
	for i, m := range r.msgs {
		out[len(r.msgs)-1-i] = m
	}
	return out, nil
}
func (r *hierarchicalReader) SumTokens(_ context.Context, _, _, _, _ string) (int64, error) {
	return 0, errors.New("not used")
}

func hierarchicalEnv(coverage int64, summary string) ContextEnvelope {
	env := ContextEnvelope{
		Provider: ProviderInfo{
			ContextWindow:   100000,
			SafetyCeiling:   100000,
			ReservedOutput:  4096,
			SystemTokens:    512,
			ToolSchemaTokens: 512,
			SafetyMargin:    1024,
		},
		MaxMessages:  64,
		SafetyMargin: 1024,
	}
	if summary != "" {
		env.AcceptedCheckpoint = &ContextSource{
			Type:                SourceCompactionSummary,
			ID:                  "latest",
			Authority:           AuthorityEvidence,
			Content:             summary,
			CoverageEndSequence: coverage,
		}
	}
	return env
}

// seq 1..6: u/a/u/a covered by checkpoint (coverage end 4), then u/a after.
func hierarchicalMessages() []Message {
	roles := []string{"user", "assistant", "user", "assistant", "user", "assistant"}
	msgs := make([]Message, len(roles))
	for i, role := range roles {
		msgs[i] = Message{
			ID:         string(rune('A' + i)),
			Role:       role,
			Content:    "content-" + string(rune('a'+i)) + " NEEDLE",
			Sequence:   int64(i + 1),
			TokenCount: 10,
		}
	}
	return msgs
}

func TestHierarchicalCoverageExcludesSummarizedMessages(t *testing.T) {
	reader := &hierarchicalReader{msgs: hierarchicalMessages()}
	res, err := AssembleEnvelope(context.Background(), reader, "s", hierarchicalEnv(4, "summary of turns 1-4"))
	if err != nil {
		t.Fatal(err)
	}
	// Messages with sequence <= 4 must be gone; 5 (user) and 6 (assistant) stay.
	var seqs []int64
	for _, m := range res.Messages {
		if m.Role == "system" {
			continue
		}
		seqs = append(seqs, m.Sequence)
	}
	if len(seqs) != 2 || seqs[0] != 5 || seqs[1] != 6 {
		t.Fatalf("projected sequences = %v, want [5 6]", seqs)
	}
	// The summary itself must be present exactly once (untrusted preamble
	// appended to the latest user message).
	if n := strings.Count(joinContents(res), "summary of turns 1-4"); n != 1 {
		t.Fatalf("summary appears %d times, want 1", n)
	}
	// Trace records the exclusion reason.
	var covered int
	for _, e := range res.Trace.Entries {
		if e.RejectReason == "covered_by_checkpoint" {
			covered++
		}
	}
	if covered != 4 {
		t.Fatalf("covered_by_checkpoint entries = %d, want 4", covered)
	}
}

func TestHierarchicalUnknownCoverageKeepsFlatProjection(t *testing.T) {
	// CoverageEndSequence 0 = unknown: every message still projects
	// (legacy summary + full-history behavior).
	reader := &hierarchicalReader{msgs: hierarchicalMessages()}
	res, err := AssembleEnvelope(context.Background(), reader, "s", hierarchicalEnv(0, "summary"))
	if err != nil {
		t.Fatal(err)
	}
	var seqs []int64
	for _, m := range res.Messages {
		if m.Role == "system" {
			continue
		}
		seqs = append(seqs, m.Sequence)
	}
	if len(seqs) != 6 {
		t.Fatalf("projected sequences = %v, want all 6 (flat projection)", seqs)
	}
}

func TestHierarchicalLatestUserProtectedInsideCoverage(t *testing.T) {
	// Degenerate case: the checkpoint claims to cover everything,
	// including the newest user message. Priority 6 must still win.
	msgs := hierarchicalMessages()
	reader := &hierarchicalReader{msgs: msgs}
	res, err := AssembleEnvelope(context.Background(), reader, "s", hierarchicalEnv(6, "summary"))
	if err != nil {
		t.Fatal(err)
	}
	var hasUser bool
	for _, m := range res.Messages {
		if m.Role == "user" && m.Sequence == 5 {
			hasUser = true
		}
	}
	if !hasUser {
		t.Fatal("latest user turn dropped despite priority-6 protection")
	}
	// Sequence 6 assistant is not the latest user turn; coverage applies.
	for _, m := range res.Messages {
		if m.Sequence == 6 {
			t.Fatal("covered assistant message projected; want summary-only")
		}
	}
}

func TestHierarchicalSmallBudgetPrefersRecentOverSummaryOnly(t *testing.T) {
	// With coverage exclusion freeing budget, a tight budget still keeps
	// post-checkpoint messages plus the summary — never a bare latest
	// user turn while recent context fits.
	msgs := hierarchicalMessages()
	reader := &hierarchicalReader{msgs: msgs}
	env := hierarchicalEnv(4, "compact summary")
	// Budget that cannot hold all 6 messages (300 tokens each) but easily
	// holds the latest pair plus the summary.
	env.Provider.ContextWindow = 3000
	env.Provider.SafetyCeiling = 3000
	env.Provider.ReservedOutput = 1500
	env.Provider.SystemTokens = 300
	env.Provider.ToolSchemaTokens = 300
	env.Provider.SafetyMargin = 0
	msgs[4].TokenCount = 300 // latest user
	msgs[5].TokenCount = 300 // latest assistant
	for i := 0; i <= 3; i++ {
		msgs[i].TokenCount = 300
	}
	res, err := AssembleEnvelope(context.Background(), reader, "s", env)
	if err != nil {
		t.Fatal(err)
	}
	joined := joinContents(res)
	if !strings.Contains(joined, "compact summary") || !strings.Contains(joined, "content-e") {
		t.Fatalf("projection missing summary or post-checkpoint message: %q", joined)
	}
	if strings.Contains(joined, "content-a") {
		t.Fatalf("covered message leaked into tight-budget projection: %q", joined)
	}
}

func joinContents(res *AssembleResult) string {
	var b strings.Builder
	for _, m := range res.Messages {
		b.WriteString(m.Content)
		b.WriteString("\n")
	}
	return b.String()
}
