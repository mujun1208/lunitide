package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/lunitide/lunitide/internal/gateway"
	"github.com/lunitide/lunitide/internal/toolruntime"
	"strings"
	"time"
)

// mcpToolPrefix namespaces merged MCP endpoint tools inside the model tool
// list: mcp_<endpointULID>_<tool>. The endpoint ID is a fixed 26-char ULID,
// so the split point is deterministic.
const mcpToolPrefix = "mcp_"

// mcpToolName composes the chat-facing tool name for one ready endpoint
// tool. ok is false when the composed name exceeds the 64-char function
// name budget common across providers or carries characters outside the
// portable [A-Za-z0-9_-] set; such tools are skipped rather than renamed.
func mcpToolName(endpointID, tool string) (string, bool) {
	name := mcpToolPrefix + endpointID + "_" + tool
	if len(name) > 64 {
		return "", false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') && c != '_' && c != '-' {
			return "", false
		}
	}
	return name, true
}

// parseMcpToolName splits a chat-facing mcp_ tool name back into its
// endpoint ID and MCP tool name.
func parseMcpToolName(name string) (endpointID, tool string, ok bool) {
	rest := strings.TrimPrefix(name, mcpToolPrefix)
	if len(name) <= len(mcpToolPrefix) || len(rest) < 28 || rest[26] != '_' {
		return "", "", false
	}
	return rest[:26], rest[27:], true
}

// mcpToolDefinitions merges ready MCP endpoint tools into the engine tool
// list. When the describe cache carries a real input schema it is used
// verbatim (after a JSON object sanity check); otherwise the tool falls
// back to a pass-through object schema. Invoke still enforces pinning,
// state and breaker per call regardless of which schema was advertised.
const mcpDirectToolCap = 12

func (e *Engine) mcpToolDefinitions() []gateway.ToolDefinition {
	if e.mcp6Registry == nil {
		return nil
	}
	snapshot := e.mcp6Registry.ReadyToolSnapshot()
	if len(snapshot) == 0 {
		return nil
	}
	if len(snapshot) > mcpDirectToolCap {
		return mcpGatewayToolDefinitions(len(snapshot))
	}
	defs := make([]gateway.ToolDefinition, 0, len(snapshot))
	for _, t := range snapshot {
		name, ok := mcpToolName(t.EndpointID, t.Tool)
		if !ok {
			continue
		}
		description := "MCP tool " + t.Tool + " on endpoint " + t.EndpointID + " (arguments pass through to the endpoint)"
		if t.Description != "" {
			description = t.Description
		}
		schema := []byte(`{"type":"object","additionalProperties":true}`)
		if len(t.Schema) > 0 && json.Valid(t.Schema) && t.Schema[0] == '{' {
			schema = t.Schema
		}
		defs = append(defs, gateway.ToolDefinition{Name: name, Description: description, Schema: schema})
	}
	return defs
}

func mcpGatewayToolDefinitions(n int) []gateway.ToolDefinition {
	return []gateway.ToolDefinition{
		{Name: "mcp.search", Description: fmt.Sprintf("Search the %d connected MCP tools by name or description; then call mcp.call with the returned name", n), Schema: []byte(`{"type":"object","properties":{"query":{"type":"string","minLength":1,"maxLength":200}},"required":["query"],"additionalProperties":false}`)},
		{Name: "mcp.call", Description: "Invoke one MCP tool previously returned by mcp.search (name is mcp_<endpoint>_<tool>)", Schema: []byte(`{"type":"object","properties":{"name":{"type":"string","minLength":1,"maxLength":64},"arguments":{"type":"object"}},"required":["name"],"additionalProperties":false}`)},
	}
}

