package m8app_test

import (
	"context"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/m8app"
)

func TestConversationExpertsCatalogAndRules(t *testing.T) {
	items := m8app.ConversationExperts()
	if len(items) != len(m8app.ConversationExpertIDs) {
		t.Fatalf("conversation experts = %d, want %d", len(items), len(m8app.ConversationExpertIDs))
	}
	wantName := map[string]string{
		"ppt-expert":       "PPT专家",
		"report-writer":    "报告编写专家",
		"novel-writer":     "小说编写专家",
		"excel-maker":      "Excel表格制作专家",
		"ui-designer":      "UI专家",
		"pm-expert":        "产品经理专家",
		"architect-expert": "系统架构师专家",
		"db-expert":        "数据库设计专家",
		"repo-expert":      "系统项目结构规范专家",
		"standards-expert": "开发规范专家",
		"test-expert":      "系统测试专家",
		"hardware-expert":  "硬件配置专家",
		"dev-expert":       "开发专家",
	}
	foundRepo, foundStandards, foundTest, foundHardware, foundDev := false, false, false, false, false
	for _, item := range items {
		if item.ID == "repo-expert" {
			foundRepo = true
		}
		if item.ID == "standards-expert" {
			foundStandards = true
		}
		if item.ID == "test-expert" {
			foundTest = true
		}
		if item.ID == "hardware-expert" {
			foundHardware = true
		}
		if item.ID == "dev-expert" {
			foundDev = true
		}
		if want, ok := wantName[item.ID]; ok && want != item.Name {
			t.Fatalf("%s name = %q, want %q", item.ID, item.Name, want)
		}
		if item.Usage != m8app.CatalogUsageBoth {
			t.Fatalf("%s usage = %q, want both (center + 对话)", item.ID, item.Usage)
		}
		if item.ResolvedKind() != m8app.ExpertKindAgent {
			t.Fatalf("%s kind = %q, want agent", item.ID, item.ResolvedKind())
		}
		if strings.Contains(item.SixSection.Identity, "对话技能包") {
			t.Fatalf("%s identity still calls itself a conversation skill pack", item.ID)
		}
		if !strings.Contains(item.SixSection.Identity, "独立智能体") {
			t.Fatalf("%s identity must say 独立智能体", item.ID)
		}
		if strings.Contains(item.SixSection.Identity, "你是月汐的") {
			t.Fatalf("%s identity must not call itself 月汐的", item.ID)
		}
		body := strings.Join([]string{
			item.SixSection.Identity, item.SixSection.Mission, item.SixSection.Rules,
			item.SixSection.Workflow, item.SixSection.DeliverableTemplate, item.SixSection.SuccessMetrics,
		}, "\n")
		switch item.ID {
		case "ppt-expert":
			for _, needle := range []string{"大纲", "演讲备注", "pptx.gen", "结构", "desktop=true", `A["封面<br/>副标题"]`, "收集素材", "web.search", "九步", "再思考", "文类", "slides[].notes"} {
				if !strings.Contains(body, needle) {
					t.Fatalf("PPT专家 missing %q", needle)
				}
			}
		case "report-writer":
			for _, needle := range []string{"去AI味", "结构", "赋能", "首先/其次", "翻译腔", "docx.gen", "desktop=true", "流水线", "两轮", "完整章节", "特别简单", "封面", "使用说明书", "质量体检"} {
				if !strings.Contains(body, needle) {
					t.Fatalf("报告编写专家 missing %q", needle)
				}
			}
		case "novel-writer":
			for _, needle := range []string{"去AI味", "人物", "场景", "虚构", "不是工作报告", "desktop=true", "docx.gen", "起承转合", "分章", "提纲", "kind=novel", "账本", "晋升"} {
				if !strings.Contains(body, needle) {
					t.Fatalf("小说编写专家 missing %q", needle)
				}
			}
		case "excel-maker":
			for _, needle := range []string{"excel.gen", "冻结", "公式", "表头", "desktop=true", "月度汇总"} {
				if !strings.Contains(body, needle) {
					t.Fatalf("Excel表格制作专家 missing %q", needle)
				}
			}
		case "ui-designer":
			for _, needle := range []string{
				"月汐", "令牌", "a11y", "暗色", "禁止把月汐 chrome 换成",
				"空态", "加载", "企业", "组件语法", "微信绿",
			} {
				if !strings.Contains(body, needle) {
					t.Fatalf("UI专家 missing %q", needle)
				}
			}
		case "pm-expert":
			for _, needle := range []string{
				"PRD", "用户故事", "优先级", "路线图", "指标", "发现",
				"pm-advisor", "项目管理", "不替代",
			} {
				if !strings.Contains(body, needle) {
					t.Fatalf("产品经理专家 missing %q", needle)
				}
			}
			if strings.Contains(body, "九阶段交付治理") {
				t.Fatal("产品经理专家 must not duplicate pm-advisor stage-gate copy")
			}
		case "architect-expert":
			for _, needle := range []string{
				"$arch", "$sage", "$flow", "$vet", "$vibe", "$build",
				"证据先行", "八维", "短名单", "Mermaid", "C4", "ADR",
				"pm-expert", "improve-architecture", "solution-architect", "db-expert",
			} {
				if !strings.Contains(body, needle) {
					t.Fatalf("系统架构师专家 missing %q", needle)
				}
			}
			if strings.Contains(body, "229+") || strings.Contains(body, "229 项技术目录倾倒") {
				t.Fatal("系统架构师专家 must not dump a 229-item catalog")
			}
			if strings.Contains(body, "九阶段交付治理") {
				t.Fatal("系统架构师专家 must not duplicate pm-advisor stage-gate copy")
			}
		case "db-expert":
			for _, needle := range []string{
				"概念", "逻辑", "物理", "3NF", "Mermaid", "erDiagram",
				"SQLite", "N+1", "租户", "PII", "访问模式", "扩展-收缩",
				"产品经理专家", "系统架构师专家", "数据库优化师", "50 表",
			} {
				if !strings.Contains(body, needle) {
					t.Fatalf("数据库设计专家 missing %q", needle)
				}
			}
			if strings.Contains(body, "九阶段交付治理") {
				t.Fatal("数据库设计专家 must not duplicate pm-advisor stage-gate copy")
			}
		case "repo-expert":
			for _, needle := range []string{
				"目录", "模块", "证据", "cmd/", "internal/", "web/",
				"AGENTS.md", "系统架构师专家", "开发规范专家", "claude-flow",
				"不替代", "C4", "单体仓库", "迁移",
			} {
				if !strings.Contains(body, needle) {
					t.Fatalf("系统项目结构规范专家 missing %q", needle)
				}
			}
			if strings.Contains(body, "swarm_init") || strings.Contains(body, "npx claude-flow init") {
				t.Fatal("系统项目结构规范专家 must not clone claude-flow swarm/scaffold commands")
			}
			if strings.Contains(body, "九阶段交付治理") {
				t.Fatal("系统项目结构规范专家 must not duplicate pm-advisor stage-gate copy")
			}
		case "standards-expert":
			for _, needle := range []string{
				"AGENTS.md", "硬", "PowerShell", "gofmt", "200 页",
				"系统架构师", "系统项目结构", "数据库设计", "code-reviewer",
				"系统开发规范", "月汐",
			} {
				if !strings.Contains(body, needle) {
					t.Fatalf("开发规范专家 missing %q", needle)
				}
			}
			if strings.Contains(body, "九阶段交付治理") {
				t.Fatal("开发规范专家 must not duplicate pm-advisor stage-gate copy")
			}
		case "test-expert":
			for _, needle := range []string{
				"金字塔", "风险优先", "E2E", "探索", "流量回放",
				"能跑 ≠ 业务正确", "200 用例", "未授权抓包",
				"FullScopeTest", "QuAIA", "Agentic QE", "find-bug", "GoReplay", "七类探针",
				"系统架构师专家", "开发规范专家", "系统项目结构规范专家", "开发专家",
				"API 测试员",
			} {
				if !strings.Contains(body, needle) {
					t.Fatalf("系统测试专家 missing %q", needle)
				}
			}
			if strings.Contains(body, "九阶段交付治理") {
				t.Fatal("系统测试专家 must not duplicate pm-advisor stage-gate copy")
			}
		case "hardware-expert":
			for _, needle := range []string{
				"QPS", "并发用户", "IOPS", "BOM", "ERP", "MES", "WMS",
				"GPU", "ASR", "TTS", "WebView2", "证据先行", "短名单", "电气",
				"系统架构师专家", "上位机工程师", "capacity-planning",
				"wanghao-io", "LLM-Capacity-Planner", "仓不存在",
			} {
				if !strings.Contains(body, needle) {
					t.Fatalf("硬件配置专家 missing %q", needle)
				}
			}
			if strings.Contains(body, "九阶段交付治理") {
				t.Fatal("硬件配置专家 must not duplicate pm-advisor stage-gate copy")
			}
		case "dev-expert":
			for _, needle := range []string{
				"计划→实现→验证", "最小补丁", "开发规范专家", "系统项目结构规范专家",
				"系统架构师专家", "系统测试专家", "高级开发者",
				"Refact", "Tabby", "JeecgBoot", "生产环境",
				"Go + React + SQLite", "月汐",
			} {
				if !strings.Contains(body, needle) {
					t.Fatalf("开发专家 missing %q", needle)
				}
			}
			if strings.Contains(body, "九阶段交付治理") {
				t.Fatal("开发专家 must not duplicate pm-advisor stage-gate copy")
			}
		}
	}
	if !foundRepo {
		t.Fatal("conversation catalog missing repo-expert 系统项目结构规范专家")
	}
	if !foundStandards {
		t.Fatal("conversation catalog missing standards-expert 开发规范专家")
	}
	if !foundTest {
		t.Fatal("conversation catalog missing test-expert 系统测试专家")
	}
	if !foundHardware {
		t.Fatal("conversation catalog missing hardware-expert 硬件配置专家")
	}
	if !foundDev {
		t.Fatal("conversation catalog missing dev-expert 开发专家")
	}
}

