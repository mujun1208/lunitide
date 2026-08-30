package app

import (
	"context"
	"strings"

	"github.com/lunitide/lunitide/internal/domain/skill"
	"github.com/lunitide/lunitide/internal/m8app"
)

func extractExpertRefNames(text string) []string {
	const prefix = "[引用专家 "
	var names []string
	seen := map[string]bool{}
	rest := text
	for {
		i := strings.Index(rest, prefix)
		if i < 0 {
			break
		}
		rest = rest[i+len(prefix):]
		bar := strings.IndexByte(rest, '|')
		end := strings.IndexByte(rest, ']')
		if bar < 0 || end < 0 || bar >= end {
			continue
		}
		name := strings.TrimSpace(rest[:bar])
		if name != "" && !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
		if end+1 >= len(rest) {
			break
		}
		rest = rest[end+1:]
	}
	return names
}

func (e *Engine) composeExpertNames(ctx context.Context, sessionID, turnText string) []string {
	seen := map[string]bool{}
	var names []string
	add := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] {
			return
		}
		if _, ok := m8app.ConversationExpertByName(name); !ok {
			if item, ok := m8app.ConversationExpertByID(name); ok {
				name = item.Name
			} else {
				return
			}
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
	for _, name := range m8app.ConversationExpertNamesInText(turnText) {
		add(name)
	}
	if len(names) > 0 {
		return names
	}
	if sessionID == "" || e.sessionExperts == nil || e.m8expert == nil {
		return names
	}
	ids, err := e.sessionExperts.ListSessionExpertIDs(ctx, sessionID)
	if err != nil {
		return names
	}
	for _, id := range ids {
		detail, err := e.m8expert.Detail(ctx, m8app.DetailInput{ExpertID: id})
		if err != nil {
			continue
		}
		name, _ := detail.Expert["name"].(string)
		add(name)
	}
	return names
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
	have := map[string]bool{}
	for _, t := range e.mcp6Registry.ReadyToolSnapshot() {
		name := strings.ToLower(t.Tool)
		switch {
		case strings.HasPrefix(name, "browser_") || strings.Contains(name, "playwright"):
			have["playwright"] = true
		case strings.Contains(name, "sequential_thinking"):
			have["sequentialthinking"] = true
		case name == "create_entities" || name == "add_observations" || name == "search_nodes":
			have["memory"] = true
		case name == "fetch":
			have["fetch"] = true
		case strings.Contains(name, "read_file") || strings.Contains(name, "list_directory"):
			have["filesystem"] = true
		case strings.Contains(name, "git_"):
			have["git"] = true
		case strings.Contains(name, "sqlite") || name == "query":
			have["sqlite"] = true
		}
	}
	var out []string
	for _, id := range []string{"playwright", "fetch", "filesystem", "memory", "sequentialthinking"} {
		if have[id] {
			out = append(out, id)
		}
	}
	return out
}

func expertComposeHint(names []string, published []skill.Skill, connectedMcp []string) string {
	skills, tools, mcp, fallbacks := m8app.ComposeForExpertNames(names)
	if len(skills) == 0 && len(tools) == 0 {
		return ""
	}
	connected := map[string]bool{}
	for _, id := range connectedMcp {
		connected[id] = true
	}
	var b strings.Builder
	b.WriteString("\n\n[专家自动挂载]\n")
	b.WriteString("本会话已为「")
	b.WriteString(strings.Join(names, "、"))
	b.WriteString("」自动挂载技能与工具，不必去技能中心找。匹配后立刻 skill.invoke（skillId 见技能目录）。\n")
	if len(skills) > 0 {
		b.WriteString("自动挂载技能：")
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
	if len(mcp) > 0 {
		var ready, missing []string
		for _, id := range mcp {
			if connected[id] {
				ready = append(ready, id)
			} else {
				missing = append(missing, id)
			}
		}
		if len(ready) > 0 {
			b.WriteString("已连接 MCP：")
			b.WriteString(strings.Join(ready, "、"))
			b.WriteString("。可用 mcp.search / mcp.call 或已合并的 MCP 工具。\n")
		}
		if len(missing) > 0 {
			b.WriteString("未连接 MCP（设置里可选）：")
			b.WriteString(strings.Join(missing, "、"))
			b.WriteString("。\n")
		}
	}
	if len(fallbacks) > 0 {
		b.WriteString("MCP 回退：")
		b.WriteString(strings.Join(fallbacks, "；"))
		b.WriteByte('\n')
	}
	b.WriteString("官方 filesystem/fetch/memory/sequential-thinking MCP 月汐已用 workspace.* / web.fetch / 会话记忆 / todo.write 包住，不必再装一份。\n")
	return b.String()
}

func (e *Engine) expertComposeForTurn(ctx context.Context, sessionID, turnText string) (preferred []string, hint string) {
	names := e.composeExpertNames(ctx, sessionID, turnText)
	if len(names) == 0 {
		return nil, ""
	}
	preferred, _, _, _ = m8app.ComposeForExpertNames(names)
	var published []skill.Skill
	if skillServiceAvailable(e.skills) {
		if items, err := e.skills.List(ctx, skill.SkillStatusPublished); err == nil {
			published = items
		}
	}
	return preferred, expertComposeHint(names, published, e.connectedComposeMcpIDs())
}
