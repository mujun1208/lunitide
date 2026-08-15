// Package m5workspace holds the AdHocWorkspace domain (M5 T-5.1.4): a
// quota-bounded, expiring scratch root owned by exactly one chat Root Run.
package m5workspace

import (
	"errors"
	"time"
)

// State is the durable workspace state machine. near_quota is derived
// (used >= soft) and never stored.
type State string

const (
	StateActive         State = "active"
	StateReadonlyFull   State = "readonly_full"
	StateExpiring       State = "expiring"
	StateCleaning       State = "cleaning"
	StateCleaningFailed State = "cleaning_failed"
	StateRetained       State = "retained"
	StateDeleted        State = "deleted"
)

// Terminal states stop all further transitions (the row stays for audit).
func (s State) Terminal() bool {
	return s == StateRetained || s == StateDeleted
}

// validTransition encodes the frozen M5 workspace state machine.
func validTransition(from, to State) bool {
	switch from {
	case StateActive:
		return to == StateReadonlyFull || to == StateExpiring
	case StateReadonlyFull:
		return to == StateActive || to == StateExpiring
	case StateExpiring:
		return to == StateCleaning || to == StateRetained
	case StateCleaning:
		return to == StateDeleted || to == StateRetained || to == StateCleaningFailed
	case StateCleaningFailed:
		return to == StateCleaning || to == StateRetained
	}
	return false
}

// Frozen M5 defaults (M5/02 "统一 Run、scope 与实施前冻结参数"): soft quota
// 2 GiB, hard quota 4 GiB, expiry 7 days after last activity with a 24 hour
// cleanup grace window.
const (
	DefaultQuotaSoft   int64 = 2147483648
	DefaultQuotaHard   int64 = 4294967296
	ExpiryAfter        time.Duration = 7 * 24 * time.Hour
	GracePeriod        time.Duration = 24 * time.Hour
)

var (
	ErrInvalid    = errors.New("m5workspace: invalid workspace")
	ErrNotFound   = errors.New("m5workspace: workspace not found")
	ErrRootInUse  = errors.New("m5workspace: root canonical path already registered")
	ErrTransition = errors.New("m5workspace: illegal state transition")
	ErrVersion    = errors.New("m5workspace: version conflict")
)

// Workspace is the m5_adhoc_workspace row.
type Workspace struct {
	ID            string    `json:"id"`
	RunID         string    `json:"runId"`
	RootCanonical string    `json:"rootCanonical"`
	DisplayPath   string    `json:"displayPath"`
	GrantJSON     string    `json:"grantJson"`
	LeaseExpiry   time.Time `json:"leaseExpiry"`
	BaseDigest    string    `json:"baseDigest"`
	QuotaSoft     int64     `json:"quotaSoft"`
	QuotaHard     int64     `json:"quotaHard"`
	UsedBytes     int64     `json:"usedBytes"`
	State         State     `json:"state"`
	Version       int64     `json:"version"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

// NearQuota is the derived "approaching quota" panel state.
func (w Workspace) NearQuota() bool {
	return w.State != StateDeleted && w.QuotaSoft > 0 && w.UsedBytes >= w.QuotaSoft
}

// CanTransitionTo reports whether the frozen machine allows from -> to.
func (w Workspace) CanTransitionTo(to State) bool {
	return w.State == to || validTransition(w.State, to)
}

// Validate enforces the column-level invariants before storage.
func (w Workspace) Validate() error {
	if w.ID == "" || w.RunID == "" || w.RootCanonical == "" || w.DisplayPath == "" {
		return ErrInvalid
	}
	if w.BaseDigest == "" || len(w.BaseDigest) != 64 {
		return ErrInvalid
	}
	if w.QuotaSoft <= 0 || w.QuotaHard < w.QuotaSoft {
		return ErrInvalid
	}
	if w.UsedBytes < 0 || w.Version < 1 {
		return ErrInvalid
	}
	switch w.State {
	case StateActive, StateReadonlyFull, StateExpiring, StateCleaning, StateCleaningFailed, StateRetained, StateDeleted:
	default:
		return ErrInvalid
	}
	return nil
}
