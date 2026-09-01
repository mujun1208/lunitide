package app

import (
	"context"
	"strings"

	"github.com/lunitide/lunitide/internal/m8app"
	"github.com/lunitide/lunitide/internal/mcp6"
	"github.com/lunitide/lunitide/internal/people"
)

// turnEquipment is the single per-turn resolver for 同事 / 普通会话 / 月伴.
// Opening experts, skill bindings, MCP presets and local brain come from here.
type turnEquipment struct {
	Companion bool
	Names     []string
	ExpertIDs []string
	BindKeys  []string
	McpIDs    []string
	Brain     string
}

func (eq turnEquipment) RestrictMCP() bool {
	return !eq.Companion && len(eq.Names) > 0
}

func (e *Engine) turnEquipmentFor(ctx context.Context, sessionID, turnText string, companion bool) turnEquipment {
	eq := turnEquipment{Companion: companion, Brain: BrainLunitide}
	if companion {
		return eq
	}
	mounted := e.sessionMountedExpertIDs(ctx, sessionID)
	texts := e.priorTurnTexts(ctx, sessionID, turnText)
	eq.ExpertIDs = selectedTurnExpertIDs(mounted, texts...)
	eq.Names = e.namesForTurn(ctx, sessionID, eq.ExpertIDs, texts...)
	if e.m8expert != nil && len(eq.Names) > 0 {
		eq.BindKeys = e.m8expert.ComposeSkillsForNames(ctx, eq.Names)
	} else if len(eq.Names) > 0 {
		eq.BindKeys, _, _, _ = m8app.ComposeForExpertNames(eq.Names)
	}
	_, mcp := m8app.SplitBoundKeys(eq.BindKeys)
	_, _, catalogMcp, _ := m8app.ComposeForExpertNames(eq.Names)
	eq.McpIDs = uniqueStrings(append(mcp, catalogMcp...))
	eq.Brain = BoundBrainFromKeys(eq.BindKeys)
	return eq
}

func (e *Engine) equipmentForNames(ctx context.Context, names []string) turnEquipment {
	eq := turnEquipment{Names: uniqueStrings(names), Brain: BrainLunitide}
	if e.m8expert != nil && len(eq.Names) > 0 {
		eq.BindKeys = e.m8expert.ComposeSkillsForNames(ctx, eq.Names)
	} else if len(eq.Names) > 0 {
		eq.BindKeys, _, _, _ = m8app.ComposeForExpertNames(eq.Names)
	}
	_, mcp := m8app.SplitBoundKeys(eq.BindKeys)
	_, _, catalogMcp, _ := m8app.ComposeForExpertNames(eq.Names)
	eq.McpIDs = uniqueStrings(append(mcp, catalogMcp...))
	eq.Brain = BoundBrainFromKeys(eq.BindKeys)
	return eq
}

func (e *Engine) namesForTurn(ctx context.Context, sessionID string, expertIDs []string, turnTexts ...string) []string {
	seen := map[string]bool{}
	var names []string
	add := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] {
			return
		}
		if item, ok := m8app.ConversationExpertByID(name); ok {
			name = item.Name
		}
		seen[name] = true
		names = append(names, name)
	}
	for _, turnText := range turnTexts {
		if refs := extractExpertRefNames(turnText); len(refs) > 0 {
			for _, name := range refs {
				add(name)
			}
			return names
		}
	}
	for _, turnText := range turnTexts {
		for _, name := range m8app.ConversationExpertNamesInText(turnText) {
			add(name)
		}
		if len(names) > 0 {
			return names
		}
	}
	if e.m8expert == nil {
		return names
	}
	for _, id := range expertIDs {
		detail, err := e.m8expert.Detail(ctx, m8app.DetailInput{ExpertID: id})
		if err != nil {
			continue
		}
		name, _ := detail.Expert["name"].(string)
		add(name)
	}
	if len(names) == 0 && sessionID != "" && len(expertIDs) == 1 {
		add(expertIDs[0])
	}
	return names
}

