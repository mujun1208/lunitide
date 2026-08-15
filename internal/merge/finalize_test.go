package merge

import (
	"testing"

	"github.com/lunitide/lunitide/internal/domain/m6supply"
)

func TestPlanFinalGate(t *testing.T) {
	merged := IntentView{ID: "i1", Sequence: 1, State: m6supply.MergeIntentMerged}
	pending := IntentView{ID: "i2", Sequence: 2, State: m6supply.MergeIntentQueued}
	stale := IntentView{ID: "i3", Sequence: 3, State: m6supply.MergeIntentStale}
	rejected := IntentView{ID: "i4", Sequence: 4, State: m6supply.MergeIntentRejected}

	if plan := PlanFinalGate([]IntentView{merged}); !plan.Ready {
		t.Fatalf("all-merged root not ready: %s", plan.Reason)
	}
	if plan := PlanFinalGate(nil); !plan.Ready {
		t.Fatalf("empty root not ready: %s", plan.Reason)
	}
	if plan := PlanFinalGate([]IntentView{merged, pending}); plan.Ready {
		t.Fatal("pending intent must block the gate")
	}
	if plan := PlanFinalGate([]IntentView{merged, stale}); plan.Ready {
		t.Fatal("stale intent must block the gate")
	}
	if plan := PlanFinalGate([]IntentView{merged, rejected}); plan.Ready || plan.Reason != "rejected intents present" {
		t.Fatalf("rejected intent must hard-block: %+v", plan)
	}
}

func TestRollbackCandidatesReverseOrder(t *testing.T) {
	views := []IntentView{
		{ID: "c", Sequence: 3, State: m6supply.MergeIntentMerged},
		{ID: "a", Sequence: 1, State: m6supply.MergeIntentMerged},
		{ID: "b", Sequence: 2, State: m6supply.MergeIntentMerged},
	}
	got := RollbackCandidates(views)
	want := []string{"c", "b", "a"}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("rollback order LIFO violated: got %v want %v", got, want)
		}
	}
	// the input slice must not be reordered (callers keep merge order)
	if views[0].ID != "c" || views[1].ID != "a" {
		t.Fatal("RollbackCandidates mutated its input")
	}
}

func TestReconcileRecovery(t *testing.T) {
	views := []IntentView{
		{ID: "landed", Sequence: 1, State: m6supply.MergeIntentApplying},
		{ID: "lost", Sequence: 2, State: m6supply.MergeIntentApplying},
		{ID: "cas", Sequence: 3, State: m6supply.MergeIntentCasCheck},
		{ID: "walk", Sequence: 4, State: m6supply.MergeIntentSubmitted},
		{ID: "queue", Sequence: 5, State: m6supply.MergeIntentQueued},
		{ID: "done", Sequence: 6, State: m6supply.MergeIntentMerged},
	}
	actions := ReconcileRecovery(views, "landed", "head-new")
	byID := map[string]RecoveryAction{}
	for _, a := range actions {
		byID[a.IntentID] = a
	}
	if len(actions) != 4 {
		t.Fatalf("expected 4 actions, got %d: %+v", len(actions), actions)
	}
	if byID["landed"].To != m6supply.MergeIntentMerged {
		t.Fatalf("marker-matched applying intent must converge merged, got %s", byID["landed"].To)
	}
	if byID["lost"].To != m6supply.MergeIntentQueued {
		t.Fatalf("interrupted apply must requeue, got %s", byID["lost"].To)
	}
	if byID["cas"].To != m6supply.MergeIntentQueued {
		t.Fatalf("interrupted CAS must requeue, got %s", byID["cas"].To)
	}
	if byID["walk"].To != m6supply.MergeIntentQueued {
		t.Fatalf("interrupted submit walk must requeue, got %s", byID["walk"].To)
	}
	if _, hit := byID["queue"]; hit {
		t.Fatal("queued is its own rest point; no action expected")
	}
	if _, hit := byID["done"]; hit {
		t.Fatal("terminal intents need no recovery")
	}

	// no marker at HEAD: every applying intent requeues (never re-applied
	// blindly, never skipped)
	actions = ReconcileRecovery(views, "", "head-new")
	for _, a := range actions {
		if a.From == m6supply.MergeIntentApplying && a.To != m6supply.MergeIntentQueued {
			t.Fatalf("unmarked applying intent must requeue, got %s", a.To)
		}
	}
}

func TestFinalGateErrors(t *testing.T) {
	if err := ErrFinalizePending; err == nil {
		t.Fatal("ErrFinalizePending must be non-nil")
	}
	if err := ErrFinalizeRejected; err == nil {
		t.Fatal("ErrFinalizeRejected must be non-nil")
	}
}