func TestConversationExpertsInstructThinkSkillsToolsDrawWrite(t *testing.T) {
	needles := []string{
		"todo.write", "skill.invoke", "web.search", "web.fetch", "mermaid",
		"docx.gen", "excel.gen", "pptx.gen", "html.gen", "desktop=true", "200 页",
	}
	for _, item := range m8app.ConversationExperts() {
		body := strings.Join([]string{
			item.SixSection.Identity, item.SixSection.Mission, item.SixSection.Rules,
			item.SixSection.Workflow, item.SixSection.DeliverableTemplate, item.SixSection.SuccessMetrics,
		}, "\n")
		for _, needle := range needles {
			if !strings.Contains(body, needle) {
				t.Fatalf("%s (%s) missing capability %q", item.Name, item.ID, needle)
			}
		}
	}
	if len(m8app.ConversationExperts()) != 13 {
		t.Fatalf("want 13 specialists, got %d", len(m8app.ConversationExperts()))
	}
}

func TestConversationExpertComposeAttachLists(t *testing.T) {
	want := map[string][]string{
		"ppt-expert":       {"slide-builder", "web-researcher", "mermaid-diagrams"},
		"report-writer":    {"web-researcher", "docx-writer", "anti-ai-prose"},
		"novel-writer":     {"docx-writer", "anti-ai-prose", "fiction-continuity"},
		"excel-maker":      {"excel-analyst", "csv-workbook"},
		"ui-designer":      {"frontend-design", "ui-components", "design-system"},
		"pm-expert":        {"pm-skill", "grill-me", "to-spec"},
		"architect-expert": {"improve-architecture", "mermaid-diagrams"},
		"db-expert":        {"mermaid-diagrams"},
		"repo-expert":      {"knowledge-index", "mermaid-diagrams"},
		"standards-expert": {"code-reviewer", "grill-me"},
		"test-expert":      {"test-writer", "e2e-browser", "browser-automation", "find-bug"},
		"hardware-expert":  {"web-researcher", "hardware-bom"},
		"dev-expert":       {"implement", "tdd-loop", "debugger", "code-reviewer"},
	}
	wantTools := map[string][]string{
		"ppt-expert":       {"web.search", "pptx.gen", "skill.invoke"},
		"report-writer":    {"web.search", "docx.gen", "skill.invoke"},
		"novel-writer":     {"docx.gen", "skill.invoke"},
		"excel-maker":      {"excel.gen", "excel.parse"},
		"ui-designer":      {"workspace.write", "skill.invoke"},
		"pm-expert":        {"web.search", "skill.invoke"},
		"architect-expert": {"skill.invoke", "workspace.read"},
		"db-expert":        {"skill.invoke", "workspace.write"},
		"repo-expert":      {"workspace.list", "workspace.read"},
		"standards-expert": {"skill.invoke", "workspace.read"},
		"test-expert":      {"skill.invoke", "browser.act"},
		"hardware-expert":  {"web.search", "excel.gen"},
		"dev-expert":       {"workspace.edit", "command.run", "skill.invoke"},
	}
	for _, item := range m8app.ConversationExperts() {
		if len(item.PreferredSkills) == 0 || len(item.RequiredTools) == 0 {
			t.Fatalf("%s missing preferredSkills/requiredTools", item.ID)
		}
		skills, tools, _, _ := m8app.ComposeForExpertNames([]string{item.Name})
		for _, id := range want[item.ID] {
			if !m8app.SkillMatchesPreferred(id, "builtin://"+id, skills) && !containsStr(skills, id) {
				t.Fatalf("%s compose skills %#v missing %q", item.ID, skills, id)
			}
		}
		for _, tool := range wantTools[item.ID] {
			if !containsStr(tools, tool) {
				t.Fatalf("%s compose tools %#v missing %q", item.ID, tools, tool)
			}
		}
	}
	if got := m8app.PreferredComposeTemplateIDs(); len(got) < 10 {
		t.Fatalf("preferred compose union too small: %#v", got)
	}
}

