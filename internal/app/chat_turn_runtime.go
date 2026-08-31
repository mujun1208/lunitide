package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/lunitide/lunitide/internal/domain/skill"
	"github.com/lunitide/lunitide/internal/gateway"
	"github.com/lunitide/lunitide/internal/mcp6"
	"github.com/lunitide/lunitide/internal/people"
	"github.com/lunitide/lunitide/internal/skillapp"
	"github.com/lunitide/lunitide/internal/toolruntime"
)

func (e *Engine) mcpToolDefinitionsRestricted(allowed []string, restrict bool) []gateway.ToolDefinition {
	if e == nil || e.mcp6Registry == nil {
		return nil
	}
	snapshot := e.mcp6Registry.ReadyToolSnapshot()
	if restrict {
		var filtered []mcp6.ReadyTool
		for _, t := range snapshot {
			if e.mcpNameAllowed("", t.EndpointID, allowed, true) {
				filtered = append(filtered, t)
			}
		}
		snapshot = filtered
	}
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

func (e *Engine) searchMcpToolsFiltered(raw json.RawMessage, allowed []string, restrict bool) (string, error) {
	out, err := e.searchMcpTools(raw)
	if err != nil || !restrict {
		return out, err
	}
	var payload struct {
		Tools []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		} `json:"tools"`
	}
	if json.Unmarshal([]byte(out), &payload) != nil {
		return out, nil
	}
	kept := payload.Tools[:0]
	for _, t := range payload.Tools {
		if e.mcpNameAllowed(t.Name, "", allowed, true) {
			kept = append(kept, t)
		}
	}
	b, _ := json.Marshal(map[string]any{"tools": kept})
	return string(b), nil
}

func (e *Engine) callMcpToolByNameGuarded(ctx context.Context, raw json.RawMessage, allowed []string, restrict bool) (string, error) {
	var a struct {
		Name string `json:"name"`
	}
	if json.Unmarshal(raw, &a) != nil {
		return "", errors.New("mcp.call needs name")
	}
	endpointID, _, ok := parseMcpToolName(a.Name)
	if !ok {
		return "", errors.New("mcp.call name must be an mcp_<endpoint>_<tool> from mcp.search")
	}
	if !e.mcpNameAllowed(a.Name, endpointID, allowed, restrict) {
		return "", errors.New("未授权这个 MCP")
	}
	return e.callMcpToolByName(ctx, raw)
}

func (e *Engine) denyRestrictedMCP(name string, raw json.RawMessage, restrict bool, allowed []string) (string, bool) {
	if !restrict {
		return "", false
	}
	if name == "mcp.search" {
		if len(allowed) == 0 {
			return "未授权这个 MCP", true
		}
		return "", false
	}
	if name == "mcp.call" {
		var a struct {
			Name string `json:"name"`
		}
		_ = json.Unmarshal(raw, &a)
		endpointID, _, _ := parseMcpToolName(a.Name)
		if !e.mcpNameAllowed(a.Name, endpointID, allowed, true) {
			return "未授权这个 MCP", true
		}
		return "", false
	}
	if endpointID, _, ok := parseMcpToolName(name); ok {
		if !e.mcpNameAllowed(name, endpointID, allowed, true) {
			return "未授权这个 MCP", true
		}
	}
	return "", false
}

