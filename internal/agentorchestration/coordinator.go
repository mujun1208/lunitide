package agentorchestration

import (
	"context"
	"sort"
	"sync/atomic"
	"time"

	"github.com/oklog/ulid/v2"
)

type Clock interface{ Now() time.Time }
type realClock struct{}

func (realClock) Now() time.Time { return time.Now().UTC() }

type IDGenerator func() string

type Coordinator struct {
	repo   Repository
	limits Limits
	now    Clock
	id     IDGenerator
	serial atomic.Uint64
}

func New(repo Repository, limits Limits, ids IDGenerator) (*Coordinator, error) {
	if repo == nil || limits.MaxDepth < 0 || limits.MaxConcurrency < 1 {
		return nil, ErrInvalid
	}
	c := &Coordinator{repo: repo, limits: limits, now: realClock{}, id: ids}
	if c.id == nil {
		c.id = func() string { c.serial.Add(1); return ulid.Make().String() }
	}
	return c, nil
}

// ListPlanRuns returns a stable flat parent/depth tree for one plan.
func (c *Coordinator) ListPlanRuns(ctx context.Context, planID string) ([]AgentRun, error) {
	if planID == "" {
		return nil, ErrInvalid
	}
	var out []AgentRun
	err := c.repo.Transact(ctx, func(tx Transaction) error {
		for _, r := range tx.ListRuns() {
			if r.PlanID == planID {
				out = append(out, r)
			}
		}
		sort.Slice(out, func(i, j int) bool {
			if out[i].CreatedAt.Equal(out[j].CreatedAt) {
				return out[i].ID < out[j].ID
			}
			return out[i].CreatedAt.Before(out[j].CreatedAt)
		})
		return nil
	})
	return out, err
}

func (c *Coordinator) CreateRoot(ctx context.Context, planID, nodeID, role string, todo Todo) (AgentRun, error) {
	return c.create(ctx, "", planID, nodeID, role, todo)
}

func (c *Coordinator) SpawnChild(ctx context.Context, parentID, nodeID, role string, todo Todo) (AgentRun, error) {
	var parent AgentRun
	err := c.repo.Transact(ctx, func(tx Transaction) error {
		var ok bool
		parent, ok = tx.Get(parentID)
		if !ok {
			return ErrNotFound
		}
		return nil
	})
	if err != nil {
		return AgentRun{}, err
	}
	return c.create(ctx, parentID, parent.PlanID, nodeID, role, todo)
}

func (c *Coordinator) create(ctx context.Context, parentID, planID, nodeID, role string, todo Todo) (AgentRun, error) {
	if planID == "" || nodeID == "" || role == "" || todo.ID == "" || todo.Title == "" {
		return AgentRun{}, ErrInvalid
	}
	now := c.now.Now()
	r := AgentRun{ID: c.id(), ParentRunID: parentID, PlanID: planID, NodeID: nodeID, Role: role, Todo: todo, Status: StatusQueued, CreatedAt: now, UpdatedAt: now, Version: 1}
	err := c.repo.Transact(ctx, func(tx Transaction) error {
		if _, exists := tx.Get(r.ID); exists {
			return ErrInvalid
		}
		if parentID != "" {
			p, ok := tx.Get(parentID)
			if !ok {
				return ErrNotFound
			}
			if p.Status.Terminal() || p.Status == StatusCancelRequested {
				return ErrInvalidTransition
			}
			r.Depth = p.Depth + 1
		}
		if r.Depth > c.limits.MaxDepth {
			return ErrDepthLimit
		}
		active := 0
		for _, x := range tx.ListRuns() {
			if !x.Status.Terminal() {
				active++
			}
		}
		if active >= c.limits.MaxConcurrency {
			return ErrConcurrencyLimit
		}
		tx.Put(r)
		tx.Append(Event{RunID: r.ID, Type: "run_created", To: StatusQueued, At: now})
		return nil
	})
	return r, err
}

func (c *Coordinator) Get(ctx context.Context, id string) (AgentRun, error) {
	var r AgentRun
	err := c.repo.Transact(ctx, func(tx Transaction) error {
		var ok bool
		r, ok = tx.Get(id)
		if !ok {
			return ErrNotFound
		}
		return nil
	})
	return r, err
}

func (c *Coordinator) Events(ctx context.Context, runID string) ([]Event, error) {
	var e []Event
	err := c.repo.Transact(ctx, func(tx Transaction) error { e = tx.ListEvents(runID); return nil })
	return e, err
}

func (c *Coordinator) Start(ctx context.Context, id string) (AgentRun, error) {
	return c.transition(ctx, id, StatusRunning, "")
}
func (c *Coordinator) Complete(ctx context.Context, id string) (AgentRun, error) {
	return c.transition(ctx, id, StatusSucceeded, "")
}
func (c *Coordinator) Fail(ctx context.Context, id, reason string) (AgentRun, error) {
	return c.transition(ctx, id, StatusFailed, reason)
}
func (c *Coordinator) Timeout(ctx context.Context, id, reason string) (AgentRun, error) {
	return c.transition(ctx, id, StatusTimedOut, reason)
}
func (c *Coordinator) AcknowledgeCancellation(ctx context.Context, id string) (AgentRun, error) {
	return c.transition(ctx, id, StatusCancelled, "")
}