func containsStr(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}

func TestEnsureBuiltinExpertsSeedsConversationRoster(t *testing.T) {
	svc := openExpertService(t)
	if err := m8app.EnsureBuiltinExperts(context.Background(), svc); err != nil {
		t.Fatal(err)
	}
	listed, err := svc.List(context.Background(), m8app.ExpertFilter{State: "enabled"})
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, row := range listed.Experts {
		names[row.Name] = true
	}
	for _, item := range m8app.ConversationExperts() {
		if !names[item.Name] {
			t.Fatalf("seeded library missing %q (picker will not see it)", item.Name)
		}
	}
	if !names["pm-advisor"] {
		t.Fatal("seeded library missing pm-advisor; conversation PM expert must not replace workbench advisor")
	}
	if err := m8app.EnsureBuiltinExperts(context.Background(), svc); err != nil {
		t.Fatalf("idempotent seed: %v", err)
	}
}

func TestEnsureBuiltinExpertsRefreshesStaleConversationBodies(t *testing.T) {
	svc := openExpertService(t)
	ctx := context.Background()
	created := createExpert(t, svc, "PPT专家")
	before, err := svc.Detail(ctx, m8app.DetailInput{ExpertID: created.ExpertID})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(before.SixSection), "上台稿导演") {
		t.Fatal("fixture already has catalog recipe")
	}
	if err := m8app.EnsureBuiltinExperts(ctx, svc); err != nil {
		t.Fatal(err)
	}
	after, err := svc.Detail(ctx, m8app.DetailInput{ExpertID: created.ExpertID})
	if err != nil {
		t.Fatal(err)
	}
	body := string(after.SixSection)
	if !strings.Contains(body, "上台稿导演") || !strings.Contains(body, "独立智能体") {
		t.Fatalf("stale PPT专家 body not refreshed: %s", body)
	}
	if len(after.Versions) < 2 {
		t.Fatal("refresh must append a new version")
	}
}

