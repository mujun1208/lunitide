// T-6.3.1/T-6.3.2 application service: the governed fan-out/fan-in. Create
// runs the frozen verification order and lands one delegation row in
// grant_reserved together with the budget reserve (the freeze record the
// envelope requires); Settle consumes the actual usage, settles the child
// into the root's open barrier and audits conservation — arrival, budget
// and state transition commit in one single-writer transaction.
//
// Governance rules baked in:
//
//   - depth / per-parent fan-out / tree children are refused against the
//     hard caps BEFORE any row is written (M6-DLG-002)
//   - capabilitySet must be a subset of the parent's capabilities — a
//     child never widens rights (M6-DLG-001)
//   - children inherit neither approvals nor secrets: the envelope only
//     carries the explicit capability set and the budget grant; no
//     approval ticket or secret reference ever enters the envelope JSON
//   - deadline overrun flips the delegation to expired; a settle past the
//     deadline is a late arrival (M6-JOIN-001)
package m6app

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/lunitide/lunitide/internal/delegation"
	"github.com/lunitide/lunitide/internal/domain/m6supply"
	"github.com/lunitide/lunitide/internal/providerapp"
	"github.com/oklog/ulid/v2"
)

var (
	// ErrDelegationNotFound: no such delegation row.
	ErrDelegationNotFound = errors.New("m6app: delegation not found")
	// ErrLimitsExceeded maps to M6-DLG-002 (depth/fan-out/tree caps).
	ErrLimitsExceeded = errors.New("m6app: delegation limits exceeded")
	// ErrEnvelopeRejected maps to M6-DLG-001 (static chain failed).
	ErrEnvelopeRejected = errors.New("m6app: delegation envelope rejected")
	// ErrDelegationSettled: terminal state — replay answers the original
	// settlement through the idempotency record instead.
	ErrDelegationSettled = errors.New("m6app: delegation already settled")
	// ErrDelegationLate maps to M6-JOIN-001: settle past the deadline.
	ErrDelegationLate = errors.New("m6app: delegation deadline exceeded")
	// ErrBundleInvalid: ResultBundle shape validation failed.
	ErrBundleInvalid = errors.New("m6app: result bundle invalid")
)

// ParentCapabilities resolves the capability superset one parent may hand
// out (Root policy in production; fixed sets in tests).
type ParentCapabilities func(rootID, parentID string) []string

// CreateDelegationRequest is the wire-shaped creation input.
type CreateDelegationRequest struct {
	RootID        string
	ParentID      string
	Objective     string
	InputDigests  []string
	CapabilitySet []string
	Grant         delegation.BudgetGrant
	DeadlineMS    int64
	Depth         int
}

// CreateDelegationResult answers delegation.create.
type CreateDelegationResult struct {
	DelegationID      string `json:"delegationId"`
	EnvelopeSignature string `json:"envelopeSignature"`
}

// ResultBundle is the child's settle payload (wire shape).
type ResultBundle struct {
	Claims           map[string]any `json:"claims"`
	PatchDigest      string         `json:"patchDigest"`
	TestEvidenceRefs []string       `json:"testEvidenceRefs"`
	Usage            map[string]any `json:"usage"`
	ResultDigest     string         `json:"resultDigest"`
}

// SettleDelegationResult answers delegation.settle.
type SettleDelegationResult struct {
	SettledAt      time.Time        `json:"settledAt"`
	BudgetConsumed map[string]int64 `json:"budgetConsumed"`
	BarrierState   string           `json:"barrierState"`
}

// DelegationService implements delegation.create / delegation.settle.
type DelegationService struct {
	uow        UnitOfWork
	budget     *BudgetService
	barriers   *BarrierService
	signer     *delegation.Signer
	parentCaps ParentCapabilities
	clock      Clock
}

func NewDelegationService(uow UnitOfWork, budget *BudgetService, barriers *BarrierService, signer *delegation.Signer, parentCaps ParentCapabilities) *DelegationService {
	return &DelegationService{uow: uow, budget: budget, barriers: barriers, signer: signer, parentCaps: parentCaps, clock: systemClock{}}
}

