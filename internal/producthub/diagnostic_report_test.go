package producthub

import (
	"strings"
	"testing"
)

func TestReportStatesReadbackLimits(t *testing.T) {
	ed := Edition{LiveProbe: ProbeScore{Passed: 49, Total: 49}, HealthScore: 100}
	md, _ := RenderReport(ed)
	for _, banned := range []string{
		"这一环是实测通过率",
		"这一节核对了功能、链路",
		"没有未关闭的问题、错误或 BUG",
	} {
		if strings.Contains(md, banned) {
			t.Fatalf("overclaim %q", banned)
		}
	}
	if !strings.Contains(md, "不是本机键鼠、麦克风、真实供应商或真实进程的实测") {
		t.Fatal("readback limit missing")
	}
	if !strings.Contains(md, "这不是说产品没有别的问题") {
		t.Fatal("empty problem line overclaims")
	}
}

func TestTemplateChainIsWrittenFromTheHandler(t *testing.T) {
	name := "创建办公任务"
	ed := Edition{Features: []Card{{
		StableKey:  "feature.office.studio.task-create",
		Name:       name,
		Domain:     "office",
		Module:     "office",
		ChainClass: "crud-bridge",
		Chain:      defaultChain("crud-bridge", name),
		Scaffold:   Scaffold{Bridge: []string{"office.task.create"}},
	}}}
	md, _ := RenderReport(ed)
	if !strings.Contains(md, "1. handleOfficeStudio — office.task.create。这次从源码读到的处理函数。") {
		t.Fatal("template steps were not replaced by the traced handler")
	}
	unclear := between(md, "步骤未按真实调用写清", "\n")
	if strings.Contains(unclear, name) {
		t.Fatalf("still listed as unwritten: %s", unclear)
	}
}

func TestCatalogTemplateChainsAreWrittenWhenTheHandlerExists(t *testing.T) {
	cards := clarifyTemplateChains(Merge(Seed(), LiveCatalog(), nil))
	hops, ready := traceCalls(cards)
	if !ready {
		t.Fatal("call index not ready")
	}
	var left []string
	for _, c := range cards {
		var handlers []string
		for _, method := range claimedBridges(c) {
			if hops[method].Handler != "" {
				handlers = append(handlers, hops[method].Handler)
			}
		}
		if len(handlers) == 0 {
			if len(c.Chain.Steps) > 0 && c.Chain.Steps[0].Name != unwrittenStepName {
				left = append(left, c.Name)
			}
			continue
		}
		seen := map[string]bool{}
		for _, step := range c.Chain.Steps {
			seen[step.Name] = true
		}
		for _, handler := range handlers {
			if !seen[handler] {
				left = append(left, c.Name+" 缺 "+handler)
			}
		}
		if c.Chain.Steps[0].Description != "这次从源码读到的处理函数。" && c.Chain.Steps[0].Description != "按工具名分派，不把函数内部分支串成一条链路。" {
			left = append(left, c.Name+" 第一步不是源码调用")
		}
	}
	if len(left) > 0 {
		t.Fatalf("chain not written from the handler: %s", strings.Join(left, "、"))
	}
}

