package app

import (
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/llmadapter"
)

func TestClassifyChatLaneT01GreetingIsChat(t *testing.T) {
	if got := classifyChatLane(LaneInput{Goal: "你好"}); got != LaneChat {
		t.Fatalf("你好 => %s want chat", got)
	}
}

func TestClassifyChatLaneT05ArticleWithoutFileIsChat(t *testing.T) {
	if got := classifyChatLane(LaneInput{Goal: "写一篇文章介绍光合作用"}); got != LaneChat {
		t.Fatalf("got %s want chat", got)
	}
	if got := classifyChatLane(LaneInput{Goal: "把这段话润色得更顺：今天开会很顺利"}); got != LaneChat {
		t.Fatalf("润色 => %s want chat", got)
	}
	if got := classifyChatLane(LaneInput{Goal: "写一篇关于修改 v1 版本的文章"}); got != LaneChat {
		t.Fatalf("article about a revision => %s want chat", got)
	}
}

func TestFileRevisionKeepsWriteAndRun(t *testing.T) {
	goal := "修改 v1 版本的产物，覆盖 index.html 和 README.md"
	lane := classifyChatLane(LaneInput{Goal: goal})
	if lane != LaneTask {
		t.Fatalf("revision of an existing deliverable => %s want task", lane)
	}
	defs := []llmadapter.ToolDefinition{
		{Name: "workspace.write"}, {Name: "workspace.read"}, {Name: "command.run"},
		{Name: "skill.try"}, {Name: "user.ask"},
	}
	got := applyLaneTools(defs, buildLaneContract(lane, RouteUnspecified, CouncilOverlay{}))
	seen := map[string]bool{}
	for _, d := range got {
		seen[d.Name] = true
	}
	for _, name := range []string{"workspace.write", "command.run", "workspace.read"} {
		if !seen[name] {
			t.Fatalf("full surface must keep %s, got %v", name, seen)
		}
	}
	// v2 FR-100: even the prose lane keeps the full surface — budget, not
	// tools, is the only lane difference.
	prose := applyLaneTools(defs, buildLaneContract(LaneChat, RouteUnspecified, CouncilOverlay{}))
	if len(prose) != len(defs) {
		t.Fatalf("prose lane must keep the full tool surface: %v", namesOf(prose))
	}
}

func TestSkillTrialFollowUpPromotesReadOnlyLane(t *testing.T) {
	if got := promoteLaneForSkillTrial(LaneChat, true); got != LaneTask {
		t.Fatalf("chat trial follow-up => %s want task", got)
	}
	if got := promoteLaneForSkillTrial(LaneTask, true); got != LaneTask {
		t.Fatalf("task trial => %s", got)
	}
	if got := promoteLaneForSkillTrial(LaneAgent, true); got != LaneAgent {
		t.Fatalf("agent trial => %s", got)
	}
	if got := promoteLaneForSkillTrial(LaneChat, false); got != LaneChat {
		t.Fatalf("no trial => %s", got)
	}
}

func TestArtifactRevisionKeepsWriteOnEveryDeliverable(t *testing.T) {
	defs := []llmadapter.ToolDefinition{
		{Name: "docx.gen"}, {Name: "pptx.gen"}, {Name: "excel.gen"}, {Name: "pdf.gen"},
		{Name: "office.generate"}, {Name: "workspace.write"}, {Name: "command.run"},
		{Name: "user.ask"},
	}
	cases := []string{
		"修改上周的周报",
		"把演示文稿升级一版",
		"调整表格里的数字，改原文件",
		"修复这个页面，在原文件上再改一版",
		"把已有 PDF 改版",
	}
	for _, goal := range cases {
		lane := classifyChatLane(LaneInput{Goal: goal})
		if lane != LaneTask {
			t.Fatalf("%q => %s want task", goal, lane)
		}
		seen := map[string]bool{}
		for _, d := range applyLaneTools(defs, buildLaneContract(lane, RouteUnspecified, CouncilOverlay{})) {
			seen[d.Name] = true
		}
		for _, name := range []string{"docx.gen", "pptx.gen", "excel.gen", "pdf.gen", "office.generate", "workspace.write", "command.run"} {
			if !seen[name] {
				t.Fatalf("%q dropped %s", goal, name)
			}
		}
	}
	if got := classifyChatLane(LaneInput{Goal: "写周报"}); got != LaneTask {
		t.Fatalf("fresh weekly report => %s want task", got)
	}
	// v2: any 周报 mention is an office deliverable (task tier); G-301/G-302
	// own ask-first at run time. An article without office keywords stays chat.
	if got := classifyChatLane(LaneInput{Goal: "写一篇关于修改周报的文章"}); got != LaneTask {
		t.Fatalf("article naming a weekly report => %s want task", got)
	}
	if got := classifyChatLane(LaneInput{Goal: "调整这段话的语气"}); got == LaneTask {
		t.Fatal("prose polish must stay off the write lane")
	}
	finish := "继续完成输出产物"
	if route := detectTaskRoute(finish); route == RouteR0 || route == RouteR1 || route == RouteR3 {
		t.Fatalf("%q route %s drops workspace.write before the lane runs", finish, route)
	}
	if got := classifyChatLane(LaneInput{Goal: finish}); got != LaneTask {
		t.Fatalf("%q => %s want task", finish, got)
	}
	seen := map[string]bool{}
	for _, d := range applyLaneTools(defs, buildLaneContract(LaneTask, RouteUnspecified, CouncilOverlay{})) {
		seen[d.Name] = true
	}
	if !seen["workspace.write"] || !seen["command.run"] {
		t.Fatalf("finish-the-deliverable must keep write and run, got %v", seen)
	}
	// v2 FR-100: the read-only label is history — chat keeps the same surface
	// and differs only in step budget.
	readonly := applyLaneTools(defs, buildLaneContract(LaneChat, RouteUnspecified, CouncilOverlay{}))
	if len(readonly) != len(defs) {
		t.Fatalf("prose lane must keep the full surface: %v", namesOf(readonly))
	}
}

