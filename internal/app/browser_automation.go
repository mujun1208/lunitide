package app

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/lunitide/lunitide/internal/domain/m7flow"
	"github.com/lunitide/lunitide/internal/m7app"
	"github.com/lunitide/lunitide/internal/mcp6"
	"github.com/lunitide/lunitide/internal/toolruntime"
)

const playwrightPackage = "@playwright/mcp"

func (e *Engine) playwrightEndpoint() (endpointID string, ok bool) {
	if e == nil || e.mcp6Registry == nil {
		return "", false
	}
	for _, t := range e.mcp6Registry.ReadyToolSnapshot() {
		if strings.Contains(strings.ToLower(t.Tool), "browser") {
			return t.EndpointID, true
		}
	}
	return "", false
}

func (e *Engine) findPlaywrightTool(op string) (endpointID, tool string, ok bool) {
	if e == nil || e.mcp6Registry == nil {
		return "", "", false
	}
	want := map[string][]string{
		"click":    {"browser_click", "click"},
		"type":     {"browser_type", "type"},
		"snapshot": {"browser_snapshot", "snapshot"},
		"navigate": {"browser_navigate", "browser_goto", "navigate"},
	}
	candidates, ok := want[op]
	if !ok {
		return "", "", false
	}
	endpoint, ready := e.playwrightEndpoint()
	if !ready {
		return "", "", false
	}
	for _, t := range e.mcp6Registry.ReadyToolSnapshot() {
		if t.EndpointID != endpoint {
			continue
		}
		lower := strings.ToLower(t.Tool)
		for _, name := range candidates {
			if lower == name || strings.HasSuffix(lower, "_"+name) {
				return endpoint, t.Tool, true
			}
		}
	}
	return "", "", false
}

func (e *Engine) ensurePlaywrightMCP(ctx context.Context) bool {
	if _, ok := e.playwrightEndpoint(); ok {
		return true
	}
	if e.m7mcp == nil {
		return false
	}
	if _, err := e.invokeMcpInstallPreset(ctx, []byte(`{"presetId":"playwright"}`)); err != nil {
		return false
	}
	deadline := time.Now().Add(45 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := e.playwrightEndpoint(); ok {
			return true
		}
		time.Sleep(500 * time.Millisecond)
	}
	return false
}

func (e *Engine) invokeBrowserActViaPlaywright(ctx context.Context, op, selector, text, pageURL string) (toolruntime.Result, error) {
	endpointID, tool, ok := e.findPlaywrightTool(op)
	if !ok {
		if !e.ensurePlaywrightMCP(ctx) {
			return toolruntime.Result{}, nil
		}
		endpointID, tool, ok = e.findPlaywrightTool(op)
		if !ok {
			return toolruntime.Result{}, nil
		}
	}
	args := map[string]any{}
	switch op {
	case "click":
		if strings.TrimSpace(selector) == "" {
			return toolruntime.Result{}, nil
		}
		args["element"] = selector
		args["selector"] = selector
	case "type":
		if strings.TrimSpace(text) == "" {
			return toolruntime.Result{}, nil
		}
		args["text"] = text
		if strings.TrimSpace(selector) != "" {
			args["element"] = selector
			args["selector"] = selector
		}
	case "navigate":
		if strings.TrimSpace(pageURL) == "" {
			return toolruntime.Result{}, nil
		}
		args["url"] = strings.TrimSpace(pageURL)
	case "snapshot":
		// pass-through
	default:
		return toolruntime.Result{}, nil
	}
	raw, _ := json.Marshal(args)
	out, err := e.invokeMcpTool(ctx, endpointID, tool, raw)
	if err != nil {
		return toolruntime.Result{}, err
	}
	snap := ""
	if op != "snapshot" {
		snap = e.playwrightSnapshotFollowup(ctx)
	}
	return toolruntime.Result{Output: appendPostActSnapshot(op, out, snap)}, nil
}

func (e *Engine) playwrightSnapshotFollowup(ctx context.Context) string {
	endpointID, tool, ok := e.findPlaywrightTool("snapshot")
	if !ok {
		return ""
	}
	out, err := e.invokeMcpTool(ctx, endpointID, tool, json.RawMessage(`{}`))
	if err != nil {
		return ""
	}
	return out
}

// appendPostActSnapshot mirrors OpenClaw: after click/type/navigate the
// model sees a fresh page tree so it does not keep acting on stale refs.
func appendPostActSnapshot(op, primary, snapshot string) string {
	primary = strings.TrimSpace(primary)
	snapshot = strings.TrimSpace(snapshot)
	if snapshot == "" || op == "snapshot" || op == "read" {
		return primary
	}
	if primary == "" {
		return snapshot
	}
	return primary + "\n\n[snapshot after " + op + "]\n" + snapshot
}

// SeedPlaywrightMcp registers the bundled Playwright MCP server when missing
// so browser.act click/type/snapshot works without manual setup.
func (e *Engine) SeedPlaywrightMcp(ctx context.Context) {
	if e == nil || e.m7mcp == nil {
		return
	}
	eps, err := e.m7mcp.List(ctx, "")
	if err != nil {
		return
	}
	if mcpEndpointHasPackage(eps, playwrightPackage) {
		return
	}
	p, ok := mcp6.PresetByID("playwright")
	if !ok {
		return
	}
	res, err := e.m7mcp.Add(ctx, m7app.McpAddInput{
		Origin: m7flow.McpOriginManual, Transport: m7flow.McpTransportStdio,
		Command: p.Command, Args: p.Args, RiskConfirmed: true,
		Actor: "system", IdempotencyKey: "seed-playwright",
	})
	if err != nil {
		return
	}
	ep, err := e.m7mcp.Toggle(ctx, res.EndpointID, true, "system")
	if err != nil {
		return
	}
	e.admitSettingsMcp(ctx, m7flow.McpEndpointConfig{
		EndpointID: ep.EndpointID,
		Transport:  p.Transport,
		Command:    p.Command,
		ArgsJSON:   mustJSONArgs(p.Args),
		Enabled:    true,
		State:      ep.State,
	})
}
