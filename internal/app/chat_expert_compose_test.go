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
		catalogTestSkill("tpl-hardware-bom", "硬件 BOM。", `{}`),
		catalogTestSkill("tpl-implement", "驱动实现。", `{}`),
		catalogTestSkill("tpl-tdd-loop", "TDD。", `{}`),
		catalogTestSkill("tpl-debugger", "排障。", `{}`),
	}
	wantSkill := map[string][]string{
		"PPT专家":         {"tpl-slide-builder", "tpl-web-researcher", "tpl-mermaid-diagrams"},
		"报告编写专家":      {"tpl-web-researcher", "tpl-docx-writer", "tpl-anti-ai-prose"},
		"小说编写专家":      {"tpl-docx-writer", "tpl-anti-ai-prose", "tpl-fiction-continuity"},
		"Excel表格制作专家": {"tpl-excel-analyst", "tpl-csv-workbook"},
		"UI专家":         {"frontend-design", "ui-components", "design-system"},
		"产品经理专家":      {"pm-skill", "tpl-grill-me", "tpl-to-spec"},
		"系统架构师专家":     {"tpl-improve-architecture", "tpl-mermaid-diagrams"},
		"数据库设计专家":     {"tpl-mermaid-diagrams", "tpl-pm-phase-3"},
		"系统项目结构规范专家": {"tpl-knowledge-index", "tpl-mermaid-diagrams"},
		"开发规范专家":      {"tpl-code-reviewer", "tpl-grill-me"},
		"系统测试专家":      {"tpl-test-writer", "tpl-e2e-browser", "browser-automation"},
		"硬件配置专家":      {"tpl-web-researcher", "tpl-hardware-bom"},
		"开发专家":        {"tpl-implement", "tpl-tdd-loop", "tpl-debugger"},
	}
	wantTool := map[string]string{
		"PPT专家":         "pptx.gen",
		"报告编写专家":      "docx.gen",
		"小说编写专家":      "docx.gen",
		"Excel表格制作专家": "excel.gen",
		"UI专家":         "html.gen",
		"产品经理专家":      "web.search",
		"系统架构师专家":     "workspace.read",
		"数据库设计专家":     "skill.invoke",
		"系统项目结构规范专家": "workspace.list",
		"开发规范专家":      "workspace.read",
		"系统测试专家":      "browser.act",
		"硬件配置专家":      "excel.gen",
		"开发专家":        "workspace.edit",
	}
	if len(wantSkill) != 13 {
		t.Fatalf("want 13 specialists, got %d", len(wantSkill))
	}
	for _, item := range m8app.ConversationExperts() {
		hint := expertComposeHint([]string{item.Name}, published, nil)
		if !strings.Contains(hint, "[专家自动挂载]") || !strings.Contains(hint, item.Name) {
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
