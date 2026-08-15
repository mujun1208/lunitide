// finalize.go (T-6.4.3) is the final-tree test gate and the crash
// recovery reconciler.
//
// The gate stands between "every intent merged" and COMPLETE: the final
// tree gets a fresh dependency install and a full test/scan run, and only
// a passing report may flip the root to COMPLETE. A failing report freezes
// the root in RECOVERY_REQUIRED with evidence retained and rollback
// candidates generated in reverse merge order (TST-001: a failed final
// run NEVER completes — child-local test passes are not a substitute).
//
// Recovery is EffectJournal replay in effect: intent state + the outbox
// rows committed alongside it are the journal, and the git HEAD marker is
// ground truth for whether an APPLYING intent's effect actually landed.
// Reconciling the two never re-applies a landed patch and never skips a
// lost one (RTO bound comes from the writer loop cadence, not a rebuild).
package merge

import (
	"context"
	"errors"
	"fmt"

	"github.com/lunitide/lunitide/internal/domain/m6supply"
)

// ErrFinalizePending: the root still has non-terminal intents; the gate
// may not open.
var ErrFinalizePending = errors.New("merge: intents still pending")

// ErrFinalizeRejected: the root carries a rejected intent; merging more or
// finalizing requires a root decision (retry or drop the child).
var ErrFinalizeRejected = errors.New("merge: root has rejected intents")

// TestOutcome is the gate report for one final-tree run.
type TestOutcome struct {
	Passed      bool
	TestsRef    string // evidence reference (report artifact)
	FinalDigest string // final-tree digest handed to M7
	Detail      string
}

// TestGate runs the final-tree verification (reinstall locked deps, full
// test/scan). Production wires the command service; tests fake it.
type TestGate interface {
	RunFinalTests(ctx context.Context, rootID, treeHead string) (TestOutcome, error)
}

// FinalPlan decides whether the gate may open for a root.
type FinalPlan struct {
	Ready  bool
	Reason string
}

// PlanFinalGate requires every intent of the root to be merged. Pending
// intents block the gate; rejected intents block it harder (a decision is
// required, not just patience).
func PlanFinalGate(intents []IntentView) FinalPlan {
	for _, v := range intents {
		switch v.State {
		case m6supply.MergeIntentMerged:
			continue
		case m6supply.MergeIntentRejected:
			return FinalPlan{Reason: "rejected intents present"}
		default:
			if !v.Terminal() {
				return FinalPlan{Reason: fmt.Sprintf("intent %s still %s", v.ID, v.State)}
			}
		}
	}
	return FinalPlan{Ready: true}
}

// RollbackCandidates lists merged intents in reverse merge order — the
// LIFO rollback proposal the operator may apply after FINAL_TEST_FAILED.
func RollbackCandidates(merged []IntentView) []string {
	ordered := append([]IntentView(nil), merged...)
	for i := 1; i < len(ordered); i++ {
		for j := i; j > 0 && ordered[j].Sequence > ordered[j-1].Sequence; j-- {
			ordered[j], ordered[j-1] = ordered[j-1], ordered[j]
		}
	}
	out := make([]string, 0, len(ordered))
	for _, v := range ordered {
		out = append(out, v.ID)
	}
	return out
}

// RecoveryAction is one convergence step for a crashed writer walk.
type RecoveryAction struct {
	IntentID string
	From     string
	To       string // converged state
	Reason   string
}

// ReconcileRecovery converges non-terminal intents after a crash.
// headIntent is the intent id stamped into the final tree's HEAD commit
// ("" when HEAD carries no marker or predates delegation). The applying
// intent whose marker sits at HEAD already landed: it is recorded merged
// at the observed head (never re-applied). Everything else resets to the
// queue and the writer loop retries with the idempotent apply.
func ReconcileRecovery(intents []IntentView, headIntent, observedHead string) []RecoveryAction {
	var actions []RecoveryAction
	for _, v := range intents {
		if v.Terminal() {
			continue
		}
		switch v.State {
		case m6supply.MergeIntentApplying:
			if v.ID == headIntent && observedHead != "" {
				actions = append(actions, RecoveryAction{
					IntentID: v.ID, From: v.State, To: m6supply.MergeIntentMerged,
					Reason: "effect landed at HEAD (marker match); record merged at " + observedHead,
				})
				continue
			}
			actions = append(actions, RecoveryAction{
				IntentID: v.ID, From: v.State, To: m6supply.MergeIntentQueued,
				Reason: "apply interrupted before landing; idempotent retry",
			})
		case m6supply.MergeIntentCasCheck:
			actions = append(actions, RecoveryAction{
				IntentID: v.ID, From: v.State, To: m6supply.MergeIntentQueued,
				Reason: "CAS interrupted; re-run compare",
			})
		case m6supply.MergeIntentSubmitted, m6supply.MergeIntentValidating:
			actions = append(actions, RecoveryAction{
				IntentID: v.ID, From: v.State, To: m6supply.MergeIntentQueued,
				Reason: "submit walk interrupted; requeue",
			})
			// queued / stale / rebase_required states are already their
			// own durable rest points — the writer loop picks them up.
		}
	}
	return actions
}
