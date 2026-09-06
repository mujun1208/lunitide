package toolruntime

import (
	"context"
	"encoding/json"
)

// ExecutionGate checks live capability grants before hooks, approvals or IO.
// It applies equally to chat, streaming, plans and delegated tool execution.
type ExecutionGate func(context.Context, string, json.RawMessage) error

type ExecutionScope func(context.Context, string, json.RawMessage) (context.Context, func(), error)

func (r *Runtime) SetExecutionScope(scope ExecutionScope) {
	r.gateMu.Lock()
	r.executionScope = scope
	r.gateMu.Unlock()
}

func (r *Runtime) beginExecutionScope(ctx context.Context, name string, args json.RawMessage) (context.Context, func(), error) {
	r.gateMu.RLock()
	scope := r.executionScope
	r.gateMu.RUnlock()
	if scope != nil {
		return scope(ctx, name, args)
	}
	return ctx, func() {}, r.checkExecutionGate(ctx, name, args)
}

func (r *Runtime) SetExecutionGate(gate ExecutionGate) {
	r.gateMu.Lock()
	r.executionGate = gate
	r.gateMu.Unlock()
}

func (r *Runtime) checkExecutionGate(ctx context.Context, name string, args json.RawMessage) error {
	r.gateMu.RLock()
	gate := r.executionGate
	r.gateMu.RUnlock()
	if gate != nil {
		return gate(ctx, name, args)
	}
	return nil
}
