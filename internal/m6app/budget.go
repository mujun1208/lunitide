// T-6.3.3 application service: the budget conservation ledger. Every
// mutation runs inside the caller's single-writer transaction and keeps
// the invariant granted = reserved + consumed + refundable on every
// (root, dimension) account — the 0046 CHECK is the hard stop, this
// service guarantees transitions never reach it violated.
//
// Semantics (M6_ERROR_CATALOG_V2):
//
//	reserve  : granted += n && reserved += n — refused whole (BGT-001,
//	           nothing frozen) when any dimension would exceed its cap
//	consume  : reserved -= n && consumed += n — usage may never eat into
//	           units that were not reserved (BGT-001)
//	refund   : reserved -= n && refundable += n — only unstarted children;
//	           consumed units are never refundable (no double refunds)
//	drift    : conservation audit after settle; nonzero drift is BGT-002
//	           (freeze tree + P0) and rolls the whole transaction back
//
// The per-dimension caps are the Root policy; the design's m6_budget_account
// row intentionally has no cap column, so the caps ride on the service
// (wired from the run policy at composition time).
package m6app

import (
	"errors"
	"fmt"
	"github.com/lunitide/lunitide/internal/delegation"
	"github.com/lunitide/lunitide/internal/domain/m6supply"
	"github.com/oklog/ulid/v2"
)

var (
	// ErrBudgetInsufficient maps to M6-BGT-001: the grant would overdraw
	// the root policy cap or the usage overdraws the reservation.
	ErrBudgetInsufficient = errors.New("m6app: budget insufficient")
	// ErrBudgetDrift maps to M6-BGT-002: conservation broken mid-flight.
	ErrBudgetDrift = errors.New("m6app: budget conservation drift")
	// ErrBudgetOverconsume: usage touches unreserved units.
	ErrBudgetOverconsume = errors.New("m6app: usage exceeds reservation")
)

// BudgetPolicy carries the root's per-dimension caps (the amount the root
// may hand out to children in total). Zero means the dimension is capped
// at zero — roots that do not carry a policy cannot delegate.
type BudgetPolicy struct {
	Caps map[string]int64
}

func (p BudgetPolicy) cap(dimension string) int64 {
	if p.Caps == nil {
		return 0
	}
	return p.Caps[dimension]
}

// BudgetService is the conservation ledger. Stateless beyond the policy;
// every operation takes the open transaction so callers compose reserve +
// arrival + state transitions atomically.
type BudgetService struct {
	policy BudgetPolicy
	clock  Clock
}

func NewBudgetService(policy BudgetPolicy) *BudgetService {
	return &BudgetService{policy: policy, clock: systemClock{}}
}

// SetClock substitutes the wall clock (tests).
func (s *BudgetService) SetClock(c Clock) { s.clock = c }

// dimensionAmount maps the wire grant fields onto the frozen DB dimension
// enum (cpu_seconds / tokens / cost / wall_clock).
func dimensionAmount(dim string, g delegation.BudgetGrant) int64 {
	switch dim {
	case m6supply.BudgetCPUSeconds:
		return g.CPUSeconds
	case m6supply.BudgetTokens:
		return g.Tokens
	case m6supply.BudgetCost:
		return g.Cost
	case m6supply.BudgetWallClock:
		return g.WallClockMs
	}
	return 0
}

// GrantOfDimensions expands a wire grant into per-dimension amounts.
func GrantOfDimensions(g delegation.BudgetGrant) map[string]int64 {
	out := make(map[string]int64, len(m6supply.BudgetDimensions))
	for _, d := range m6supply.BudgetDimensions {
		out[d] = dimensionAmount(d, g)
	}
	return out
}

// Reserve freezes the grant for one child: granted += n and reserved += n
// per dimension. The whole reservation is refused before any write when a
// dimension would exceed the policy cap (BGT-001: 拒绝划拨，不冻结).
func (s *BudgetService) Reserve(tx Tx, rootID string, grant delegation.BudgetGrant) error {
	// pre-flight: verify every dimension fits before touching any row
	for _, dim := range m6supply.BudgetDimensions {
		n := dimensionAmount(dim, grant)
		if n < 0 {
			return fmt.Errorf("%w: negative %s", ErrBudgetInsufficient, dim)
		}
		if n == 0 {
			continue
		}
		acct, err := tx.GetM6BudgetAccount(rootID, dim)
		if errors.Is(err, m6supply.ErrNotFound) {
			acct = m6supply.BudgetAccount{}
		} else if err != nil {
			return err
		}
		if acct.Granted+n > s.policy.cap(dim) {
			return fmt.Errorf("%w: %s cap %d", ErrBudgetInsufficient, dim, s.policy.cap(dim))
		}
	}
	now := s.clock.Now().UTC()
	for _, dim := range m6supply.BudgetDimensions {
		n := dimensionAmount(dim, grant)
		if n == 0 {
			continue
		}
		existing, err := tx.GetM6BudgetAccount(rootID, dim)
		if errors.Is(err, m6supply.ErrNotFound) {
			created := m6supply.BudgetAccount{
				ID: ulid.Make().String(), RootID: rootID, Dimension: dim,
				Granted: n, Reserved: n, Version: 1, CreatedAt: now, UpdatedAt: now,
			}
			if err := tx.PutM6BudgetAccount(created); err != nil {
				return err
			}
			continue
		}
		if err != nil {
			return err
		}
		if _, err := tx.UpdateM6BudgetAccountBalances(existing.ID, existing.Version,
			existing.Granted+n, existing.Reserved+n, existing.Consumed, existing.Refundable, now); err != nil {
			return err
		}
	}
	return nil
}

