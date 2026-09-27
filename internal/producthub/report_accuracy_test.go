package producthub

import (
	"context"
	"fmt"
	"os"
	"slices"
	"sort"
	"strings"
	"testing"
)

func TestComputerControlAndMCPAreNotFalseUnwired(t *testing.T) {
	s := New(&MemoryPersist{})
	ed, err := s.Generate(context.Background(), "boot")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range ed.Findings {
		if f.ErrorCode != "PH_021" || !isOpenFinding(f.Status) {
			continue
		}
		if strings.Contains(f.Evidence, "computer.control") || f.StableKey == "feature.assets.mcp.invoke" {
			t.Fatalf("report called a real entry unwired: %+v", f)
		}
	}
}

func TestPluginEnableIsNotAnExtraPlugin(t *testing.T) {
	s := New(&MemoryPersist{})
	ed, err := s.Generate(context.Background(), "boot")
	if err != nil {
		t.Fatal(err)
	}
	md, _ := RenderReport(ed)
	acc := between(md, "### 准确", "### 能力")
	if !strings.Contains(acc, "插件功能卡 23，图谱 Plugin 节点 23。数量一致。") {
		t.Fatalf("plugin count %s", acc)
	}
}

func TestMissingDictationRuntimeIsNotCalledUnfinished(t *testing.T) {
	ed := Edition{
		Findings: []Finding{{
			Severity: "warn", ErrorCode: "PH_L01", StableKey: "probe.dictate", Title: "听写", Status: "open",
			Evidence: "听写模型未就绪，没有开始识别：voice model not installed: runtime",
		}},
	}
	md, _ := RenderReport(ed)
	tasks := between(md, "### 任务完成", "### 宕机")
	if strings.Contains(tasks, "任务完成不了") {
		t.Fatalf("missing runtime was called unfinished: %s", tasks)
	}
	if !strings.Contains(tasks, "精识别运行时没装上，不是流式听写已经坏了") {
		t.Fatalf("runtime sentence missing: %s", tasks)
	}
}

func TestInstalledModelLengthTimeoutIsNotABrokenDownload(t *testing.T) {
	evidence := "耗时 16.972s。本机模型目录已核对。这次没有在时限内拿到服务器文件长度：timeout"
	found := TaskFindings([]TaskResult{{ID: "download", Title: "下载", Status: "untested", Evidence: evidence}})
	if strings.Contains(found[0].RootCause, "没有跑起来") {
		t.Fatalf("local model check was called unstarted: %s", found[0].RootCause)
	}
	ed := Edition{Findings: found}
	md, _ := RenderReport(ed)
	tasks := between(md, "### 任务完成", "### 宕机")
	if strings.Contains(tasks, "任务完成不了") {
		t.Fatalf("length timeout was called unfinished: %s", tasks)
	}
	if !strings.Contains(tasks, "本机模型目录已核对") || !strings.Contains(tasks, "不是下载已经坏了") {
		t.Fatalf("length timeout sentence missing: %s", tasks)
	}
}

func TestDeadlineExceededIsNotCalledInsideTheBudget(t *testing.T) {
	ed := Edition{Findings: []Finding{{
		Severity: "warn", ErrorCode: "PH_L03", StableKey: "probe.download", Title: "下载", Status: "open",
		Evidence: "耗时 20s。本机模型目录已核对。这次没有在时限内拿到服务器文件长度：context deadline exceeded",
	}}}
	md, _ := RenderReport(ed)
	speed := between(md, "### 速度与效率", "### 准确")
	if strings.Contains(speed, "在探测预算 20s 内") {
		t.Fatalf("a deadline was called inside the budget: %s", speed)
	}
	if !strings.Contains(speed, "用满探测预算 20s") || !strings.Contains(speed, "没有在时限内完成") {
		t.Fatalf("deadline sentence missing: %s", speed)
	}
}

