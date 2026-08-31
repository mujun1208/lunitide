package m8app

import (
	"strings"
)

// BoundMcpPrefix marks an MCP preset id stored in expert_skill_bindings.
const BoundMcpPrefix = "mcp:"

// BoundBrainPrefix marks a local CLI brain stored in expert_skill_bindings.
const BoundBrainPrefix = "brain:"

// SplitBoundKeys separates skill catalog ids from mcp:<preset> bindings.
// brain:<kind> is ignored here so it never looks like a skill name.
func SplitBoundKeys(keys []string) (skills, mcp []string) {
	for _, raw := range keys {
		key := strings.TrimSpace(raw)
		if key == "" {
			continue
		}
		if rest, ok := strings.CutPrefix(key, BoundMcpPrefix); ok {
			rest = strings.TrimSpace(rest)
			if rest != "" {
				mcp = append(mcp, rest)
			}
			continue
		}
		if strings.HasPrefix(key, BoundBrainPrefix) {
			continue
		}
		skills = append(skills, key)
	}
	return skills, mcp
}

// BoundBrainFromKeys reads brain:codex / brain:claude from stored bindings.
func BoundBrainFromKeys(keys []string) string {
	for _, raw := range keys {
		rest, ok := strings.CutPrefix(strings.TrimSpace(raw), BoundBrainPrefix)
		if !ok {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(rest)) {
		case "codex", "claude":
			return strings.ToLower(strings.TrimSpace(rest))
		}
	}
	return "lunitide"
}

// BindKeysFromCatalog is the first-install seed: preferred skills plus mcp:<id>.
func BindKeysFromCatalog(item CatalogItem) []string {
	out := append([]string{}, item.PreferredSkills...)
	for _, id := range item.PreferredMcp {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		out = append(out, BoundMcpPrefix+id)
	}
	return out
}

// SkillMatchesPreferred answers whether a published skill (name / entryPoint)
// is one of the conversation-expert preferred catalog template IDs.
// Template IDs are catalog ids (slide-builder); stored names are often tpl-*.
func SkillMatchesPreferred(name, entryPoint string, preferred []string) bool {
	name = strings.TrimSpace(name)
	entry := strings.TrimSpace(entryPoint)
	for _, raw := range preferred {
		p := strings.TrimSpace(raw)
		if p == "" {
			continue
		}
		if name == p || name == "tpl-"+p || strings.TrimPrefix(name, "tpl-") == p {
			return true
		}
		if strings.HasSuffix(entry, "://"+p) || strings.HasSuffix(entry, "://tpl-"+p) {
			return true
		}
	}
	return false
}

// ConversationExpertByName answers one shipped specialist by display name.
func ConversationExpertByName(name string) (CatalogItem, bool) {
	name = strings.TrimSpace(name)
	if name == "" {
		return CatalogItem{}, false
	}
	for _, item := range ConversationExperts() {
		if item.Name == name || item.DisplayName == name {
			return item, true
		}
	}
	return CatalogItem{}, false
}

// ConversationExpertByID answers one shipped specialist by catalog id.
func ConversationExpertByID(id string) (CatalogItem, bool) {
	id = strings.TrimSpace(id)
	if id == "" {
		return CatalogItem{}, false
	}
	for _, item := range ConversationExperts() {
		if item.ID == id {
			return item, true
		}
	}
	return CatalogItem{}, false
}

// ComposeForExpertNames unions preferredSkills / requiredTools / preferredMcp
// for the named conversation specialists (deduped, catalog order).
func ComposeForExpertNames(names []string) (skills, tools, mcp []string, fallbacks []string) {
	seenSkill, seenTool, seenMcp, seenFb := map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{}
	add := func(dst *[]string, seen map[string]bool, values []string) {
		for _, v := range values {
			v = strings.TrimSpace(v)
			if v == "" || seen[v] {
				continue
			}
			seen[v] = true
			*dst = append(*dst, v)
		}
	}
	for _, name := range names {
		item, ok := ConversationExpertByName(name)
		if !ok {
			item, ok = ConversationExpertByID(name)
		}
		if !ok {
			continue
		}
		add(&skills, seenSkill, item.PreferredSkills)
		add(&tools, seenTool, item.RequiredTools)
		add(&mcp, seenMcp, item.PreferredMcp)
		if fb := strings.TrimSpace(item.McpFallback); fb != "" && !seenFb[fb] {
			seenFb[fb] = true
			fallbacks = append(fallbacks, fb)
		}
	}
	return skills, tools, mcp, fallbacks
}

// PreferredComposeTemplateIDs is the union of every specialist's preferredSkills.
func PreferredComposeTemplateIDs() []string {
	skills, _, _, _ := ComposeForExpertNames(ConversationExpertIDs)
	return skills
}

// ConversationExpertNamesInText finds specialist display names or ids in text.
func ConversationExpertNamesInText(text string) []string {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	var names []string
	seen := map[string]bool{}
	for _, item := range ConversationExperts() {
		if strings.Contains(text, item.Name) || strings.Contains(text, item.ID) ||
			(item.DisplayName != "" && strings.Contains(text, item.DisplayName)) {
			if seen[item.Name] {
				continue
			}
			seen[item.Name] = true
			names = append(names, item.Name)
		}
	}
	return names
}