func (e *Engine) resolvePublishedSkillID(ctx context.Context, raw string) (string, error) {
	id := strings.TrimSpace(raw)
	if id == "" {
		return "", errors.New("skillId required")
	}
	if !skillServiceAvailable(e.skills) {
		return "", errors.New("skill service unavailable")
	}
	if validCanonicalULID(id) {
		if sk, err := e.skills.Get(ctx, id); err == nil && sk != nil {
			if sk.Status != "" && sk.Status != skill.SkillStatusPublished {
				return "", errors.New("skill is not published")
			}
			return sk.ID, nil
		}
		return id, nil
	}
	published, err := e.skills.List(ctx, skill.SkillStatusPublished)
	if err != nil {
		return "", err
	}
	norm := strings.TrimPrefix(strings.ToLower(id), "tpl-")
	match := func(sk skill.Skill) bool {
		if strings.EqualFold(sk.Name, id) || strings.EqualFold(sk.ID, id) {
			return true
		}
		name := strings.TrimPrefix(strings.ToLower(sk.Name), "tpl-")
		if name == norm {
			return true
		}
		entry := strings.ToLower(sk.EntryPoint)
		return strings.HasSuffix(entry, "://"+norm) || strings.HasSuffix(entry, "/"+norm)
	}
	for _, sk := range published {
		if match(sk) {
			return sk.ID, nil
		}
	}
	for _, tpl := range skillapp.Catalog() {
		if tpl.ID != id && tpl.Name != id && strings.TrimPrefix(tpl.Name, "tpl-") != norm {
			continue
		}
		for _, sk := range published {
			if skillMatchesPreferred(sk, []string{tpl.ID, tpl.Name, strings.TrimPrefix(tpl.Name, "tpl-")}) {
				return sk.ID, nil
			}
		}
		return "", errors.New("技能「" + tpl.DisplayName + "」还没发布。请到技能中心安装并发布后再 skill.invoke")
	}
	return "", errors.New("skill not found: " + id)
}

func (e *Engine) invokeSkillManageTool(ctx context.Context, args json.RawMessage) (toolruntime.Result, error) {
	if !skillServiceAvailable(e.skills) {
		return toolruntime.Result{}, errors.New("skill service unavailable")
	}
	var a struct {
		Action       string   `json:"action"`
		SkillID      string   `json:"skillId"`
		Name         string   `json:"name"`
		DisplayName  string   `json:"displayName"`
		Description  string   `json:"description"`
		Version      string   `json:"version"`
		Permissions  []string `json:"permissions"`
		EntryPoint   string   `json:"entryPoint"`
		ManifestJSON string   `json:"manifestJson"`
	}
	if json.Unmarshal(args, &a) != nil {
		return toolruntime.Result{}, errors.New("invalid skill.manage arguments")
	}
	switch strings.ToLower(strings.TrimSpace(a.Action)) {
	case "create":
		if strings.TrimSpace(a.Name) == "" || strings.TrimSpace(a.ManifestJSON) == "" {
			return toolruntime.Result{}, errors.New("skill.manage create needs name and manifestJson")
		}
		created, err := e.invokeSkillCreateTool(ctx, args)
		if err != nil {
			return toolruntime.Result{}, err
		}
		return toolruntime.Result{Output: created.Output + " 仍是草稿，请到技能中心发布后才能 skill.invoke。"}, nil
	case "patch":
		id := strings.TrimSpace(a.SkillID)
		if id == "" {
			return toolruntime.Result{}, errors.New("skill.manage patch needs skillId")
		}
		var display, desc, entry, manifest *string
		if strings.TrimSpace(a.DisplayName) != "" {
			display = &a.DisplayName
		}
		if a.Description != "" {
			desc = &a.Description
		}
		if strings.TrimSpace(a.EntryPoint) != "" {
			entry = &a.EntryPoint
		}
		if strings.TrimSpace(a.ManifestJSON) != "" {
			manifest = &a.ManifestJSON
		}
		var perms []skill.PermissionLevel
		for _, p := range a.Permissions {
			perms = append(perms, skill.PermissionLevel(p))
		}
		updated, err := e.skills.UpdateFields(ctx, id, display, desc, entry, manifest, perms, nil, 0)
		if err != nil {
			return toolruntime.Result{}, err
		}
		label := updated.DisplayName
		if label == "" {
			label = updated.Name
		}
		return toolruntime.Result{Output: "技能「" + label + "」已更新（id=" + updated.ID + "，status=" + string(updated.Status) + "）。写盘已执行，发布仍需你在技能中心确认。"}, nil
	default:
		return toolruntime.Result{}, errors.New("skill.manage action must be create or patch")
	}
}

