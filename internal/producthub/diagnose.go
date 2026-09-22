package producthub

import (
	"fmt"
	"strings"

	"github.com/lunitide/lunitide/internal/producthub/generated"
)

func Diagnose(cards []Card, live []Candidate) []Finding {
	findings, _, _ := diagnoseCatalog(cards, live)
	return findings
}

func diagnoseCatalog(cards []Card, live []Candidate) ([]Finding, ProbeScore, []Card) {
	var out []Finding
	for i := range cards {
		c := cards[i]
		if strings.HasPrefix(c.StableKey, "landscape.") {
			continue
		}
		if len(c.Methods) == 0 {
			out = append(out, finding("warn", "PH_014", c.StableKey, "功能没有入口方法",
				"methods 为空", "生成模板未覆盖或种子未写 methods",
				"给该 stable_key 补 methods，或确认 chain_class 有 defaultMethods",
				"重新检测后该卡有语音或菜单入口", "open"))
		}
		if !seedComplete(c) && c.Provenance != "live" && !strings.HasPrefix(c.StableKey, "landscape.") {
			if len(c.Chain.Steps) > 0 && len(c.Chain.Branches) > 0 {
				var fail bool
				for _, b := range c.Chain.Branches {
					if b.Type == "failure" && b.Retry == nil && b.Fallback == nil && strings.TrimSpace(b.Description) == "" {
						fail = true
					}
				}
				if fail {
					out = append(out, finding("error", "PH_015", c.StableKey, "失败分支悬空",
						"failure 没有 retry/fallback/说明", "链路模板或种子未闭合",
						"按 closedChain 补 retry 与 fallback", "诊断复核该卡失败分支已闭合", "open"))
				}
			}
		}
		for _, tool := range append(append([]string{}, c.Attributes.Tools...), c.Scaffold.Bridge...) {
			if strings.Contains(tool, "missing.") || strings.Contains(tool, "orphan.") {
				out = append(out, finding("error", "PH_004", c.StableKey, "孤儿引用",
					"引用 "+tool, "活源已删除该工具或方法",
					"从卡属性去掉该引用，或恢复对应 Bridge/技能", "重建后该引用消失且本条 status=fixed", "open"))
			}
		}
		if c.Provenance == "seed-retired" {
			out = append(out, finding("warn", "PH_016", c.StableKey, "种子锚点已不在活源",
				c.Name+" 仅存在于初版种子", "产品已删对应 Page/方法/入口",
				"确认退役后在图上保持弃用标记；不要手删种子讲解", "总览里该卡带 status:弃用", "open"))
		}
	}
	probe, gaps := coverage(cards, live)
	out = append(out, gaps...)
	if len(out) == 0 {
		out = append(out, clearFinding(probe))
	}
	return out, probe, cards
}

// refreshFindings re-checks the saved booklet against the current live catalog.
// It does not write an edition and does not call a model.
func refreshFindings(ed Edition) ([]Finding, ProbeScore, int) {
	probe, gaps := coverage(ed.Features, LiveCatalog())
	var kept []Finding
	for _, f := range ed.Findings {
		switch f.ErrorCode {
		case "PH_019", "PH_020", "PH_000":
			continue
		default:
			kept = append(kept, f)
		}
	}
	next := append(kept, gaps...)
	if len(gaps) == 0 && !hasBlocking(kept) {
		next = append(next, clearFinding(probe))
	}
	next = mergeFindingStatus(ed.Findings, next)
	return next, probe, healthScore(next, probe)
}

func hasBlocking(in []Finding) bool {
	for _, f := range in {
		if isResolvedFinding(f.Status) {
			continue
		}
		if f.Severity == "error" || f.Severity == "warn" {
			return true
		}
	}
	return false
}

func clearFinding(probe ProbeScore) Finding {
	return finding("info", "PH_000", "product.lunitide", "本轮未发现阻断问题",
		fmt.Sprintf("活源覆盖 %d/%d。页面、设置、媒体动作、插件和动词都在当前说明书里。", probe.Passed, probe.Total),
		"活源与说明书对齐",
		"无需改代码。活源再变时点「重新检测」写入新快照。",
		"健康分保持，覆盖分子不掉", "wont_fix")
}

