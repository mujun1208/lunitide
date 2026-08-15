// T-6.4.2/T-6.4.3 application service: the Root Writer walk and the
// final-tree gate. One writer per root (lease-fenced, MRG-002), intents
// consumed in (root, sequence) total order, head CAS before every apply
// (MRG-001), and every state change committed in the SAME transaction as
// its outbox event and audit row.
//
// The apply step is intentionally transaction-external (git is not
// transactional): the intent is first persisted as applying — the
// EffectJournal entry — then the patch lands, then the merged row and
// events commit. A crash between the last two steps is reconciled by
// Recover() against the HEAD commit marker, so a landed patch is never
// re-applied and a lost one is retried idempotently. A plain apply
// failure also leaves the intent in applying; convergence to queued is a
// recovery action (applying -> merged is the only live edge, keeping
// state machine C strict).
package m6app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/lunitide/lunitide/internal/domain/m6supply"
	"github.com/lunitide/lunitide/internal/merge"
	"github.com/lunitide/lunitide/internal/providerapp"
	"github.com/oklog/ulid/v2"
)

var (
	// ErrMergeNotFound: no such merge intent.
	ErrMergeNotFound = errors.New("m6app: merge intent not found")
	// ErrMergeSequenceConflict: (root, sequence) already carries an
	// intent — the total-order slot is taken.
	ErrMergeSequenceConflict = errors.New("m6app: merge sequence conflict")
	// ErrPatchUnavailable: the patch bytes behind the pinned digest could
	// not be produced (child worktree gone / digest mismatch).
	ErrPatchUnavailable = errors.New("m6app: patch unavailable")
	// ErrApplyFailed: the patch did not apply; the intent stays applying
	// until Recover() requeues it.
	ErrApplyFailed = errors.New("m6app: patch apply failed")
	// ErrMergeServiceUnavailable: service not wired.
	ErrMergeServiceUnavailable = errors.New("m6app: merge service unavailable")
)

// HeadResolver reads the root tree's current head (production: git
// rev-parse on the main tree; tests: in-memory map).
type HeadResolver func(ctx context.Context, rootID string) (string, error)

// PatchSource produces the patch bytes for one pinned digest (production:
// the child worktree export verified against the digest; tests: fake).
type PatchSource func(ctx context.Context, rootID, childID, patchDigest string) ([]byte, error)

// PatchApplier applies verified patch bytes to the final tree and returns
// the new head (production: WorktreeManager.ApplyToRoot; tests: fake).
type PatchApplier func(ctx context.Context, rootID, intentID string, patch []byte, expectedHead string) (string, error)

// HeadMarkerReader reads the intent id stamped into the final-tree HEAD
// commit (crash-recovery ground truth; tests: fake).
type HeadMarkerReader func(ctx context.Context, rootID string) (string, error)

// SubmitMergeRequest is the merge.submit wire shape.
type SubmitMergeRequest struct {
	RootID       string
	Sequence     int64
	ChildID      string
	ExpectedHead string
	PatchDigest  string
	TestsRef     string
}

// SubmitMergeResult answers merge.submit.
type SubmitMergeResult struct {
	IntentID string `json:"intentId"`
	State    string `json:"state"` // queued | stale
}

// ProcessResult is one writer step outcome.
type ProcessResult struct {
	IntentID string
	State    string // merged | stale | empty
	NewHead  string
}

// FinalizeResult reports the gate verdict.
type FinalizeResult struct {
	RootID             string
	State              string // complete | recovery_required
	FinalDigest        string
	RollbackCandidates []string
}

// MergeService implements merge.submit, the writer walk, recovery and
// the final gate.
type MergeService struct {
	uow    UnitOfWork
	outbox *OutboxService
	gates  merge.TestGate
	heads  HeadResolver
	patch  PatchSource
	apply  PatchApplier
	marker HeadMarkerReader
	clock  Clock

	mu     sync.Mutex
	leases map[string]*merge.WriterLease
}

func NewMergeService(uow UnitOfWork, outbox *OutboxService, gates merge.TestGate,
	heads HeadResolver, patch PatchSource, apply PatchApplier, marker HeadMarkerReader) *MergeService {
	return &MergeService{
		uow: uow, outbox: outbox, gates: gates, heads: heads, patch: patch,
		apply: apply, marker: marker, clock: systemClock{}, leases: map[string]*merge.WriterLease{},
	}
}

// SetClock substitutes the wall clock (tests).
func (s *MergeService) SetClock(c Clock) { s.clock = c }

