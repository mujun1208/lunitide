// T-6.3.4 application service: the JoinBarrier. One barrier row per root
// fan-out; children arrive with an outcome and the barrier closes exactly
// once per policy:
//
//	ALL        closes when every expected child has arrived
//	QUORUM     closes when q successes arrive, or when all children
//	           arrived below quorum (the shortfall is durable evidence)
//	FAIL_FAST  closes on the first non-success outcome, or when all arrive
//
// Invariants (M6_ERROR_CATALOG_V2):
//
//	every child settles exactly once — UNIQUE(barrier_id, child_id);
//	duplicate arrivals answer the existing settlement (JOIN-001) and
//	never refund the budget twice
//	CLOSED is terminal — a late arrival is recorded as evidence but can
//	neither reopen the barrier nor flip the verdict
//
// Arrivals settle in the caller's transaction (the wire method runs one
// TransactM6; delegation.settle composes arriveTx with the budget consume
// so arrival + settlement + conservation audit commit atomically).
package m6app

import (
	"context"
	"errors"
	"fmt"

	"github.com/lunitide/lunitide/internal/domain/m6supply"
	"github.com/lunitide/lunitide/internal/providerapp"
	"github.com/oklog/ulid/v2"
)

var (
	// ErrBarrierNotFound: the barrier row does not exist.
	ErrBarrierNotFound = errors.New("m6app: barrier not found")
	// ErrBarrierPolicy: policy/quorum combination invalid for creation.
	ErrBarrierPolicy = errors.New("m6app: barrier policy invalid")
	// ErrBarrierClosed maps to M6-JOIN-001 on paths that must not treat a
	// late arrival as evidence-only (e.g. settling into a closed barrier
	// with a different verdict).
	ErrBarrierClosed = errors.New("m6app: barrier already closed")
)

// ArriveResult is the settle outcome for one child.
type ArriveResult struct {
	State          string // open | closed
	AlreadySettled bool   // duplicate arrival answered with the existing row
	ClosedReason   string // set when this arrival closed the barrier
}

// BarrierService implements barrier create + arrive.
type BarrierService struct {
	uow   UnitOfWork
	clock Clock
}

func NewBarrierService(uow UnitOfWork) *BarrierService {
	return &BarrierService{uow: uow, clock: systemClock{}}
}

// SetClock substitutes the wall clock (tests).
func (s *BarrierService) SetClock(c Clock) { s.clock = c }