func TestDiagnosticReportChecksTheProductAndGivesEvidencePlans(t *testing.T) {
	ed := Edition{
		EditionID:   "diag",
		GeneratedAt: "2026-09-26T00:00:00Z",
		HealthScore: 25,
		LiveProbe:   ProbeScore{Passed: 1, Total: 4},
		Features: []Card{
			{
				StableKey: "feature.assets.plugin.llm", Name: "插件：LLM", Domain: "assets", Module: "plugins",
				Summary:    "在插件页启用或使用「LLM」。",
				Methods:    []Method{{Type: "menu", Entry: "plugins"}},
				Chain:      Chain{Steps: []Step{{Index: 1, Name: "启用", Detail: "开关", Description: "打开插件"}}},
				Attributes: Attributes{Tools: []string{"llm"}},
				Scaffold:   Scaffold{Pages: []string{"plugins"}, Runtime: []string{"harness:llm"}},
			},
			{
				StableKey: "feature.office.task.create", Name: "创建任务", Domain: "office", Module: "office",
				Summary: "创建办公任务。",
			},
			{
				StableKey: "feature.execution.command.run", Name: "执行命令", Domain: "execution", Module: "command",
				Summary:    "执行白名单命令。",
				Methods:    []Method{{Type: "menu", Entry: "settings"}},
				Attributes: Attributes{Skills: []string{"skill.invoke"}, MCPs: []string{"mcp.demo"}},
				Scaffold:   Scaffold{Bridge: []string{"command.run"}},
			},
			{
				StableKey: "feature.foundation.project.open", Name: "打开项目", Domain: "foundation", Module: "project",
				Summary: "打开项目。",
				Methods: []Method{{Type: "menu", Entry: "projects"}},
				Chain:   Chain{Steps: []Step{{Index: 1, Name: "打开", Detail: "项目", Description: "进入项目"}}},
			},
		},
		Graph: Graph{Nodes: []GraphNode{
			{Type: "Plugin", Name: "插件：LLM", StableKey: "plugin.llm"},
			{Type: "Skill", Name: "调用技能", StableKey: "skill.invoke"},
			{Type: "Mcp", Name: "连接 MCP", StableKey: "mcp.connect"},
			{Type: "Domain", Name: "资产与智能", StableKey: "domain.assets"},
			{Type: "Module", Name: "plugins", StableKey: "module.assets.plugins"},
		}, Edges: []GraphEdge{{From: "feature.assets.plugin.llm", To: "plugin.llm", Rel: "uses"}}},
		Findings: []Finding{{
			Severity: "error", ErrorCode: "PH_L02", StableKey: "probe.play", Title: "播放", Status: "open",
			Evidence: "没有播出来", RootCause: "这一次真实跑动没有完成",
			Fix: "净化会再播 0.2 秒探测音。", Verify: "复查通过才改为 fixed",
		}},
	}
	md, _ := RenderReport(ed)
	for _, heading := range []string{
		"### 功能", "### 链路", "### 工具", "### 技能", "### MCP", "### 插件",
		"### 资产", "### 项目", "### 开发能力", "### 任务处理", "### 脚手架", "### 架构",
		"### 实测", "### 日志与故障", "### 竞品对照", "### 优化方案",
	} {
		if !strings.Contains(md, heading) {
			t.Fatalf("report missing %s", heading)
		}
	}
	if !strings.Contains(md, "创建任务") || !strings.Contains(md, "步骤还不清楚") {
		t.Fatal("empty chain was not listed")
	}
	if !strings.Contains(md, "净化会再播 0.2 秒探测音") {
		t.Fatal("probe plan missing")
	}
	if !strings.Contains(md, "插件：LLM") || !strings.Contains(md, "打开项目") || !strings.Contains(md, "执行命令") {
		t.Fatal("inventory dropped a stored item")
	}
	if strings.Contains(md, "FunASR") || strings.Contains(md, "SenseVoice") || strings.Contains(md, "100分") || strings.Contains(md, "下次生成会自动出现") {
		t.Fatal("report invented an upgrade or a perfect score")
	}
	link := md[strings.Index(md, "### 链路"):]
	link = link[:strings.Index(link, "### 工具")]
	if !strings.Contains(link, "插件：LLM") || !strings.Contains(link, "打开项目") {
		t.Fatal("hand-written steps without a handler were treated as clear")
	}
}

func TestDedicatedHandlersAreNotCalledMissingBranches(t *testing.T) {
	cards := Merge(Seed(), LiveCatalog(), nil)
	hops, ready := traceCalls(cards)
	if !ready {
		t.Fatal("call index not ready")
	}
	for method, hop := range hops {
		if hop.Handler == "" {
			continue
		}
		text := hopText(hop)
		if strings.Contains(text, "方法分支不在") || strings.Contains(text, "没有定义") || strings.Contains(text, "任务走不通") {
			t.Fatalf("%s %s", method, text)
		}
	}
}

