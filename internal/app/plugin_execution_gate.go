package app

import (
	"context"
	"encoding/json"

	"github.com/lunitide/lunitide/internal/m8app"
)

// CheckCapability enforces the current plugin grant at a capability's actual
// entry point. A missing plugin service retains legacy standalone engines.
func (e *Engine) CheckCapability(ctx context.Context, pluginIDs ...string) error {
	if e == nil || e.m8plugin == nil {
		return nil
	}
	return e.m8plugin.RequireEnabled(ctx, pluginIDs...)
}

// AcquireCapability must span the complete operation, including asynchronous
// streams. Call release when that operation ends, not when an ACK is returned.
func (e *Engine) AcquireCapability(ctx context.Context, pluginIDs ...string) (context.Context, func(), error) {
	if e == nil || e.m8plugin == nil {
		return ctx, func() {}, nil
	}
	return e.m8plugin.AcquireCapability(ctx, pluginIDs...)
}

func (e *Engine) acquireToolCapability(ctx context.Context, name string, args json.RawMessage) (context.Context, func(), error) {
	return e.AcquireCapability(ctx, m8app.ToolPluginIDs(name, args)...)
}

func (e *Engine) checkToolCapability(ctx context.Context, name string, args json.RawMessage) error {
	return e.CheckCapability(ctx, m8app.ToolPluginIDs(name, args)...)
}

func (e *Engine) wirePluginExecutionGate() {
	if e.tools != nil {
		e.tools.SetExecutionGate(e.checkToolCapability)
		e.tools.SetExecutionScope(e.acquireToolCapability)
	}
}