// Consume settles actual usage against the reservation: reserved -= n and
// consumed += n per dimension. Usage above the reservation is refused
// (only reserved units may be consumed) and rolls the caller's transaction
// back — no partial settlement ever lands.
func (s *BudgetService) Consume(tx Tx, rootID string, usage delegation.BudgetGrant) (map[string]int64, error) {
	consumed := make(map[string]int64, len(m6supply.BudgetDimensions))
	// pre-flight: every dimension must have enough reserved
	for _, dim := range m6supply.BudgetDimensions {
		n := dimensionAmount(dim, usage)
		if n < 0 {
			return nil, fmt.Errorf("%w: negative %s", ErrBudgetOverconsume, dim)
		}
		if n == 0 {
			continue
		}
		acct, err := tx.GetM6BudgetAccount(rootID, dim)
		if errors.Is(err, m6supply.ErrNotFound) {
			return nil, fmt.Errorf("%w: no reservation for %s", ErrBudgetOverconsume, dim)
		}
		if err != nil {
			return nil, err
		}
		if acct.Reserved < n {
			return nil, fmt.Errorf("%w: %s reserved %d want %d", ErrBudgetOverconsume, dim, acct.Reserved, n)
		}
	}
	now := s.clock.Now().UTC()
	for _, dim := range m6supply.BudgetDimensions {
		n := dimensionAmount(dim, usage)
		if n == 0 {
			continue
		}
		acct, err := tx.GetM6BudgetAccount(rootID, dim)
		if err != nil {
			return nil, err
		}
		if _, err := tx.UpdateM6BudgetAccountBalances(acct.ID, acct.Version,
			acct.Granted, acct.Reserved-n, acct.Consumed+n, acct.Refundable, now); err != nil {
			return nil, err
		}
		consumed[dim] = n
	}
	return consumed, nil
}

// Refund returns the reservation of an unstarted child: reserved -= n and
// refundable += n. Consumed units are never refundable — the pre-flight
// reservation check makes a double refund impossible.
func (s *BudgetService) Refund(tx Tx, rootID string, grant delegation.BudgetGrant) error {
	for _, dim := range m6supply.BudgetDimensions {
		n := dimensionAmount(dim, grant)
		if n < 0 {
			return fmt.Errorf("%w: negative %s", ErrBudgetInsufficient, dim)
		}
		if n == 0 {
			continue
		}
		acct, err := tx.GetM6BudgetAccount(rootID, dim)
		if errors.Is(err, m6supply.ErrNotFound) {
			return fmt.Errorf("%w: no reservation for %s", ErrBudgetOverconsume, dim)
		}
		if err != nil {
			return err
		}
		if acct.Reserved < n {
			return fmt.Errorf("%w: %s reserved %d want %d", ErrBudgetOverconsume, dim, acct.Reserved, n)
		}
	}
	now := s.clock.Now().UTC()
	for _, dim := range m6supply.BudgetDimensions {
		n := dimensionAmount(dim, grant)
		if n == 0 {
			continue
		}
		acct, err := tx.GetM6BudgetAccount(rootID, dim)
		if err != nil {
			return err
		}
		if _, err := tx.UpdateM6BudgetAccountBalances(acct.ID, acct.Version,
			acct.Granted, acct.Reserved-n, acct.Consumed, acct.Refundable+n, now); err != nil {
			return err
		}
	}
	return nil
}

// CheckConservation re-reads every account of the root and fails with
// ErrBudgetDrift when granted != reserved + consumed + refundable anywhere
// (BGT-002: freeze tree + P0). Callers run it right before commit so a
// drift aborts the transaction.
func (s *BudgetService) CheckConservation(tx Tx, rootID string) error {
	accounts, err := tx.ListM6BudgetAccounts(rootID)
	if err != nil {
		return err
	}
	for _, a := range accounts {
		if drift := a.Drift(); drift != 0 {
			return fmt.Errorf("%w: %s drift %d", ErrBudgetDrift, a.Dimension, drift)
		}
	}
	return nil
}
