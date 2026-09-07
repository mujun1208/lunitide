package app

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/lunitide/lunitide/internal/llmadapter"
	"github.com/lunitide/lunitide/internal/toolruntime"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

// mcpToolPrefix namespaces merged MCP endpoint tools inside the model tool
// list: mcp_<endpointULID>_<tool>. The endpoint ID is a fixed 26-char ULID,
// so the split point is deterministic.
const mcpToolPrefix = "mcp_"

// mcpToolName composes the chat-facing tool name for one ready endpoint
// tool. ok is false when the composed name exceeds the 64-char function
// name budget common across providers or needs an opaque stable alias.
func mcpToolName(endpointID, tool string) (string, bool) {
	if len(endpointID) != 26 || strings.TrimSpace(tool) == "" || strings.ContainsAny(tool, " \t\r\n\x00") || len(tool) > 1024 {
		return "", false
	}
	name := mcpToolPrefix + endpointID + "_" + tool
	portable := len(name) <= 64
	for _, c := range name {
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') && c != '_' && c != '-' {
			portable = false
			break
		}
	}
	if portable {
		return name, true
	}
	// Keep the endpoint prefix stable for scope checks; resolve the opaque tool
	// suffix against the current admitted catalogue before invocation.
	digest := sha256.Sum256([]byte(tool))
	return fmt.Sprintf("%s%s_x%x", mcpToolPrefix, endpointID, digest[:16]), true
}

// parseMcpToolName splits a chat-facing mcp_ tool name back into its
// endpoint ID and MCP tool name.
func parseMcpToolName(name string) (endpointID, tool string, ok bool) {
	if !strings.HasPrefix(name, mcpToolPrefix) {
		return "", "", false
	}
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

func (e *Engine) mcpToolDefinitions() []llmadapter.ToolDefinition {
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
	defs := make([]llmadapter.ToolDefinition, 0, len(snapshot))
	for _, t := range snapshot {
		name, ok := mcpToolName(t.EndpointID, t.Tool)
		if !ok {
			continue
		}
		description := "MCP tool " + t.Tool + " on endpoint " + t.EndpointID + " (arguments pass through to the endpoint)"
		if t.Description != "" {
			description = t.Description
		}
		schema := mcpInputSchema(t.Schema)
		defs = append(defs, llmadapter.ToolDefinition{Name: name, Description: description, Schema: schema})
	}
	return defs
}

func mcpGatewayToolDefinitions(n int) []llmadapter.ToolDefinition {
	return []llmadapter.ToolDefinition{
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
	alias := mcpToolPrefix + endpointID + "_" + tool
	original, matches := "", 0
	for _, entry := range e.mcp6Registry.ReadyToolSnapshot() {
		if entry.EndpointID != endpointID {
			continue
		}
		name, ok := mcpToolName(entry.EndpointID, entry.Tool)
		if ok && name == alias {
			original = entry.Tool
			matches++
		}
	}
	if matches != 1 {
		return "", errors.New("MCP tool is no longer available or its alias is ambiguous; search again")
	}
	tool = original
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
	if err := e.CheckCapability(ctx, "browser"); err != nil {
		return toolruntime.Result{}, err
	}
	var a browserActCall
	if json.Unmarshal(raw, &a) != nil || strings.TrimSpace(a.Op) == "" {
		return toolruntime.Result{}, errors.New("browser.act needs op")
	}
	if err := rejectBrowserHumanWall(a.URL, ""); err != nil {
		return toolruntime.Result{}, err
	}
	switch a.Op {
	case "click", "type", "snapshot", "scroll", "back", "hover", "select", "press", "tabs", "wait", "dialog":
		out, err := e.invokeBrowserActViaPlaywright(ctx, a)
		if err != nil && browserActLooksStale(err, out.Output) {
			return toolruntime.Result{}, err
		}
		if err != nil {
			return out, err
		}
		return finishBrowserAct(a.URL, out.Output, out)
	case "navigate":
		u := strings.TrimSpace(a.URL)
		if u == "" {
			return toolruntime.Result{}, errors.New("browser.act navigate/read needs url")
		}
		if out, err := e.invokeBrowserActViaPlaywright(ctx, a); err != nil {
			return toolruntime.Result{}, err
		} else if out.Output != "" {
			e.browserLastURL.Store(session, u)
			return finishBrowserAct(u, out.Output, out)
		}
		args, _ := json.Marshal(map[string]string{"url": u})
		out, err := e.executeUserTool(ctx, mode, session, "web.fetch", args)
		if err != nil {
			return out, err
		}
		e.browserLastURL.Store(session, u)
		out = markBrowserNavigateFetch(out)
		return finishBrowserAct(u, out.Output, out)
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
	return e.searchMcpToolsScoped(raw, nil, false)
}
func (e *Engine) searchMcpToolsScoped(raw json.RawMessage, allowed []string, restrict bool) (string, error) {
	var a struct {
		Query string `json:"query"`
	}
	if json.Unmarshal(raw, &a) != nil || strings.TrimSpace(a.Query) == "" || utf8.RuneCountInString(a.Query) > 200 {
		return "", errors.New("mcp.search needs query")
	}
	if e.mcp6Registry == nil {
		return "", errors.New("MCP gateway unavailable")
	}
	q := strings.ToLower(strings.TrimSpace(a.Query))
	type hit struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		InputSchema json.RawMessage `json:"inputSchema"`
		score       int
	}
	hits := []hit{}
	for _, t := range e.mcp6Registry.ReadyToolSnapshot() {
		if !e.mcpNameAllowed("", t.EndpointID, allowed, restrict) {
			continue
		}
		name, ok := mcpToolName(t.EndpointID, t.Tool)
		if !ok {
			continue
		}
		blob := strings.ToLower(t.Tool + " " + t.Description + " " + name)
		score := mcpSearchScore(q, blob)
		if score == 0 {
			continue
		}
		hits = append(hits, hit{Name: name, Description: t.Description, InputSchema: mcpInputSchema(t.Schema), score: score})
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].score > hits[j].score })
	if len(hits) > 12 {
		hits = hits[:12]
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
