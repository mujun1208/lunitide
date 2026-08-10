package handoff

import (
	"errors"
	"time"

	"github.com/oklog/ulid/v2"
)

// Status is the lifecycle state of a handoff capsule.
type Status string

const (
	StatusActive    Status = "active"
	StatusActivated Status = "activated"
	StatusExpired   Status = "expired"
	StatusRevoked   Status = "revoked"
)

// IsTerminal returns true if the status is terminal.
func (s Status) IsTerminal() bool {
	return s == StatusActivated || s == StatusExpired || s == StatusRevoked
}

// Capsule is a cross-window/session context transfer.
// It binds a source session to a destination session and carries
// a compacted checkpoint summary plus active task state.
// Capsules are Engine-validated and activated; the Renderer only displays them.
type Capsule struct {
	ID               string     `json:"id"`
	SourceSessionID  string     `json:"sourceSessionId"`
	DestSessionID    *string    `json:"destSessionId,omitempty"`
	CheckpointID     string     `json:"checkpointId"`
	ActiveTasksJSON  string     `json:"activeTasksJson"`
	RecentMessageIDs string     `json:"recentMessageIds"`
	Digest           string     `json:"digest"`
	Status           Status     `json:"status"`
	CreatedAt        time.Time  `json:"createdAt"`
	ActivatedAt      *time.Time `json:"activatedAt,omitempty"`
	ExpiresAt        *time.Time `json:"expiresAt,omitempty"`
}

// CanTransitionTo returns whether the status can transition to the target.
func (c Capsule) CanTransitionTo(target Status) bool {
	switch c.Status {
	case StatusActive:
		return target == StatusActivated || target == StatusExpired || target == StatusRevoked
	case StatusActivated, StatusExpired, StatusRevoked:
		return false
	default:
		return false
	}
}

// TransitionTo attempts to move the capsule to the target status.
func (c Capsule) TransitionTo(target Status) (Capsule, error) {
	if !c.CanTransitionTo(target) {
		return c, errors.New("invalid capsule status transition")
	}
	result := c
	result.Status = target
	now := time.Now().UTC()
	switch target {
	case StatusActivated:
		result.ActivatedAt = &now
	}
	return result, nil
}

// Validate checks invariants for a handoff capsule.
func (c Capsule) Validate() error {
	if !canonicalULID(c.ID) || !canonicalULID(c.SourceSessionID) || !canonicalULID(c.CheckpointID) {
		return errors.New("capsule id, source_session_id or checkpoint_id is not a canonical ULID")
	}
	if c.DestSessionID != nil && !canonicalULID(*c.DestSessionID) {
		return errors.New("capsule dest_session_id is not a canonical ULID")
	}
	if len(c.ActiveTasksJSON) > 65536 {
		return errors.New("capsule active_tasks_json too large")
	}
	if len(c.RecentMessageIDs) > 65536 {
		return errors.New("capsule recent_message_ids too large")
	}
	if len(c.Digest) != 64 || !isHex(c.Digest) {
		return errors.New("capsule digest must be 64 hex chars")
	}
	switch c.Status {
	case StatusActive, StatusActivated, StatusExpired, StatusRevoked:
	default:
		return errors.New("capsule status invalid")
	}
	if c.CreatedAt.IsZero() || c.CreatedAt.Location() != time.UTC {
		return errors.New("capsule created_at must be UTC")
	}
	if c.Status == StatusActivated && c.ActivatedAt == nil {
		return errors.New("capsule activated_at must be set when status is activated")
	}
	if c.Status != StatusActivated && c.ActivatedAt != nil {
		return errors.New("capsule activated_at must be nil when status is not activated")
	}
	if c.ActivatedAt != nil && c.ActivatedAt.Location() != time.UTC {
		return errors.New("capsule activated_at must be UTC")
	}
	if c.ExpiresAt != nil && c.ExpiresAt.Location() != time.UTC {
		return errors.New("capsule expires_at must be UTC")
	}
	return nil
}

func canonicalULID(v string) bool {
	u, err := ulid.ParseStrict(v)
	return err == nil && u.String() == v && v[0] <= '7'
}

func isHex(s string) bool {
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}