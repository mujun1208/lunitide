// Package agentrun implements the M4 reliable single-agent runtime domain:
// durable Run/Turn/Step/ToolCall state machines, budgets, effect journal,
// workspace registration/grant/lease, change sets, command jobs, plans,
// evidence and reviews. M4 has no parent_run_id/delegation (M6 scope) and no
// arbitrary shell, child fan-out or third-party execution.
//
// State machines (authoritative: PRD M4 row + module-detail §6.1):
//
//	AgentRun: queued → running ↔ paused_review | paused_budget
//	          running → completed | failed | cancelled | interrupted | outcome_unknown
//	ToolCall: proposed → policy_checked → awaiting_review → approved → running
//	          running → succeeded | failed | cancelled | outcome_unknown
//	          awaiting_review → denied
//	ChangeSet: draft → previewed → approved → applied → reverted | conflicted
//
// Terminal states are first-terminal-wins; run status transitions are guarded
// by a version CAS at the storage layer.
package agentrun

import (
	"errors"
	"fmt"
	"time"

	"github.com/oklog/ulid/v2"
)

// RunStatus is the lifecycle state of an AgentRun.
type RunStatus string

const (
	RunQueued         RunStatus = "queued"
	RunRunning        RunStatus = "running"
	RunPausedReview   RunStatus = "paused_review"
	RunPausedBudget   RunStatus = "paused_budget"
	RunCompleted      RunStatus = "completed"
	RunFailed         RunStatus = "failed"
	RunCancelled      RunStatus = "cancelled"
	RunInterrupted    RunStatus = "interrupted"
	RunOutcomeUnknown RunStatus = "outcome_unknown"
)

// Terminal reports whether the run state is terminal. Terminal states are
// first-terminal-wins: no further transition is allowed once reached.
func (s RunStatus) Terminal() bool {
	switch s {
	case RunCompleted, RunFailed, RunCancelled, RunInterrupted, RunOutcomeUnknown:
		return true
	}
	return false
}

// validRunTransition encodes the authoritative run state machine.
func validRunTransition(from, to RunStatus) bool {
	switch from {
	case RunQueued:
		return to == RunRunning
	case RunRunning:
		switch to {
		case RunPausedReview, RunPausedBudget,
			RunCompleted, RunFailed, RunCancelled, RunInterrupted, RunOutcomeUnknown:
			return true
		}
	case RunPausedReview, RunPausedBudget:
		return to == RunRunning
	}
	return false
}

// Budget declares the resource envelope for a run. All dimensions are
// enforced fail-closed; when HardCeiling is set, exceeding any dimension
// terminates the run instead of pausing it. Zero means "not configured" and
// is rejected by Validate for mandatory dimensions per the M4 PRD: model
// turns, tool calls, tokens, cost, wall-clock, output bytes, retries,
// no-progress and a hard-ceiling flag must all be present.
type Budget struct {
	MaxModelTurns       int64 `json:"maxModelTurns"`
	MaxToolCalls        int64 `json:"maxToolCalls"`
	MaxTokens           int64 `json:"maxTokens"`
	MaxCostMicros       int64 `json:"maxCostMicros"`
	MaxWallClockSeconds int64 `json:"maxWallClockSeconds"`
	MaxOutputBytes      int64 `json:"maxOutputBytes"`
	MaxRetries          int64 `json:"maxRetries"`
	MaxNoProgress       int64 `json:"maxNoProgress"`
	HardCeiling         bool  `json:"hardCeiling"`
}

func (b Budget) Validate() error {
	if b.MaxModelTurns < 1 || b.MaxToolCalls < 1 || b.MaxTokens < 1 ||
		b.MaxCostMicros < 1 || b.MaxWallClockSeconds < 1 || b.MaxOutputBytes < 1 {
		return errors.New("budget must set positive limits for model turns, tool calls, tokens, cost, wall-clock and output bytes")
	}
	if b.MaxRetries < 0 || b.MaxNoProgress < 0 {
		return errors.New("budget retry/no-progress limits must not be negative")
	}
	return nil
}

// Usage tracks consumed budget dimensions for a run.
type Usage struct {
	ModelTurns       int64 `json:"modelTurns"`
	ToolCalls        int64 `json:"toolCalls"`
	Tokens           int64 `json:"tokens"`
	CostMicros       int64 `json:"costMicros"`
	WallClockSeconds int64 `json:"wallClockSeconds"`
	OutputBytes      int64 `json:"outputBytes"`
	Retries          int64 `json:"retries"`
	NoProgress       int64 `json:"noProgress"`
}

// Add returns the component-wise sum. Negative usage is never valid.
func (u Usage) Add(v Usage) Usage {
	return Usage{u.ModelTurns + v.ModelTurns, u.ToolCalls + v.ToolCalls,
		u.Tokens + v.Tokens, u.CostMicros + v.CostMicros,
		u.WallClockSeconds + v.WallClockSeconds, u.OutputBytes + v.OutputBytes,
		u.Retries + v.Retries, u.NoProgress + v.NoProgress}
}