// SetClock substitutes the wall clock (tests).
func (s *DelegationService) SetClock(c Clock) { s.clock = c }

// usageOf extracts the four wire usage fields; non-numeric or negative
// values fail the bundle validation.
func usageOf(u map[string]any) (delegation.BudgetGrant, error) {
	g := delegation.BudgetGrant{}
	if u == nil {
		return g, nil
	}
	pick := func(field string) (int64, error) {
		v, ok := u[field]
		if !ok || v == nil {
			return 0, nil
		}
		switch n := v.(type) {
		case float64:
			if n < 0 || n != float64(int64(n)) {
				return 0, fmt.Errorf("%w: usage.%s", ErrBundleInvalid, field)
			}
			return int64(n), nil
		case int64:
			if n < 0 {
				return 0, fmt.Errorf("%w: usage.%s", ErrBundleInvalid, field)
			}
			return n, nil
		case int:
			if n < 0 {
				return 0, fmt.Errorf("%w: usage.%s", ErrBundleInvalid, field)
			}
			return int64(n), nil
		default:
			return 0, fmt.Errorf("%w: usage.%s", ErrBundleInvalid, field)
		}
	}
	var err error
	if g.CPUSeconds, err = pick("cpuSeconds"); err != nil {
		return g, err
	}
	if g.Tokens, err = pick("tokens"); err != nil {
		return g, err
	}
	if g.Cost, err = pick("cost"); err != nil {
		return g, err
	}
	if g.WallClockMs, err = pick("wallClockMs"); err != nil {
		return g, err
	}
	return g, nil
}

