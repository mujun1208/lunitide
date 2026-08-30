package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/lunitide/lunitide/internal/domain/m7flow"
	"github.com/lunitide/lunitide/internal/gateway"
	"github.com/lunitide/lunitide/internal/m7app"
	"github.com/lunitide/lunitide/internal/m8app"
	"github.com/lunitide/lunitide/internal/mcp6"
	"github.com/oklog/ulid/v2"
)

// settingsPlaneToolDefinitions exposes curated MCP install and plugin
// install as chat-callable tools (ordinary chat and 月伴 alike). These
// wrap the existing settings-plane services — they do not unfreeze the
// M5 mcp.invoke stub.
func (e *Engine) settingsPlaneToolDefinitions() []gateway.ToolDefinition {
	var defs []gateway.ToolDefinition
	if e.m7mcp != nil {
		defs = append(defs,
			gateway.ToolDefinition{Name: "mcp.presets", Description: "List curated MCP server presets that can be installed with mcp.install (id, name, description, whether an extra path argument is required)", Schema: []byte(`{"type":"object","properties":{},"additionalProperties":false}`)},
			gateway.ToolDefinition{Name: "mcp.install", Description: "Install one curated MCP preset from mcp.presets; pass arg when the preset needs a directory or repo path", Schema: []byte(`{"type":"object","properties":{"presetId":{"type":"string","minLength":1,"maxLength":64},"arg":{"type":"string","maxLength":512,"description":"placeholder value when the preset needsArgs"}},"required":["presetId"],"additionalProperties":false}`)},
		)
	}
	if e.m8plugin != nil {
		defs = append(defs,
			gateway.ToolDefinition{Name: "plugin.search", Description: "Search locally known plugins (market source degrades to the installed catalogue)", Schema: []byte(`{"type":"object","properties":{"query":{"type":"string","minLength":1,"maxLength":200},"kind":{"type":"string"}},"required":["query"],"additionalProperties":false}`)},
			gateway.ToolDefinition{Name: "plugin.install", Description: "Toggle a named harness roster card (web-search, git, clipboard, …). This does not download Cordis/TypeScript packages and does not add Git or Python. origin is market, local, or dev; source is the roster id.", Schema: []byte(`{"type":"object","properties":{"origin":{"type":"string","enum":["market","local","dev"]},"source":{"type":"string","minLength":1,"maxLength":512}},"required":["origin","source"],"additionalProperties":false}`)},
		)
	}
	return defs
}

func (e *Engine) invokeSettingsPlaneTool(ctx context.Context, name string, raw json.RawMessage) (string, error) {
	switch name {
	case "mcp.presets":
		return e.invokeMcpPresets()
	case "mcp.install":
		return e.invokeMcpInstallPreset(ctx, raw)
	case "plugin.search":
		return e.invokePluginSearch(ctx, raw)
	case "plugin.install":
		return e.invokePluginInstall(ctx, raw)
	default:
		return "", errors.New("unknown settings-plane tool")
	}
}