// writerLeaseTTL bounds one writer epoch (a crashed writer loses the
// fence after this and a new epoch may claim the root).
const writerLeaseTTL = 60 * time.Second

// AcquireWriterLease claims or renews the single-writer fence for a root.
// A live non-expired lease held by another owner is refused (fenced).
func (s *MergeService) AcquireWriterLease(rootID, owner string) (merge.WriterLease, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.clock.Now().UTC()
	if held := s.leases[rootID]; held != nil && now.Before(held.ExpiresAt) && held.Owner != owner {
		return merge.WriterLease{}, fmt.Errorf("%w: root %s held by %s", merge.ErrWriterFenced, rootID, held.Owner)
	}
	epoch := int64(1)
	if held := s.leases[rootID]; held != nil && held.Owner == owner {
		epoch = held.Epoch // renewal keeps the epoch
	} else if held != nil {
		epoch = held.Epoch + 1 // takeover after expiry advances the epoch
	}
	lease := merge.WriterLease{RootID: rootID, Epoch: epoch, Owner: owner, ExpiresAt: now.Add(writerLeaseTTL)}
	s.leases[rootID] = &lease
	return lease, nil
}

// checkFence validates a held lease against the current fence.
func (s *MergeService) checkFence(held merge.WriterLease) error {
	s.mu.Lock()
	current := s.leases[held.RootID]
	s.mu.Unlock()
	return merge.CheckFencing(held, current, s.clock.Now().UTC())
}