func (e *Engine) rememberMcpPreset(endpointID, presetID string) {
	if e == nil || endpointID == "" || presetID == "" {
		return
	}
	e.mcpPresetByEP.Store(endpointID, presetID)
	e.saveMcpPresets()
}

func (e *Engine) endpointPresetID(endpointID string) string {
	if e == nil || endpointID == "" {
		return ""
	}
	if raw, ok := e.mcpPresetByEP.Load(endpointID); ok {
		if id, _ := raw.(string); id != "" {
			return id
		}
	}
	if e.mcp6Registry == nil {
		return ""
	}
	ep, err := e.mcp6Registry.Get(endpointID)
	if err != nil || ep == nil {
		return heuristicPresetFromTools(e, endpointID)
	}
	if id := presetIDFromCommandArgs(ep.Command, ep.Args); id != "" {
		return id
	}
	return heuristicPresetFromTools(e, endpointID)
}

func presetIDFromCommandArgs(command string, args []string) string {
	blob := strings.ToLower(strings.Join(append([]string{command}, args...), " "))
	for _, p := range mcp6.Presets() {
		for _, arg := range p.Args {
			arg = strings.ToLower(strings.TrimSpace(arg))
			if arg == "" || arg == "-y" || strings.HasPrefix(arg, "{{") {
				continue
			}
			if !strings.HasPrefix(arg, "@") && !strings.Contains(arg, "mcp") && !mcp6.PresetPackageAllowed(arg) {
				continue
			}
			if strings.Contains(blob, arg) {
				return p.ID
			}
		}
	}
	return ""
}

func heuristicPresetFromTools(e *Engine, endpointID string) string {
	if e == nil || e.mcp6Registry == nil {
		return ""
	}
	for _, t := range e.mcp6Registry.ReadyToolSnapshot() {
		if t.EndpointID != endpointID {
			continue
		}
		name := strings.ToLower(t.Tool)
		switch {
		case strings.HasPrefix(name, "browser_") || strings.Contains(name, "playwright"):
			return "playwright"
		case strings.Contains(name, "sequential_thinking"):
			return "sequentialthinking"
		case name == "create_entities" || name == "add_observations" || name == "search_nodes":
			return "memory"
		case name == "fetch":
			return "fetch"
		case strings.Contains(name, "read_file") || strings.Contains(name, "list_directory"):
			return "filesystem"
		}
	}
	return ""
}

func (e *Engine) mcpPresetAllowed(presetID string, allowed []string, restrict bool) bool {
	if !restrict {
		return true
	}
	presetID = strings.ToLower(strings.TrimSpace(presetID))
	if presetID == "" {
		return false
	}
	for _, id := range allowed {
		if strings.ToLower(strings.TrimSpace(id)) == presetID {
			return true
		}
	}
	return false
}

func (e *Engine) mcpNameAllowed(name, endpointID string, allowed []string, restrict bool) bool {
	if !restrict {
		return true
	}
	if name == "mcp.search" || name == "mcp.call" {
		return len(allowed) > 0
	}
	if endpointID == "" {
		var ok bool
		endpointID, _, ok = parseMcpToolName(name)
		if !ok {
			return false
		}
	}
	return e.mcpPresetAllowed(e.endpointPresetID(endpointID), allowed, true)
}

func peopleAgentReplyStale(msgs []people.Message, started people.Message) bool {
	if started.MessageID == "" {
		return false
	}
	seen := false
	for _, m := range msgs {
		if m.MessageID == started.MessageID {
			seen = true
			continue
		}
		if seen && m.Kind == "text" && m.SenderID == started.SenderID && strings.TrimSpace(m.Body) != "" {
			return true
		}
	}
	return false
}

func peopleAgentCollision(msgs []people.Message, startedID, agentID string, otherAgents map[string]bool) bool {
	if startedID == "" || agentID == "" {
		return false
	}
	seen := false
	for _, m := range msgs {
		if m.MessageID == startedID {
			seen = true
			continue
		}
		if seen && m.Kind == "text" && m.SenderID != agentID && otherAgents[m.SenderID] && strings.TrimSpace(m.Body) != "" {
			return true
		}
	}
	return false
}