func coverage(cards []Card, live []Candidate) (ProbeScore, []Finding) {
	index := map[string]int{}
	for i, c := range cards {
		index[c.StableKey] = i
	}
	pages := map[string]struct{}{}
	for _, p := range generated.Pages {
		pages[p.ID] = struct{}{}
	}
	var gaps []Finding
	passed := 0
	total := 0
	for _, item := range live {
		if item.StableKey == "" {
			continue
		}
		total++
		i, ok := index[item.StableKey]
		if !ok {
			gaps = append(gaps, finding("error", "PH_019", item.StableKey, "活源还没有功能卡",
				item.Name+" 在当前活源里，这一版说明书没有这张卡",
				"上次生成之后活源变了，或合并时丢掉了这张卡",
				"点「重新检测」再生成一版。不要改 Go/TS。",
				"重新检测后这张卡出现，本条消失", "open"))
			continue
		}
		var missing []string
		for _, page := range cards[i].Scaffold.Pages {
			if _, known := pages[page]; !known {
				missing = append(missing, page)
			}
		}
		if len(missing) > 0 {
			cards[i].Probe = ProbeScore{Passed: 0, Total: 1}
			gaps = append(gaps, finding("warn", "PH_020", item.StableKey, "功能卡指向了不存在的页面",
				"页面 "+strings.Join(missing, "、"),
				"脚手架里的页面 id 不在当前前台目录",
				"等前台目录补上该页，或改活源里的页面 id。不自动改码。",
				"重新检测后该引用消失", "open"))
			continue
		}
		if strings.TrimSpace(cards[i].Summary) == "" || len(cards[i].Methods) == 0 {
			cards[i].Probe = ProbeScore{Passed: 0, Total: 1}
			continue
		}
		cards[i].Probe = ProbeScore{Passed: 1, Total: 1}
		passed++
	}
	return ProbeScore{Passed: passed, Total: total}, gaps
}

func finding(sev, code, key, title, evidence, root, fix, verify, status string) Finding {
	f := Finding{
		Severity: sev, ErrorCode: code, StableKey: key, Title: title,
		Evidence: evidence, RootCause: root, Fix: fix, Verify: verify, Status: status,
	}
	f.ApplyPrompt = applyPrompt(f)
	return f
}

func healthScore(findings []Finding, probe ProbeScore) int {
	total := probe.Total
	if total < 1 {
		total = 1
	}
	score := 100 * probe.Passed / total
	for _, f := range findings {
		if isResolvedFinding(f.Status) || f.ErrorCode == "PH_000" || f.ErrorCode == "PH_014" || f.ErrorCode == "PH_019" || f.ErrorCode == "PH_020" {
			continue
		}
		switch f.Severity {
		case "error":
			score -= 8
		case "warn":
			score -= 3
		}
	}
	if score < 0 {
		return 0
	}
	if score > 100 {
		return 100
	}
	return score
}

func countKinds(ch []Change) (added, updated, removed int) {
	for _, c := range ch {
		switch c.Kind {
		case "added":
			added++
		case "updated":
			updated++
		case "removed":
			removed++
		}
	}
	return
}

func domainStats(cards []Card) []DomainStat {
	type acc struct {
		name    string
		modules map[string]struct{}
		cards   int
	}
	by := map[string]*acc{}
	for id, name := range domainNames {
		by[id] = &acc{name: name, modules: map[string]struct{}{}}
	}
	for _, c := range cards {
		a := by[c.Domain]
		if a == nil {
			continue
		}
		a.cards++
		if c.Module != "" {
			a.modules[c.Module] = struct{}{}
		}
	}
	order := []string{"dialog", "office", "assets", "execution", "foundation"}
	var out []DomainStat
	for _, id := range order {
		a := by[id]
		out = append(out, DomainStat{ID: id, Name: a.name, Modules: len(a.modules), Cards: a.cards})
	}
	return out
}