func TestFirstAndLaterDeliverablesKeepFileTools(t *testing.T) {
	// v2: a PriorDeliverable follow-up lands on task; "继续完成输出产物" is
	// recognized as artifact-revision wording and stays task too (the chat
	// tier can only paste).
	for _, tc := range []struct {
		goal string
		want ChatLane
	}{
		{"继续完成输出产物", LaneTask},
		{"把按钮改成蓝色", LaneTask},
		{"优化一下", LaneTask},
		{"再出一版", LaneTask},
		{"[引用技能 POC|abc]\n接着做", LaneTask},
	} {
		if got := classifyChatLane(LaneInput{Goal: tc.goal, PriorDeliverable: true}); got != tc.want {
			t.Fatalf("follow-up %q => %s want %s", tc.goal, got, tc.want)
		}
	}
	if got := classifyChatLane(LaneInput{Goal: "[引用技能 POC|abc]\n做一个可运行页面"}); got != LaneTask {
		t.Fatalf("first skill output => %s want task", got)
	}
	if got := classifyChatLane(LaneInput{Goal: "做一个可运行的销售页面"}); got != LaneTask {
		t.Fatalf("first runnable page => %s want task", got)
	}
	if got := classifyChatLane(LaneInput{Goal: "写一个 html 小游戏"}); got != LaneTask {
		t.Fatalf("first html => %s want task", got)
	}
	if got := classifyChatLane(LaneInput{Goal: "把按钮改成蓝色"}); got == LaneTask {
		t.Fatal("a button tweak with no open file must stay off the deliverable lane")
	}
	if got := classifyChatLane(LaneInput{Goal: "写一篇关于光合作用的文章", PriorDeliverable: true}); got == LaneTask {
		t.Fatal("in-place prose must stay off the file lane")
	}
	if got := classifyChatLane(LaneInput{Goal: "你好", PriorDeliverable: true}); got != LaneChat {
		t.Fatalf("greeting => %s want chat", got)
	}
	if got := classifyChatLane(LaneInput{Goal: "写周报"}); got != LaneTask {
		t.Fatalf("fresh weekly report => %s want task", got)
	}
	if !checkpointWasDeliverable(chatTurnCheckpoint{LastTools: []string{"skill.invoke"}}) {
		t.Fatal("a skill trial counts as an open deliverable")
	}
	if !checkpointWasDeliverable(chatTurnCheckpoint{Goal: "写周报", LastTools: []string{"user.ask"}}) {
		t.Fatal("a report that only asked must still leave the file writable")
	}
	if checkpointWasDeliverable(chatTurnCheckpoint{Goal: "写一篇关于光合作用的文章"}) {
		t.Fatal("prose is not a file")
	}
	if got := classifyChatLane(LaneInput{Goal: "本周修了支付超时", PriorDeliverable: true}); got != LaneTask {
		t.Fatalf("content after a report ask => %s want task", got)
	}
	if got := classifyChatLane(LaneInput{Goal: "写一份说明文档"}); got != LaneTask {
		t.Fatalf("first readme => %s want task", got)
	}
	if !shouldWidenAndRetry(widenInput{Goal: "继续完成输出产物", AssistantText: "本环境未提供文件写入，请复制保存为 index.html"}) {
		t.Fatal("a paste refusal must retry with the full tool surface")
	}
	if checkpointWasDeliverable(chatTurnCheckpoint{LastTools: []string{"user.ask"}}) {
		t.Fatal("asking a question is not a file")
	}

	catalog := []llmadapter.ToolDefinition{
		{Name: "workspace.write"}, {Name: "workspace.edit"}, {Name: "command.run"},
		{Name: "docx.gen"}, {Name: "html.gen"}, {Name: "office.generate"}, {Name: "user.ask"},
	}
	// v2 FR-100: the first weekly report (task) and the later overwrite both
	// see the full catalog — the restore ladder is gone because nothing strips
	// the write family anymore.
	weekly := applyLaneTools(catalog, buildLaneContract(LaneTask, RouteUnspecified, CouncilOverlay{}))
	seen := map[string]bool{}
	for _, d := range weekly {
		seen[d.Name] = true
	}
	for _, name := range []string{"workspace.write", "docx.gen", "office.generate", "command.run"} {
		if !seen[name] {
			t.Fatalf("first weekly report must see %s, got %v", name, seen)
		}
	}
	landed := applyLaneTools(catalog, buildLaneContract(LaneTask, RouteR4, CouncilOverlay{}))
	seen = map[string]bool{}
	for _, d := range landed {
		seen[d.Name] = true
	}
	if !seen["workspace.write"] || !seen["command.run"] {
		t.Fatalf("later overwrite must keep write and run, got %v", seen)
	}
}

