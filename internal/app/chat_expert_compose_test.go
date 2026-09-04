package app

import (
	"context"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/domain/skill"
	"github.com/lunitide/lunitide/internal/m8app"
)

func TestExpertComposeHintListsPreferredForEachSpecialist(t *testing.T) {
	published := []skill.Skill{
		catalogTestSkill("tpl-slide-builder", "演示文稿助手。", `{}`),
		catalogTestSkill("tpl-web-researcher", "联网调研。", `{}`),
		catalogTestSkill("tpl-mermaid-diagrams", "Mermaid 结构图。", `{}`),
		catalogTestSkill("tpl-docx-writer", "文档撰写。", `{}`),
		catalogTestSkill("tpl-anti-ai-prose", "去AI味。", `{}`),
		catalogTestSkill("tpl-fiction-continuity", "小说连续性。", `{}`),
		catalogTestSkill("tpl-excel-analyst", "表格分析。", `{}`),
		catalogTestSkill("tpl-csv-workbook", "CSV 工作簿。", `{}`),
		catalogTestSkill("frontend-design", "前端设计。", `{}`),
		catalogTestSkill("ui-components", "组件。", `{}`),
		catalogTestSkill("design-system", "设计系统。", `{}`),
		catalogTestSkill("pm-skill", "产品经理。", `{}`),
		catalogTestSkill("tpl-grill-me", "深度追问。", `{}`),
		catalogTestSkill("tpl-to-spec", "写规格。", `{}`),
		catalogTestSkill("tpl-improve-architecture", "架构改进。", `{}`),
		catalogTestSkill("tpl-pm-phase-3", "数据库交付。", `{}`),
		catalogTestSkill("tpl-knowledge-index", "知识索引。", `{}`),
		catalogTestSkill("tpl-code-reviewer", "代码审查。", `{}`),
		catalogTestSkill("tpl-test-writer", "测试补全。", `{}`),
		catalogTestSkill("tpl-e2e-browser", "E2E。", `{}`),
		catalogTestSkill("browser-automation", "浏览器自动化。", `{}`),
		catalogTestSkill("tpl-find-bug", "找缺陷。", `{}`),
		catalogTestSkill("tpl-hardware-bom", "硬件 BOM。", `{}`),
		catalogTestSkill("tpl-implement", "驱动实现。", `{}`),
		catalogTestSkill("tpl-tdd-loop", "TDD。", `{}`),
		catalogTestSkill("tpl-debugger", "排障。", `{}`),
		catalogTestSkill("tpl-aircraft-maintenance-engineer", "机务维修专家人设。", `{}`),
		catalogTestSkill("tpl-mro-manual-rag", "机务手册检索。", `{}`),
		catalogTestSkill("tpl-mro-fault-tree", "排故故障树。", `{}`),
		catalogTestSkill("tpl-mro-checklist", "机务检查单。", `{}`),
		catalogTestSkill("tpl-uas-airworthiness-advisor", "低空适航顾问。", `{}`),
		catalogTestSkill("tpl-tooling-chemical-advisor", "工具化工品顾问。", `{}`),
		catalogTestSkill("tpl-parts-supply-advisor", "航材供应顾问。", `{}`),
		catalogTestSkill("tpl-mx-planning-advisor", "维修计划顾问。", `{}`),
	}
	wantSkill := map[string][]string{
		"PPT专家":       {"tpl-slide-builder", "tpl-web-researcher", "tpl-mermaid-diagrams"},
		"报告编写专家":      {"tpl-web-researcher", "tpl-docx-writer", "tpl-anti-ai-prose"},
		"小说编写专家":      {"tpl-docx-writer", "tpl-anti-ai-prose", "tpl-fiction-continuity"},
		"Excel表格制作专家": {"tpl-excel-analyst", "tpl-csv-workbook"},
		"UI专家":        {"frontend-design", "ui-components", "design-system"},
		"产品经理专家":      {"pm-skill", "tpl-grill-me", "tpl-to-spec"},
		"系统架构师专家":     {"tpl-improve-architecture", "tpl-mermaid-diagrams"},
		"数据库设计专家":     {"tpl-mermaid-diagrams", "tpl-pm-phase-3"},
		"系统项目结构规范专家":  {"tpl-knowledge-index", "tpl-mermaid-diagrams"},
		"开发规范专家":      {"tpl-code-reviewer", "tpl-grill-me"},
		"系统测试专家":      {"tpl-test-writer", "tpl-e2e-browser", "browser-automation", "tpl-find-bug"},
		"硬件配置专家":      {"tpl-web-researcher", "tpl-hardware-bom"},
		"开发专家":        {"tpl-implement", "tpl-tdd-loop", "tpl-debugger"},
		"航空机务维修专家":    {"tpl-aircraft-maintenance-engineer", "tpl-mro-manual-rag", "tpl-mro-fault-tree", "tpl-mro-checklist"},
		"低空适航专家":      {"tpl-uas-airworthiness-advisor", "tpl-mro-manual-rag"},
		"航空工具化工品专家":  {"tpl-tooling-chemical-advisor"},
		"航空航材专家":      {"tpl-parts-supply-advisor"},
		"航空维修计划专家":   {"tpl-mx-planning-advisor"},
	}
	wantTool := map[string]string{
		"PPT专家":       "pptx.gen",
		"报告编写专家":      "docx.gen",
		"小说编写专家":      "docx.gen",
		"Excel表格制作专家": "excel.gen",
		"UI专家":        "workspace.write",
		"产品经理专家":      "web.search",
		"系统架构师专家":     "workspace.read",
		"数据库设计专家":     "skill.invoke",
		"系统项目结构规范专家":  "workspace.list",
		"开发规范专家":      "workspace.read",
		"系统测试专家":      "browser.act",
		"硬件配置专家":      "excel.gen",
		"开发专家":        "workspace.edit",
		"航空机务维修专家":    "kb.search",
		"低空适航专家":      "kb.search",
		"航空工具化工品专家":  "kb.search",
		"航空航材专家":      "kb.search",
		"航空维修计划专家":   "kb.search",
	}
	if len(wantSkill) != 18 {
		t.Fatalf("want 18 compose-audited specialists, got %d", len(wantSkill))
	}
	for _, item := range m8app.ConversationExperts() {
		hint := expertComposeHint([]string{item.Name}, published, nil)
		if !strings.Contains(hint, "[专家装备]") || !strings.Contains(hint, "[稳定身份]") || !strings.Contains(hint, "[本轮任务]") || !strings.Contains(hint, item.Name) || strings.Contains(hint, "已自动挂载") {
			t.Fatalf("%s compose hint missing header:\n%s", item.Name, hint)
		}
		for _, name := range wantSkill[item.Name] {
			if !strings.Contains(hint, name) {
				t.Fatalf("%s compose missing skill %q:\n%s", item.Name, name, hint)
			}
		}
		if tool := wantTool[item.Name]; !strings.Contains(hint, tool) {
			t.Fatalf("%s compose missing tool %q:\n%s", item.Name, tool, hint)
		}
	}
}