func (e *Engine) invokeMcpPresets() (string, error) {
	type row struct {
		ID          string `json:"presetId"`
		Name        string `json:"name"`
		Description string `json:"description"`
		NeedsArgs   bool   `json:"needsArgs"`
		ArgHint     string `json:"argHint,omitempty"`
		Category    string `json:"category"`
	}
	presets := mcp6.Presets()
	items := make([]row, 0, len(presets))
	for _, p := range presets {
		items = append(items, row{ID: p.ID, Name: p.Name, Description: p.Description, NeedsArgs: p.NeedsArgs, ArgHint: p.ArgHint, Category: p.Category})
	}
	b, err := json.Marshal(map[string]any{"items": items})
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func (e *Engine) invokeMcpInstallPreset(ctx context.Context, raw json.RawMessage) (string, error) {
	if e.m7mcp == nil {
		return "", errors.New("MCP 服务暂时不可用")
	}
	var a struct {
		PresetID string `json:"presetId"`
		Arg      string `json:"arg"`
	}
	if json.Unmarshal(raw, &a) != nil || strings.TrimSpace(a.PresetID) == "" {
		return "", errors.New("mcp.install needs presetId")
	}
	preset, ok := mcp6.PresetByID(strings.TrimSpace(a.PresetID))
	if !ok {
		return "", errors.New("unknown MCP preset id; call mcp.presets first")
	}
	args := preset.Args
	if preset.NeedsArgs {
		arg := strings.TrimSpace(a.Arg)
		if arg == "" {
			return "", errors.New("this preset needs arg: " + preset.ArgHint)
		}
		args = preset.ResolveArgs(arg)
	}
	res, err := e.m7mcp.Add(ctx, m7app.McpAddInput{
		Origin:         m7flow.McpOriginManual,
		Transport:      preset.Transport,
		Command:        preset.Command,
		Args:           args,
		RiskConfirmed:  true,
		Actor:          "chat",
		IdempotencyKey: ulid.Make().String(),
	})
	if err != nil {
		return "", err
	}
	e.admitSettingsMcp(ctx, m7flow.McpEndpointConfig{
		EndpointID: res.EndpointID,
		Transport:  preset.Transport,
		Command:    preset.Command,
		ArgsJSON:   mustJSONArgs(args),
		Enabled:    true,
		State:      res.State,
	})
	b, _ := json.Marshal(map[string]any{"endpointId": res.EndpointID, "state": res.State, "presetId": preset.ID})
	return string(b), nil
}

func (e *Engine) invokePluginSearch(ctx context.Context, raw json.RawMessage) (string, error) {
	if e.m8plugin == nil {
		return "", errors.New("插件服务暂时不可用")
	}
	var a struct {
		Query string `json:"query"`
		Kind  string `json:"kind"`
	}
	if json.Unmarshal(raw, &a) != nil || strings.TrimSpace(a.Query) == "" {
		return "", errors.New("plugin.search needs query")
	}
	list, err := e.m8plugin.List(ctx, a.Kind, "")
	if err != nil {
		return "", err
	}
	needle := strings.ToLower(strings.TrimSpace(a.Query))
	type hit struct {
		InstallID string `json:"installId"`
		PluginID  string `json:"pluginId"`
		Kind      string `json:"kind"`
		State     string `json:"state"`
		Origin    string `json:"origin"`
	}
	var items []hit
	for _, plugin := range list.Plugins {
		haystack := strings.ToLower(plugin.PluginID + " " + plugin.Publisher + " " + plugin.Kind + " " + plugin.Origin)
		if !strings.Contains(haystack, needle) {
			continue
		}
		items = append(items, hit{InstallID: plugin.InstallID, PluginID: plugin.PluginID, Kind: plugin.Kind, State: plugin.State, Origin: plugin.Origin})
		if len(items) >= 20 {
			break
		}
	}
	b, _ := json.Marshal(map[string]any{"items": items})
	return string(b), nil
}

func (e *Engine) invokePluginInstall(ctx context.Context, raw json.RawMessage) (string, error) {
	if e.m8plugin == nil {
		return "", errors.New("插件服务暂时不可用")
	}
	var a struct {
		Origin string `json:"origin"`
		Source string `json:"source"`
	}
	if json.Unmarshal(raw, &a) != nil || (a.Origin != "market" && a.Origin != "local" && a.Origin != "dev") || strings.TrimSpace(a.Source) == "" {
		return "", errors.New("plugin.install needs origin (market|local|dev) and source")
	}
	res, err := e.m8plugin.Install(ctx, m8app.InstallInput{
		Origin:    a.Origin,
		Source:    strings.TrimSpace(a.Source),
		RequestID: ulid.Make().String(),
		Actor:     "chat",
	})
	if err != nil {
		return "", err
	}
	b, _ := json.Marshal(res)
	return string(b), nil
}