func TestClassifyChatLaneT03WeeklyReportNoMaterialIsTask(t *testing.T) {
	if detectTaskRoute("写周报") != RouteR4 {
		t.Fatalf("写周报 route=%s want R4", detectTaskRoute("写周报"))
	}
	if got := classifyChatLane(LaneInput{Goal: "写周报"}); got != LaneTask {
		t.Fatalf("写周报 => %s want task", got)
	}
}

func TestClassifyChatLaneT04WeeklyReportWithPointsIsTask(t *testing.T) {
	goal := "写周报\n- 完成登录页\n- 修复支付超时\n- 下周联调"
	if !hasTurnMaterials(goal, false, false) {
		t.Fatal("three bullets must count as materials")
	}
	if got := classifyChatLane(LaneInput{Goal: goal}); got != LaneTask {
		t.Fatalf("got %s want task (infer bullets without HasTurnMaterials flag)", got)
	}
}

func TestClassifyChatLaneT06OnlineIndustryReportIsTask(t *testing.T) {
	if got := classifyChatLane(LaneInput{Goal: "根据网上公开资料写一份行业报告"}); got != LaneTask {
		t.Fatalf("got %s want task", got)
	}
}

func TestClassifyChatLaneOpenWordWeeklyIsAgent(t *testing.T) {
	if got := classifyChatLane(LaneInput{Goal: "打开 Word 写周报"}); got != LaneAgent {
		t.Fatalf("got %s want agent", got)
	}
}

func TestClassifyChatLaneDesktopFolderIsAgent(t *testing.T) {
	goal := "帮我在桌面创建一个文件夹，名字叫可可"
	if got := classifyChatLane(LaneInput{Goal: goal}); got != LaneAgent {
		t.Fatalf("got %s want agent", got)
	}
	_, contract := applyLaneOverrides(LaneAgent, LaneInput{Goal: goal}, RouteUnspecified, CouncilOverlay{})
	if contract.DisableReasoning {
		t.Fatal("desktop mkdir must keep thinking/tools, not chat DisableReasoning")
	}
}

func TestClassifyChatLaneHostActsAreAgent(t *testing.T) {
	goals := []string{
		"帮我在桌面创建一个文件夹，名字叫可可",
		"make a folder on my desktop named keke",
		"new folder on the desktop called 可可",
		"帮我删掉桌面上的可可",
		"delete the folder 可可 on the desktop",
		"帮我把桌面上的可可改名叫可可2",
		"rename the desktop folder 可可 to 可可2",
		"帮我把这个文件复制到桌面",
		"copy this file to my desktop",
		"把可可文件夹移到桌面",
		"move the 可可 folder to the desktop",
		"帮我在桌面新建一个 txt 文件",
		"create a text file on my desktop",
		"解压到桌面",
		"下载这个到桌面",
		"清空回收站",
		"帮我在 D 盘建个目录叫 backup",
	}
	defs := []llmadapter.ToolDefinition{
		{Name: "command.run"},
		{Name: "workspace.read"},
		{Name: "workspace.write"},
		{Name: "skill.invoke"},
		{Name: "computer.act"},
	}
	for _, goal := range goals {
		if detectTaskRoute(goal) == RouteR2 || detectTaskRoute(goal) == RouteR3 {
			t.Errorf("%q must stay unspecified/full-tool, not shrink to %s", goal, detectTaskRoute(goal))
		}
		got := classifyChatLane(LaneInput{Goal: goal})
		if got != LaneAgent {
			t.Errorf("%q => %s want agent", goal, got)
			continue
		}
		_, contract := applyLaneOverrides(got, LaneInput{Goal: goal}, detectTaskRoute(goal), CouncilOverlay{})
		if contract.DisableReasoning || !contract.ContinueNudges || contract.MaxMainToolSteps < 8 {
			t.Errorf("%q contract DisableReasoning=%v ContinueNudges=%v steps=%d", goal, contract.DisableReasoning, contract.ContinueNudges, contract.MaxMainToolSteps)
		}
		kept := applyLaneTools(defs, contract)
		hasRun := false
		for _, d := range kept {
			if d.Name == "command.run" {
				hasRun = true
			}
		}
		if !hasRun {
			t.Errorf("%q agent must keep command.run, kept=%v", goal, kept)
		}
	}
	if got := classifyChatLane(LaneInput{Goal: "写一篇文章介绍光合作用"}); got != LaneChat {
		t.Fatalf("photosynthesis article => %s want chat", got)
	}
	if got := classifyChatLane(LaneInput{Goal: "写一篇关于创建文件夹的文章"}); got != LaneChat {
		t.Fatalf("article about folders => %s want chat", got)
	}
	if got := classifyChatLane(LaneInput{Goal: "写周报放到桌面"}); got == LaneAgent {
		t.Fatal("写周报放到桌面 must stay office, not a desktop folder act")
	}
	if got := classifyChatLane(LaneInput{Goal: "生成 Word 周报，放到桌面。"}); got == LaneAgent {
		t.Fatal("生成周报到桌面 must stay office")
	}
	if got := classifyChatLane(LaneInput{Goal: "生成周报保存到桌面"}); got == LaneAgent {
		t.Fatal("保存到桌面 on a weekly report is still office.generate")
	}
	if got := classifyChatLane(LaneInput{Goal: "帮我删掉桌面上的周报.docx"}); got != LaneAgent {
		t.Fatalf("delete weekly-report file on desktop => %s want agent", got)
	}
}