func TestChainsIncludeTheWholeBranch(t *testing.T) {
	cards := []Card{
		{Name: "导出办公产物", Scaffold: Scaffold{Bridge: []string{"office.artifact.export"}}},
		{Name: "读取工作区文件", Scaffold: Scaffold{Bridge: []string{"desktop.files.readChunk"}}},
		{Name: "电脑控制", Scaffold: Scaffold{Bridge: []string{"computer.control"}}},
	}
	hops, ready := traceCalls(cards)
	if !ready {
		t.Fatal("call index not ready")
	}
	export := hops["office.artifact.export"].Steps
	if !containsName(export, "Export") || !containsName(export, "ExportFormal") {
		t.Fatalf("export branch incomplete: %v", export)
	}
	if containsName(export, "projectIDForSession") || len(export) == 24 {
		t.Fatalf("export branch spilled or stopped at 24: %v", export)
	}
	read := hops["desktop.files.readChunk"].Steps
	if !containsName(read, "ReadFull") || len(read) == 24 {
		t.Fatalf("readChunk branch incomplete or stopped at 24: %v", read)
	}
	cc := hops["computer.control"]
	text := hopText(cc)
	if !strings.Contains(text, "按工具名分派") || len(cc.Steps) >= 24 {
		t.Fatalf("computer.control still a truncated list: %s", text)
	}
	catalog := Merge(Seed(), LiveCatalog(), nil)
	all, _ := traceCalls(catalog)
	for method, hop := range all {
		if len(hop.Steps) == 24 {
			t.Fatalf("%s still stops at 24 calls", method)
		}
	}
}

func containsName(steps []string, name string) bool {
	for _, step := range steps {
		if step == name {
			return true
		}
	}
	return false
}

func TestLocalCallsAreNotReportedAsMissing(t *testing.T) {
	cards := []Card{
		{Name: "保存自动化任务", Scaffold: Scaffold{Bridge: []string{"automation.job.set", "automation.job.trigger"}}},
		{Name: "查看系统健康", Scaffold: Scaffold{Bridge: []string{"system.health"}}},
		{Name: "电脑控制", Scaffold: Scaffold{Bridge: []string{"computer.control"}}},
		{Name: "连接 MCP", Scaffold: Scaffold{Bridge: []string{"mcp.security.review"}}},
	}
	hops, ready := traceCalls(cards)
	if !ready {
		t.Fatal("call index not ready")
	}
	for _, method := range []string{"automation.job.set", "automation.job.trigger", "system.health", "computer.control", "mcp.security.review"} {
		text := hopText(hops[method])
		if strings.Contains(text, "没有定义") || strings.Contains(text, "任务走不通") || strings.Contains(text, "任务完成不了") {
			t.Fatalf("%s: %s", method, text)
		}
	}
	lines := taskLines(cards, nil, nil, hops, ready)
	if strings.Contains(lines, "任务完成不了") || strings.Contains(lines, "任务走不通") {
		t.Fatal(lines)
	}
	all := Merge(Seed(), LiveCatalog(), nil)
	catalogHops, _ := traceCalls(all)
	var still []string
	for method, hop := range catalogHops {
		if len(hop.Missing) > 0 {
			still = append(still, method+" "+strings.Join(hop.Missing, ","))
		}
	}
	if len(still) > 0 {
		t.Fatalf("still missing: %s", strings.Join(still, "；"))
	}
}