// Create verifies and persists one delegation. The idempotency key is the
// wire key; replays answer the original delegation (delegation.create is
// idempotent per key, TSK-001 semantics).
func (s *DelegationService) Create(ctx context.Context, key string, req CreateDelegationRequest) (CreateDelegationResult, error) {
	if s == nil || s.uow == nil {
		return CreateDelegationResult{}, ErrServiceUnavailable
	}
	if req.RootID == "" || req.ParentID == "" || req.Objective == "" || len(req.Objective) > 8192 ||
		len(req.InputDigests) == 0 || len(req.InputDigests) > 64 || len(req.CapabilitySet) == 0 ||
		len(req.CapabilitySet) > 64 || req.DeadlineMS < 1000 || req.DeadlineMS > 86400000 ||
		req.Depth < 0 || req.Depth > 16 || req.Grant.Negative() || !req.Grant.NonZero() {
		return CreateDelegationResult{}, ErrBundleInvalid
	}
	for _, d := range req.InputDigests {
		if len(d) != 64 {
			return CreateDelegationResult{}, ErrBundleInvalid
		}
	}
	digestIn := requestDigestOf("delegation.create", req.RootID, req.ParentID, req.Objective,
		fmt.Sprintf("%v", req.InputDigests), fmt.Sprintf("%v", req.CapabilitySet),
		fmt.Sprintf("%d/%d/%d/%d", req.Grant.CPUSeconds, req.Grant.Tokens, req.Grant.Cost, req.Grant.WallClockMs),
		fmt.Sprintf("%d/%d", req.DeadlineMS, req.Depth))
	var result CreateDelegationResult
	err := s.uow.TransactM6(ctx, func(tx Tx) error {
		now := s.clock.Now().UTC()
		if record, found, err := tx.Idempotency("delegation.create", key, now); err != nil {
			return err
		} else if found {
			if record.Digest != digestIn {
				return ErrIdempotencyConflict
			}
			return json.Unmarshal(record.Response, &result)
		}
		// governance caps BEFORE any row is written (DLG-002)
		if req.Depth > delegation.MaxDepth {
			return fmt.Errorf("%w: depth %d", ErrLimitsExceeded, req.Depth)
		}
		fanout, err := tx.CountActiveM6DelegationsByParent(req.RootID, req.ParentID)
		if err != nil {
			return err
		}
		if fanout >= delegation.MaxFanOut {
			return fmt.Errorf("%w: fan-out %d", ErrLimitsExceeded, fanout)
		}
		tree, err := tx.CountActiveM6DelegationsByRoot(req.RootID)
		if err != nil {
			return err
		}
		if tree >= delegation.MaxTreeChildren {
			return fmt.Errorf("%w: tree children %d", ErrLimitsExceeded, tree)
		}
		// capability subset (DLG-001)
		var caps []string
		if s.parentCaps != nil {
			caps = s.parentCaps(req.RootID, req.ParentID)
		}
		env := &delegation.Envelope{
			Schema:        delegation.Schema,
			DelegationID:  ulid.Make().String(),
			RootID:        req.RootID,
			ParentID:      req.ParentID,
			ChildID:       ulid.Make().String(),
			Depth:         req.Depth,
			Objective:     req.Objective,
			InputDigests:  req.InputDigests,
			CapabilitySet: req.CapabilitySet,
			BudgetGrant:   req.Grant,
			Deadline:      now.Add(time.Duration(req.DeadlineMS) * time.Millisecond).UTC().Format(time.RFC3339Nano),
		}
		env.Nonce, err = delegation.GenerateNonce()
		if err != nil {
			return err
		}
		if s.signer == nil {
			return fmt.Errorf("%w: no signer", ErrEnvelopeRejected)
		}
		if err := s.signer.Sign(env); err != nil {
			return fmt.Errorf("%w: %v", ErrEnvelopeRejected, err)
		}
		static := staticVerify(env, s.signer, caps, now)
		if static != nil {
			return fmt.Errorf("%w: %v", ErrEnvelopeRejected, static)
		}
		// budget freeze (BGT-001 refuses whole, nothing frozen)
		if s.budget == nil {
			return ErrServiceUnavailable
		}
		if err := s.budget.Reserve(tx, req.RootID, req.Grant); err != nil {
			return err
		}
		raw, _ := json.Marshal(env)
		row := m6supply.Delegation{
			ID: env.DelegationID, RootID: req.RootID, ParentID: req.ParentID,
			Envelope:       string(raw),
			EnvelopeDigest: delegation.Digest(env), Nonce: env.Nonce,
			Depth: req.Depth, State: m6supply.DelegationGrantReserved,
			Version: 1, CreatedAt: now, UpdatedAt: now,
		}
		if err := tx.PutM6Delegation(row); err != nil {
			return err
		}
		audit := providerapp.Audit{
			ID: ulid.Make().String(), Action: "delegation.created", AggregateID: row.ID,
			Actor: delegationActor, CreatedAt: now,
			Metadata: delegationAuditMeta(row.ID, row.State),
		}
		if err := tx.PutAudit(audit); err != nil {
			return err
		}
		result = CreateDelegationResult{DelegationID: row.ID, EnvelopeSignature: env.Signature}
		return tx.PutIdempotency(providerapp.Record{
			Operation: "delegation.create", Key: key, Digest: digestIn,
			Response: marshalJSON(result), CreatedAt: now, ExpiresAt: now.Add(24 * time.Hour),
		})
	})
	return result, err
}

// staticVerify adapts delegation.Verify onto the concrete key type.
func staticVerify(env *delegation.Envelope, signer *delegation.Signer, parentCaps []string, now time.Time) error {
	resolver := delegation.KeyResolver(func(keyID string) (ed25519.PublicKey, bool) {
		if signer != nil && keyID == signer.KeyID {
			return signer.Public, true
		}
		return nil, false
	})
	return delegation.Verify(env, resolver, parentCaps, now)
}