func TestClassifyChatLaneVagueTaskIsTask(t *testing.T) {
	if got := classifyChatLane(LaneInput{Goal: "帮我做个任务"}); got != LaneTask {
		t.Fatalf("got %s want task", got)
	}
}

func TestClassifyChatLaneT18ResumeUsesPriorGoal(t *testing.T) {
	prior := "写周报\n- 完成登录页\n- 修复支付超时\n- 下周联调"
	if got := classifyChatLane(LaneInput{Goal: prior, HasTurnMaterials: true}); got != LaneTask {
		t.Fatalf("prior goal => %s", got)
	}
}

func TestApplyLaneOverridesT20NoWebStaysTask(t *testing.T) {
	in := LaneInput{Goal: "写周报，不要联网"}
	lane := classifyChatLane(in)
	lane, contract := applyLaneOverrides(lane, in, RouteR4, CouncilOverlay{})
	if lane != LaneTask {
		t.Fatalf("lane=%s want task", lane)
	}
	if contract.MaxMainToolSteps < 1 {
		t.Fatalf("task lane must keep its budget: %#v", contract)
	}
	// v2: 不要联网 is a user-explicit action — it gates auto-search behavior
	// (the office pipeline and the lookup ladder), never the tool surface.
	if len(fallbackWebSearchArgs("写周报，不要联网")) != 0 {
		t.Fatal("user-explicit offline must not auto web.search")
	}
}

func TestApplyLaneToolsKeepFullSurfaceOnEveryLane(t *testing.T) {
	// v2 FR-100 (replaces the T17 strip contract): every lane — including the
	// ask and materials lanes — keeps the whole catalog. Search-before-
	// discipline now lives in prompts and run-time gates, not whitelists.
	defs := []llmadapter.ToolDefinition{
		{Name: "web.search"}, {Name: "web.fetch"}, {Name: "docx.gen"}, {Name: "user.ask"},
		{Name: "todo.write"}, {Name: "workspace.read"}, {Name: "pptx.gen"}, {Name: "workspace.write"},
	}
	for _, lane := range []ChatLane{LaneChat, LaneTask, LaneAgent} {
		got := applyLaneTools(defs, buildLaneContract(lane, RouteR4, CouncilOverlay{}))
		if len(got) != len(defs) {
			t.Fatalf("lane %s stripped the surface: %v", lane, namesOf(got))
		}
	}
}

func TestApplyLaneToolsKeepsSpecialistGensEverywhere(t *testing.T) {
	defs := []llmadapter.ToolDefinition{
		{Name: "pptx.gen"}, {Name: "docx.gen"}, {Name: "excel.gen"},
		{Name: "web.search"}, {Name: "workspace.write"}, {Name: "user.ask"}, {Name: "skill.invoke"},
	}
	for _, lane := range []ChatLane{LaneChat, LaneTask, LaneAgent} {
		got := applyLaneTools(defs, buildLaneContract(lane, RouteR4, CouncilOverlay{}))
		for _, name := range []string{"pptx.gen", "docx.gen", "excel.gen", "web.search", "workspace.write", "user.ask", "skill.invoke"} {
			if !hasLaneTool(got, name) {
				t.Fatalf("lane %s dropped %s: %#v", lane, name, namesOf(got))
			}
		}
	}
}

func TestApplyLaneToolsKeepsSkillInvokeOnAskAndTalk(t *testing.T) {
	defs := []llmadapter.ToolDefinition{
		{Name: "skill.invoke"}, {Name: "web.search"}, {Name: "user.ask"},
	}
	ask := applyLaneTools(defs, buildLaneContract(LaneTask, RouteR4, CouncilOverlay{}))
	if !hasLaneTool(ask, "skill.invoke") || !hasLaneTool(ask, "web.search") {
		t.Fatalf("task must keep skill.invoke and web.search: %#v", namesOf(ask))
	}
	talk := applyLaneTools(defs, buildLaneContract(LaneChat, RouteR1, CouncilOverlay{}))
	if !hasLaneTool(talk, "skill.invoke") || !hasLaneTool(talk, "web.search") {
		t.Fatalf("chat must keep skill.invoke and web.search: %#v", namesOf(talk))
	}
}

func hasLaneTool(defs []llmadapter.ToolDefinition, name string) bool {
	for _, d := range defs {
		if d.Name == name {
			return true
		}
	}
	return false
}

func namesOf(defs []llmadapter.ToolDefinition) []string {
	out := make([]string, 0, len(defs))
	for _, d := range defs {
		out = append(out, d.Name)
	}
	return out
}

func TestOfficeResearchPipelineRoutesByMaterialsNotLane(t *testing.T) {
	// v2: the lane no longer gates the office pipeline — an office task id or
	// a capability turn does, and the task tier routes by materials instead
	// of silently starting the nine-step research pipeline.
	if shouldStartOfficeResearch(buildLaneContract(LaneTask, RouteR4, CouncilOverlay{}), "task-1", false) {
		t.Fatal("an office task id must skip the research pipeline")
	}
	if shouldStartOfficeResearch(buildLaneContract(LaneTask, RouteR4, CouncilOverlay{}), "", true) {
		t.Fatal("a capability turn must skip the research pipeline")
	}
	if !shouldStartOfficeResearch(buildLaneContract(LaneTask, RouteR4, CouncilOverlay{}), "", false) {
		t.Fatal("no task id and no capability work keeps the pipeline available")
	}
	blob := bundledWorkflowInjectionForLane("写周报", LaneTask)
	if strings.Contains(blob, "两轮 web.search") {
		t.Fatalf("task without materials must not inject the research pipeline: %s", blob)
	}
	if !strings.Contains(blob, "先核实或问清缺什么") {
		t.Fatalf("task without materials must nudge to verify first: %s", blob)
	}
}