func TestEnsureBuiltinExpertsAddsMissingToExistingLibrary(t *testing.T) {
	svc := openExpertService(t)
	ctx := context.Background()
	createExpert(t, svc, "自定义安全专家")
	// Older install: conversation specialists through 开发规范 already seeded;
	// the three implementer/test/hardware cards must still be added.
	for _, name := range []string{
		"PPT专家", "报告编写专家", "小说编写专家", "Excel表格制作专家", "UI专家", "产品经理专家",
		"系统架构师专家", "数据库设计专家", "系统项目结构规范专家", "开发规范专家",
	} {
		createExpert(t, svc, name)
	}
	if err := m8app.EnsureBuiltinExperts(ctx, svc); err != nil {
		t.Fatal(err)
	}
	listed, err := svc.List(ctx, m8app.ExpertFilter{})
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for _, row := range listed.Experts {
		counts[row.Name]++
	}
	if counts["自定义安全专家"] != 1 {
		t.Fatalf("user-added expert wiped or duplicated: %d", counts["自定义安全专家"])
	}
	if counts["产品经理专家"] != 1 {
		t.Fatalf("产品经理专家 count = %d, want 1 (no wipe, no duplicate)", counts["产品经理专家"])
	}
	if counts["系统架构师专家"] != 1 {
		t.Fatal("existing install did not receive 系统架构师专家")
	}
	if counts["数据库设计专家"] != 1 {
		t.Fatal("existing install did not receive 数据库设计专家")
	}
	if counts["系统项目结构规范专家"] != 1 {
		t.Fatal("existing install did not receive 系统项目结构规范专家")
	}
	if counts["开发规范专家"] != 1 {
		t.Fatal("existing install did not receive 开发规范专家")
	}
	if counts["系统测试专家"] != 1 {
		t.Fatal("existing install did not receive 系统测试专家")
	}
	if counts["硬件配置专家"] != 1 {
		t.Fatal("existing install did not receive 硬件配置专家")
	}
	if counts["开发专家"] != 1 {
		t.Fatal("existing install did not receive 开发专家")
	}
	if counts["pm-advisor"] != 1 {
		t.Fatal("existing install lost pm-advisor")
	}
	if counts["PPT专家"] != 1 {
		t.Fatalf("PPT专家 count = %d, want 1 (no wipe, no duplicate)", counts["PPT专家"])
	}
	for _, item := range m8app.ConversationExperts() {
		if counts[item.Name] != 1 {
			t.Fatalf("%s count = %d, want 1 after additive seed", item.Name, counts[item.Name])
		}
	}
}