// Settle consumes the reported usage, settles the child into the root's
// open barrier (arrival + budget + state in one transaction) and audits
// conservation (BGT-002 rolls everything back on drift).
func (s *DelegationService) Settle(ctx context.Context, key, delegationID string, bundle ResultBundle) (SettleDelegationResult, error) {
	if s == nil || s.uow == nil {
		return SettleDelegationResult{}, ErrServiceUnavailable
	}
	if len(bundle.PatchDigest) != 64 || len(bundle.ResultDigest) != 64 ||
		len(bundle.Claims) == 0 || len(bundle.TestEvidenceRefs) > 64 {
		return SettleDelegationResult{}, ErrBundleInvalid
	}
	for _, ref := range bundle.TestEvidenceRefs {
		if ref == "" || len(ref) > 512 {
			return SettleDelegationResult{}, ErrBundleInvalid
		}
	}
	usage, err := usageOf(bundle.Usage)
	if err != nil {
		return SettleDelegationResult{}, err
	}
	digestIn := requestDigestOf("delegation.settle", delegationID, bundle.PatchDigest, bundle.ResultDigest)
	var result SettleDelegationResult
	err = s.uow.TransactM6(ctx, func(tx Tx) error {
		now := s.clock.Now().UTC()
		if record, found, err := tx.Idempotency("delegation.settle", key, now); err != nil {
			return err
		} else if found {
			if record.Digest != digestIn {
				return ErrIdempotencyConflict
			}
			return json.Unmarshal(record.Response, &result)
		}
		row, err := tx.GetM6Delegation(delegationID)
		if errors.Is(err, m6supply.ErrNotFound) {
			return ErrDelegationNotFound
		}
		if err != nil {
			return err
		}
		switch row.State {
		case m6supply.DelegationSettled:
			return ErrDelegationSettled
		case m6supply.DelegationRejected, m6supply.DelegationExpired:
			return ErrDelegationSettled
		case m6supply.DelegationPlanned:
			return ErrDelegationSettled
		}
		// deadline: a settle past the deadline is a late arrival (JOIN-001)
		var env delegation.Envelope
		if err := json.Unmarshal([]byte(row.Envelope), &env); err != nil {
			return fmt.Errorf("%w: envelope unreadable", ErrEnvelopeRejected)
		}
		deadline, err := delegation.DeadlineOf(&env)
		if err != nil {
			return fmt.Errorf("%w: deadline unreadable", ErrEnvelopeRejected)
		}
		if now.UTC().After(deadline) {
			if _, err := tx.UpdateM6DelegationState(row.ID, row.Version, m6supply.DelegationExpired, now, nil); err != nil {
				return err
			}
			return ErrDelegationLate
		}
		// budget consume (usage against the reservation)
		if s.budget == nil {
			return ErrServiceUnavailable
		}
		consumed, err := s.budget.Consume(tx, row.RootID, usage)
		if err != nil {
			return err
		}
		// barrier arrival (child settles exactly once)
		barrierState := "not_applicable"
		if s.barriers != nil {
			barrier, err := tx.FindOpenM6BarrierByRoot(row.RootID)
			if err == nil {
				arrive, aerr := s.barriers.ArriveTx(tx, barrier.ID, env.ChildID, 0, m6supply.ArrivalSucceeded, bundle.ResultDigest)
				if aerr != nil {
					return aerr
				}
				barrierState = arrive.State
			} else if !errors.Is(err, m6supply.ErrNotFound) {
				return err
			}
		}
		// conservation audit before commit (BGT-002)
		if err := s.budget.CheckConservation(tx, row.RootID); err != nil {
			return err
		}
		settled, err := tx.UpdateM6DelegationState(row.ID, row.Version, m6supply.DelegationSettled, now, &now)
		if err != nil {
			return err
		}
		audit := providerapp.Audit{
			ID: ulid.Make().String(), Action: "delegation.settled", AggregateID: row.ID,
			Actor: delegationActor, CreatedAt: now,
			Metadata: delegationAuditMeta(row.ID, settled.State),
		}
		if err := tx.PutAudit(audit); err != nil {
			return err
		}
		result = SettleDelegationResult{SettledAt: now, BudgetConsumed: consumed, BarrierState: barrierState}
		return tx.PutIdempotency(providerapp.Record{
			Operation: "delegation.settle", Key: key, Digest: digestIn,
			Response: marshalJSON(result), CreatedAt: now, ExpiresAt: now.Add(24 * time.Hour),
		})
	})
	return result, err
}

// delegationActor mirrors the desktop-host actor the app package records
// for run mutations; delegation rows carry the same attribution.
const delegationActor = "desktop-host"

// delegationAuditMeta shapes the audit metadata for delegation lifecycle
// rows (0048 action set).
func delegationAuditMeta(id, state string) []byte {
	type meta struct {
		DelegationID string `json:"delegationId"`
		State        string `json:"state"`
	}
	return marshalJSON(meta{DelegationID: id, State: state})
}