func TestBundledWorkflowWeeklyReportOmitsResearch(t *testing.T) {
	blob := bundledWorkflowInjection("写周报")
	if strings.Contains(blob, "普通资料用 web.search") || strings.Contains(blob, "两轮 web.search") {
		t.Fatal(blob)
	}
	if !strings.Contains(blob, "先核实或问清缺什么") && !strings.Contains(blob, "离线") && !strings.Contains(blob, "已有材料") {
		t.Fatal(blob)
	}
}

func TestHasTurnMaterialsDoesNotTreatWeeklyAsMaterial(t *testing.T) {
	if hasTurnMaterials("写周报", false, false) {
		t.Fatal("周报 two characters are not materials")
	}
	if !hasTurnMaterials("见附件写周报", false, false) {
		t.Fatal("见附件 is materials")
	}
	if !hasTurnMaterials("写周报", true, false) {
		t.Fatal("this-turn attachment is materials")
	}
}

func TestClassifyChatLaneOfficeTaskDoesNotForceAgent(t *testing.T) {
	if got := classifyChatLane(LaneInput{Goal: "写周报", OfficeTaskID: "task-1"}); got != LaneTask {
		t.Fatalf("empty office 写周报 => %s want task", got)
	}
	ask := applyLaneTools([]llmadapter.ToolDefinition{{Name: "office.generate"}, {Name: "web.search"}, {Name: "docx.gen"}, {Name: "user.ask"}}, buildLaneContract(LaneTask, RouteR4, CouncilOverlay{}))
	for _, name := range []string{"office.generate", "web.search", "docx.gen", "user.ask"} {
		if !hasLaneTool(ask, name) {
			t.Fatalf("task must keep %s: %#v", name, namesOf(ask))
		}
	}
}

func TestClassifyChatLaneWeatherLookupKeepsSearch(t *testing.T) {
	goal := "今天合肥的天气怎么样"
	if detectTaskRoute(goal) != RouteR1 {
		t.Fatalf("weather route=%s want R1", detectTaskRoute(goal))
	}
	in := LaneInput{Goal: goal}
	lane := classifyChatLane(in)
	// v2: a weather turn is a host act — the agent tier only raises the
	// budget; web.search/weather.get stay on the surface for every tier.
	if lane != LaneAgent {
		t.Fatalf("weather must stay a host-act agent turn, got %s", lane)
	}
	_, contract := applyLaneOverrides(lane, in, RouteR1, CouncilOverlay{})
	if contract.MaxMainToolSteps < 4 {
		t.Fatalf("weather lookup must carry a search budget: %#v", contract)
	}
	defs := applyLaneTools([]llmadapter.ToolDefinition{{Name: "web.search"}, {Name: "weather.get"}, {Name: "workspace.read"}}, contract)
	if !hasLaneTool(defs, "web.search") || !hasLaneTool(defs, "weather.get") {
		t.Fatalf("weather tools stripped: %#v", namesOf(defs))
	}
}

func TestClassifyChatLaneNewsKeepsSearchWithoutReportPipeline(t *testing.T) {
	in := LaneInput{Goal: "今天有什么新闻"}
	lane := classifyChatLane(in)
	if lane != LaneChat {
		t.Fatalf("新闻 => %s want chat", lane)
	}
	_, contract := applyLaneOverrides(lane, in, RouteR1, CouncilOverlay{})
	// v2: SkipOfficeResearchPipeline is gone — the office pipeline only
	// starts for office deliverables, so a chat-tier news lookup never sees
	// it; assert the budget it does get instead.
	if contract.MaxMainToolSteps < 4 {
		t.Fatalf("news lookup must search without the report pipeline: %#v", contract)
	}
}

func TestStartOfficeWorkflowsTaskWithMaterialsWritesFromReference(t *testing.T) {
	// v2: a task-tier weekly report with materials runs write-from-reference —
	// docx opens in write mode on the provided bullets, no research pipeline.
	req := llmadapter.Request{Model: "m", Messages: []llmadapter.Message{{Role: llmadapter.RoleUser, Content: "写周报\n- 完成登录页\n- 修复支付超时\n- 下周联调"}}}
	turn := &chatTurnCheckpoint{Goal: "写周报\n- 完成登录页\n- 修复支付超时\n- 下周联调"}
	c := buildLaneContract(LaneTask, RouteR4, CouncilOverlay{})
	startOfficeWorkflowsIfNeeded(&req, turn, func(bridge.Event) error { return nil }, c, "", false)
	if turn.PptActive {
		t.Fatal("a weekly report is not a ppt task")
	}
	if !turn.DocxActive || turn.DocxStage != docxStageWrite {
		t.Fatalf("materials at hand must go write-from-reference: active=%v stage=%s", turn.DocxActive, turn.DocxStage)
	}
	if !strings.Contains(req.Messages[len(req.Messages)-1].Content, "已有材料") &&
		!strings.Contains(req.Messages[len(req.Messages)-1].Content, "离线") {
		t.Fatalf("task with materials should inject the reference-only instruction: %#v", req.Messages)
	}
	for _, m := range req.Messages {
		if strings.Contains(m.Content, "报告流水线") || strings.Contains(m.Content, "两轮 web.search") {
			t.Fatal("write-from-reference must not inject the research pipeline")
		}
	}
}