func (c *Coordinator) transition(ctx context.Context, id string, to Status, detail string) (AgentRun, error) {
	var out AgentRun
	now := c.now.Now()
	err := c.repo.Transact(ctx, func(tx Transaction) error {
		r, ok := tx.Get(id)
		if !ok {
			return ErrNotFound
		}
		if r.Status == to && to.Terminal() {
			out = r
			return nil
		}
		if r.Status.Terminal() {
			return ErrInvalidTransition
		}
		if !allowed(r.Status, to) {
			return ErrInvalidTransition
		}
		from := r.Status
		r.Status = to
		r.UpdatedAt = now
		r.Version++
		r.Failure = detail
		if to.Terminal() {
			v := now
			r.TerminalAt = &v
		}
		tx.Put(r)
		tx.Append(Event{RunID: id, Type: "status_changed", From: from, To: to, Detail: detail, At: now})
		out = r
		return nil
	})
	return out, err
}

func allowed(from, to Status) bool {
	switch to {
	case StatusRunning:
		return from == StatusQueued
	case StatusJoining:
		return from == StatusRunning
	case StatusCancelRequested:
		return from == StatusRunning || from == StatusJoining
	case StatusCancelled:
		return from == StatusQueued || from == StatusCancelRequested
	case StatusSucceeded, StatusFailed, StatusTimedOut:
		return from == StatusRunning || from == StatusJoining
	}
	return false
}

// JoinChildren atomically records joining and resolves when its all/any policy
// has a definitive result. If children are still running it returns joining.
func (c *Coordinator) JoinChildren(ctx context.Context, id string, mode JoinMode) (AgentRun, error) {
	if mode != JoinAll && mode != JoinAny {
		return AgentRun{}, ErrInvalid
	}
	var out AgentRun
	now := c.now.Now()
	err := c.repo.Transact(ctx, func(tx Transaction) error {
		r, ok := tx.Get(id)
		if !ok {
			return ErrNotFound
		}
		if r.Status.Terminal() {
			out = r
			return nil
		}
		if r.Status != StatusRunning && r.Status != StatusJoining {
			return ErrInvalidTransition
		}
		kids := tx.ListChildren(id)
		if len(kids) == 0 {
			return ErrNoChildren
		}
		if r.Status == StatusRunning {
			from := r.Status
			r.Status = StatusJoining
			r.Version++
			r.UpdatedAt = now
			tx.Append(Event{RunID: id, Type: "status_changed", From: from, To: StatusJoining, At: now})
		}
		allTerminal, anySuccess, anyFailed := true, false, false
		for _, k := range kids {
			allTerminal = allTerminal && k.Status.Terminal()
			anySuccess = anySuccess || k.Status == StatusSucceeded
			anyFailed = anyFailed || (k.Status == StatusFailed || k.Status == StatusCancelled || k.Status == StatusTimedOut)
		}
		resolve := false
		target := Status("")
		detail := ""
		if mode == JoinAll && allTerminal {
			resolve = true
			if anyFailed {
				target = StatusFailed
				detail = "one or more children failed"
			} else {
				target = StatusSucceeded
			}
		}
		if mode == JoinAny && (anySuccess || allTerminal) {
			resolve = true
			if anySuccess {
				target = StatusSucceeded
			} else {
				target = StatusFailed
				detail = "no child succeeded"
			}
		}
		if resolve {
			from := r.Status
			r.Status = target
			r.Failure = detail
			r.Version++
			r.UpdatedAt = now
			v := now
			r.TerminalAt = &v
			tx.Append(Event{RunID: id, Type: "status_changed", From: from, To: target, Detail: detail, At: now})
		}
		tx.Put(r)
		out = r
		return nil
	})
	return out, err
}

// CancelRun requests cancellation recursively. Queued work is cancelled
// immediately; running/joining work remains cancel_requested until acknowledged.
func (c *Coordinator) CancelRun(ctx context.Context, id string) (AgentRun, error) {
	var out AgentRun
	now := c.now.Now()
	err := c.repo.Transact(ctx, func(tx Transaction) error {
		if _, ok := tx.Get(id); !ok {
			return ErrNotFound
		}
		var visit func(string)
		visit = func(cur string) {
			r, _ := tx.Get(cur)
			for _, k := range tx.ListChildren(cur) {
				visit(k.ID)
			}
			if r.Status.Terminal() {
				if cur == id {
					out = r
				}
				return
			}
			from := r.Status
			if from == StatusQueued {
				r.Status = StatusCancelled
				v := now
				r.TerminalAt = &v
			} else {
				r.Status = StatusCancelRequested
			}
			r.Version++
			r.UpdatedAt = now
			tx.Put(r)
			tx.Append(Event{RunID: r.ID, Type: "status_changed", From: from, To: r.Status, At: now})
			if cur == id {
				out = r
			}
		}
		visit(id)
		return nil
	})
	return out, err
}

// ReconcileRestart makes interrupted work safely resumable and closes pending
// cancellations. It is idempotent and emits an event for every changed run.
func (c *Coordinator) ReconcileRestart(ctx context.Context) error {
	now := c.now.Now()
	return c.repo.Transact(ctx, func(tx Transaction) error {
		for _, r := range tx.ListRuns() {
			from := r.Status
			switch from {
			case StatusRunning, StatusJoining:
				r.Status = StatusQueued
			case StatusCancelRequested:
				r.Status = StatusCancelled
				v := now
				r.TerminalAt = &v
			default:
				continue
			}
			r.Version++
			r.UpdatedAt = now
			tx.Put(r)
			tx.Append(Event{RunID: r.ID, Type: "restart_reconciled", From: from, To: r.Status, At: now})
		}
		return nil
	})
}
