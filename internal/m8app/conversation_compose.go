package m8app

import (
	"sort"
	"strings"
	"unicode/utf8"
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
	// Pre-0.4.60 display name; catalog id stayed mro-expert.
	if name == "航空机务专家" {
		return ConversationExpertByID("mro-expert")
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

const intentEquipMinScore = 6
const intentEquipMaxNames = 2

// ConversationExpertsMatchingIntent returns specialists named in text, or
// scored from scene/description/skills when the user never said the card name.
func ConversationExpertsMatchingIntent(text string) []string {
	if names := ConversationExpertNamesInText(text); len(names) > 0 {
		return names
	}
	query := strings.ToLower(strings.TrimSpace(text))
	if query == "" || intentQueryTooShort(query) || intentQueryDefinitional(query) {
		return nil
	}
	type ranked struct {
		name  string
		score int
	}
	var hits []ranked
	for _, item := range ConversationExperts() {
		score := conversationExpertIntentScore(item, query)
		if score < intentEquipMinScore {
			continue
		}
		hits = append(hits, ranked{name: item.Name, score: score})
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].score > hits[j].score })
	if len(hits) > intentEquipMaxNames {
		hits = hits[:intentEquipMaxNames]
	}
	out := make([]string, 0, len(hits))
	for _, hit := range hits {
		out = append(out, hit.name)
	}
	return out
}

var conversationIntentAliases = map[string][]string{
	"ppt-expert":       {"ppt", "pptx", "幻灯片", "演示稿", "路演", "做ppt"},
	"report-writer":    {"写报告", "工作报告", "说明书", "周报", "调研报告"},
	"novel-writer":     {"写小说", "写一章", "小说"},
	"excel-maker":      {"excel", "xlsx", "表格", "做表"},
	"ui-designer":      {"界面设计", "设计稿"},
	"pm-expert":        {"prd", "用户故事", "产品经理"},
	"architect-expert": {"系统架构", "c4"},
	"db-expert":        {"数据库", "schema", "建表"},
	"mro-expert":       {"机务", "维修手册", "amm", "飞机维修"},
}

func conversationExpertIntentScore(item CatalogItem, query string) int {
	hay := strings.ToLower(strings.Join([]string{
		item.Name, item.DisplayName, item.Scene, item.Description, item.Category,
		strings.Join(item.PreferredSkills, " "), strings.Join(item.RequiredTools, " "),
	}, " "))
	score := 0
	if utf8.RuneCountInString(query) >= 4 && strings.Contains(hay, query) {
		score += 8
	}
	for _, alias := range conversationIntentAliases[item.ID] {
		if strings.Contains(query, strings.ToLower(alias)) {
			score += 6
		}
	}
	for _, tok := range intentQueryTokens(query) {
		if tok == "" {
			continue
		}
		if strings.Contains(hay, tok) {
			score += 3
		}
	}
	return score
}

func intentQueryTokens(q string) []string {
	q = strings.ToLower(strings.TrimSpace(q))
	if q == "" {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] || intentTokenTooShort(s) {
			return
		}
		seen[s] = true
		out = append(out, s)
	}
	for _, f := range strings.Fields(q) {
		add(f)
	}
	runes := []rune(q)
	for i := 0; i < len(runes)-1; i++ {
		if runes[i] >= 0x4E00 && runes[i] <= 0x9FFF && runes[i+1] >= 0x4E00 && runes[i+1] <= 0x9FFF {
			add(string(runes[i : i+2]))
		}
	}
	return out
}

// intentQueryDefinitional is true for "what does X mean / what is the
// difference" meta questions that merely name a domain noun without asking for
// work. Equipping a specialist on these (e.g. "数据库是什么意思") is a false
// positive, so intent matching skips them — unless the sentence also carries a
// real task verb (做/写/生成/设计…), which flips it back to an actionable task.
func intentQueryDefinitional(q string) bool {
	markers := []string{"什么意思", "啥意思", "的意思", "什么梗", "这个词", "什么区别", "有区别吗"}
	hit := false
	for _, m := range markers {
		if strings.Contains(q, m) {
			hit = true
			break
		}
	}
	if !hit {
		return false
	}
	for _, verb := range []string{"做", "写一", "写个", "写份", "生成", "建", "设计", "部署", "画", "填", "转成", "导出", "分析", "优化", "实现", "开发", "整理", "制作", "起草"} {
		if strings.Contains(q, verb) {
			return false
		}
	}
	return true
}

func intentQueryTooShort(q string) bool {
	n := utf8.RuneCountInString(q)
	if n < 2 {
		return true
	}
	return n < 3 && intentASCII(q)
}

func intentTokenTooShort(s string) bool {
	n := utf8.RuneCountInString(s)
	if n < 2 {
		return true
	}
	return n < 3 && intentASCII(s)
}

func intentASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] > 127 {
			return false
		}
	}
	return true
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