func TestStartOfficeWorkflowsKeepsTaskPipeline(t *testing.T) {
	req := llmadapter.Request{Model: "m", Messages: []llmadapter.Message{{Role: llmadapter.RoleUser, Content: "根据网上公开资料写一份行业报告"}}}
	turn := &chatTurnCheckpoint{Goal: "根据网上公开资料写一份行业报告"}
	c := buildLaneContract(LaneTask, RouteR4, CouncilOverlay{})
	startOfficeWorkflowsIfNeeded(&req, turn, func(bridge.Event) error { return nil }, c, "", false)
	if !turn.DocxActive {
		t.Fatal("task must still start the report pipeline")
	}
}

func TestDocxGenBlockedSkipPipelineT16(t *testing.T) {
	turn := &chatTurnCheckpoint{DocxActive: true, DocxKind: docxKindReport, SkipOfficeResearch: true}
	if blocked, _ := docxGenBlocked(turn, "docx.gen"); blocked {
		t.Fatal("SkipOfficeResearch must let L2 call docx.gen")
	}
}

func TestEveryLaneKeepsWebSearchInSurface(t *testing.T) {
	// v2 FR-100 (replaces laneAllowsWebSearch): no lane — ask, materials, or
	// research — may drop the web family from the surface. Behavioral search
	// discipline moved to prompts and the run-time gates.
	defs := []llmadapter.ToolDefinition{{Name: "web.search"}, {Name: "web.fetch"}}
	for _, lane := range []ChatLane{LaneChat, LaneTask, LaneAgent} {
		got := applyLaneTools(defs, buildLaneContract(lane, RouteR4, CouncilOverlay{}))
		if !hasLaneTool(got, "web.search") {
			t.Fatalf("lane %s dropped web.search: %#v", lane, namesOf(got))
		}
	}
}

func TestCapToolLoopLimitFollowsLane(t *testing.T) {
	if got := capToolLoopLimit(24, buildLaneContract(LaneTask, RouteR4, CouncilOverlay{})); got != 12 {
		t.Fatalf("task cap=%d want 12", got)
	}
	if got := capToolLoopLimit(16, buildLaneContract(LaneChat, RouteR4, CouncilOverlay{})); got != 4 {
		t.Fatalf("chat cap=%d want 4", got)
	}
	if got := capToolLoopLimit(32, buildLaneContract(LaneAgent, RouteR2, CouncilOverlay{})); got != 24 {
		t.Fatalf("agent cap=%d want 24", got)
	}
	if laneMayExtendToolLoop(buildLaneContract(LaneChat, RouteUnspecified, CouncilOverlay{})) {
		t.Fatal("chat must not extend the tool loop")
	}
	if !laneMayExtendToolLoop(buildLaneContract(LaneAgent, RouteR2, CouncilOverlay{})) {
		t.Fatal("agent may extend")
	}
}

func TestBundledWorkflowWeeklyReportStopsAfterAsk(t *testing.T) {
	blob := bundledWorkflowInjection("写周报")
	if strings.Contains(blob, "不要在勘查后停下等待确认") {
		t.Fatal("ask-first must not inherit execute-until-done discipline")
	}
	if !strings.Contains(blob, "问清缺什么") && !strings.Contains(blob, "问一句即停") {
		t.Fatal(blob)
	}
}

func TestResolveLaneGoalResumeUsesCheckpoint(t *testing.T) {
	prior := "写周报\n- 完成登录页\n- 修复支付超时\n- 下周联调"
	if got := resolveLaneGoal("继续", prior); got != prior {
		t.Fatalf("resume goal=%q", got)
	}
	goal := resolveLaneGoal("继续", prior)
	if classifyChatLane(LaneInput{Goal: goal}) != LaneTask {
		t.Fatal("T18: 继续 + prior weekly points must be task")
	}
	blob := bundledWorkflowInjectionForLane(goal, LaneTask)
	if strings.Contains(blob, "问清缺什么") || strings.Contains(blob, "问一句即停") {
		t.Fatalf("resume must not re-ask: %s", blob)
	}
	if !strings.Contains(blob, "已有材料") && !strings.Contains(blob, "离线") {
		t.Fatalf("resume task workflow = %s", blob)
	}
}

