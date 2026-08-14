package agentrun

import (
	"fmt"
	"time"
)

// EffectStatus is the lifecycle state of a journaled external effect.
type EffectStatus string

const (
	EffectPrepared       EffectStatus = "prepared"
	EffectCommitted      EffectStatus = "committed"
	EffectFailed         EffectStatus = "failed"
	EffectOutcomeUnknown EffectStatus = "outcome_unknown"
)

// Terminal reports whether the effect state is final. outcome_unknown is
// resolvable exactly once by reconciliation.
func (s EffectStatus) Terminal() bool { return s == EffectCommitted || s == EffectFailed }

// EffectJournal records one external side effect of a run so crashes can be
// reconciled without blind retries. EffectKey is the idempotency identity and
// is unique across the system.
type EffectJournal struct {
	ID            string       `json:"id"`
	RunID         string       `json:"runId"`
	EffectKey     string       `json:"effectKey"`
	RequestDigest string       `json:"requestDigest"`
	ReceiptID     string       `json:"receiptId,omitempty"`
	Status        EffectStatus `json:"status"`
	CreatedAt     time.Time    `json:"createdAt"`
	UpdatedAt     time.Time    `json:"updatedAt"`
}

func (e EffectJournal) Validate() error {
	if !canonicalULID(e.ID) || !canonicalULID(e.RunID) {
		return fmt.Errorf("%w: effect IDs must be uppercase canonical ULIDs", ErrInvalid)
	}
	if len(e.EffectKey) < 1 || len(e.EffectKey) > 256 {
		return fmt.Errorf("%w: effect key length", ErrInvalid)
	}
	if !validHexDigest(e.RequestDigest) {
		return fmt.Errorf("%w: effect request_digest must be a lowercase sha256 hex digest", ErrInvalid)
	}
	switch e.Status {
	case EffectPrepared, EffectCommitted, EffectFailed, EffectOutcomeUnknown:
	default:
		return fmt.Errorf("%w: unknown effect status %q", ErrInvalid, e.Status)
	}
	if e.CreatedAt.IsZero() || e.UpdatedAt.Before(e.CreatedAt) {
		return fmt.Errorf("%w: effect timestamps", ErrInvalid)
	}
	return nil
}

// Resolve moves a prepared effect to committed/failed/outcome_unknown, or
// reconciles an outcome_unknown effect to committed/failed.
func (e EffectJournal) Resolve(to EffectStatus, receiptID string, at time.Time) (EffectJournal, error) {
	if e.Status.Terminal() {
		return e, ErrTerminal
	}
	switch e.Status {
	case EffectPrepared:
		if to != EffectCommitted && to != EffectFailed && to != EffectOutcomeUnknown {
			return e, fmt.Errorf("%w: effect %s -> %s", ErrInvalidTransition, e.Status, to)
		}
	case EffectOutcomeUnknown:
		if to != EffectCommitted && to != EffectFailed {
			return e, fmt.Errorf("%w: effect %s -> %s", ErrInvalidTransition, e.Status, to)
		}
	}
	e.Status = to
	if receiptID != "" {
		e.ReceiptID = receiptID
	}
	e.UpdatedAt = at
	return e, nil
}

// RunEvent is an ordered domain event inside a run. Events are append-only
// and share the writer transaction with the state change they describe.
type RunEvent struct {
	ID        string    `json:"id"`
	RunID     string    `json:"runId"`
	Sequence  int64     `json:"sequence"`
	EventType string    `json:"eventType"`
	Payload   []byte    `json:"payload"` // canonical JSON
	CreatedAt time.Time `json:"createdAt"`
}

func (e RunEvent) Validate() error {
	if !canonicalULID(e.ID) || !canonicalULID(e.RunID) {
		return fmt.Errorf("%w: event IDs must be uppercase canonical ULIDs", ErrInvalid)
	}
	if e.Sequence < 1 {
		return fmt.Errorf("%w: event sequence must be positive", ErrInvalid)
	}
	if len(e.EventType) < 1 || len(e.EventType) > 128 {
		return fmt.Errorf("%w: event type length", ErrInvalid)
	}
	if len(e.Payload) < 2 {
		return fmt.Errorf("%w: event payload must be valid JSON", ErrInvalid)
	}
	if e.CreatedAt.IsZero() {
		return fmt.Errorf("%w: event timestamp", ErrInvalid)
	}
	return nil
}
