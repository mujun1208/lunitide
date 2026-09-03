package app

import (
	"context"
	"strings"

	"github.com/lunitide/lunitide/internal/domain/skill"
	"github.com/lunitide/lunitide/internal/m8app"
	"github.com/lunitide/lunitide/internal/mcp6"
)

func (e *Engine) sessionMountedExpertIDs(ctx context.Context, sessionID string) []string {
	if e == nil || sessionID == "" || e.sessionExperts == nil {
		return nil
	}
	ids, err := e.sessionExperts.ListSessionExpertIDs(ctx, sessionID)
	if err != nil {
		return nil
	}
	return ids
}

func (e *Engine) composeExpertNames(ctx context.Context, sessionID, turnText string) []string {
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
	refs := extractExpertRefNames(turnText)
	if len(refs) > 0 {
		for _, name := range refs {
			add(name)
		}
		return names
	}
	for _, name := range m8app.ConversationExpertsMatchingIntent(turnText) {
		add(name)
	}
	if len(names) > 0 {
		return names
	}
	return e.turnEquipmentFor(ctx, sessionID, turnText, false).Names
}

func skillMatchesPreferred(sk skill.Skill, preferred []string) bool {
	return m8app.SkillMatchesPreferred(sk.Name, sk.EntryPoint, preferred)
}

func pinPreferredSkills(ranked []catalogRankedSkill, preferred []string) []catalogRankedSkill {
	if len(preferred) == 0 || len(ranked) == 0 {
		return ranked
	}
	seen := map[string]bool{}
	var head []catalogRankedSkill
	for _, p := range preferred {
		for _, item := range ranked {
			if seen[item.skill.ID] || item.skill.ID == "" && seen[item.skill.Name] {
				continue
			}
			if !skillMatchesPreferred(item.skill, []string{p}) {
				continue
			}
			key := item.skill.ID
			if key == "" {
				key = item.skill.Name
			}
			seen[key] = true
			head = append(head, item)
		}
	}
	var tail []catalogRankedSkill
	for _, item := range ranked {
		key := item.skill.ID
		if key == "" {
			key = item.skill.Name
		}
		if seen[key] {
			continue
		}
		tail = append(tail, item)
	}
	return append(head, tail...)
}

func (e *Engine) connectedComposeMcpIDs() []string {
	if e == nil || e.mcp6Registry == nil {
		return nil
	}
	seen := map[string]bool{}
	have := map[string]bool{}
	var out []string
	for _, t := range e.mcp6Registry.ReadyToolSnapshot() {
		if seen[t.EndpointID] {
			continue
		}
		seen[t.EndpointID] = true
		id := e.endpointPresetID(t.EndpointID)
		if id == "" || have[id] {
			continue
		}
		have[id] = true
		out = append(out, id)
	}
	return out
}