func TestOfficePipelineSkipsWhenUserOptsOut(t *testing.T) {
	// v2: with the lookupOptedOut lane downgrade gone, a research goal that
	// says 不要联网 stays on its research lane (full budget, full surface),
	// but the web-research pipeline must not auto-start and the reference
	// office instruction takes over. User-explicit action gates behavior,
	// never tools.
	goal := "根据网上公开资料写一份行业报告，不要联网\n- 完成登录页\n- 修复支付超时\n- 下周联调"
	in := LaneInput{Goal: goal}
	lane := classifyChatLane(in)
	if lane != LaneTask {
		t.Fatalf("research goal stays task even when opted out, got %s", lane)
	}
	_, contract := applyLaneOverrides(lane, in, RouteR4, CouncilOverlay{})
	req := llmadapter.Request{Model: "m", Messages: []llmadapter.Message{{Role: llmadapter.RoleUser, Content: goal}}}
	turn := &chatTurnCheckpoint{Goal: goal}
	startOfficeWorkflowsIfNeeded(&req, turn, func(bridge.Event) error { return nil }, contract, "", false)
	if turn.PptActive {
		t.Fatal("report goal must not start the PPT pipeline")
	}
	// The docx workflow activates in write-from-reference mode: the generator
	// stays reachable for the auto-gen fallback, but no web research.
	if !turn.DocxActive {
		t.Fatal("opted-out report turn keeps the docx write-from-reference workflow")
	}
	if turn.DocxStage != docxStageWrite {
		t.Fatalf("opted-out docx turn goes straight to write, got %s", turn.DocxStage)
	}
	foundReference := false
	for _, m := range req.Messages {
		if strings.Contains(m.Content, referenceOfficeInstruction) {
			foundReference = true
		}
		if strings.Contains(m.Content, "两轮 web.search") {
			t.Fatal("opted-out turn must not carry the web-research pipeline prompt")
		}
		if strings.Contains(m.Content, reportPipelineInstruction) {
			t.Fatal("opted-out turn must not carry the report research pipeline prompt")
		}
	}
	if !foundReference {
		t.Fatal("opted-out turn must inject the reference-office instruction")
	}
}

func TestClassifyChatLaneNovelIsTask(t *testing.T) {
	if got := classifyChatLane(LaneInput{Goal: "帮我写小说"}); got != LaneTask {
		t.Fatalf("小说 => %s want task", got)
	}
}

func TestClassifyChatLanePlaySongIsAgent(t *testing.T) {
	if detectTaskRoute("放首歌") != RouteR2 {
		t.Fatalf("放首歌 route=%s want R2", detectTaskRoute("放首歌"))
	}
	if got := classifyChatLane(LaneInput{Goal: "放首歌"}); got != LaneAgent {
		t.Fatalf("放首歌 => %s want agent", got)
	}
}

func TestClassifyChatLaneGenerateSongIsTask(t *testing.T) {
	if got := classifyChatLane(LaneInput{Goal: "帮我生成一首可以听的歌"}); got != LaneTask {
		t.Fatalf("生成歌曲 => %s want task", got)
	}
	lane, contract := applyLaneOverrides(classifyChatLane(LaneInput{Goal: "朗读这段：春风又绿江南岸"}), LaneInput{Goal: "朗读这段：春风又绿江南岸"}, RouteUnspecified, CouncilOverlay{})
	if lane != LaneTask || contract.MaxMainToolSteps < 8 {
		t.Fatalf("朗读 => lane=%s steps=%d", lane, contract.MaxMainToolSteps)
	}
	if companionShouldAutoMediaPlay("帮我生成一首可以听的歌") {
		t.Fatal("generate-listen must not auto-start desktop media.play")
	}
}

func TestHasTurnMaterialsAs上周IsNotMaterial(t *testing.T) {
	if hasTurnMaterials("写周报，如上周一样", false, false) {
		t.Fatal("如上周 must not count as 如上 materials")
	}
	if !hasTurnMaterials("写周报，如上", false, false) {
		t.Fatal("bare 如上 is materials")
	}
}

func TestBundledWorkflowPPTAskOmitsResearch(t *testing.T) {
	// v2: research stays allowed even without materials (G-304) — the clause
	// that must disappear is the nine-step report pipeline, replaced by the
	// verify-first nudge.
	blob := bundledWorkflowInjection("做一份PPT查竞品")
	if strings.Contains(blob, "两轮 web.search") {
		t.Fatal(blob)
	}
	if !strings.Contains(blob, "先核实或问清缺什么") {
		t.Fatal(blob)
	}
}

func TestChatLanesOffRestoresWeeklyResearchClause(t *testing.T) {
	t.Setenv("LUNITIDE_CHAT_LANES", "off")
	blob := bundledWorkflowInjection("写周报")
	if !strings.Contains(blob, "两轮 web.search") {
		t.Fatal("kill switch must restore the old report pipeline clause")
	}
	ai, arch := "01ARZ3NDEKTSV4RRFFQ69G5FAV", "01ARZ3NDEKTSV4RRFFQ69G5FAW"
	if got := selectedTurnExpertIDs([]string{ai, arch}, "润色这段"); len(got) != 0 {
		t.Fatalf("kill switch must restore old council door, got %#v", got)
	}
}

func TestCouncilChairFollowsResolvedLane(t *testing.T) {
	// v2: 写周报 has no materials, so the task chair is the verify-materials
	// chair, never the full-delivery agent chair.
	chair := councilChairInstructionForLane(formatCouncilBrief("写周报", nil), false, LaneTask, "写周报")
	if strings.Contains(chair, "必须把交付做完") {
		t.Fatalf("task chair hijacked: %q", chair)
	}
	if !strings.Contains(chair, "先核实材料") {
		t.Fatalf("task chair = %q", chair)
	}
}

func TestApplyLaneOverridesFlashRouteBecomesAgent(t *testing.T) {
	in := LaneInput{Goal: "帮我处理一下这个"}
	lane := classifyChatLane(in)
	lane, _ = applyLaneOverrides(lane, in, RouteR2, CouncilOverlay{})
	if lane != LaneAgent {
		t.Fatalf("flash R2 => %s want agent", lane)
	}
}

