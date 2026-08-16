// Package budget implements the M9 slice-4 organization budget ledger
// (T-9.4.1): atomic reservation under hard limits, a double-entry journal
// and idempotent settlement — a new M9 org-level entity that deliberately
// does NOT duplicate the M6 budget kernel (internal/domain/m6supply
// BudgetAccount four-dimension conservation stays the per-run kernel; the
// org ledger only guards the organization ceiling in one accounting unit).
//
// Settlement idempotency follows the M6 CloudTask receipt contract: the
// receipt id is the idempotency key and the payload digest makes
// same-key-different-payload replays fail as M9-025 instead of silently
// returning the first result (mirroring M6-TSK-001 semantics).
//
// UNKNOWN economics (threat model m9-06 S3/S9): reservations carry an
// expiry; expired headroom is released (DoS mitigation), but a run whose
// outcome is unknown is parked in OutcomeUnknown isolation — its amount
// moves to the Isolated bucket (neither available nor settled) and is
// never auto-released or guess-posted. Only a real receipt (governance
// reconciliation) settles it. Over-consumption at settlement posts the
// actual spend and is flagged through OverLimit rather than clamped.
//
// Conservation: every mutation posts symmetric journal legs and balances
// satisfy available = HardLimit - Reserved - Settled - Isolated;
// VerifyConservation replays the journal from zero and must reproduce the
// live balances (乱序/重复注入检测).
//
// Concept error taxonomy (04 错误目录, wire-alias only):
//
//	M9-023 BUDGET_RESERVATION_FAILED
//	M9-024 BUDGET_HARD_LIMIT_EXCEEDED
//	M9-025 LEDGER_IDEMPOTENCY_CONFLICT
package budget

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// ConceptError is an M9 concept-taxonomy error (code + name).
type ConceptError struct {
	Code string
	Name string
}

func (e *ConceptError) Error() string { return e.Code + " " + e.Name }

func (e *ConceptError) Is(target error) bool {
	var other *ConceptError
	if errors.As(target, &other) {
		return other.Code == e.Code
	}
	return false
}

var (
	ErrReservationFailed   = &ConceptError{"M9-023", "BUDGET_RESERVATION_FAILED"}
	ErrHardLimitExceeded   = &ConceptError{"M9-024", "BUDGET_HARD_LIMIT_EXCEEDED"}
	ErrIdempotencyConflict = &ConceptError{"M9-025", "LEDGER_IDEMPOTENCY_CONFLICT"}
)

// Code extracts the M9 concept code when err carries one.
func Code(err error) string {
	var ce *ConceptError
	if errors.As(err, &ce) {
		return ce.Code
	}
	return ""
}

// Reservation lifecycle states.
const (
	ReservationReserved       = "reserved"
	ReservationSettled        = "settled"
	ReservationReleased       = "released"        // expiry released the headroom
	ReservationOutcomeUnknown = "outcome_unknown" // parked: no guessed posting (S9)
)

// Journal entry kinds. Each kind names one symmetric leg pair so the
// journal can be replayed from zero.
const (
	EntryLimit    = "limit"    // hard limit set
	EntryReserve  = "reserve"  // available → reserved
	EntryRelease  = "release"  // reserved → available (expiry)
	EntryUnreserve = "unreserve" // reserved → available (settlement/park unreserves)
	EntryPark     = "park"     // available → isolated (outcome_unknown)
	EntryUnpark   = "unpark"   // isolated → available (receipt resolves a parked run)
	EntrySettle   = "settle"   // available → settled (actual spend posted)
)

// Account is one (org_id, account) hard-limit line. Available is derived:
// HardLimit - Reserved - Settled - Isolated.
type Account struct {
	OrgID     string
	AccountID string
	HardLimit int64
	Reserved  int64
	Settled   int64
	Isolated  int64
}

// Available reports the unreserved, unsettled, unisolated headroom.
func (a *Account) Available() int64 { return a.HardLimit - a.Reserved - a.Settled - a.Isolated }

// OverLimit reports a settlement-time breach (actual spend beyond the
// ceiling — posted, flagged, never clamped).
func (a *Account) OverLimit() bool { return a.Reserved+a.Settled > a.HardLimit }

