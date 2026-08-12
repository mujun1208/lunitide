// Package agentorchestration implements the durable coordination state machine
// for hierarchical agent work. It deliberately does not execute models.
package agentorchestration

import (
	"errors"
	"time"
)

type Status string

const (
	StatusQueued          Status = "queued"
	StatusRunning         Status = "running"
	StatusJoining         Status = "joining"
	StatusSucceeded       Status = "succeeded"
	StatusFailed          Status = "failed"
	StatusCancelRequested Status = "cancel_requested"
	StatusCancelled       Status = "cancelled"
	StatusTimedOut        Status = "timed_out"
)

func (s Status) Terminal() bool {
	return s == StatusSucceeded || s == StatusFailed || s == StatusCancelled || s == StatusTimedOut
}

type Todo struct {
	ID          string
	Title       string
	Description string
	Metadata    map[string]string
}

type AgentRun struct {
	ID, ParentRunID, PlanID, NodeID, Role string
	Todo                                  Todo
	Status                                Status
	Depth                                 int
	Failure                               string
	CreatedAt, UpdatedAt                  time.Time
	TerminalAt                            *time.Time
	Version                               uint64
}

type Event struct {
	Sequence uint64
	RunID    string
	Type     string
	From, To Status
	Detail   string
	At       time.Time
}

type JoinMode string

const (
	JoinAll JoinMode = "all"
	JoinAny JoinMode = "any"
)

type Limits struct{ MaxDepth, MaxConcurrency int }

var (
	ErrNotFound          = errors.New("agent run not found")
	ErrInvalid           = errors.New("invalid agent run")
	ErrInvalidTransition = errors.New("invalid agent run status transition")
	ErrDepthLimit        = errors.New("agent run depth limit reached")
	ErrConcurrencyLimit  = errors.New("agent run concurrency limit reached")
	ErrNoChildren        = errors.New("agent run has no children")
)

func cloneRun(r AgentRun) AgentRun {
	r.Todo.Metadata = cloneMap(r.Todo.Metadata)
	if r.TerminalAt != nil {
		v := *r.TerminalAt
		r.TerminalAt = &v
	}
	return r
}

func cloneMap(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
