package merge

import (
	"errors"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/domain/m6supply"
)

func TestWriterStateMachineEdges(t *testing.T) {
	legal := [][2]string{
		{m6supply.MergeIntentSubmitted, m6supply.MergeIntentValidating},
		{m6supply.MergeIntentValidating, m6supply.MergeIntentQueued},
		{m6supply.MergeIntentValidating, m6supply.MergeIntentRejected},
		{m6supply.MergeIntentQueued, m6supply.MergeIntentCasCheck},
		{m6supply.MergeIntentCasCheck, m6supply.MergeIntentApplying},
		{m6supply.MergeIntentCasCheck, m6supply.MergeIntentStale},
		{m6supply.MergeIntentApplying, m6supply.MergeIntentMerged},
		{m6supply.MergeIntentStale, m6supply.MergeIntentRebaseNeeded},
		{m6supply.MergeIntentRebaseNeeded, m6supply.MergeIntentQueued},
	}
	for _, e := range legal {
		if err := ValidateTransition(e[0], e[1]); err != nil {
			t.Fatalf("legal edge rejected: %s -> %s: %v", e[0], e[1], err)
		}
	}
	illegal := [][2]string{
		{m6supply.MergeIntentApplying, m6supply.MergeIntentQueued}, // apply failure converges via Recover only
		{m6supply.MergeIntentMerged, m6supply.MergeIntentQueued},
		{m6supply.MergeIntentStale, m6supply.MergeIntentApplying},
		{m6supply.MergeIntentQueued, m6supply.MergeIntentMerged},
		{"unknown", m6supply.MergeIntentQueued},
	}
	for _, e := range illegal {
		if err := ValidateTransition(e[0], e[1]); err == nil {
			t.Fatalf("illegal edge accepted: %s -> %s", e[0], e[1])
		}
	}
	// the compressed writer walks are valid chains
	if err := WalkStep(m6supply.MergeIntentQueued, m6supply.MergeIntentCasCheck, m6supply.MergeIntentApplying); err != nil {
		t.Fatalf("writer apply walk rejected: %v", err)
	}
	if err := WalkStep(m6supply.MergeIntentQueued, m6supply.MergeIntentCasCheck, m6supply.MergeIntentStale); err != nil {
		t.Fatalf("writer stale walk rejected: %v", err)
	}
	if err := WalkStep(m6supply.MergeIntentRebaseNeeded, m6supply.MergeIntentQueued, m6supply.MergeIntentCasCheck, m6supply.MergeIntentApplying); err != nil {
		t.Fatalf("rebase requeue walk rejected: %v", err)
	}
}

func TestWriterCASVerdicts(t *testing.T) {
	if ResolveCAS("h1", "h1") != CASMatch {
		t.Fatalf("equal heads must match")
	}
	if ResolveCAS("h1", "h2") != CASStale {
		t.Fatalf("different heads must be stale (MRG-001)")
	}
}

func TestWriterFencing(t *testing.T) {
	now := time.Now().UTC()
	held := WriterLease{RootID: "r", Epoch: 2, Owner: "a", ExpiresAt: now.Add(time.Minute)}
	// live lease, no successor: allowed
	if err := CheckFencing(held, &held, now); err != nil {
		t.Fatalf("live lease fenced: %v", err)
	}
	// expired lease: fenced even without a successor (MRG-002)
	expired := held
	expired.ExpiresAt = now.Add(-time.Second)
	if err := CheckFencing(expired, nil, now); !errors.Is(err, ErrWriterFenced) {
		t.Fatalf("expired lease accepted: %v", err)
	}
	// a newer epoch supersedes: fenced (MRG-002)
	newer := held
	newer.Epoch = 3
	if err := CheckFencing(held, &newer, now); !errors.Is(err, ErrWriterFenced) {
		t.Fatalf("superseded epoch accepted: %v", err)
	}
	// an older recorded epoch does not fence the newer holder
	if err := CheckFencing(newer, &held, now); err != nil {
		t.Fatalf("newest epoch fenced by an older one: %v", err)
	}
}

func TestWriterTotalOrder(t *testing.T) {
	mk := func(id string, seq int64, state string) IntentView {
		return IntentView{ID: id, RootID: "r", Sequence: seq, State: state}
	}
	intents := []IntentView{
		mk("i3", 3, m6supply.MergeIntentQueued),
		mk("i1", 1, m6supply.MergeIntentMerged), // terminal: skipped
		mk("i2", 2, m6supply.MergeIntentQueued),
	}
	next, err := NextIntent(intents)
	if err != nil || next.ID != "i2" {
		t.Fatalf("NextIntent = %q err=%v (want i2: lowest sequence among non-terminal)", next.ID, err)
	}
	// drained queue
	_, err = NextIntent([]IntentView{mk("i1", 1, m6supply.MergeIntentMerged)})
	if !errors.Is(err, ErrNoIntent) {
		t.Fatalf("drained queue err=%v", err)
	}
	// applications never overtake an earlier non-terminal sequence: i3
	// (seq 3) may not apply while i2 (seq 2) is still queued; i1-sized
	// heads pass (i1 itself is terminal, nothing earlier is pending)
	if err := ValidateOrder(mk("i3", 3, m6supply.MergeIntentQueued), intents); err == nil {
		t.Fatalf("overtake accepted")
	}
	if err := ValidateOrder(mk("i2", 2, m6supply.MergeIntentQueued), intents); err != nil {
		t.Fatalf("head of order refused: %v", err)
	}
	// stale intents requeue in total order against the moved head
	stale := []IntentView{
		mk("s2", 2, m6supply.MergeIntentStale),
		mk("s1", 1, m6supply.MergeIntentStale),
	}
	plan := PlanRebase(stale, "h9")
	if len(plan) != 2 || plan[0].IntentID != "s1" || plan[1].IntentID != "s2" {
		t.Fatalf("PlanRebase order = %+v", plan)
	}
	for _, p := range plan {
		if p.NewExpectedHead != "h9" || p.Edge[0] != m6supply.MergeIntentRebaseNeeded || p.Edge[1] != m6supply.MergeIntentQueued {
			t.Fatalf("rebase advance malformed: %+v", p)
		}
	}
}