func TestMissingSourceIsNotAMissingHandler(t *testing.T) {
	cards := []Card{
		{Name: "甲", StableKey: "feature.a", Methods: []Method{{Type: "menu", Entry: "home"}}, Chain: Chain{Steps: []Step{{Index: 1, Name: "手写"}}}},
		{Name: "乙", StableKey: "feature.b", Methods: []Method{{Type: "menu", Entry: "home"}}, Chain: Chain{Steps: []Step{{Index: 1, Name: "手写"}}}},
	}
	out := applyCallChains(cards, nil, false)
	for _, c := range out {
		if len(c.Chain.Steps) == 0 || c.Chain.Steps[0].Name != unreadSourceStepName {
			t.Fatalf("%s step %#v", c.Name, c.Chain.Steps)
		}
	}
	plans := diagnosticGapPlans(Edition{Features: out})
	if strings.Contains(plans, "没有对上处理函数") || strings.Contains(plans, "甲：") {
		t.Fatalf("missing source listed as handler gaps: %s", plans)
	}
	if !strings.Contains(plans, "没有产品源码") || !strings.Contains(plans, "2 张") {
		t.Fatalf("source gap missing: %s", plans)
	}
}

func TestUsableReportDoesNotRepeatCallsOrInventMissingHandlers(t *testing.T) {
	s := New(&MemoryPersist{})
	ed, err := s.Generate(context.Background(), "boot")
	if err != nil {
		t.Fatal(err)
	}
	md, _ := RenderReport(ed)
	calls := between(md, "### 真实调用", "### 速度与效率")
	if strings.Count(calls, "创建办公任务：") != 1 {
		t.Fatalf("office task call repeated: %d", strings.Count(calls, "创建办公任务："))
	}
	plans := between(md, "### 优化方案", "### [")
	if strings.Contains(plans, "步骤未按真实调用写清") {
		t.Fatalf("optimization still lists a missing handler:\n%s", plans)
	}
	for _, want := range []string{
		"执行白名单命令：command.start → handleCommandStart",
		"试用专家：expert.try → handleExpertTry",
		"新建对话：session.create → handleSessionCreate",
		"发到消息通道：im.send →",
		"月伴语音对话：voice.start → handleVoiceStart",
		"唤醒月伴：voice.start → handleVoiceStart",
		"Token 精简：context.compact.preview → handleContextCompactPreview",
		"Token 精简：context.compact.commit → handleContextCompactCommit",
		"插话发生在已经开始的语音会话里",
		"这一卡只说明从导航进入该页",
		"这个插件在运行名单里",
	} {
		if !strings.Contains(md, want) {
			t.Fatalf("missing %s", want)
		}
	}
}

func TestLandscapeCardsAreComparisonNotes(t *testing.T) {
	s := New(&MemoryPersist{})
	ed, err := s.Generate(context.Background(), "boot")
	if err != nil {
		t.Fatal(err)
	}
	md, _ := RenderReport(ed)
	if strings.Contains(md, "未按真实调用写清 — 无处理函数") {
		t.Fatal("a card was still called a missing handler")
	}
	for _, want := range []string{"选定对照维", "收集出处", "观察前沿", "记入图景"} {
		if !strings.Contains(md, want) {
			t.Fatalf("landscape step missing: %s", want)
		}
	}
}

func TestInstalledReportUsesCompiledCalls(t *testing.T) {
	idx := indexFromCompiledHops()
	if !idx.ready {
		t.Fatal("installed report has no compiled calls")
	}
	cards := []Card{
		{
			Name: "创建办公任务", StableKey: "feature.office.studio.task-create",
			Chain:    Chain{Steps: []Step{{Index: 1, Name: "手写"}}},
			Scaffold: Scaffold{Bridge: []string{"office.task.create"}},
		},
		{
			Name: "电脑控制键鼠", StableKey: "feature.execution.computer.mouse",
			Chain:    Chain{Steps: []Step{{Index: 1, Name: "手写"}}},
			Scaffold: Scaffold{Bridge: []string{"computer.control"}},
		},
		{
			Name: "调用 MCP 工具", StableKey: "feature.assets.mcp.invoke",
			Chain:    Chain{Steps: []Step{{Index: 1, Name: "手写"}}},
			Scaffold: Scaffold{Bridge: []string{"mcp.invoke"}},
		},
	}
	hops, ready := traceWithIndex(cards, idx)
	if !ready {
		t.Fatal("compiled index did not trace")
	}
	out := applyCallChains(cards, hops, ready)
	for _, c := range out {
		if len(c.Chain.Steps) == 0 || c.Chain.Steps[0].Name == unreadSourceStepName || c.Chain.Steps[0].Name == unwrittenStepName {
			t.Fatalf("%s stayed unwritten: %#v", c.Name, c.Chain.Steps)
		}
	}
	md := renderTraced(out, hops, ready)
	for _, want := range []string{
		"ExecuteTool — computer.control。按工具名分派，不把函数内部分支串成一条链路。",
		"office.task.create → handleOfficeStudio",
		"mcp.invoke",
		"这版程序编译时从源码写入的调用",
	} {
		if !strings.Contains(md, want) {
			t.Fatalf("missing %s\n%s", want, md)
		}
	}
	if strings.Contains(md, "没有产品源码") || strings.Contains(md, "未读到源码") {
		t.Fatal("compiled calls were described as missing source")
	}
}