func (u Usage) Validate() error {
	if u.ModelTurns < 0 || u.ToolCalls < 0 || u.Tokens < 0 || u.CostMicros < 0 ||
		u.WallClockSeconds < 0 || u.OutputBytes < 0 || u.Retries < 0 || u.NoProgress < 0 {
		return fmt.Errorf("%w: usage must not be negative", ErrInvalid)
	}
	return nil
}

// Covers reports whether b includes all already consumed usage.
func (b Budget) Covers(u Usage) bool { return b.ExceededBy(u) == "" }

// StrictlyExpands reports whether every old capacity is preserved and at
// least one is raised. HardCeiling is policy, not capacity.
func (b Budget) StrictlyExpands(old Budget) bool {
	if b.MaxModelTurns < old.MaxModelTurns || b.MaxToolCalls < old.MaxToolCalls ||
		b.MaxTokens < old.MaxTokens || b.MaxCostMicros < old.MaxCostMicros ||
		b.MaxWallClockSeconds < old.MaxWallClockSeconds || b.MaxOutputBytes < old.MaxOutputBytes ||
		b.MaxRetries < old.MaxRetries || b.MaxNoProgress < old.MaxNoProgress {
		return false
	}
	return b.MaxModelTurns > old.MaxModelTurns || b.MaxToolCalls > old.MaxToolCalls ||
		b.MaxTokens > old.MaxTokens || b.MaxCostMicros > old.MaxCostMicros ||
		b.MaxWallClockSeconds > old.MaxWallClockSeconds || b.MaxOutputBytes > old.MaxOutputBytes ||
		b.MaxRetries > old.MaxRetries || b.MaxNoProgress > old.MaxNoProgress
}

// ExceededBy reports the first budget dimension that the usage exceeds,
// or "" when usage is within budget.
func (b Budget) ExceededBy(u Usage) string {
	switch {
	case u.ModelTurns > b.MaxModelTurns:
		return "modelTurns"
	case u.ToolCalls > b.MaxToolCalls:
		return "toolCalls"
	case u.Tokens > b.MaxTokens:
		return "tokens"
	case u.CostMicros > b.MaxCostMicros:
		return "cost"
	case u.WallClockSeconds > b.MaxWallClockSeconds:
		return "wallClock"
	case u.OutputBytes > b.MaxOutputBytes:
		return "outputBytes"
	case b.MaxRetries > 0 && u.Retries > b.MaxRetries:
		return "retries"
	case b.MaxNoProgress > 0 && u.NoProgress > b.MaxNoProgress:
		return "noProgress"
	}
	return ""
}

// AgentRun is a durable single-agent execution rooted at a session.
type AgentRun struct {
	ID        string    `json:"id"`
	SessionID string    `json:"sessionId"`
	Status    RunStatus `json:"status"`
	Budget    Budget    `json:"budget"`
	Used      Usage     `json:"used"`
	Version   int64     `json:"version"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

var (
	ErrNotFound          = errors.New("agent run not found")
	ErrInvalid           = errors.New("invalid agent run")
	ErrInvalidTransition = errors.New("invalid agent run status transition")
	ErrTerminal          = errors.New("agent run is terminal")
	ErrVersionConflict   = errors.New("agent run version conflict")
	ErrBudgetExceeded    = errors.New("agent run budget exceeded")
	ErrReservation       = errors.New("usage reservation not found or not active")
)

func canonicalULID(id string) bool {
	u, err := ulid.ParseStrict(id)
	return err == nil && u.String() == id && id[0] <= '7'
}

func (r AgentRun) Validate() error {
	if !canonicalULID(r.ID) || !canonicalULID(r.SessionID) {
		return fmt.Errorf("%w: IDs must be uppercase canonical ULIDs", ErrInvalid)
	}
	switch r.Status {
	case RunQueued, RunRunning, RunPausedReview, RunPausedBudget,
		RunCompleted, RunFailed, RunCancelled, RunInterrupted, RunOutcomeUnknown:
	default:
		return fmt.Errorf("%w: unknown status %q", ErrInvalid, r.Status)
	}
	if err := r.Budget.Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	if r.CreatedAt.IsZero() || r.UpdatedAt.Before(r.CreatedAt) || r.Version < 1 {
		return fmt.Errorf("%w: timestamps/version", ErrInvalid)
	}
	return nil
}

// CanTransitionTo reports whether the run may move to the target status.
func (r AgentRun) CanTransitionTo(to RunStatus) bool {
	if r.Status.Terminal() {
		return false
	}
	return validRunTransition(r.Status, to)
}

// Transition returns a copy of the run moved to the target status with the
// version incremented, or ErrInvalidTransition/ErrTerminal.
func (r AgentRun) Transition(to RunStatus, at time.Time) (AgentRun, error) {
	if r.Status.Terminal() {
		return r, ErrTerminal
	}
	if !validRunTransition(r.Status, to) {
		return r, fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, r.Status, to)
	}
	r.Status = to
	r.Version++
	r.UpdatedAt = at
	return r, nil
}
