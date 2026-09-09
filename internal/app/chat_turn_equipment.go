package app

import (
	"context"
	"strings"
	"unicode/utf16"

	officedomain "github.com/lunitide/lunitide/internal/domain/officestudio"
	"github.com/lunitide/lunitide/internal/m8app"
	"github.com/lunitide/lunitide/internal/mcp6"
	"github.com/lunitide/lunitide/internal/people"
)

// The chip is bounded display data. Do not truncate the actual resolver output
// or change which experts, skills and MCP tools participate in execution.
func equipDisplayLabels(labels []string, maxItems, maxUnits int) []string {
	out := make([]string, 0, min(len(labels), maxItems))
	for _, label := range labels {
		if len(out) == maxItems {
			break
		}
		label = strings.TrimSpace(label)
		if label == "" {
			continue
		}
		units := utf16.Encode([]rune(label))
		if len(units) > maxUnits {
			end := maxUnits - 1
			if end > 0 && units[end-1] >= 0xd800 && units[end-1] <= 0xdbff {
				end--
			}
			label = string(utf16.Decode(units[:end])) + "…"
		}
		out = append(out, label)
	}
	return out
}

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
	if capabilityWorkTask(turnText) || skillTrialsActive(ctx, sessionID) {
		return eq
	}
	if companion && !companionWantsTools(turnText) && len(m8app.ConversationExpertsMatchingIntent(turnText)) == 0 {
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
	// A deliberately mounted expert keeps its identity and stored equipment.
	// Automatic intent matching is the fallback for an unselected task, not a
	// replacement for the expert the user is currently talking to.
	for _, id := range expertIDs {
		if e.m8expert == nil {
			if item, ok := m8app.ConversationExpertByID(id); ok {
				add(item.Name)
			}
			continue
		}
		detail, err := e.m8expert.Detail(ctx, m8app.DetailInput{ExpertID: id})
		if err != nil {
			continue
		}
		name, _ := detail.Expert["name"].(string)
		add(name)
	}
	if len(names) > 0 {
		return names
	}
	if e.officeStudio != nil && sessionID != "" {
		orgID, _, err := e.boundOrgState(ctx)
		if err == nil {
			if tasks, readErr := e.officeStudio.Store.ListOfficeTasks(officedomain.WithScope(ctx, orgID), sessionID, 1); readErr == nil && len(tasks) > 0 {
				return []string{"办公交付专家"}
			}
		}
	}
	for _, turnText := range turnTexts {
		for _, name := range m8app.ConversationExpertsMatchingIntent(turnText) {
			add(name)
		}
		if len(names) > 0 {
			return names
		}
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
	if id := presetIDFromTarget(ep.Command, ep.Args, ep.URL); id != "" {
		return id
	}
	return heuristicPresetFromTools(e, endpointID)
}

func presetIDFromCommandArgs(command string, args []string) string {
	packageName := func(args []string) string {
		for _, arg := range args {
			if strings.HasPrefix(arg, "-") {
				continue
			}
			if at := strings.LastIndex(arg, "@"); at > 0 {
				arg = arg[:at]
			}
			if at := strings.Index(arg, "=="); at > 0 {
				arg = arg[:at]
			}
			return arg
		}
		return ""
	}
	if command != "npx" && command != "uvx" {
		return ""
	}
	target := packageName(args)
	for _, p := range mcp6.Presets() {
		if p.Command == command && packageName(p.Args) == target && target != "" {
			return p.ID
		}
	}
	return ""
}

func presetIDFromTarget(command string, args []string, url string) string {
	if url != "" {
		for _, p := range mcp6.Presets() {
			if p.Transport == "https" && p.URL == url {
				return p.ID
			}
		}
	}
	return presetIDFromCommandArgs(command, args)
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
