package agentrun

import (
	"errors"
	"fmt"
	"time"
)

// TurnStatus is the lifecycle state of an agent turn.
type TurnStatus string

const (
	TurnRunning   TurnStatus = "running"
	TurnCompleted TurnStatus = "completed"
	TurnFailed    TurnStatus = "failed"
)

func (s TurnStatus) Terminal() bool { return s == TurnCompleted || s == TurnFailed }

// AgentTurn is one model interaction round inside a run. Turns are uniquely
// ordered per run by TurnNo.
type AgentTurn struct {
	ID        string     `json:"id"`
	RunID     string     `json:"runId"`
	TurnNo    int64      `json:"turnNo"`
	Status    TurnStatus `json:"status"`
	Version   int64      `json:"version"`
	CreatedAt time.Time  `json:"createdAt"`
	UpdatedAt time.Time  `json:"updatedAt"`
}

func (t AgentTurn) Validate() error {
	if !canonicalULID(t.ID) || !canonicalULID(t.RunID) {
		return fmt.Errorf("%w: turn IDs must be uppercase canonical ULIDs", ErrInvalid)
	}
	if t.TurnNo < 1 {
		return fmt.Errorf("%w: turn_no must be positive", ErrInvalid)
	}
	switch t.Status {
	case TurnRunning, TurnCompleted, TurnFailed:
	default:
		return fmt.Errorf("%w: unknown turn status %q", ErrInvalid, t.Status)
	}
	if t.CreatedAt.IsZero() || t.UpdatedAt.Before(t.CreatedAt) || t.Version < 1 {
		return fmt.Errorf("%w: turn timestamps/version", ErrInvalid)
	}
	return nil
}

// StepKind classifies a step inside a turn.
type StepKind string

const (
	StepModel  StepKind = "model"
	StepTool   StepKind = "tool"
	StepReview StepKind = "review"
)

// StepStatus is the lifecycle state of an agent step.
type StepStatus string

const (
	StepPending   StepStatus = "pending"
	StepRunning   StepStatus = "running"
	StepCompleted StepStatus = "completed"
	StepFailed    StepStatus = "failed"
)

func (s StepStatus) Terminal() bool { return s == StepCompleted || s == StepFailed }

// AgentStep is a single ordered unit of work inside a turn.
type AgentStep struct {
	ID        string     `json:"id"`
	TurnID    string     `json:"turnId"`
	StepNo    int64      `json:"stepNo"`
	Kind      StepKind   `json:"kind"`
	Status    StepStatus `json:"status"`
	CreatedAt time.Time  `json:"createdAt"`
	UpdatedAt time.Time  `json:"updatedAt"`
}

func (s AgentStep) Validate() error {
	if !canonicalULID(s.ID) || !canonicalULID(s.TurnID) {
		return fmt.Errorf("%w: step IDs must be uppercase canonical ULIDs", ErrInvalid)
	}
	if s.StepNo < 1 {
		return fmt.Errorf("%w: step_no must be positive", ErrInvalid)
	}
	switch s.Kind {
	case StepModel, StepTool, StepReview:
	default:
		return fmt.Errorf("%w: unknown step kind %q", ErrInvalid, s.Kind)
	}
	switch s.Status {
	case StepPending, StepRunning, StepCompleted, StepFailed:
	default:
		return fmt.Errorf("%w: unknown step status %q", ErrInvalid, s.Status)
	}
	if s.CreatedAt.IsZero() || s.UpdatedAt.Before(s.CreatedAt) {
		return fmt.Errorf("%w: step timestamps", ErrInvalid)
	}
	return nil
}

// CallStatus is the lifecycle state of a tool call.
type CallStatus string

const (
	CallProposed       CallStatus = "proposed"
	CallPolicyChecked  CallStatus = "policy_checked"
	CallAwaitingReview CallStatus = "awaiting_review"
	CallApproved       CallStatus = "approved"
	CallRunning        CallStatus = "running"
	CallSucceeded      CallStatus = "succeeded"
	CallFailed         CallStatus = "failed"
	CallDenied         CallStatus = "denied"
	CallCancelled      CallStatus = "cancelled"
	CallOutcomeUnknown CallStatus = "outcome_unknown"
)