func (e *Engine) localBrainPrompt(ctx context.Context, agent people.Contact, threadID, sessionID, userText string) string {
	var b strings.Builder
	b.WriteString(e.peopleAgentPrompt(ctx, agent))
	b.WriteString(e.peopleLocalBrainMemoryHint(ctx, sessionID, userText, agent.SubjectID))
	b.WriteString("\n[本机大脑] 一次性执行。若工作目录有上一轮回复，先接上再答这一句。\n")
	if e.people != nil && threadID != "" {
		if msgs, err := e.people.ListMessages(ctx, threadID, 12); err == nil {
			b.WriteString("\n[同事聊天摘录]\n")
			for _, m := range msgs {
				if m.Kind != "text" || strings.TrimSpace(m.Body) == "" {
					continue
				}
				who := m.SenderID
				if m.SenderID == agent.SubjectID {
					who = agent.Nickname
				}
				b.WriteString(who)
				b.WriteString("：")
				b.WriteString(clipRunes(m.Body, 400))
				b.WriteByte('\n')
			}
		}
	}
	b.WriteString("\n用户：")
	b.WriteString(strings.TrimSpace(userText))
	b.WriteByte('\n')
	return b.String()
}

func (e *Engine) sessionLocalBrainPrompt(ctx context.Context, sessionID, userText string, names []string) string {
	var b strings.Builder
	eq := e.turnEquipmentFor(ctx, sessionID, userText, false)
	if len(names) > 0 {
		b.WriteString(e.peopleAgentPrompt(ctx, people.Contact{Nickname: names[0], SubjectID: firstNonEmpty(eq.ExpertIDs)}))
	}
	b.WriteString(e.peopleLocalBrainMemoryHint(ctx, sessionID, userText, eq.ExpertIDs...))
	b.WriteString("\n[本机大脑] 一次性执行。可接上上一轮。不要改口成月汐。\n")
	if e.messageReader != nil && sessionID != "" {
		if msgs, err := e.messageReader.ListMessages(ctx, sessionID, "backward", 8); err == nil {
			b.WriteString("\n[会话摘录]\n")
			for i := len(msgs) - 1; i >= 0; i-- {
				m := msgs[i]
				if strings.TrimSpace(m.Content) == "" {
					continue
				}
				b.WriteString(m.Role)
				b.WriteString("：")
				b.WriteString(clipRunes(m.Content, 400))
				b.WriteByte('\n')
			}
		}
	}
	b.WriteString("\n用户：")
	b.WriteString(strings.TrimSpace(userText))
	b.WriteByte('\n')
	return b.String()
}

func firstNonEmpty(ids []string) string {
	for _, id := range ids {
		if strings.TrimSpace(id) != "" {
			return id
		}
	}
	return ""
}

func (e *Engine) trySessionLocalBrain(ctx context.Context, sessionID, userText string, state *streamState) (text, note string, ok bool) {
	if e == nil || state == nil || state.brain == "" || state.brain == BrainLunitide {
		return "", "", false
	}
	names := e.composeExpertNames(ctx, sessionID, userText)
	prompt := e.sessionLocalBrainPrompt(ctx, sessionID, userText, names)
	work := localBrainWorkDir(firstNonEmpty(e.turnEquipmentFor(ctx, sessionID, userText, false).ExpertIDs))
	if work == "" && len(names) > 0 {
		work = localBrainWorkDir(names[0])
	}
	out, err := runLocalBrain(ctx, state.brain, prompt, work, userText)
	if err == nil && strings.TrimSpace(out) != "" {
		return localBrainPrefix(state.brain) + out, "", true
	}
	return "", localBrainFallbackNotice(state.brain, err), false
}

func packEntrypointOrDefault(entry string) string {
	entry = strings.TrimSpace(entry)
	if entry == "" || entry == "plugin/main.ts" {
		return "pack://manifest"
	}
	return entry
}