// Create opens one barrier for a root fan-out. QUORUM requires
// 1 <= quorum <= expectedChildren; ALL/FAIL_FAST take no quorum.
func (s *BarrierService) Create(ctx context.Context, rootID, policy string, expectedChildren, quorum int) (m6supply.Barrier, error) {
	if s == nil || s.uow == nil {
		return m6supply.Barrier{}, ErrServiceUnavailable
	}
	switch policy {
	case m6supply.BarrierPolicyAll, m6supply.BarrierPolicyFailFast:
		quorum = 0
	case m6supply.BarrierPolicyQuorum:
		if quorum < 1 || quorum > expectedChildren {
			return m6supply.Barrier{}, ErrBarrierPolicy
		}
	default:
		return m6supply.Barrier{}, ErrBarrierPolicy
	}
	if expectedChildren < 1 || expectedChildren > 100 {
		return m6supply.Barrier{}, ErrBarrierPolicy
	}
	var out m6supply.Barrier
	err := s.uow.TransactM6(ctx, func(tx Tx) error {
		now := s.clock.Now().UTC()
		out = m6supply.Barrier{
			ID: ulid.Make().String(), RootID: rootID, Policy: policy,
			ExpectedChildren: expectedChildren, Quorum: quorum,
			State: m6supply.BarrierStateOpen, Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		if err := tx.PutM6Barrier(out); err != nil {
			return err
		}
		return tx.PutAudit(providerapp.Audit{
			ID: ulid.Make().String(), Action: "barrier.created",
			AggregateID: out.ID, Actor: delegationActor, CreatedAt: now,
			Metadata: barrierAuditMeta(out.ID, rootID, "", out.State),
		})
	})
	return out, err
}

// Arrive settles one child outcome. Duplicate arrivals are idempotent
// (JOIN-001 semantics: answer the existing settlement, no second refund);
// arrivals after CLOSE are recorded as late evidence and never change the
// closed state.
func (s *BarrierService) Arrive(ctx context.Context, barrierID, childID string, attempt int64, outcome, resultDigest string) (ArriveResult, error) {
	if s == nil || s.uow == nil {
		return ArriveResult{}, ErrServiceUnavailable
	}
	if outcome != m6supply.ArrivalSucceeded && outcome != m6supply.ArrivalFailed &&
		outcome != m6supply.ArrivalCancelled && outcome != m6supply.ArrivalExpired {
		return ArriveResult{}, ErrBarrierPolicy
	}
	var out ArriveResult
	err := s.uow.TransactM6(ctx, func(tx Tx) error {
		result, err := s.ArriveTx(tx, barrierID, childID, attempt, outcome, resultDigest)
		out = result
		return err
	})
	return out, err
}

// ArriveTx is the transactional core shared with DelegationService.Settle
// (arrival + budget consume + conservation audit commit atomically).
func (s *BarrierService) ArriveTx(tx Tx, barrierID, childID string, attempt int64, outcome, resultDigest string) (ArriveResult, error) {
	now := s.clock.Now().UTC()
	barrier, err := tx.GetM6Barrier(barrierID)
	if errors.Is(err, m6supply.ErrNotFound) {
		return ArriveResult{}, ErrBarrierNotFound
	}
	if err != nil {
		return ArriveResult{}, err
	}
	// duplicate arrival: answer the existing settlement (JOIN-001)
	if existing, err := tx.GetM6BarrierArrival(barrierID, childID); err == nil {
		_ = existing
		return ArriveResult{State: barrier.State, AlreadySettled: true}, nil
	} else if !errors.Is(err, m6supply.ErrNotFound) {
		return ArriveResult{}, err
	}
	// late arrival after CLOSE: evidence only, never reopens
	if barrier.State == m6supply.BarrierStateClosed {
		late := m6supply.BarrierArrival{
			ID: ulid.Make().String(), BarrierID: barrierID, ChildID: childID,
			Attempt: attempt, Outcome: outcome, ResultDigest: resultDigest, ArrivedAt: now,
		}
		if err := tx.PutM6BarrierArrival(late); err != nil {
			return ArriveResult{}, err
		}
		if err := tx.PutAudit(providerapp.Audit{
			ID: ulid.Make().String(), Action: "barrier.arrived",
			AggregateID: barrierID, Actor: delegationActor, CreatedAt: now,
			Metadata: barrierAuditMeta(barrierID, childID, outcome, "late"),
		}); err != nil {
			return ArriveResult{}, err
		}
		return ArriveResult{State: m6supply.BarrierStateClosed, AlreadySettled: false}, nil
	}
	arrival := m6supply.BarrierArrival{
		ID: ulid.Make().String(), BarrierID: barrierID, ChildID: childID,
		Attempt: attempt, Outcome: outcome, ResultDigest: resultDigest, ArrivedAt: now,
	}
	if err := tx.PutM6BarrierArrival(arrival); err != nil {
		return ArriveResult{}, err
	}
	if err := tx.PutAudit(providerapp.Audit{
		ID: ulid.Make().String(), Action: "barrier.arrived",
		AggregateID: barrierID, Actor: delegationActor, CreatedAt: now,
		Metadata: barrierAuditMeta(barrierID, childID, outcome, barrier.State),
	}); err != nil {
		return ArriveResult{}, err
	}
	arrivals, err := tx.ListM6BarrierArrivals(barrierID)
	if err != nil {
		return ArriveResult{}, err
	}
	reason := closeReason(barrier, arrivals)
	if reason == "" {
		return ArriveResult{State: m6supply.BarrierStateOpen, AlreadySettled: false}, nil
	}
	closed, err := tx.CloseM6Barrier(barrier.ID, barrier.Version, reason, now)
	if errors.Is(err, m6supply.ErrVersionConflict) {
		// a concurrent arrival closed it first — its verdict stands
		fresh, gerr := tx.GetM6Barrier(barrierID)
		if gerr != nil {
			return ArriveResult{}, gerr
		}
		return ArriveResult{State: fresh.State, AlreadySettled: false}, nil
	}
	if err != nil {
		return ArriveResult{}, err
	}
	return ArriveResult{State: closed.State, AlreadySettled: false, ClosedReason: reason}, nil
}

// closeReason decides whether this arrival closes the barrier and why.
func closeReason(barrier m6supply.Barrier, arrivals []m6supply.BarrierArrival) string {
	allArrived := len(arrivals) >= barrier.ExpectedChildren
	switch barrier.Policy {
	case m6supply.BarrierPolicyAll:
		if allArrived {
			return m6supply.BarrierClosedAllArrived
		}
	case m6supply.BarrierPolicyQuorum:
		succeeded := 0
		for _, a := range arrivals {
			if a.Outcome == m6supply.ArrivalSucceeded {
				succeeded++
			}
		}
		if succeeded >= barrier.Quorum {
			return m6supply.BarrierClosedQuorumReached
		}
		if allArrived {
			return m6supply.BarrierClosedBelowQuorum
		}
	case m6supply.BarrierPolicyFailFast:
		for _, a := range arrivals {
			if a.Outcome != m6supply.ArrivalSucceeded {
				return m6supply.BarrierClosedFailFast
			}
		}
		if allArrived {
			return m6supply.BarrierClosedAllArrived
		}
	}
	return ""
}

// Cancel closes an open barrier administratively (root cancel / deadline).
// Late arrivals after this point are evidence-only.
func (s *BarrierService) Cancel(ctx context.Context, barrierID, reason string) (m6supply.Barrier, error) {
	if s == nil || s.uow == nil {
		return m6supply.Barrier{}, ErrServiceUnavailable
	}
	var out m6supply.Barrier
	err := s.uow.TransactM6(ctx, func(tx Tx) error {
		barrier, err := tx.GetM6Barrier(barrierID)
		if errors.Is(err, m6supply.ErrNotFound) {
			return ErrBarrierNotFound
		}
		if err != nil {
			return err
		}
		if barrier.State == m6supply.BarrierStateClosed {
			out = barrier
			return nil
		}
		now := s.clock.Now().UTC()
		closed, err := tx.CloseM6Barrier(barrier.ID, barrier.Version, reason, now)
		if errors.Is(err, m6supply.ErrVersionConflict) {
			return fmt.Errorf("retry: barrier version moved")
		}
		if err != nil {
			return err
		}
		out = closed
		return nil
	})
	return out, err
}

// barrierAuditMeta shapes the audit metadata for barrier lifecycle rows.
func barrierAuditMeta(barrierID, childID, outcome, state string) []byte {
	type meta struct {
		BarrierID string `json:"barrierId"`
		ChildID   string `json:"childId,omitempty"`
		Outcome   string `json:"outcome,omitempty"`
		State     string `json:"state"`
	}
	return marshalJSON(meta{BarrierID: barrierID, ChildID: childID, Outcome: outcome, State: state})
}
