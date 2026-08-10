package compaction

import (
	"errors"
	"time"

	"github.com/oklog/ulid/v2"
)

// Trigger describes what initiated the compaction.
type Trigger string

const (
	TriggerAutomatic Trigger = "automatic"
	TriggerManual    Trigger = "manual"
	TriggerHandoff   Trigger = "handoff"
)

// Status is the lifecycle state of a checkpoint.
type Status string

const (
	StatusPending    Status = "pending"
	StatusRunning    Status = "running"
	StatusSucceeded  Status = "succeeded"
	StatusFailed     Status = "failed"
	StatusSuperseded Status = "superseded"
)

// TerminalStatuses returns true if the status is a terminal state.
func (s Status) IsTerminal() bool {
	return s == StatusSucceeded || s == StatusFailed || s == StatusSuperseded
}

// SummarySchemaVersion is the current structured summary schema version.
const SummarySchemaVersion = "1.0"

// Checkpoint is a versioned compaction summary of a source message range.
// It is derived data; it never substitutes for source messages.
// Only accepted (succeeded) checkpoints are used in prompt assembly.
type Checkpoint struct {
	ID                  string    `json:"id"`
	SessionID           string    `json:"sessionId"`
	Version             int64     `json:"version"`
	SourceStartID       string    `json:"sourceStartId"`
	SourceEndID         string    `json:"sourceEndId"`
	SourceStartSeq      int64     `json:"sourceStartSeq"`
	SourceEndSeq        int64     `json:"sourceEndSeq"`
	SourceDigest        string    `json:"sourceDigest"`
	PrevCheckpointID    *string   `json:"prevCheckpointId,omitempty"`
	PrevCheckpointDigest *string  `json:"prevCheckpointDigest,omitempty"`
	SummarySchemaVersion string   `json:"summarySchemaVersion"`
	Trigger             Trigger   `json:"trigger"`
	TriggerReason       string    `json:"triggerReason"`
	Status              Status    `json:"status"`
	Provider            string    `json:"provider"`
	Model               string    `json:"model"`
	SummaryJSON         string    `json:"summaryJson"`
	HumanSummary        string    `json:"humanSummary"`
	FailureCode         *string   `json:"failureCode,omitempty"`
	CreatedAt           time.Time `json:"createdAt"`
	CompletedAt         *time.Time `json:"completedAt,omitempty"`
}

// CanTransitionTo returns whether the status can transition to the target.
func (c Checkpoint) CanTransitionTo(target Status) bool {
	switch c.Status {
	case StatusPending:
		return target == StatusRunning || target == StatusFailed
	case StatusRunning:
		return target == StatusSucceeded || target == StatusFailed
	case StatusSucceeded:
		return target == StatusSuperseded
	case StatusFailed:
		return target == StatusPending
	case StatusSuperseded:
		return false
	default:
		return false
	}
}

// TransitionTo attempts to move the checkpoint to the target status.
// It returns the new checkpoint state and an error if the transition is invalid.
func (c Checkpoint) TransitionTo(target Status) (Checkpoint, error) {
	if !c.CanTransitionTo(target) {
		return c, errors.New("invalid checkpoint status transition")
	}
	result := c
	result.Status = target
	now := time.Now().UTC()
	if target.IsTerminal() {
		result.CompletedAt = &now
	}
	if target == StatusRunning && c.Status == StatusPending {
		result.CompletedAt = nil
	}
	return result, nil
}

// Validate checks invariants for a checkpoint.
func (c Checkpoint) Validate() error {
	if !canonicalULID(c.ID) || !canonicalULID(c.SessionID) {
		return errors.New("checkpoint id or session_id is not a canonical ULID")
	}
	if !canonicalULID(c.SourceStartID) || !canonicalULID(c.SourceEndID) {
		return errors.New("checkpoint source message ids are not canonical ULIDs")
	}
	if c.Version < 1 {
		return errors.New("checkpoint version must be positive")
	}
	if c.SourceStartSeq < 1 || c.SourceEndSeq < 1 || c.SourceStartSeq > c.SourceEndSeq {
		return errors.New("checkpoint source range invalid")
	}
	if len(c.SourceDigest) != 64 || !isHex(c.SourceDigest) {
		return errors.New("checkpoint source_digest must be 64 hex chars")
	}
	if c.PrevCheckpointID != nil && !canonicalULID(*c.PrevCheckpointID) {
		return errors.New("checkpoint prev_checkpoint_id is not a canonical ULID")
	}
	if c.PrevCheckpointDigest != nil && (len(*c.PrevCheckpointDigest) != 64 || !isHex(*c.PrevCheckpointDigest)) {
		return errors.New("checkpoint prev_checkpoint_digest must be 64 hex chars")
	}
	if len(c.SummarySchemaVersion) > 16 {
		return errors.New("checkpoint summary_schema_version too long")
	}
	switch c.Trigger {
	case TriggerAutomatic, TriggerManual, TriggerHandoff:
	default:
		return errors.New("checkpoint trigger invalid")
	}
	if len(c.TriggerReason) > 1024 {
		return errors.New("checkpoint trigger_reason too long")
	}
	switch c.Status {
	case StatusPending, StatusRunning, StatusSucceeded, StatusFailed, StatusSuperseded:
	default:
		return errors.New("checkpoint status invalid")
	}
	if len(c.Provider) > 128 || len(c.Model) > 128 {
		return errors.New("checkpoint provider/model too long")
	}
	if len(c.SummaryJSON) < 2 || len(c.SummaryJSON) > 65536 {
		return errors.New("checkpoint summary_json size out of bounds")
	}
	if len(c.HumanSummary) > 32768 {
		return errors.New("checkpoint human_summary too long")
	}
	if c.FailureCode != nil && len(*c.FailureCode) > 64 {
		return errors.New("checkpoint failure_code too long")
	}
	if c.CreatedAt.IsZero() || c.CreatedAt.Location() != time.UTC {
		return errors.New("checkpoint created_at must be UTC")
	}
	if c.Status.IsTerminal() != (c.CompletedAt != nil) {
		return errors.New("checkpoint completed_at must be set iff status is terminal")
	}
	if c.CompletedAt != nil && c.CompletedAt.Location() != time.UTC {
		return errors.New("checkpoint completed_at must be UTC")
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