package app

import (
	"context"
	"encoding/json"
	"github.com/lunitide/lunitide/internal/domain/skill"
	"log"
	"sort"
	"strings"
)

// skillCatalogInjection builds the installed-skill directory appended to the
// system instruction (c4-skill): one metadata-only line per published skill
// (name + trigger keywords + one-sentence summary) plus a usage rule, so the
// model can proactively invoke skills. Query hits are sorted to the front
// (Claude Code / Trae catalog-then-body): the full SKILL body is never
// injected here. Companion idle turns with zero hits inject nothing.
func (e *Engine) skillCatalogInjection(ctx context.Context, query string, companion bool, preferred []string) string {
	if !skillServiceAvailable(e.skills) {
		return ""
	}
	skills, err := e.skills.List(ctx, skill.SkillStatusPublished)
	if err != nil {
		log.Printf("skill catalog injection skipped: skill list unavailable: %v", err)
		return ""
	}
	if len(skills) == 0 {
		return ""
	}
	ranked := pinPreferredSkills(rankSkillsForCatalog(skills, query), preferred)
	maxItems := skillInjectMaxItems
	if companion {
		hits := catalogHitCount(ranked)
		if hits == 0 {
			return ""
		}
		maxItems = 4
		ranked = ranked[:hits]
		if len(ranked) > maxItems {
			ranked = ranked[:maxItems]
		}
	}
	const header = "[可用技能目录]\n"
	const usage = "使用规则：目录只含名称与一行摘要。需要正文或 references 时先 skill.view；匹配用户请求时立刻 skill.invoke（skillId 见下行，input 用用户原话）。禁止猜测技能正文。\n"
	const truncNotice = "（技能目录已截断）\n"
	var b strings.Builder
	b.WriteString(header)
	// Reserve the header, the usage rule and the worst-case truncation
	// notice up front so the finished block always fits the byte budget.
	budget := skillInjectMaxBytes - len(header) - len(usage) - len(truncNotice)
	injected := 0
	truncated := false
	for _, item := range ranked {
		if injected == maxItems {
			truncated = true
			break
		}
		line := skillCatalogLine(item.skill)
		if len(line) > budget {
			if budget <= 0 {
				truncated = true
				break
			}
			// Defensive: a single oversized line is UTF-8-safe truncated to
			// the remaining budget rather than blowing the global cap.
			b.WriteString(truncateUTF8Bytes(line, budget))
			truncated = true
			break
		}
		b.WriteString(line)
		budget -= len(line)
		injected++
	}
	if truncated {
		b.WriteString(truncNotice)
	}
	b.WriteString(usage)
	return b.String()
}

type catalogRankedSkill struct {
	skill skill.Skill
	score int
}

func rankSkillsForCatalog(skills []skill.Skill, query string) []catalogRankedSkill {
	ranked := make([]catalogRankedSkill, 0, len(skills))
	for _, sk := range skills {
		ranked = append(ranked, catalogRankedSkill{skill: sk, score: catalogSkillScore(sk, query)})
	}
	sort.SliceStable(ranked, func(i, j int) bool { return ranked[i].score > ranked[j].score })
	return ranked
}

func catalogHitCount(ranked []catalogRankedSkill) int {
	n := 0
	for _, item := range ranked {
		if item.score <= 0 {
			break
		}
		n++
	}
	return n
}

func catalogSkillScore(sk skill.Skill, query string) int {
	query = strings.TrimSpace(query)
	if query == "" {
		return 0
	}
	hay := strings.ToLower(strings.Join([]string{
		sk.Name, sk.DisplayName, sk.Description, strings.Join(skillCatalogTriggers(sk.ManifestJSON), " "),
	}, " "))
	score := 0
	if strings.Contains(hay, strings.ToLower(query)) {
		score += 8
	}
	for _, tok := range catalogQueryTokens(query) {
		if tok == "" {
			continue
		}
		if strings.Contains(hay, tok) {
			score += 3
		}
	}
	return score
}

func catalogQueryTokens(q string) []string {
	q = strings.ToLower(strings.TrimSpace(q))
	if q == "" {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			return
		}
		seen[s] = true
		out = append(out, s)
	}
	for _, f := range strings.Fields(q) {
		add(f)
	}
	var compact []rune
	for _, r := range q {
		if r == ' ' || r == '\t' || r == '\n' {
			continue
		}
		compact = append(compact, r)
	}
	cjk := false
	for _, r := range compact {
		if r >= 0x4E00 && r <= 0x9FFF {
			cjk = true
			break
		}
	}
	if cjk {
		if len(compact) >= 2 {
			add(string(compact))
		}
		for i := 0; i+1 < len(compact); i++ {
			add(string(compact[i : i+2]))
		}
	}
	return out
}

// skillCatalogLine renders one published skill as a single catalog line:
//
//   - name：one-sentence summary。当用户提到“t1、t2”时使用。（skillId=ULID）
//
// The skillId suffix lets the model call the skill.invoke tool without a
// name→ID lookup round-trip. The line carries metadata only - never the
// skill manifest body.
func skillCatalogLine(sk skill.Skill) string {
	suffix := "（skillId=" + sk.ID + "）\n"
	if triggers := skillCatalogTriggers(sk.ManifestJSON); len(triggers) > 0 {
		return "- " + sk.Name + "：" + skillCatalogSummary(sk.Description, sk.DisplayName) + "。当用户提到“" + strings.Join(triggers, "、") + "”时使用。" + suffix
	}
	return "- " + sk.Name + "：" + skillCatalogSummary(sk.Description, sk.DisplayName) + "。" + suffix
}

// skillCatalogSummary collapses a description to its first sentence (or the
// display name when empty), bounded to 60 runes so each catalog line stays a
// one-sentence digest.
func skillCatalogSummary(description, displayName string) string {
	summary := description
	if summary == "" {
		summary = displayName
	}
	if idx := strings.Index(summary, "。"); idx >= 0 {
		summary = summary[:idx]
	}
	if r := []rune(summary); len(r) > 60 {
		summary = string(r[:60])
	}
	return summary
}

// skillCatalogTriggers extracts up to four non-empty trigger keywords from
// the skill manifest ("triggers" key, as written by catalog installs). A
// missing or malformed manifest answers no triggers; the summary line then
// carries the trigger scenario alone.
func skillCatalogTriggers(manifestJSON string) []string {
	var m struct {
		Triggers []string `json:"triggers"`
	}
	if json.Unmarshal([]byte(manifestJSON), &m) != nil {
		return nil
	}
	var triggers []string
	for _, t := range m.Triggers {
		if t = strings.TrimSpace(t); t != "" {
			triggers = append(triggers, t)
			if len(triggers) == 4 {
				break
			}
		}
	}
	return triggers
}
