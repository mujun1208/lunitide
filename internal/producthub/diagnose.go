package producthub

import (
	"strings"
)

func Diagnose(cards []Card, live []Candidate) []Finding {
	var out []Finding
	liveKeys := map[string]struct{}{}
	for _, c := range live {
		liveKeys[c.StableKey] = struct{}{}
	}
	moduleCount := map[string]int{}
	for _, c := range cards {
		if c.Module != "" && !strings.HasPrefix(c.StableKey, "landscape.") {
			moduleCount[c.Domain+"/"+c.Module]++
		}
		if len(c.Methods) == 0 && !strings.HasPrefix(c.StableKey, "landscape.") {
			out = append(out, finding("warn", "PH_014", c.StableKey, "功能没有入口方法",
				"methods 为空", "生成模板未覆盖或种子未写 methods",
				"给该 stable_key 补 methods，或确认 chain_class 有 defaultMethods",
				"重新点「生成最新说明书」后该卡 D 区有语音/菜单入口", "open"))
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
	for _, c := range live {
		if strings.HasPrefix(c.StableKey, "feature.office.page.") || strings.HasPrefix(c.StableKey, "feature.foundation.settings.") {
			if _, ok := liveKeys[c.StableKey]; ok {
				_ = ok
			}
		}
	}
	for mod, n := range moduleCount {
		if n == 0 {
			out = append(out, finding("warn", "PH_017", mod, "模块零卡",
				mod+" 没有功能卡", "采集器未扫到该模块活源",
				"检查 LiveCatalog 是否漏 Page/设置/动词", "该模块至少 1 张卡", "open"))
		}
	}
	if len(out) == 0 {
		out = append(out, finding("info", "PH_000", "product.lunitide", "本轮未发现阻断问题",
			"探针与一致性规则通过", "活源与种子可对齐",
			"无需改代码；继续用生成按钮跟踪后续版本", "健康分保持或上升", "wont_fix"))
	}
	return out
}

func finding(sev, code, key, title, evidence, root, fix, verify, status string) Finding {
	f := Finding{
		Severity: sev, ErrorCode: code, StableKey: key, Title: title,
		Evidence: evidence, RootCause: root, Fix: fix, Verify: verify, Status: status,
	}
	f.ApplyPrompt = applyPrompt(f)
	return f
}

func healthScore(findings []Finding, cardCount int) int {
	score := 100
	for _, f := range findings {
		if isResolvedFinding(f.Status) {
			continue
		}
		switch f.Severity {
		case "error":
			score -= 8
		case "warn":
			score -= 3
		}
	}
	if cardCount < 30 {
		score -= 10
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

