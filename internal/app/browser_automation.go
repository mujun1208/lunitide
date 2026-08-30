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

type browserActCall struct {
	Op        string `json:"op"`
	URL       string `json:"url"`
	Selector  string `json:"selector"`
	Text      string `json:"text"`
	Key       string `json:"key"`
	Direction string `json:"direction"`
	MS        int    `json:"ms"`
	Accept    *bool  `json:"accept"`
	Tab       string `json:"tab"`
	Index     *int   `json:"index"`
}

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

func playwrightToolNames(op string) []string {
	switch op {
	case "click":
		return []string{"browser_click", "click"}
	case "type":
		return []string{"browser_type", "type"}
	case "snapshot":
		return []string{"browser_snapshot", "snapshot"}
	case "navigate":
		return []string{"browser_navigate", "browser_goto", "navigate"}
	case "scroll":
		return []string{"browser_scroll", "scroll"}
	case "back":
		return []string{"browser_navigate_back", "browser_back", "navigate_back"}
	case "hover":
		return []string{"browser_hover", "hover"}
	case "select":
		return []string{"browser_select_option", "select_option"}
	case "press":
		return []string{"browser_press_key", "browser_press", "press_key", "press"}
	case "tabs":
		return []string{"browser_tabs", "tabs"}
	case "wait":
		return []string{"browser_wait_for", "browser_wait", "wait_for"}
	case "dialog":
		return []string{"browser_handle_dialog", "handle_dialog"}
	default:
		return nil
	}
}

func (e *Engine) findPlaywrightTool(op string) (endpointID, tool string, ok bool) {
	if e == nil || e.mcp6Registry == nil {
		return "", "", false
	}
	candidates := playwrightToolNames(op)
	if len(candidates) == 0 {
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

func (e *Engine) invokeBrowserActViaPlaywright(ctx context.Context, call browserActCall) (toolruntime.Result, error) {
	op := strings.TrimSpace(call.Op)
	if op == "scroll" {
		if _, _, ok := e.findPlaywrightTool("scroll"); !ok {
			key := "PageDown"
			if strings.EqualFold(call.Direction, "up") {
				key = "PageUp"
			}
			return e.invokeBrowserActViaPlaywright(ctx, browserActCall{Op: "press", Key: key})
		}
	}
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
	args, skip := playwrightArgs(call)
	if skip {
		return toolruntime.Result{}, nil
	}
	raw, _ := json.Marshal(args)
	out, err := e.invokeMcpTool(ctx, endpointID, tool, raw)
	if err != nil {
		return toolruntime.Result{}, err
	}
	if browserActNeedsSnapshot(call) {
		snap := e.playwrightSnapshotFollowup(ctx)
		return toolruntime.Result{Output: appendPostActSnapshot(op, out, snap)}, nil
	}
	return toolruntime.Result{Output: strings.TrimSpace(out)}, nil
}

func playwrightArgs(call browserActCall) (map[string]any, bool) {
	args := map[string]any{}
	selector := strings.TrimSpace(call.Selector)
	text := strings.TrimSpace(call.Text)
	switch strings.TrimSpace(call.Op) {
	case "click", "hover":
		if selector == "" {
			return nil, true
		}
		args["element"] = selector
		args["selector"] = selector
	case "type":
		if text == "" {
			return nil, true
		}
		args["text"] = text
		if selector != "" {
			args["element"] = selector
			args["selector"] = selector
		}
	case "select":
		if selector == "" || text == "" {
			return nil, true
		}
		args["element"] = selector
		args["selector"] = selector
		args["values"] = []string{text}
	case "navigate":
		if strings.TrimSpace(call.URL) == "" {
			return nil, true
		}
		args["url"] = strings.TrimSpace(call.URL)
	case "scroll":
		dir := strings.ToLower(strings.TrimSpace(call.Direction))
		if dir != "up" {
			dir = "down"
		}
		args["direction"] = dir
		if selector != "" {
			args["selector"] = selector
		}
	case "press":
		key := strings.TrimSpace(call.Key)
		if key == "" {
			key = text
		}
		if key == "" {
			return nil, true
		}
		args["key"] = key
	case "tabs":
		action := strings.TrimSpace(call.Tab)
		if action == "" {
			action = "list"
		}
		args["action"] = action
		if call.Index != nil {
			args["index"] = *call.Index
		}
	case "wait":
		if call.MS > 0 {
			args["time"] = call.MS
		}
		if selector != "" {
			args["selector"] = selector
			args["text"] = selector
		}
	case "dialog":
		accept := true
		if call.Accept != nil {
			accept = *call.Accept
		}
		args["accept"] = accept
	case "snapshot", "back":
		// pass-through
	default:
		return nil, true
	}
	return args, false
}

func browserActNeedsSnapshot(call browserActCall) bool {
	switch strings.TrimSpace(call.Op) {
	case "snapshot", "read", "wait", "dialog":
		return false
	case "tabs":
		return strings.TrimSpace(call.Tab) != "" && strings.TrimSpace(call.Tab) != "list"
	default:
		return true
	}
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
