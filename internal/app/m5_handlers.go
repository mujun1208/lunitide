package app

import (
	"context"

	"github.com/lunitide/lunitide/internal/bridge"
)

// M5 placeholder handlers. The five method schemas (run.send, run.cancel,
// browser.act, mcp.invoke, workspace.convert) are frozen contract-first; the
// runtime behavior lands slice by slice (run.send/run.cancel in Slice 1,
// browser.act in Slice 4, workspace.convert in Slice 5). Until the M5 feature
// flag ships, every method answers with a stable, non-retryable
// FEATURE_DISABLED error so the renderer can hide the entry points and the
// contract test keeps the dispatch table in sync with the schemas.

func m5FeatureDisabled(r bridge.Request) bridge.Response {
	return r.Fail("FEATURE_DISABLED", "该能力尚未开放", false)
}

func handleRunSend(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	return m5FeatureDisabled(r)
}

func handleRunCancel(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	return m5FeatureDisabled(r)
}

func handleBrowserAct(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	return m5FeatureDisabled(r)
}

func handleMcpInvoke(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	return m5FeatureDisabled(r)
}

func handleWorkspaceConvert(e *Engine, ctx context.Context, r bridge.Request) bridge.Response {
	return m5FeatureDisabled(r)
}