func TestClassifyChatLaneSkillCreateIsTaskWithCreateTools(t *testing.T) {
	goals := []string{
		"创建歌曲技能",
		"帮我创建一个技能，可以生成歌曲",
		"[引用技能 skill-creator|01ARZ3NDEKTSV4RRFFQ69G5FAV]\n保存这个技能",
	}
	defs := []llmadapter.ToolDefinition{
		{Name: "skill.create"}, {Name: "skill.manage"}, {Name: "skill.try"},
		{Name: "skill.invoke"}, {Name: "web.search"}, {Name: "workspace.write"},
	}
	for _, goal := range goals {
		in := LaneInput{Goal: goal}
		lane := classifyChatLane(in)
		if lane != LaneTask {
			t.Fatalf("%q => %s want task", goal, lane)
		}
		lane, contract := applyLaneOverrides(lane, in, RouteUnspecified, CouncilOverlay{})
		if lane != LaneTask || contract.MaxMainToolSteps < 8 {
			t.Fatalf("%q contract = %#v", goal, contract)
		}
		got := applyLaneTools(defs, contract)
		if !hasLaneTool(got, "skill.create") || !hasLaneTool(got, "skill.manage") {
			t.Fatalf("%q stripped create tools: %#v", goal, namesOf(got))
		}
	}
}

func TestClassifyChatLaneVideoURLIsTaskKeepsUnderstand(t *testing.T) {
	goals := []string{
		"https://weixin.qq.com/sph/A1b2C3 帮我解读总结",
		"https://v.douyin.com/ieFxxxx/ 总结这个视频",
		"帮我解析 https://www.bilibili.com/video/BV1xx411c7mD",
	}
	defs := []llmadapter.ToolDefinition{
		{Name: "video.understand"}, {Name: "web.fetch"}, {Name: "web.search"},
		{Name: "skill.invoke"}, {Name: "office.generate"},
	}
	for _, goal := range goals {
		in := LaneInput{Goal: goal}
		lane := classifyChatLane(in)
		if lane != LaneTask {
			t.Fatalf("%q => %s want task", goal, lane)
		}
		lane, contract := applyLaneOverrides(lane, in, detectTaskRoute(goal), CouncilOverlay{})
		if lane == LaneChat {
			t.Fatalf("%q stayed chat: %s", goal, lane)
		}
		if contract.MaxMainToolSteps < 8 {
			t.Fatalf("%q must keep an understand budget: %#v", goal, contract)
		}
		got := applyLaneTools(defs, contract)
		if !hasLaneTool(got, "video.understand") || !hasLaneTool(got, "web.fetch") {
			t.Fatalf("%q stripped video tools: %#v", goal, namesOf(got))
		}
	}
}

func TestFinishLeftoverStaysOnTheFullToolSurface(t *testing.T) {
	for _, goal := range []string{"把拒绝的，前面没做完的做完", "好继续", "继续"} {
		if got := classifyChatLane(LaneInput{Goal: goal}); got != LaneAgent {
			t.Fatalf("%q => %s want agent", goal, got)
		}
	}
	if got := classifyChatLane(LaneInput{Goal: "写一篇关于没做完的文章"}); got == LaneAgent {
		t.Fatal("an essay that mentions unfinished work must stay off the operate lane")
	}
	if !looksLikeResume("好继续") {
		t.Fatal("好继续 must resume the unfinished task")
	}
	if got := classifyChatLane(LaneInput{Goal: "好，继续"}); got != LaneAgent {
		t.Fatalf("spoken continue => %s want agent", got)
	}
	prior := "试用技能：POC 快速构建，把页面交互验证和 README 做完"
	if got := resolveLaneGoal("好继续", prior); got != prior {
		t.Fatalf("resume goal=%q", got)
	}
	defs := []llmadapter.ToolDefinition{{Name: "workspace.read"}, {Name: "workspace.write"}, {Name: "command.run"}, {Name: "computer.act"}}
	contract := buildLaneContract(LaneAgent, RouteUnspecified, CouncilOverlay{})
	if contract.MaxMainToolSteps < 24 || !contract.ContinueNudges {
		t.Fatalf("contract=%#v", contract)
	}
	got := applyLaneTools(defs, contract)
	if !hasLaneTool(got, "workspace.write") || !hasLaneTool(got, "command.run") || !hasLaneTool(got, "computer.act") {
		t.Fatalf("full surface missing tools: %#v", got)
	}
}

func TestMediaCenterPlayUsesTheFullToolSurface(t *testing.T) {
	for _, goal := range []string{
		"帮我找一部好看点的电影在媒体中心比方出来",
		"在媒体中心播放《大都会》",
	} {
		if got := classifyChatLane(LaneInput{Goal: goal}); got != LaneAgent {
			t.Fatalf("%q => %s want agent", goal, got)
		}
	}
	if got := classifyChatLane(LaneInput{Goal: "写一篇关于媒体中心的文章"}); got == LaneAgent {
		t.Fatal("an essay about the media center must stay off the operate lane")
	}
	contract := buildLaneContract(LaneAgent, RouteUnspecified, CouncilOverlay{})
	defs := []llmadapter.ToolDefinition{{Name: "skill.invoke"}, {Name: "media.play"}, {Name: "workspace.read"}}
	got := applyLaneTools(defs, contract)
	if !hasLaneTool(got, "media.play") || contract.MaxMainToolSteps < 24 {
		t.Fatalf("media.play missing or steps=%d tools=%#v", contract.MaxMainToolSteps, got)
	}
}
