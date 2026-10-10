package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/lunitide/lunitide/internal/domain/m7flow"
	"github.com/lunitide/lunitide/internal/llmadapter"
	"github.com/lunitide/lunitide/internal/m7app"
	"github.com/lunitide/lunitide/internal/m8app"
	"github.com/lunitide/lunitide/internal/mcp6"
	"github.com/oklog/ulid/v2"
)

// settingsPlaneToolDefinitions exposes curated MCP install and plugin
// install as chat-callable tools (ordinary chat and 月伴 alike). These
// wrap the existing settings-plane services — they do not unfreeze the
// M5 mcp.invoke stub.
func (e *Engine) settingsPlaneToolDefinitions() []llmadapter.ToolDefinition {
	// capability.discover is unconditional: it works with zero MCP services
	// wired (built-in tools and skills still match) and is the escape hatch
	// for "no such tool" turns (capability-self-bootstrap P2).
	defs := []llmadapter.ToolDefinition{
		{Name: "capability.discover", Description: "Discover how to get a capability the current tool list lacks. Give need as one short sentence about what the task requires (e.g. 搜索某公司公开信息 / 深度编辑已有 Word / 生成图表). It searches built-in tools, connected MCP endpoint tools, installed skills, and the curated one-click MCP preset catalog, and returns matches with how to use or install each. Call this BEFORE answering that something cannot be done for lack of a tool; never claim a capability exists without a match here.", Schema: []byte(`{"type":"object","properties":{"need":{"type":"string","minLength":1,"maxLength":400}},"required":["need"],"additionalProperties":false}`)},
	}
	if e.m7mcp != nil {
		defs = append(defs,
			llmadapter.ToolDefinition{Name: "mcp.presets", Description: "List curated one-click MCP presets for mcp.install. The current catalog is free and needs no token. Filesystem already has a local sandbox path in argDefault; do not ask the user to type a directory or API key.", Schema: []byte(`{"type":"object","properties":{},"additionalProperties":false}`)},
			llmadapter.ToolDefinition{Name: "mcp.install", Description: "Install one curated MCP preset from mcp.presets by presetId. Ask the user first. Do not ask for a path, token, or connection string; omit arg and the sandbox is used automatically for filesystem. In manual-approval mode an approval card pops up and the install runs once the user approves; tools become available from the next turn, so report the install result and continue the task then.", Schema: []byte(`{"type":"object","properties":{"presetId":{"type":"string","minLength":1,"maxLength":64},"arg":{"type":"string","maxLength":512,"description":"unused for the current one-click catalog; filesystem sandboxes itself when omitted"}},"required":["presetId"],"additionalProperties":false}`)},
		)
	}
	if e.m8plugin != nil {
		defs = append(defs,
			llmadapter.ToolDefinition{Name: "plugin.search", Description: "Search locally known plugins (market source degrades to the installed catalogue)", Schema: []byte(`{"type":"object","properties":{"query":{"type":"string","minLength":1,"maxLength":200},"kind":{"type":"string"}},"required":["query"],"additionalProperties":false}`)},
			llmadapter.ToolDefinition{Name: "plugin.install", Description: "Toggle a named harness roster card (web-search, git, clipboard, …). This does not download Cordis/TypeScript packages and does not add Git or Python. origin is market, local, or dev; source is the roster id.", Schema: []byte(`{"type":"object","properties":{"origin":{"type":"string","enum":["market","local","dev"]},"source":{"type":"string","minLength":1,"maxLength":512}},"required":["origin","source"],"additionalProperties":false}`)},
		)
	}
	return defs
}