func uniqueStrings(ids []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, id := range ids {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

func filterLiveComposeMCP(ids []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, id := range ids {
		id = strings.ToLower(strings.TrimSpace(id))
		if _, ok := mcp6.PresetByID(id); !ok || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

func expertComposeHint(names []string, published []skill.Skill, connectedMcp []string, preferred ...[]string) string {
	skills, tools, mcp, fallbacks := m8app.ComposeForExpertNames(names)
	if len(preferred) > 0 && len(preferred[0]) > 0 {
		storedSkills, storedMcp := m8app.SplitBoundKeys(preferred[0])
		if len(storedSkills) > 0 {
			skills = storedSkills
		}
		if len(storedMcp) > 0 {
			mcp = storedMcp
		}
	}
	if len(skills) == 0 && len(tools) == 0 {
		return ""
	}
	connected := map[string]bool{}
	for _, id := range connectedMcp {
		connected[strings.ToLower(strings.TrimSpace(id))] = true
	}
	var b strings.Builder
	b.WriteString("\n\n[专家装备]\n")
	b.WriteString("[稳定身份] 你就是「")
	b.WriteString(strings.Join(names, "、"))
	b.WriteString("」。不要改口成月汐主编排。\n")
	b.WriteString("[岗位装备] 由专家私下 skill.invoke，不是用户已在输入框点选的技能。\n")
	if len(skills) > 0 {
		b.WriteString("可调用技能：")
		parts := make([]string, 0, len(skills))
		for _, id := range skills {
			label := id
			for _, sk := range published {
				if skillMatchesPreferred(sk, []string{id}) {
					label = sk.Name
					if sk.ID != "" {
						label += "（skillId=" + sk.ID + "）"
					}
					break
				}
			}
			parts = append(parts, label)
		}
		b.WriteString(strings.Join(parts, "、"))
		b.WriteByte('\n')
	}
	if len(tools) > 0 {
		b.WriteString("必备工具：")
		b.WriteString(strings.Join(tools, "、"))
		b.WriteByte('\n')
	}
	liveIDs := uniqueStrings(append(filterLiveComposeMCP(mcp), filterLiveComposeMCP(connectedMcp)...))
	if len(liveIDs) > 0 {
		var ready, missing []string
		preferred := map[string]bool{}
		for _, id := range filterLiveComposeMCP(mcp) {
			preferred[id] = true
		}
		for _, id := range liveIDs {
			if connected[id] {
				ready = append(ready, id)
			} else if preferred[id] {
				missing = append(missing, id)
			}
		}
		if len(ready) > 0 {
			b.WriteString("已连接 MCP：")
			b.WriteString(strings.Join(ready, "、"))
			b.WriteString("。可用 mcp.search / mcp.call 或已合并的 MCP 工具。\n")
		}
		if len(missing) > 0 {
			b.WriteString("未连接 MCP（去 MCP 页打开）：")
			b.WriteString(strings.Join(filterLiveComposeMCP(missing), "、"))
			b.WriteString("。\n")
		}
	}
	if len(fallbacks) > 0 {
		b.WriteString("MCP 回退：")
		b.WriteString(strings.Join(fallbacks, "；"))
		b.WriteByte('\n')
	}
	b.WriteString("官方 filesystem/fetch/memory/sequential-thinking MCP 月汐已用 workspace.* / web.fetch / 会话记忆 / todo.write 包住，不必再装一份。\n")
	b.WriteString("[本轮任务] 只处理用户这一句，不要重开整份说明书。\n")
	return b.String()
}

// turnEquipInfo is the user-visible half of auto-equip. expertComposeForTurn
// tells the model what it just got; this tells the operator, so silent backend
// equipping ("做个PPT" → PPT专家) is no longer invisible. It returns the equipped
// specialists, their bound skills, and any preferred MCP that is not connected
// yet (so the chip can offer a connect link). Emitted as EventEquip.
func (e *Engine) turnEquipInfo(ctx context.Context, sessionID, turnText string) (experts, skills, missingMcp []string) {
	names := e.composeExpertNames(ctx, sessionID, turnText)
	if len(names) == 0 {
		return nil, nil, nil
	}
	boundSkills, _, mcp, _ := m8app.ComposeForExpertNames(names)
	connected := map[string]bool{}
	for _, id := range e.connectedComposeMcpIDs() {
		connected[strings.ToLower(strings.TrimSpace(id))] = true
	}
	for _, id := range filterLiveComposeMCP(mcp) {
		if !connected[id] {
			missingMcp = append(missingMcp, id)
		}
	}
	return names, boundSkills, missingMcp
}

func (e *Engine) expertComposeForTurn(ctx context.Context, sessionID, turnText string) (preferred []string, hint string) {
	names := e.composeExpertNames(ctx, sessionID, turnText)
	if len(names) == 0 {
		return nil, ""
	}
	if e.m8expert != nil {
		preferred = e.m8expert.ComposeSkillsForNames(ctx, names)
	} else {
		preferred, _, _, _ = m8app.ComposeForExpertNames(names)
	}
	var published []skill.Skill
	if skillServiceAvailable(e.skills) {
		if items, err := e.skills.List(ctx, skill.SkillStatusPublished); err == nil {
			published = items
		}
	}
	return preferred, expertComposeHint(names, published, e.connectedComposeMcpIDs(), preferred)
}
