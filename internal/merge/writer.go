// writer.go (T-6.4.2) is the Root Writer discipline: one writer per root,
// merge intents consumed in (root, sequence) total order, head CAS before
// every apply, and a fencing lease so a stale writer epoch can never touch
// the final tree again (MRG-002). The domain rules here are pure — the
// transactional walk lives in m6app.
//
// State machine C (0046 m6_merge_intent.state CHECK set):
//
//	submitted -> validating -> queued -> cas_check -> applying -> merged
//	                    └-> rejected        └-> stale -> rebase_required
//
// rebase_required -> queued is the requeue edge after a serial rebase
// (MRG-001: the Root Writer rebases stale children in total order; the
// child never re-runs). merged/rejected are terminal.
package merge

import (
	"errors"
	"fmt"
	"time"

	"github.com/lunitide/lunitide/internal/domain/m6supply"
)

// ErrWriterFenced maps to M6-MRG-002: an operation attempted under a
// fencing lease that is no longer the current epoch (or has expired). The
// writer must stop immediately — a fenced writer touching the final tree
// is a P0 freeze.
var ErrWriterFenced = errors.New("merge: writer fenced (MRG-002)")

// ErrTransitionIllegal: the requested state change is not an edge of
// state machine C.
var ErrTransitionIllegal = errors.New("merge: illegal intent transition")

// WriterLease is the single-writer fence for one root. Epoch is
// monotonically increasing; only the newest non-expired epoch may run.
type WriterLease struct {
	RootID    string
	Epoch     int64
	Owner     string
	ExpiresAt time.Time
}

// CheckFencing validates that this lease still governs the root at `now`
// against the currently recorded lease. A nil current means no writer has
// claimed the root yet.
func CheckFencing(held WriterLease, current *WriterLease, now time.Time) error {
	if now.UTC().After(held.ExpiresAt) {
		return fmt.Errorf("%w: lease expired at %s", ErrWriterFenced, held.ExpiresAt.Format(time.RFC3339))
	}
	if current != nil {
		if current.Epoch > held.Epoch {
			return fmt.Errorf("%w: epoch %d superseded by %d", ErrWriterFenced, held.Epoch, current.Epoch)
		}
	}
	return nil
}

// CASOutcome is the verdict of comparing an intent's expected head with
// the root's current head.
type CASOutcome int

const (
	CASMatch CASOutcome = iota
	CASStale            // MRG-001: expected != current, rebase path
)

// ResolveCAS is the head compare-and-swap verdict.
func ResolveCAS(expectedHead, currentHead string) CASOutcome {
	if expectedHead == currentHead {
		return CASMatch
	}
	return CASStale
}

// transitions is the edge set of state machine C (state names mirror the
// 0046 m6_merge_intent.state CHECK set via the m6supply constants).
var transitions = map[string]map[string]bool{
	m6supply.MergeIntentSubmitted:    {m6supply.MergeIntentValidating: true},
	m6supply.MergeIntentValidating:   {m6supply.MergeIntentQueued: true, m6supply.MergeIntentRejected: true},
	m6supply.MergeIntentQueued:       {m6supply.MergeIntentCasCheck: true},
	m6supply.MergeIntentCasCheck:     {m6supply.MergeIntentApplying: true, m6supply.MergeIntentStale: true},
	m6supply.MergeIntentApplying:     {m6supply.MergeIntentMerged: true},
	m6supply.MergeIntentStale:        {m6supply.MergeIntentRebaseNeeded: true},
	m6supply.MergeIntentRebaseNeeded: {m6supply.MergeIntentQueued: true},
	m6supply.MergeIntentMerged:       {},
	m6supply.MergeIntentRejected:     {},
}

// ValidateTransition enforces the edge set.
func ValidateTransition(from, to string) error {
	edges, ok := transitions[from]
	if !ok {
		return fmt.Errorf("%w: unknown state %q", ErrTransitionIllegal, from)
	}
	if !edges[to] {
		return fmt.Errorf("%w: %s -> %s", ErrTransitionIllegal, from, to)
	}
	return nil
}

// WalkStep chains the legal internal edges a single writer iteration may
// compress (submitted -> validating -> queued happens inside one
// transaction at submit; queued -> cas_check -> applying -> merged inside
// one apply transaction). It validates each hop in order.
func WalkStep(edges ...string) error {
	for i := 0; i+1 < len(edges); i++ {
		if err := ValidateTransition(edges[i], edges[i+1]); err != nil {
			return err
		}
	}
	return nil
}

// IntentView is the storage-independent projection the writer rules
// operate on (ordering, CAS, terminality).
type IntentView struct {
	ID           string
	RootID       string
	Sequence     int64
	ExpectedHead string
	CurrentHead  string
	State        string
}

// Terminal says the intent left the queue for good.
func (v IntentView) Terminal() bool {
	return v.State == m6supply.MergeIntentMerged || v.State == m6supply.MergeIntentRejected
}

// NextIntent picks the head of the total order: lowest sequence among
// non-terminal intents. ErrNoIntent when the queue is drained.
var ErrNoIntent = errors.New("merge: no pending intent")

func NextIntent(intents []IntentView) (IntentView, error) {
	var best *IntentView
	for i := range intents {
		v := &intents[i]
		if v.Terminal() {
			continue
		}
		if best == nil || v.Sequence < best.Sequence {
			best = v
		}
	}
	if best == nil {
		return IntentView{}, ErrNoIntent
	}
	return *best, nil
}

// ValidateOrder rejects a claim to sequence n while an earlier sequence
// for the same root is still unknown-but-referenced: the writer never
// applies sequence k+1 before k is terminal. Submissions may arrive out
// of order; applications may not.
func ValidateOrder(pending IntentView, known []IntentView) error {
	for _, v := range known {
		if v.RootID != pending.RootID || v.Terminal() {
			continue
		}
		if v.Sequence < pending.Sequence {
			return fmt.Errorf("merge: intent %s (seq %d) waits for seq %d", pending.ID, pending.Sequence, v.Sequence)
		}
	}
	return nil
}

// RebaseAdvance is the serial-rebase verdict for a stale intent: after
// the root head moved, the child's patch must be replayed on the new
// base. The domain rule only decides the requeue edge and the new
// expected head; the textual rebase itself is a git operation.
type RebaseAdvance struct {
	IntentID        string
	NewExpectedHead string
	Edge            [2]string // rebase_required -> queued
}

// PlanRebase walks stale intents in total order and produces their
// requeue plan against the root's current head.
func PlanRebase(stale []IntentView, currentHead string) []RebaseAdvance {
	ordered := append([]IntentView(nil), stale...)
	for i := 1; i < len(ordered); i++ {
		for j := i; j > 0 && ordered[j].Sequence < ordered[j-1].Sequence; j-- {
			ordered[j], ordered[j-1] = ordered[j-1], ordered[j]
		}
	}
	out := make([]RebaseAdvance, 0, len(ordered))
	for _, v := range ordered {
		out = append(out, RebaseAdvance{
			IntentID: v.ID, NewExpectedHead: currentHead,
			Edge: [2]string{m6supply.MergeIntentRebaseNeeded, m6supply.MergeIntentQueued},
		})
	}
	return out
}