// Reservation is one atomic pre-dispatch reservation.
type Reservation struct {
	ID        string
	OrgID     string
	AccountID string
	Amount    int64
	ExpiresAt time.Time
	State     string
	ReceiptID string
	Actual    int64
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Receipt is the settlement event, idempotent per M6 CloudTask semantics.
type Receipt struct {
	ReceiptID      string // M6 CloudTask idempotency key
	ReservationID  string
	ActualAmount   int64
	PayloadDigest  string // same key + different digest → M9-025
	OutcomeUnknown bool   // S9: park instead of posting a guess
}

// Entry is one append-only double-entry journal line.
type Entry struct {
	Seq       int64
	At        time.Time
	OrgID     string
	AccountID string
	Kind      string
	Amount    int64
	Ref       string // reservation or receipt id
}

type receiptRecord struct {
	receipt Receipt
}

// Ledger is the org budget ledger. All mutations hold one mutex so the
// check-and-reserve is atomic under concurrency (T-15).
type Ledger struct {
	mu           sync.Mutex
	seq          int64
	accounts     map[string]*Account
	reservations map[string]*Reservation
	receipts     map[string]*receiptRecord
	journal      []Entry
}

// New builds an empty ledger.
func New() *Ledger {
	return &Ledger{
		accounts:     make(map[string]*Account),
		reservations: make(map[string]*Reservation),
		receipts:     make(map[string]*receiptRecord),
	}
}

func accountKey(orgID, accountID string) string { return orgID + "/" + accountID }

// SetLimit creates or updates one account's hard limit. Lowering a limit
// below the account's committed usage is refused (conservation).
func (l *Ledger) SetLimit(orgID, accountID string, hardLimit int64, at time.Time) error {
	if orgID == "" || accountID == "" {
		return errors.New("budget: org and account ids are required")
	}
	if hardLimit < 0 {
		return fmt.Errorf("budget: hard limit %d must be >= 0", hardLimit)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	key := accountKey(orgID, accountID)
	a, ok := l.accounts[key]
	if !ok {
		a = &Account{OrgID: orgID, AccountID: accountID}
		l.accounts[key] = a
	}
	if a.Reserved+a.Settled+a.Isolated > hardLimit {
		return fmt.Errorf("budget: limit %d below committed reserved+settled+isolated %d for %s", hardLimit, a.Reserved+a.Settled+a.Isolated, key)
	}
	a.HardLimit = hardLimit
	l.post(a, at, EntryLimit, hardLimit, "limit")
	return nil
}

// Reserve atomically checks headroom and reserves (T-15). Insufficient
// headroom refuses with M9-024 before any runner cost exists; defective
// inputs refuse with M9-023; replaying the same reservation id with
// different parameters is an idempotency conflict (M9-025).
func (l *Ledger) Reserve(reservationID, orgID, accountID string, amount int64, expiresAt, now time.Time) (*Reservation, error) {
	if reservationID == "" {
		return nil, errors.New("budget: reservation id required")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if existing, ok := l.reservations[reservationID]; ok {
		if existing.OrgID == orgID && existing.AccountID == accountID && existing.Amount == amount && existing.ExpiresAt.Equal(expiresAt) {
			return existing, nil // idempotent replay
		}
		return nil, fmt.Errorf("%w: reservation %s replayed with different parameters", ErrIdempotencyConflict, reservationID)
	}
	if amount <= 0 {
		return nil, fmt.Errorf("%w: reservation amount %d must be positive", ErrReservationFailed, amount)
	}
	if !expiresAt.After(now) {
		return nil, fmt.Errorf("%w: reservation expires at %s which is not after now", ErrReservationFailed, expiresAt.Format(time.RFC3339))
	}
	key := accountKey(orgID, accountID)
	a, ok := l.accounts[key]
	if !ok {
		return nil, fmt.Errorf("%w: account %s has no hard limit configured", ErrReservationFailed, key)
	}
	// Atomic check-and-reserve: successful reservations can never overshoot
	// the hard limit, concurrent or not (S1).
	if a.Reserved+a.Settled+a.Isolated+amount > a.HardLimit {
		return nil, fmt.Errorf("%w: account %s headroom %d < requested %d", ErrHardLimitExceeded, key, a.Available(), amount)
	}
	a.Reserved += amount
	r := &Reservation{
		ID: reservationID, OrgID: orgID, AccountID: accountID, Amount: amount,
		ExpiresAt: expiresAt, State: ReservationReserved, CreatedAt: now, UpdatedAt: now,
	}
	l.reservations[reservationID] = r
	l.post(a, now, EntryReserve, amount, reservationID)
	return r, nil
}

// Settle posts one receipt. Duplicate receipts (same id and digest) return
// the original settlement without double charging (T-16); the same id with
// a different digest is an idempotency conflict (M9-025). An
// outcome_unknown receipt parks the reservation in isolation (no guessed
// posting, S9); a real receipt later resolves it.
func (l *Ledger) Settle(rcpt Receipt, now time.Time) (*Reservation, error) {
	if rcpt.ReceiptID == "" || rcpt.ReservationID == "" || rcpt.PayloadDigest == "" {
		return nil, errors.New("budget: receipt id, reservation id and payload digest are required")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if rec, ok := l.receipts[rcpt.ReceiptID]; ok {
		if rec.receipt.PayloadDigest != rcpt.PayloadDigest {
			return nil, fmt.Errorf("%w: receipt %s replayed with different payload digest", ErrIdempotencyConflict, rcpt.ReceiptID)
		}
		// T-16: idempotent — the settlement already posted exactly once.
		r, ok := l.reservations[rcpt.ReservationID]
		if !ok {
			return nil, fmt.Errorf("budget: receipt %s references missing reservation %s", rcpt.ReceiptID, rcpt.ReservationID)
		}
		return r, nil
	}
	r, ok := l.reservations[rcpt.ReservationID]
	if !ok {
		return nil, fmt.Errorf("budget: unknown reservation %s", rcpt.ReservationID)
	}
	if rcpt.ActualAmount < 0 {
		return nil, fmt.Errorf("budget: actual amount %d must be >= 0", rcpt.ActualAmount)
	}
	a := l.accounts[accountKey(r.OrgID, r.AccountID)]
	l.receipts[rcpt.ReceiptID] = &receiptRecord{receipt: rcpt}

	switch r.State {
	case ReservationReserved:
		if rcpt.OutcomeUnknown {
			// S9: no guessed posting — unreserve, then move the amount to
			// isolation (two symmetric legs keep the journal replayable).
			a.Reserved -= r.Amount
			l.post(a, now, EntryUnreserve, r.Amount, rcpt.ReceiptID)
			a.Isolated += r.Amount
			l.post(a, now, EntryPark, r.Amount, rcpt.ReceiptID)
			r.State = ReservationOutcomeUnknown
			r.ReceiptID = rcpt.ReceiptID
			r.UpdatedAt = now
			return r, nil
		}
		// Normal settlement: unreserve, then post the actual spend.
		a.Reserved -= r.Amount
		l.post(a, now, EntryUnreserve, r.Amount, rcpt.ReceiptID)
		return l.commitSettle(a, r, rcpt, now), nil
	case ReservationOutcomeUnknown:
		// Governance reconciliation delivers a real receipt for a parked run.
		if rcpt.OutcomeUnknown {
			return nil, errors.New("budget: outcome_unknown receipt cannot resolve an outcome_unknown reservation")
		}
		a.Isolated -= r.Amount
		l.post(a, now, EntryUnpark, r.Amount, rcpt.ReceiptID)
		return l.commitSettle(a, r, rcpt, now), nil
	case ReservationReleased:
		// Late receipt after expiry release: park or post the real spend
		// directly against available (over-consumption is flagged, not clamped).
		if rcpt.OutcomeUnknown {
			a.Isolated += r.Amount
			r.State = ReservationOutcomeUnknown
			r.ReceiptID = rcpt.ReceiptID
			r.UpdatedAt = now
			l.post(a, now, EntryPark, r.Amount, rcpt.ReceiptID)
			return r, nil
		}
		return l.commitSettle(a, r, rcpt, now), nil
	case ReservationSettled:
		// A second, different receipt trying to settle the same reservation.
		return nil, fmt.Errorf("%w: reservation %s already settled by receipt %s", ErrIdempotencyConflict, r.ID, r.ReceiptID)
	default:
		return nil, fmt.Errorf("budget: reservation %s in unknown state %s", r.ID, r.State)
	}
}

// commitSettle posts the actual spend; under-spend returns headroom
// implicitly, over-spend draws available down (flagged via OverLimit).
func (l *Ledger) commitSettle(a *Account, r *Reservation, rcpt Receipt, now time.Time) *Reservation {
	a.Settled += rcpt.ActualAmount
	r.State = ReservationSettled
	r.ReceiptID = rcpt.ReceiptID
	r.Actual = rcpt.ActualAmount
	r.UpdatedAt = now
	l.post(a, now, EntrySettle, rcpt.ActualAmount, rcpt.ReceiptID)
	return r
}

// ReleaseExpired returns expired reserved headroom (S3 DoS mitigation).
// OutcomeUnknown reservations are never auto-released (S9 isolation).
func (l *Ledger) ReleaseExpired(now time.Time) []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	var released []string
	ids := make([]string, 0, len(l.reservations))
	for id := range l.reservations {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		r := l.reservations[id]
		if r.State != ReservationReserved || !now.After(r.ExpiresAt) {
			continue
		}
		a := l.accounts[accountKey(r.OrgID, r.AccountID)]
		a.Reserved -= r.Amount
		r.State = ReservationReleased
		r.UpdatedAt = now
		l.post(a, now, EntryRelease, r.Amount, r.ID)
		released = append(released, id)
	}
	return released
}

// Account returns a snapshot of one account line.
func (l *Ledger) Account(orgID, accountID string) (Account, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	a, ok := l.accounts[accountKey(orgID, accountID)]
	if !ok {
		return Account{}, false
	}
	return *a, true
}

// Reservation returns a live reservation view.
func (l *Ledger) Reservation(id string) (*Reservation, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	r, ok := l.reservations[id]
	return r, ok
}

// Journal returns the append-only double-entry journal (audit export).
func (l *Ledger) Journal() []Entry {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]Entry, len(l.journal))
	copy(out, l.journal)
	return out
}

// VerifyConservation replays the journal from zero and compares against
// the live balances; any drift (tampering, out-of-order or duplicated
// injected lines) breaks the check.
func (l *Ledger) VerifyConservation() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	type replay struct {
		limit, reserved, settled, isolated int64
	}
	state := make(map[string]*replay)
	for _, e := range l.journal {
		key := accountKey(e.OrgID, e.AccountID)
		st, ok := state[key]
		if !ok {
			st = &replay{}
			state[key] = st
		}
		switch e.Kind {
		case EntryLimit:
			st.limit = e.Amount
		case EntryReserve:
			st.reserved += e.Amount
		case EntryRelease, EntryUnreserve:
			st.reserved -= e.Amount
		case EntryPark:
			st.isolated += e.Amount
		case EntryUnpark:
			st.isolated -= e.Amount
		case EntrySettle:
			st.settled += e.Amount
		default:
			return fmt.Errorf("budget: journal entry %d unknown kind %q", e.Seq, e.Kind)
		}
		if st.reserved < 0 || st.settled < 0 || st.isolated < 0 {
			return fmt.Errorf("budget: journal replay went negative at entry %d (%s)", e.Seq, e.Kind)
		}
	}
	for key, a := range l.accounts {
		st, ok := state[key]
		if !ok {
			return fmt.Errorf("budget: account %s has no journal history", key)
		}
		if st.limit != a.HardLimit || st.reserved != a.Reserved || st.settled != a.Settled || st.isolated != a.Isolated {
			return fmt.Errorf("budget: conservation broken for %s: journal(limit=%d reserved=%d settled=%d isolated=%d) != live(limit=%d reserved=%d settled=%d isolated=%d)",
				key, st.limit, st.reserved, st.settled, st.isolated, a.HardLimit, a.Reserved, a.Settled, a.Isolated)
		}
	}
	return nil
}

func (l *Ledger) post(a *Account, at time.Time, kind string, amount int64, ref string) {
	l.seq++
	l.journal = append(l.journal, Entry{
		Seq: l.seq, At: at, OrgID: a.OrgID, AccountID: a.AccountID,
		Kind: kind, Amount: amount, Ref: ref,
	})
}

// String renders one account line for ops dashboards.
func (a Account) String() string {
	return strings.Join([]string{a.OrgID, a.AccountID,
		fmt.Sprintf("limit=%d reserved=%d settled=%d isolated=%d available=%d", a.HardLimit, a.Reserved, a.Settled, a.Isolated, a.Available())}, "/")
}