func TestExpertComposeHintNeverListsArchivedMCP(t *testing.T) {
	for _, name := range []string{"数据库设计专家", "开发专家"} {
		hint := expertComposeHint([]string{name}, nil, nil)
		if strings.Contains(hint, "sqlite") || strings.Contains(hint, "git") {
			t.Fatalf("%s compose still lists archived MCP:\n%s", name, hint)
		}
	}
	leaked := expertComposeHint([]string{"数据库设计专家"}, nil, []string{"sqlite", "git", "playwright"})
	if strings.Contains(leaked, "sqlite") || strings.Contains(leaked, "git") {
		t.Fatalf("connected leftover still listed:\n%s", leaked)
	}
	if !strings.Contains(leaked, "playwright") {
		t.Fatalf("live MCP dropped:\n%s", leaked)
	}
}

func TestExtractExpertRefNames(t *testing.T) {
	got := extractExpertRefNames("[引用专家 PPT专家|01ARZ3NDEKTSV4RRFFQ69G5FAV] 请做一份介绍")
	if len(got) != 1 || got[0] != "PPT专家" {
		t.Fatalf("refs = %#v", got)
	}
}

func TestComposeExpertNamesTurnChipsWinOverSessionMounts(t *testing.T) {
	e := &Engine{}
	got := e.composeExpertNames(context.Background(), "", "[引用专家 报告编写专家|01ARZ3NDEKTSV4RRFFQ69G5FAV] 做好了没有")
	if len(got) != 1 || got[0] != "报告编写专家" {
		t.Fatalf("chip compose = %#v", got)
	}
	named := e.composeExpertNames(context.Background(), "", "请 PPT专家做一份介绍")
	if len(named) != 1 || named[0] != "PPT专家" {
		t.Fatalf("name compose = %#v", named)
	}
}