func TestConversationPMExpertDistinctFromAgencyProductManager(t *testing.T) {
	var agencyName, convoName string
	for _, item := range m8app.AgencyAgentsCatalog() {
		switch item.ID {
		case "product-manager":
			agencyName = item.Name
		case "pm-expert":
			convoName = item.Name
		}
	}
	if agencyName != "产品经理" {
		t.Fatalf("agency product-manager name = %q, want 产品经理 (market card)", agencyName)
	}
	if convoName != "产品经理专家" {
		t.Fatalf("pm-expert name = %q, want 产品经理专家", convoName)
	}
}

func TestConversationArchitectExpertDistinctFromAgencyArchitects(t *testing.T) {
	names := map[string]string{}
	var architectUsage, architectDivision string
	for _, item := range m8app.AgencyAgentsCatalog() {
		switch item.ID {
		case "architect-expert", "engineering-software-architect", "engineering-backend-architect":
			names[item.ID] = item.Name
		}
		if item.ID == "architect-expert" {
			architectUsage = item.Usage
			architectDivision = item.Division
		}
	}
	if names["architect-expert"] != "系统架构师专家" {
		t.Fatalf("architect-expert name = %q, want 系统架构师专家", names["architect-expert"])
	}
	if architectUsage != m8app.CatalogUsageBoth {
		t.Fatalf("architect-expert usage = %q, want both", architectUsage)
	}
	if architectDivision != "engineering" {
		t.Fatalf("architect-expert division = %q, want engineering", architectDivision)
	}
	if names["engineering-software-architect"] != "软件架构师" {
		t.Fatalf("agency software architect name = %q, want 软件架构师", names["engineering-software-architect"])
	}
	if names["engineering-backend-architect"] != "后端架构师" {
		t.Fatalf("agency backend architect name = %q, want 后端架构师", names["engineering-backend-architect"])
	}
	if names["architect-expert"] == names["engineering-software-architect"] || names["architect-expert"] == names["engineering-backend-architect"] {
		t.Fatal("architect-expert name collides with an agency architect card")
	}
}

