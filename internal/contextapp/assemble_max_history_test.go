// UX-06 history-depth cap: instant (voice companion) assembly limits verbatim
// history to the newest MaxHistoryTurns user turns so the model does not
// proactively reference conversation beyond the recent window. The latest user
// turn (priority 6) is always retained regardless of the cap. Storage is never
// touched — this is pure projection behavior.
package contextapp

import (
	"context"
	"testing"
)

// TestMaxHistoryTurnsCapsRecentWindow verifies that with MaxHistoryTurns=2
// only the newest two user turns (and everything at/after the oldest kept
// user turn's sequence) survive; older messages are excluded free of budget.
func TestMaxHistoryTurnsCapsRecentWindow(t *testing.T) {
	// seq 1..6: u/a/u/a/u/a. User turns at seq 1,3,5; latest user is seq 5.
	reader := &hierarchicalReader{msgs: hierarchicalMessages()}
	env := hierarchicalEnv(0, "")
	env.MaxHistoryTurns = 2
	env.ContextMode = ContextModeInstant

	res, err := AssembleEnvelope(context.Background(), reader, "s", env)
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
	// Newest two user turns start at seq 3 → keep 3,4,5,6; drop 1,2.
	if len(seqs) != 4 || seqs[0] != 3 || seqs[3] != 6 {
		t.Fatalf("projected sequences = %v, want [3 4 5 6]", seqs)
	}
	var beyond int
	for _, e := range res.Trace.Entries {
		if e.RejectReason == "beyond_max_history_turns" {
			beyond++
		}
	}
	if beyond != 2 {
		t.Fatalf("beyond_max_history_turns entries = %d, want 2", beyond)
	}
}

// TestMaxHistoryTurnsZeroKeepsFullWindow verifies the default (deep/typing)
// behavior: MaxHistoryTurns=0 means no turn cap, every message projects.
func TestMaxHistoryTurnsZeroKeepsFullWindow(t *testing.T) {
	reader := &hierarchicalReader{msgs: hierarchicalMessages()}
	env := hierarchicalEnv(0, "")
	env.MaxHistoryTurns = 0
	env.ContextMode = ContextModeDeep

	res, err := AssembleEnvelope(context.Background(), reader, "s", env)
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
		t.Fatalf("projected sequences = %v, want all 6 (no turn cap)", seqs)
	}
}

// TestMaxHistoryTurnsProtectsLatestUser verifies priority-6 protection: even
// with MaxHistoryTurns=1 the latest user turn is always retained.
func TestMaxHistoryTurnsProtectsLatestUser(t *testing.T) {
	reader := &hierarchicalReader{msgs: hierarchicalMessages()}
	env := hierarchicalEnv(0, "")
	env.MaxHistoryTurns = 1
	env.ContextMode = ContextModeInstant

	res, err := AssembleEnvelope(context.Background(), reader, "s", env)
	if err != nil {
		t.Fatal(err)
	}
	var hasLatestUser bool
	for _, m := range res.Messages {
		if m.Role == "user" && m.Sequence == 5 {
			hasLatestUser = true
		}
	}
	if !hasLatestUser {
		t.Fatal("latest user turn dropped despite priority-6 protection")
	}
}