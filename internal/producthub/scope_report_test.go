package producthub

import (
	"strings"
	"testing"
)

func TestReportCoversTheRemainingScope(t *testing.T) {
	home := defaultChain("page-enter", "首页")
	ed := Edition{
		Features: []Card{
			{
				StableKey: "feature.dialog.page.home", Name: "首页", Domain: "dialog", Module: "home",
				ChainClass: "page-enter", Chain: home,
				Methods: []Method{{Type: "menu", Entry: "home"}},
				Summary: "从导航打开。", Principle: "有原理", Logic: "有逻辑", Analysis: "有分析",
			},
			{
				StableKey: "feature.office.studio.task-create", Name: "创建办公任务", Domain: "office", Module: "office",
				ChainClass: "crud-bridge",
				Chain:      Chain{Steps: []Step{{Index: 1, Name: "自写", Detail: "真实", Description: "不是模板"}}},
				Methods:    []Method{{Type: "menu", Entry: "office"}},
				Scaffold:   Scaffold{Bridge: []string{"office.task.create"}},
			},
		},
		Graph:        Graph{Nodes: []GraphNode{{ID: "feature.dialog.page.home", Type: "Feature", Name: "首页"}}},
		CatalogProbe: ProbeScore{Passed: 2, Total: 2},
		LiveProbe:    ProbeScore{Passed: 3, Total: 4},
		Findings: []Finding{{
			Severity: "error", ErrorCode: "PH_L11", StableKey: "log.PH_L11", Title: "宕机", Status: "open",
			Evidence: "panic: nil pointer",
		}},
	}
	md, _ := RenderReport(ed)
	for _, heading := range []string{
		"### 速度与效率", "### 准确", "### 能力", "### 合理性", "### 数据",
		"### 升级对照", "### 任务完成", "### 宕机", "步骤未按真实调用写清",
	} {
		if !strings.Contains(md, heading) {
			t.Fatalf("missing %s", heading)
		}
	}
	unclear := md[strings.Index(md, "步骤未按真实调用写清"):]
	unclear = unclear[:strings.Index(unclear, "### 工具")]
	if !strings.Contains(unclear, "首页") {
		t.Fatal("template chain was treated as a clear step")
	}
	if strings.Contains(unclear, "创建办公任务") {
		t.Fatal("a hand-written chain was called a template")
	}
	if strings.Contains(md, "100分") || strings.Contains(md, "排名第") {
		t.Fatal("report invented a perfect score or a competitor rank")
	}
	if !strings.Contains(md, "不编对手分数") {
		t.Fatal("upgrade section invented a comparison")
	}
	if !strings.Contains(md, "panic: nil pointer") {
		t.Fatal("crash evidence missing")
	}
	if !strings.Contains(md, "组装这份诊断用了") {
		t.Fatal("speed was not measured")
	}
}