func (e *Engine) invokeSettingsPlaneTool(ctx context.Context, name string, raw json.RawMessage) (string, error) {
	switch name {
	case "mcp.presets":
		return e.invokeMcpPresets()
	// mcp.install is deliberately absent: it executes through the tool
	// runtime so the approval gate covers it (capability-self-bootstrap P3).
	case "capability.discover":
		return e.discoverCapabilities(ctx, raw)
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
		ID              string `json:"presetId"`
		Name            string `json:"name"`
		Description     string `json:"description"`
		NeedsArgs       bool   `json:"needsArgs"`
		NeedsCredential bool   `json:"needsCredential"`
		ArgHint         string `json:"argHint,omitempty"`
		ArgDefault      string `json:"argDefault,omitempty"`
		Category        string `json:"category"`
	}
	presets := mcp6.Presets()
	items := make([]row, 0, len(presets))
	for _, p := range presets {
		argDefault := p.ArgDefault
		if p.NeedsArgs && p.ArgPlaceholder == "{{dir}}" && argDefault == "" {
			argDefault = mcp6.PrepareSandbox(p.ID)
		}
		items = append(items, row{ID: p.ID, Name: p.Name, Description: p.Description, NeedsArgs: p.NeedsArgs, NeedsCredential: p.NeedsCredential, ArgHint: p.ArgHint, ArgDefault: argDefault, Category: p.Category})
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
	// capability-self-bootstrap P4: installs stay idempotent. A surviving
	// endpoint for this preset means reconnect, never a duplicate Add.
	if endpointID, live := e.installedPresetEndpoint(preset.ID); live {
		b, _ := json.Marshal(map[string]any{"endpointId": endpointID, "state": "already_installed", "presetId": preset.ID, "note": "该预置此前已安装，本轮未重复安装。若其工具未挂载，请在设置中重新连接该端点。"})
		return string(b), nil
	}
	args := preset.Args
	if preset.NeedsArgs {
		arg := strings.TrimSpace(a.Arg)
		if arg == "" && preset.ArgPlaceholder == "{{dir}}" {
			arg = mcp6.PrepareSandbox(preset.ID)
		}
		if arg == "" {
			return "", errors.New("this preset needs arg: " + preset.ArgHint)
		}
		args = preset.ResolveArgs(arg)
	}
	res, err := e.m7mcp.Add(ctx, m7app.McpAddInput{
		Origin:         m7flow.McpOriginManual,
		Transport:      preset.Transport,
		Command:        preset.Command,
		URL:            preset.URL,
		ConfigureOnly:  preset.NeedsCredential,
		Args:           args,
		RiskConfirmed:  true,
		Actor:          "chat",
		IdempotencyKey: ulid.Make().String(),
	})
	if err != nil {
		return "", err
	}
	if preset.NeedsCredential {
		b, _ := json.Marshal(map[string]any{"endpointId": res.EndpointID, "state": "needs_configuration", "presetId": preset.ID, "message": "已保存配置。请在 MCP 已安装列表中配置凭据，再连接；不要把密钥发送到对话。配置并连接后，工具从下一轮对话开始生效。"})
		return string(b), nil
	}
	ep, err := e.m7mcp.Toggle(ctx, res.EndpointID, true, "chat")
	if err != nil {
		return "", err
	}
	if err := e.admitSettingsMcp(ctx, ep); err != nil {
		return "", err
	}
	e.rememberMcpPreset(res.EndpointID, preset.ID)
	e.attachDeclaredBindKeys(ctx, m8app.BoundMcpPrefix+preset.ID)
	b, _ := json.Marshal(map[string]any{"endpointId": res.EndpointID, "state": ep.State, "presetId": preset.ID, "note": "已安装并连接。新工具从下一轮对话开始生效；本轮向用户报告安装结果即可，不要重复安装。"})
	return string(b), nil
}

// installMcpPresetViaRuntime adapts invokeMcpInstallPreset to the
// toolruntime installer hook (capability-self-bootstrap P3). The runtime owns
// the approval gate; this closure only runs once a call is approved or in a
// mode that does not gate installs. A successful (user-approved) install is
// also settled into semantic memory (P4).
func (e *Engine) installMcpPresetViaRuntime(ctx context.Context, sessionID string, raw json.RawMessage) (string, error) {
	out, err := e.invokeMcpInstallPreset(ctx, raw)
	if err != nil {
		return out, err
	}
	var res struct {
		State    string `json:"state"`
		PresetID string `json:"presetId"`
	}
	if json.Unmarshal([]byte(out), &res) == nil {
		e.recordApprovedMcpPreset(ctx, sessionID, res.PresetID, res.State)
	}
	return out, nil
}

// mcpInstallApprovalSummary renders the permission and data-flow card for an
// mcp.install approval: what gets installed, what it can touch on this
// machine, and where credentials go. Empty falls back to the generic summary.
func mcpInstallApprovalSummary(raw json.RawMessage) string {
	var a struct {
		PresetID string `json:"presetId"`
	}
	if json.Unmarshal(raw, &a) != nil {
		return ""
	}
	preset, ok := mcp6.PresetByID(strings.TrimSpace(a.PresetID))
	if !ok {
		return ""
	}
	cred := "无需密钥"
	if preset.NeedsCredential {
		cred = "需要密钥：安装后在设置页配置，密钥不进对话"
	}
	return "安装 MCP 服务器 " + preset.Name + "：" + preset.Description +
		"。将在本机以 " + preset.Command + " 启动第三方服务端并可访问网络；" + cred + "。批准即代表信任该服务端。"
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