// Terminal reports whether the tool call state is terminal
// (first-terminal-wins).
func (s CallStatus) Terminal() bool {
	switch s {
	case CallSucceeded, CallFailed, CallDenied, CallCancelled, CallOutcomeUnknown:
		return true
	}
	return false
}

// validCallTransition encodes the authoritative tool call state machine.
// Policy "allow" without human review still passes awaiting_review → approved
// with a recorded system review decision, keeping the machine single-path.
func validCallTransition(from, to CallStatus) bool {
	switch from {
	case CallProposed:
		return to == CallPolicyChecked
	case CallPolicyChecked:
		return to == CallAwaitingReview
	case CallAwaitingReview:
		return to == CallApproved || to == CallDenied
	case CallApproved:
		return to == CallRunning
	case CallRunning:
		switch to {
		case CallSucceeded, CallFailed, CallCancelled, CallOutcomeUnknown:
			return true
		}
	}
	return false
}

// ToolCall is a durable record of one tool invocation inside a step.
// ArgsDigest binds the normalized arguments; approvals are bound to this
// digest and any argument change invalidates prior approval.
type ToolCall struct {
	ID         string     `json:"id"`
	StepID     string     `json:"stepId"`
	ToolName   string     `json:"toolName"`
	ArgsDigest string     `json:"argsDigest"`
	Status     CallStatus `json:"status"`
	CreatedAt  time.Time  `json:"createdAt"`
	UpdatedAt  time.Time  `json:"updatedAt"`
}

func (c ToolCall) Validate() error {
	if !canonicalULID(c.ID) || !canonicalULID(c.StepID) {
		return fmt.Errorf("%w: tool call IDs must be uppercase canonical ULIDs", ErrInvalid)
	}
	if len(c.ToolName) < 1 || len(c.ToolName) > 128 {
		return fmt.Errorf("%w: tool name length", ErrInvalid)
	}
	if !validHexDigest(c.ArgsDigest) {
		return fmt.Errorf("%w: args_digest must be a lowercase sha256 hex digest", ErrInvalid)
	}
	switch c.Status {
	case CallProposed, CallPolicyChecked, CallAwaitingReview, CallApproved, CallRunning,
		CallSucceeded, CallFailed, CallDenied, CallCancelled, CallOutcomeUnknown:
	default:
		return fmt.Errorf("%w: unknown tool call status %q", ErrInvalid, c.Status)
	}
	if c.CreatedAt.IsZero() || c.UpdatedAt.Before(c.CreatedAt) {
		return fmt.Errorf("%w: tool call timestamps", ErrInvalid)
	}
	return nil
}

// Transition returns a copy of the call moved to the target status.
func (c ToolCall) Transition(to CallStatus, at time.Time) (ToolCall, error) {
	if c.Status.Terminal() {
		return c, ErrTerminal
	}
	if !validCallTransition(c.Status, to) {
		return c, fmt.Errorf("%w: tool call %s -> %s", ErrInvalidTransition, c.Status, to)
	}
	c.Status = to
	c.UpdatedAt = at
	return c, nil
}

// Observation is an append-only captured fact produced by a step. The
// observation body is stored as a blob referenced by ContentDigest.
type Observation struct {
	ID            string    `json:"id"`
	StepID        string    `json:"stepId"`
	Kind          string    `json:"kind"`
	ContentDigest string    `json:"contentDigest"`
	CapturedAt    time.Time `json:"capturedAt"`
	CreatedAt     time.Time `json:"createdAt"`
}

func (o Observation) Validate() error {
	if !canonicalULID(o.ID) || !canonicalULID(o.StepID) {
		return fmt.Errorf("%w: observation IDs must be uppercase canonical ULIDs", ErrInvalid)
	}
	if len(o.Kind) < 1 || len(o.Kind) > 64 {
		return fmt.Errorf("%w: observation kind length", ErrInvalid)
	}
	if !validHexDigest(o.ContentDigest) {
		return fmt.Errorf("%w: observation content_digest must be a lowercase sha256 hex digest", ErrInvalid)
	}
	if o.CapturedAt.IsZero() || o.CreatedAt.IsZero() {
		return fmt.Errorf("%w: observation timestamps", ErrInvalid)
	}
	return nil
}

var ErrImmutable = errors.New("record is append-only and immutable")

func validHexDigest(d string) bool {
	if len(d) != 64 {
		return false
	}
	for _, c := range d {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