func TestConversationDBExpertDistinctFromAgencyDatabaseOptimizer(t *testing.T) {
	names := map[string]string{}
	var dbUsage, dbDivision string
	for _, item := range m8app.AgencyAgentsCatalog() {
		switch item.ID {
		case "db-expert", "engineering-database-optimizer":
			names[item.ID] = item.Name
		}
		if item.ID == "db-expert" {
			dbUsage = item.Usage
			dbDivision = item.Division
		}
	}
	if names["db-expert"] != "数据库设计专家" {
		t.Fatalf("db-expert name = %q, want 数据库设计专家", names["db-expert"])
	}
	if dbUsage != m8app.CatalogUsageBoth {
		t.Fatalf("db-expert usage = %q, want both", dbUsage)
	}
	if dbDivision != "data" {
		t.Fatalf("db-expert division = %q, want data", dbDivision)
	}
	if names["engineering-database-optimizer"] != "数据库优化师" {
		t.Fatalf("agency database optimizer name = %q, want 数据库优化师", names["engineering-database-optimizer"])
	}
	if names["db-expert"] == names["engineering-database-optimizer"] {
		t.Fatal("db-expert name collides with agency 数据库优化师")
	}
}

func TestConversationStandardsExpertDistinctFromAgencyCodeReviewer(t *testing.T) {
	names := map[string]string{}
	var usage, division string
	for _, item := range m8app.AgencyAgentsCatalog() {
		switch item.ID {
		case "standards-expert", "engineering-code-reviewer":
			names[item.ID] = item.Name
		}
		if item.ID == "standards-expert" {
			usage = item.Usage
			division = item.Division
		}
	}
	if names["standards-expert"] != "开发规范专家" {
		t.Fatalf("standards-expert name = %q, want 开发规范专家", names["standards-expert"])
	}
	if usage != m8app.CatalogUsageBoth {
		t.Fatalf("standards-expert usage = %q, want both", usage)
	}
	if division != "engineering" {
		t.Fatalf("standards-expert division = %q, want engineering", division)
	}
	if names["engineering-code-reviewer"] != "代码审查员" {
		t.Fatalf("agency code reviewer name = %q, want 代码审查员", names["engineering-code-reviewer"])
	}
	if names["standards-expert"] == names["engineering-code-reviewer"] {
		t.Fatal("standards-expert name collides with agency 代码审查员")
	}
}

func TestConversationRepoExpertDistinctFromArchitectAndAgency(t *testing.T) {
	names := map[string]string{}
	var repoUsage, repoDivision string
	for _, item := range m8app.AgencyAgentsCatalog() {
		switch item.ID {
		case "repo-expert", "architect-expert", "standards-expert", "engineering-software-architect", "engineering-backend-architect":
			names[item.ID] = item.Name
		}
		if item.ID == "repo-expert" {
			repoUsage = item.Usage
			repoDivision = item.Division
		}
	}
	if names["repo-expert"] != "系统项目结构规范专家" {
		t.Fatalf("repo-expert name = %q, want 系统项目结构规范专家", names["repo-expert"])
	}
	if names["architect-expert"] != "系统架构师专家" {
		t.Fatalf("architect-expert name = %q, want 系统架构师专家", names["architect-expert"])
	}
	if repoUsage != m8app.CatalogUsageBoth {
		t.Fatalf("repo-expert usage = %q, want both", repoUsage)
	}
	if repoDivision != "engineering" {
		t.Fatalf("repo-expert division = %q, want engineering", repoDivision)
	}
	if names["repo-expert"] == names["architect-expert"] {
		t.Fatal("repo-expert name collides with 系统架构师专家")
	}
	if names["repo-expert"] == names["engineering-software-architect"] || names["repo-expert"] == names["engineering-backend-architect"] {
		t.Fatal("repo-expert name collides with an agency architect card")
	}
	if names["standards-expert"] != "开发规范专家" {
		t.Fatalf("standards-expert name = %q, want 开发规范专家", names["standards-expert"])
	}
	if names["repo-expert"] == names["standards-expert"] {
		t.Fatal("repo-expert name collides with 开发规范专家")
	}
}