// invokeMcpTool executes one merged MCP tool call through the mcp6
// registry. The registry owns state gating, capability pinning, credential
// leasing and breaker accounting; the 30 s deadline mirrors the frozen
// mcp6.invoke upper bound. The result is flattened to canonical JSON so it
// can ride the normal tool-message path back to the model.
func (e *Engine) invokeMcpTool(ctx context.Context, endpointID, tool string, rawArgs json.RawMessage) (string, error) {
	if e.mcp6Registry == nil {
		return "", errors.New("MCP gateway unavailable")
	}
	var args map[string]any
	if len(rawArgs) > 0 {
		if err := json.Unmarshal(rawArgs, &args); err != nil {
			return "", errors.New("MCP tool arguments must be a JSON object")
		}
	}
	if args == nil {
		args = map[string]any{}
	}
	invokeCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	result, err := e.mcp6Registry.Invoke(invokeCtx, endpointID, tool, args)
	if err != nil {
		return "", err
	}
	b, _ := json.Marshal(result.Result)
	return string(b), nil
}

func (e *Engine) invokeBrowserAct(ctx context.Context, mode executionMode, session string, raw json.RawMessage) (toolruntime.Result, error) {
	var a browserActCall
	if json.Unmarshal(raw, &a) != nil || strings.TrimSpace(a.Op) == "" {
		return toolruntime.Result{}, errors.New("browser.act needs op")
	}
	switch a.Op {
	case "click", "type", "snapshot", "scroll", "back", "hover", "select", "press", "tabs", "wait", "dialog":
		return e.invokeBrowserActViaPlaywright(ctx, a)
	case "navigate":
		u := strings.TrimSpace(a.URL)
		if u == "" {
			return toolruntime.Result{}, errors.New("browser.act navigate/read needs url")
		}
		if out, err := e.invokeBrowserActViaPlaywright(ctx, a); err != nil {
			return toolruntime.Result{}, err
		} else if out.Output != "" {
			e.browserLastURL.Store(session, u)
			return out, nil
		}
		args, _ := json.Marshal(map[string]string{"url": u})
		out, err := e.executeUserTool(ctx, mode, session, "web.fetch", args)
		if err == nil {
			e.browserLastURL.Store(session, u)
			out = markBrowserNavigateFetch(out)
		}
		return out, err
	case "read":
		u := strings.TrimSpace(a.URL)
		if u == "" {
			if prev, ok := e.browserLastURL.Load(session); ok {
				u, _ = prev.(string)
			}
		}
		if u == "" {
			return toolruntime.Result{}, errors.New("browser.act navigate/read needs url")
		}
		args, _ := json.Marshal(map[string]string{"url": u})
		out, err := e.executeUserTool(ctx, mode, session, "web.fetch", args)
		if err == nil {
			e.browserLastURL.Store(session, u)
		}
		return out, err
	default:
		return toolruntime.Result{}, errors.New("browser.act op must be navigate, read, click, type, snapshot, scroll, back, hover, select, press, tabs, wait or dialog")
	}
}

func (e *Engine) searchMcpTools(raw json.RawMessage) (string, error) {
	var a struct {
		Query string `json:"query"`
	}
	if json.Unmarshal(raw, &a) != nil || strings.TrimSpace(a.Query) == "" {
		return "", errors.New("mcp.search needs query")
	}
	if e.mcp6Registry == nil {
		return "", errors.New("MCP gateway unavailable")
	}
	q := strings.ToLower(strings.TrimSpace(a.Query))
	type hit struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	var hits []hit
	for _, t := range e.mcp6Registry.ReadyToolSnapshot() {
		name, ok := mcpToolName(t.EndpointID, t.Tool)
		if !ok {
			continue
		}
		blob := strings.ToLower(t.Tool + " " + t.Description + " " + name)
		if !strings.Contains(blob, q) {
			continue
		}
		hits = append(hits, hit{Name: name, Description: t.Description})
		if len(hits) == 12 {
			break
		}
	}
	b, _ := json.Marshal(map[string]any{"tools": hits})
	return string(b), nil
}

func (e *Engine) callMcpToolByName(ctx context.Context, raw json.RawMessage) (string, error) {
	var a struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if json.Unmarshal(raw, &a) != nil {
		return "", errors.New("mcp.call needs name")
	}
	endpointID, tool, ok := parseMcpToolName(a.Name)
	if !ok {
		return "", errors.New("mcp.call name must be an mcp_<endpoint>_<tool> from mcp.search")
	}
	args, _ := json.Marshal(a.Arguments)
	if a.Arguments == nil {
		args = []byte(`{}`)
	}
	return e.invokeMcpTool(ctx, endpointID, tool, args)
}