// Submit validates the total-order slot, fast-fails the head CAS (the
// writer re-checks under the fence before applying) and lands the intent
// as queued or stale — with audit and outbox rows in the same
// transaction, and the idempotency record last.
func (s *MergeService) Submit(ctx context.Context, key string, req SubmitMergeRequest) (SubmitMergeResult, error) {
	if s == nil || s.uow == nil {
		return SubmitMergeResult{}, ErrMergeServiceUnavailable
	}
	digestIn := requestDigestOf("merge.submit", req.RootID, strconv.FormatInt(req.Sequence, 10), req.PatchDigest)
	var result SubmitMergeResult
	err := s.uow.TransactM6(ctx, func(tx Tx) error {
		now := s.clock.Now().UTC()
		if record, found, err := tx.Idempotency("merge.submit", key, now); err != nil {
			return err
		} else if found {
			if record.Digest != digestIn {
				return ErrIdempotencyConflict
			}
			return json.Unmarshal(record.Response, &result)
		}
		if _, err := tx.GetM6MergeIntentBySequence(req.RootID, req.Sequence); err == nil {
			return ErrMergeSequenceConflict
		} else if !errors.Is(err, m6supply.ErrNotFound) {
			return err
		}
		// UNIQUE(root_id, sequence) is the hard stop for the race window
		// fast-fail CAS: expectedHead against the live root head
		state := m6supply.MergeIntentQueued
		currentHead := ""
		if s.heads != nil {
			head, err := s.heads(ctx, req.RootID)
			if err != nil {
				return err
			}
			if merge.ResolveCAS(req.ExpectedHead, head) == merge.CASStale {
				state = m6supply.MergeIntentStale
				currentHead = head // the stale verdict records what was seen
			}
		}
		row := m6supply.MergeIntent{
			ID: ulid.Make().String(), RootID: req.RootID, ChildID: req.ChildID,
			Sequence: req.Sequence, ExpectedHead: req.ExpectedHead, CurrentHead: currentHead,
			PatchDigest: req.PatchDigest, TestsRef: req.TestsRef,
			State: state, Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		if err := tx.PutM6MergeIntent(row); err != nil {
			return err
		}
		if err := s.auditMerge(tx, "merge.submitted", row.ID, row.RootID, row.State, row.Sequence); err != nil {
			return err
		}
		if s.outbox != nil {
			if err := s.outbox.AppendTx(tx, "merge_intent", row.ID, "merge.submitted", mergeEventPayload(row)); err != nil {
				return err
			}
		}
		result = SubmitMergeResult{IntentID: row.ID, State: row.State}
		return tx.PutIdempotency(providerapp.Record{
			Operation: "merge.submit", Key: key, Digest: digestIn,
			Response: marshalJSON(result), CreatedAt: now, ExpiresAt: now.Add(24 * time.Hour),
		})
	})
	if errors.Is(err, m6supply.ErrSequenceTaken) {
		err = ErrMergeSequenceConflict
	}
	return result, err
}

// ProcessNext runs one writer step for a root: the head intent of the
// total order walks [rebase_required ->] queued -> cas_check ->
// (applying [+ merged] | stale). The apply itself is transaction-external
// (see the package comment); its EffectJournal entry is the durable
// applying row.
func (s *MergeService) ProcessNext(ctx context.Context, rootID string, held merge.WriterLease) (ProcessResult, error) {
	if s == nil || s.uow == nil {
		return ProcessResult{}, ErrMergeServiceUnavailable
	}
	if err := s.checkFence(held); err != nil {
		return ProcessResult{}, err
	}
	var intent m6supply.MergeIntent
	err := s.uow.TransactM6(ctx, func(tx Tx) error {
		intents, err := tx.ListM6MergeIntentsByRoot(rootID)
		if err != nil {
			return err
		}
		next, err := merge.NextIntent(intentViews(intents))
		if err != nil {
			return err
		}
		row, err := tx.GetM6MergeIntent(next.ID)
		if err != nil {
			return err
		}
		now := s.clock.Now().UTC()
		// the writer may only resume from the two rest points
		from := row.State
		if from != m6supply.MergeIntentQueued && from != m6supply.MergeIntentRebaseNeeded {
			return fmt.Errorf("head intent %s in state %s", row.ID, from)
		}
		if s.heads == nil {
			return ErrMergeServiceUnavailable
		}
		head, err := s.heads(ctx, rootID)
		if err != nil {
			return err
		}
		if merge.ResolveCAS(row.ExpectedHead, head) == merge.CASStale {
			if err := walkFrom(from, m6supply.MergeIntentCasCheck, m6supply.MergeIntentStale); err != nil {
				return err
			}
			stale, err := tx.UpdateM6MergeIntentState(row.ID, row.Version, m6supply.MergeIntentStale, &head, now)
			if err != nil {
				return err
			}
			intent = stale
			return nil
		}
		if err := walkFrom(from, m6supply.MergeIntentCasCheck, m6supply.MergeIntentApplying); err != nil {
			return err
		}
		applying, err := tx.UpdateM6MergeIntentState(row.ID, row.Version, m6supply.MergeIntentApplying, nil, now)
		if err != nil {
			return err
		}
		intent = applying
		return nil
	})
	if err != nil {
		if errors.Is(err, merge.ErrNoIntent) {
			return ProcessResult{State: "empty"}, nil
		}
		return ProcessResult{}, err
	}
	if intent.State == m6supply.MergeIntentStale {
		if err := s.recordStale(ctx, intent); err != nil {
			return ProcessResult{}, err
		}
		return ProcessResult{IntentID: intent.ID, State: intent.State}, nil
	}
	// applying: the EffectJournal entry is durable; land the patch
	// outside the transaction, then commit merged.
	if s.patch == nil || s.apply == nil {
		return ProcessResult{}, ErrMergeServiceUnavailable
	}
	patch, err := s.patch(ctx, rootID, intent.ChildID, intent.PatchDigest)
	if err != nil {
		return ProcessResult{IntentID: intent.ID, State: intent.State}, fmt.Errorf("%w: %v", ErrPatchUnavailable, err)
	}
	newHead, applyErr := s.apply(ctx, rootID, intent.ID, patch, intent.ExpectedHead)
	if applyErr != nil {
		// the intent stays applying; Recover() requeues it
		return ProcessResult{IntentID: intent.ID, State: intent.State}, fmt.Errorf("%w: %v", ErrApplyFailed, applyErr)
	}
	var out ProcessResult
	err = s.uow.TransactM6(ctx, func(tx Tx) error {
		now := s.clock.Now().UTC()
		fresh, err := tx.GetM6MergeIntent(intent.ID)
		if err != nil {
			return err
		}
		if fresh.State != m6supply.MergeIntentApplying {
			return fmt.Errorf("intent %s left applying as %s", intent.ID, fresh.State)
		}
		merged, err := tx.UpdateM6MergeIntentState(fresh.ID, fresh.Version, m6supply.MergeIntentMerged, &newHead, now)
		if err != nil {
			return err
		}
		if err := s.auditMerge(tx, "merge.merged", merged.ID, merged.RootID, merged.State, merged.Sequence); err != nil {
			return err
		}
		if s.outbox != nil {
			if err := s.outbox.AppendTx(tx, "merge_intent", merged.ID, "merge.merged", mergeEventPayload(merged)); err != nil {
				return err
			}
		}
		out = ProcessResult{IntentID: merged.ID, State: merged.State, NewHead: newHead}
		return nil
	})
	if err != nil {
		return ProcessResult{}, err
	}
	return out, nil
}

// walkFrom validates the state-machine walk compressed into one step:
// queued -> cas_check -> to, or rebase_required -> queued -> cas_check -> to.
func walkFrom(from string, through ...string) error {
	edges := make([]string, 0, len(through)+2)
	edges = append(edges, from)
	if from == m6supply.MergeIntentRebaseNeeded {
		edges = append(edges, m6supply.MergeIntentQueued)
	}
	edges = append(edges, through...)
	return merge.WalkStep(edges...)
}

// recordStale appends the audit + outbox rows for a CAS-conflict verdict.
func (s *MergeService) recordStale(ctx context.Context, intent m6supply.MergeIntent) error {
	return s.uow.TransactM6(ctx, func(tx Tx) error {
		if err := s.auditMerge(tx, "merge.stale", intent.ID, intent.RootID, intent.State, intent.Sequence); err != nil {
			return err
		}
		if s.outbox != nil {
			return s.outbox.AppendTx(tx, "merge_intent", intent.ID, "merge.stale", mergeEventPayload(intent))
		}
		return nil
	})
}

// Recover reconciles a crashed writer walk: intents stuck mid-walk are
// converged (landed effects recorded merged at the observed head;
// everything else requeued) and stale intents advance to
// rebase_required so the next ProcessNext pass picks them up in total
// order (MRG-001).
func (s *MergeService) Recover(ctx context.Context, rootID string) ([]merge.RecoveryAction, error) {
	if s == nil || s.uow == nil {
		return nil, ErrMergeServiceUnavailable
	}
	headIntent := ""
	if s.marker != nil {
		id, err := s.marker(ctx, rootID)
		if err != nil {
			return nil, err
		}
		headIntent = id
	}
	observedHead := ""
	if s.heads != nil {
		h, err := s.heads(ctx, rootID)
		if err != nil {
			return nil, err
		}
		observedHead = h
	}
	var actions []merge.RecoveryAction
	err := s.uow.TransactM6(ctx, func(tx Tx) error {
		intents, err := tx.ListM6MergeIntentsByRoot(rootID)
		if err != nil {
			return err
		}
		views := intentViews(intents)
		actions = merge.ReconcileRecovery(views, headIntent, observedHead)
		now := s.clock.Now().UTC()
		for _, act := range actions {
			row, err := tx.GetM6MergeIntent(act.IntentID)
			if err != nil {
				return err
			}
			var headPtr *string
			if act.To == m6supply.MergeIntentMerged {
				observed := observedHead
				headPtr = &observed
			}
			if _, err := tx.UpdateM6MergeIntentState(row.ID, row.Version, act.To, headPtr, now); err != nil {
				return err
			}
		}
		// advance stale intents to rebase_required, pinning the serial
		// rebase target as the new expected head (MRG-001: the Root Writer
		// rebases stale children in total order against the current head;
		// the child never re-runs). Without the pin the requeued intent
		// could never pass CAS again.
		for _, v := range views {
			if v.State != m6supply.MergeIntentStale {
				continue
			}
			row, err := tx.GetM6MergeIntent(v.ID)
			if err != nil {
				return err
			}
			if observedHead != "" {
				if _, err := tx.UpdateM6MergeIntentRebased(row.ID, row.Version, observedHead, now); err != nil {
					return err
				}
				continue
			}
			if _, err := tx.UpdateM6MergeIntentState(row.ID, row.Version, m6supply.MergeIntentRebaseNeeded, nil, now); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return actions, nil
}

// FinalizeRoot opens the final-tree gate once every intent is merged:
// testing event -> gate run -> complete (final digest) or
// final_test_failed -> recovery_required with reverse-order rollback
// candidates. A failing gate NEVER completes the root (TST-001).
func (s *MergeService) FinalizeRoot(ctx context.Context, rootID string) (FinalizeResult, error) {
	if s == nil || s.uow == nil {
		return FinalizeResult{}, ErrMergeServiceUnavailable
	}
	intents, err := s.listIntents(ctx, rootID)
	if err != nil {
		return FinalizeResult{}, err
	}
	plan := merge.PlanFinalGate(intentViews(intents))
	if !plan.Ready {
		if plan.Reason == "rejected intents present" {
			return FinalizeResult{}, merge.ErrFinalizeRejected
		}
		return FinalizeResult{}, merge.ErrFinalizePending
	}
	if s.heads == nil || s.gates == nil {
		return FinalizeResult{}, ErrMergeServiceUnavailable
	}
	treeHead, err := s.heads(ctx, rootID)
	if err != nil {
		return FinalizeResult{}, err
	}
	if err := s.emitFinal(ctx, rootID, "final.testing",
		string(marshalJSON(finalTestingPayload{RootID: rootID, TreeHead: treeHead}))); err != nil {
		return FinalizeResult{}, err
	}
	outcome, gateErr := s.gates.RunFinalTests(ctx, rootID, treeHead)
	candidates := merge.RollbackCandidates(intentViews(intents))
	if gateErr != nil || !outcome.Passed {
		detail := outcome.Detail
		if gateErr != nil {
			detail = gateErr.Error()
		}
		if candidates == nil {
			candidates = []string{}
		}
		payload := string(marshalJSON(finalFailedPayload{
			RootID: rootID, Detail: detail, RollbackCandidates: candidates,
		}))
		if err := s.emitFinal(ctx, rootID, "final.failed", payload); err != nil {
			return FinalizeResult{}, err
		}
		return FinalizeResult{RootID: rootID, State: m6supply.FinalGateRecovery, RollbackCandidates: candidates}, nil
	}
	payload := string(marshalJSON(finalCompletedPayload{
		RootID: rootID, FinalDigest: outcome.FinalDigest, TestsRef: outcome.TestsRef,
	}))
	if err := s.emitFinal(ctx, rootID, "final.completed", payload); err != nil {
		return FinalizeResult{}, err
	}
	return FinalizeResult{RootID: rootID, State: m6supply.FinalGateComplete, FinalDigest: outcome.FinalDigest}, nil
}

func (s *MergeService) listIntents(ctx context.Context, rootID string) ([]m6supply.MergeIntent, error) {
	var intents []m6supply.MergeIntent
	err := s.uow.TransactM6(ctx, func(tx Tx) error {
		var ierr error
		intents, ierr = tx.ListM6MergeIntentsByRoot(rootID)
		return ierr
	})
	return intents, err
}

// mergeEventFields is the outbox payload for merge lifecycle events.
type mergeEventFields struct {
	IntentID    string `json:"intentId"`
	RootID      string `json:"rootId"`
	Sequence    int64  `json:"sequence"`
	State       string `json:"state"`
	PatchDigest string `json:"patchDigest"`
}

// finalTestingPayload is the outbox payload for final.testing.
type finalTestingPayload struct {
	RootID   string `json:"rootId"`
	TreeHead string `json:"treeHead"`
}

// finalFailedPayload is the outbox payload for final.failed.
type finalFailedPayload struct {
	RootID             string   `json:"rootId"`
	Detail             string   `json:"detail"`
	RollbackCandidates []string `json:"rollbackCandidates"`
}

// finalCompletedPayload is the outbox payload for final.completed.
type finalCompletedPayload struct {
	RootID      string `json:"rootId"`
	FinalDigest string `json:"finalDigest"`
	TestsRef    string `json:"testsRef"`
}

func (s *MergeService) emitFinal(ctx context.Context, rootID, eventType, payload string) error {
	return s.uow.TransactM6(ctx, func(tx Tx) error {
		if err := s.auditMerge(tx, eventType, rootID, rootID, eventType, 0); err != nil {
			return err
		}
		if s.outbox != nil {
			return s.outbox.AppendTx(tx, "merge_final", rootID, eventType, payload)
		}
		return nil
	})
}

// auditMerge writes the 0049 audit row for a merge/final lifecycle fact.
func (s *MergeService) auditMerge(tx Tx, action, aggregateID, rootID, state string, sequence int64) error {
	type meta struct {
		RootID   string `json:"rootId"`
		State    string `json:"state"`
		Sequence int64  `json:"sequence"`
	}
	return tx.PutAudit(providerapp.Audit{
		ID: ulid.Make().String(), Action: action, AggregateID: aggregateID,
		Actor: delegationActor, CreatedAt: s.clock.Now().UTC(),
		Metadata: marshalJSON(meta{RootID: rootID, State: state, Sequence: sequence}),
	})
}

func intentViews(rows []m6supply.MergeIntent) []merge.IntentView {
	out := make([]merge.IntentView, 0, len(rows))
	for _, r := range rows {
		out = append(out, merge.IntentView{
			ID: r.ID, RootID: r.RootID, Sequence: r.Sequence,
			ExpectedHead: r.ExpectedHead, CurrentHead: r.CurrentHead, State: r.State,
		})
	}
	return out
}

func mergeEventPayload(row m6supply.MergeIntent) string {
	return string(marshalJSON(mergeEventFields{
		IntentID: row.ID, RootID: row.RootID, Sequence: row.Sequence,
		State: row.State, PatchDigest: row.PatchDigest,
	}))
}