func renderTraced(cards []Card, hops map[string]callHop, ready bool) string {
	var b strings.Builder
	for _, c := range cards {
		for _, st := range c.Chain.Steps {
			fmt.Fprintf(&b, "%d. %s — %s。%s\n", st.Index, st.Name, st.Detail, st.Description)
		}
	}
	b.WriteString(callSection(cards, hops, ready))
	return b.String()
}

func sameHop(a, b callHop) bool {
	a.FromBuild, b.FromBuild = false, false
	if a.Method != b.Method || a.Handler != b.Handler || a.Branch != b.Branch || a.Dispatch != b.Dispatch {
		return false
	}
	return slices.Equal(a.Steps, b.Steps) && slices.Equal(a.Missing, b.Missing)
}

func TestWriteCompiledCallIndex(t *testing.T) {
	if os.Getenv("LUNITIDE_WRITE_CALLS") != "1" {
		t.Skip()
	}
	root := FindProductRoot()
	live := buildCallIndex(root)
	seen := map[string]bool{}
	var methods []string
	add := func(method string) {
		if method == "" || seen[method] {
			return
		}
		seen[method] = true
		methods = append(methods, method)
	}
	for method := range live.handlers {
		add(method)
	}
	for method := range live.runtime {
		add(method)
	}
	sort.Strings(methods)
	var b strings.Builder
	b.WriteString("// Code generated from the product source. Do not edit.\n\npackage producthub\n\nfunc compiledHops() map[string]callHop {\n\treturn map[string]callHop{\n")
	n := 0
	for _, method := range methods {
		hop := live.lookup(method)
		if hop.Handler == "" {
			continue
		}
		n++
		fmt.Fprintf(&b, "\t\t%q: {Method: %q, Handler: %q", method, method, hop.Handler)
		if hop.Branch {
			b.WriteString(", Branch: true")
		}
		if hop.Dispatch {
			b.WriteString(", Dispatch: true")
		}
		writeNames(&b, "Steps", hop.Steps)
		writeNames(&b, "Missing", hop.Missing)
		b.WriteString("},\n")
	}
	b.WriteString("\t}\n}\n")
	if n < 40 {
		t.Fatalf("only %d hops", n)
	}
	if err := os.WriteFile("call_index_compiled.go", []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeNames(b *strings.Builder, field string, names []string) {
	if len(names) == 0 {
		return
	}
	fmt.Fprintf(b, ", %s: []string{", field)
	for i, name := range names {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(b, "%q", name)
	}
	b.WriteString("}")
}

func TestCompiledHopsMatchTheSource(t *testing.T) {
	root := FindProductRoot()
	if root == "" {
		t.Fatal("product root missing")
	}
	live := buildCallIndex(root)
	compiled := indexFromCompiledHops()
	if !compiled.ready {
		t.Fatal("compiled hops empty")
	}
	seen := map[string]bool{}
	check := func(method string) {
		if seen[method] {
			return
		}
		seen[method] = true
		if !sameHop(compiled.lookup(method), live.lookup(method)) {
			t.Fatalf("%s\ncompiled %#v\nlive %#v", method, compiled.lookup(method), live.lookup(method))
		}
	}
	for method := range live.handlers {
		check(method)
	}
	for method := range live.runtime {
		check(method)
	}
	if len(seen) < 40 {
		t.Fatalf("only %d methods", len(seen))
	}
}