func TestProbedEntriesResolveToHandlers(t *testing.T) {
	methods := []string{
		"agent.run.start", "appUpdate.check", "automation.job.set", "automation.job.trigger",
		"br.navigate", "computer.control", "desktop.files.readChunk",
		"media.clear", "media.create", "media.jump", "media.move", "media.mute", "media.next",
		"media.pause", "media.previous", "media.remove", "media.seek", "media.set_volume",
		"media.stop", "media.toggle", "media.unmute",
		"meetings.start", "meetings.summary.source.get", "memory.create", "memory.search",
		"message.append", "message.search", "mro.manual.register", "mro.plan.publish",
		"ocr.routing.get", "people.file.open", "people.file.pick", "people.thread.send",
		"plugin.toggle", "provider.create", "provider.test", "session.delete",
		"session.experts.set", "session.update", "skill.invoke", "system.health",
	}
	cards := make([]Card, len(methods))
	for i, method := range methods {
		cards[i] = Card{Name: method, Scaffold: Scaffold{Bridge: []string{method}}}
	}
	hops, ready := traceCalls(cards)
	if !ready {
		t.Fatal("call index not ready")
	}
	var missing []string
	for _, method := range methods {
		if hops[method].Handler == "" {
			missing = append(missing, method)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("no handler: %s", strings.Join(missing, "、"))
	}
	want := map[string]string{
		"br.navigate":             "handleBrNavigate",
		"meetings.start":          "handleMeetingsStart",
		"people.file.open":        "handlePeopleFileOpen",
		"provider.test":           "handleProviderTest",
		"system.health":           "handleSystemHealth",
		"skill.invoke":            "handleSkillInvoke",
		"desktop.files.readChunk": "HandleHost",
		"media.pause":             "handleMediaSessionCommand",
		"media.clear":             "handleMediaQueueCommand",
		"computer.control":        "ExecuteTool",
	}
	for method, handler := range want {
		if hops[method].Handler != handler {
			t.Fatalf("%s handler %s", method, hops[method].Handler)
		}
	}
	written := clarifyTemplateChains(cards)
	for _, card := range written {
		if len(card.Chain.Steps) == 0 || card.Chain.Steps[0].Name == unwrittenStepName {
			t.Fatalf("%s still unwritten", card.Name)
		}
		if card.Chain.Steps[0].Name != hops[card.Name].Handler {
			t.Fatalf("%s step %s handler %s", card.Name, card.Chain.Steps[0].Name, hops[card.Name].Handler)
		}
	}
}

func TestHandWrittenChainIsRewrittenOrMarkedUnwritten(t *testing.T) {
	traced := Edition{Features: []Card{{
		StableKey:  "feature.office.studio.task-create",
		Name:       "创建办公任务",
		Domain:     "office",
		Module:     "office",
		ChainClass: "crud-bridge",
		Chain:      Chain{Steps: []Step{{Index: 1, Name: "自写", Detail: "真实", Description: "不是模板"}}},
		Scaffold:   Scaffold{Bridge: []string{"office.task.create"}},
	}}}
	md, _ := RenderReport(traced)
	if !strings.Contains(md, "1. handleOfficeStudio — office.task.create。这次从源码读到的处理函数。") {
		t.Fatal("hand-written steps with a handler were left in place")
	}
	if strings.Contains(md, "自写") || strings.Contains(md, "不是模板") {
		t.Fatal("hand-written step still presented as the chain")
	}

	invented := Edition{Features: []Card{{
		StableKey:  "feature.dialog.music.open-player",
		Name:       "打开音乐播放软件",
		Domain:     "dialog",
		Module:     "companion",
		ChainClass: "intent-control",
		Chain: closedChain([]Step{
			{1, "用户输入", "语音/文字", "月伴说或打字；语音经 VAD 断句。"},
			{2, "语音识别", "ASR", "本地 sherpa；在线不可用切本地。"},
		}, 2, "窗口到了", "未安装", "再搜", "播报原因"),
		Scaffold: Scaffold{Bridge: []string{"computer.control"}},
	}}}
	md, _ = RenderReport(invented)
	if strings.Contains(md, "本地 sherpa") || strings.Contains(md, "语音识别") {
		t.Fatal("hand-written steps were presented as the chain")
	}
	if !strings.Contains(md, "1. ExecuteTool — computer.control。按工具名分派，不把函数内部分支串成一条链路。") {
		t.Fatal("computer.control was not written from ExecuteTool")
	}
	unclear := between(md, "步骤未按真实调用写清", "\n")
	if strings.Contains(unclear, "打开音乐播放软件") {
		t.Fatalf("traced computer control stayed unwritten: %s", unclear)
	}
}

func TestDiagnosticReportNamesABrokenLink(t *testing.T) {
	ed := Edition{
		Features: []Card{{
			StableKey: "feature.a", Name: "甲", Domain: "foundation", Module: "diagnostics",
			Methods: []Method{{Type: "menu", Entry: "diagnostics"}},
			Chain:   Chain{Steps: []Step{{Index: 1, Name: "走", Detail: "一步", Description: "往下"}}},
		}},
		Graph: Graph{
			Nodes: []GraphNode{{ID: "feature.a", StableKey: "feature.a", Type: "Feature", Name: "甲"}},
			Edges: []GraphEdge{{From: "feature.a", To: "missing.node", Rel: "uses"}},
		},
	}
	md, _ := RenderReport(ed)
	link := md[strings.Index(md, "### 链路"):]
	link = link[:strings.Index(link, "### 工具")]
	if !strings.Contains(link, "链路没有接上") || !strings.Contains(link, "feature.a → missing.node") {
		t.Fatal(link)
	}
	arch := md[strings.Index(md, "### 架构"):]
	arch = arch[:strings.Index(arch, "### 实测")]
	if !strings.Contains(arch, "Feature 1") {
		t.Fatal(arch)
	}
}