func TestConversationTestExpertDistinctFromAgencyTesters(t *testing.T) {
	names := map[string]string{}
	var usage, division string
	for _, item := range m8app.AgencyAgentsCatalog() {
		switch item.ID {
		case "test-expert", "testing-api-tester", "testing-test-results-analyzer", "testing-embedded-qa-engineer":
			names[item.ID] = item.Name
		}
		if item.ID == "test-expert" {
			usage = item.Usage
			division = item.Division
		}
	}
	if names["test-expert"] != "系统测试专家" {
		t.Fatalf("test-expert name = %q, want 系统测试专家", names["test-expert"])
	}
	if usage != m8app.CatalogUsageBoth {
		t.Fatalf("test-expert usage = %q, want both", usage)
	}
	if division != "testing" {
		t.Fatalf("test-expert division = %q, want testing", division)
	}
	if names["testing-api-tester"] != "API 测试员" {
		t.Fatalf("agency API tester name = %q, want API 测试员", names["testing-api-tester"])
	}
	if names["test-expert"] == names["testing-api-tester"] || names["test-expert"] == names["testing-test-results-analyzer"] || names["test-expert"] == names["testing-embedded-qa-engineer"] {
		t.Fatal("test-expert name collides with an agency testing card")
	}
}

func TestConversationHardwareExpertDistinctFromAgencyPCHost(t *testing.T) {
	names := map[string]string{}
	var usage, division string
	for _, item := range m8app.AgencyAgentsCatalog() {
		switch item.ID {
		case "hardware-expert", "engineering-pc-host-engineer", "engineering-mechanical-design-engineer":
			names[item.ID] = item.Name
		}
		if item.ID == "hardware-expert" {
			usage = item.Usage
			division = item.Division
		}
	}
	if names["hardware-expert"] != "硬件配置专家" {
		t.Fatalf("hardware-expert name = %q, want 硬件配置专家", names["hardware-expert"])
	}
	if usage != m8app.CatalogUsageBoth {
		t.Fatalf("hardware-expert usage = %q, want both", usage)
	}
	if division != "engineering" {
		t.Fatalf("hardware-expert division = %q, want engineering", division)
	}
	if names["engineering-pc-host-engineer"] != "上位机工程师" {
		t.Fatalf("agency PC host name = %q, want 上位机工程师", names["engineering-pc-host-engineer"])
	}
	if names["hardware-expert"] == names["engineering-pc-host-engineer"] || names["hardware-expert"] == names["engineering-mechanical-design-engineer"] {
		t.Fatal("hardware-expert name collides with an agency hardware/host card")
	}
}

func TestConversationDevExpertDistinctFromStandardsAndAgencyDeveloper(t *testing.T) {
	names := map[string]string{}
	var usage, division string
	for _, item := range m8app.AgencyAgentsCatalog() {
		switch item.ID {
		case "dev-expert", "standards-expert", "engineering-senior-developer", "engineering-frontend-developer", "engineering-code-reviewer":
			names[item.ID] = item.Name
		}
		if item.ID == "dev-expert" {
			usage = item.Usage
			division = item.Division
		}
	}
	if names["dev-expert"] != "开发专家" {
		t.Fatalf("dev-expert name = %q, want 开发专家", names["dev-expert"])
	}
	if names["standards-expert"] != "开发规范专家" {
		t.Fatalf("standards-expert name = %q, want 开发规范专家", names["standards-expert"])
	}
	if usage != m8app.CatalogUsageBoth {
		t.Fatalf("dev-expert usage = %q, want both", usage)
	}
	if division != "engineering" {
		t.Fatalf("dev-expert division = %q, want engineering", division)
	}
	if names["dev-expert"] == names["standards-expert"] {
		t.Fatal("dev-expert name collides with 开发规范专家")
	}
	if names["engineering-senior-developer"] != "高级开发者" {
		t.Fatalf("agency senior developer name = %q, want 高级开发者", names["engineering-senior-developer"])
	}
	if names["dev-expert"] == names["engineering-senior-developer"] || names["dev-expert"] == names["engineering-frontend-developer"] || names["dev-expert"] == names["engineering-code-reviewer"] {
		t.Fatal("dev-expert name collides with an agency developer card")
	}
}
