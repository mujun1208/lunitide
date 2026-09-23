package app

import (
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/llmadapter"
)

func TestClassifyChatLaneT01GreetingIsL0(t *testing.T) {
	if got := classifyChatLane(LaneInput{Goal: "你好"}); got != LaneL0 {
		t.Fatalf("你好 => %s want L0", got)
	}
}

func TestClassifyChatLaneT05ArticleWithoutFileIsL1(t *testing.T) {
	if got := classifyChatLane(LaneInput{Goal: "写一篇文章介绍光合作用"}); got != LaneL1 {
		t.Fatalf("got %s want L1", got)
	}
	if got := classifyChatLane(LaneInput{Goal: "把这段话润色得更顺：今天开会很顺利"}); got != LaneL1 {
		t.Fatalf("润色 => %s want L1", got)
	}
}

func TestClassifyChatLaneT03WeeklyReportNoMaterialIsL2Ask(t *testing.T) {
	if detectTaskRoute("写周报") != RouteR4 {
		t.Fatalf("写周报 route=%s want R4", detectTaskRoute("写周报"))
	}
	if got := classifyChatLane(LaneInput{Goal: "写周报"}); got != LaneL2Ask {
		t.Fatalf("写周报 => %s want L2-ask", got)
	}
}

func TestClassifyChatLaneT04WeeklyReportWithPointsIsL2(t *testing.T) {
	goal := "写周报\n- 完成登录页\n- 修复支付超时\n- 下周联调"
	if !hasTurnMaterials(goal, false, false) {
		t.Fatal("three bullets must count as materials")
	}
	if got := classifyChatLane(LaneInput{Goal: goal}); got != LaneL2 {
		t.Fatalf("got %s want L2 (infer bullets without HasTurnMaterials flag)", got)
	}
}

func TestClassifyChatLaneT06OnlineIndustryReportIsL3(t *testing.T) {
	if got := classifyChatLane(LaneInput{Goal: "根据网上公开资料写一份行业报告"}); got != LaneL3 {
		t.Fatalf("got %s want L3", got)
	}
}

func TestClassifyChatLaneOpenWordWeeklyIsL4(t *testing.T) {
	if got := classifyChatLane(LaneInput{Goal: "打开 Word 写周报"}); got != LaneL4 {
		t.Fatalf("got %s want L4", got)
	}
}

func TestClassifyChatLaneDesktopFolderIsL4(t *testing.T) {
	goal := "帮我在桌面创建一个文件夹，名字叫可可"
	if got := classifyChatLane(LaneInput{Goal: goal}); got != LaneL4 {
		t.Fatalf("got %s want L4", got)
	}
	_, contract := applyLaneOverrides(LaneL4, LaneInput{Goal: goal}, RouteUnspecified, CouncilOverlay{})
	if contract.DisableReasoning {
		t.Fatal("desktop mkdir must keep thinking/tools, not L1 DisableReasoning")
	}
}

func TestClassifyChatLaneHostActsAreL4(t *testing.T) {
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
		if got != LaneL4 {
			t.Errorf("%q => %s want L4", goal, got)
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
			t.Errorf("%q L4 must keep command.run, kept=%v", goal, kept)
		}
	}
	if got := classifyChatLane(LaneInput{Goal: "写一篇文章介绍光合作用"}); got != LaneL1 {
		t.Fatalf("photosynthesis article => %s want L1", got)
	}
	if got := classifyChatLane(LaneInput{Goal: "写一篇关于创建文件夹的文章"}); got != LaneL1 {
		t.Fatalf("article about folders => %s want L1", got)
	}
	if got := classifyChatLane(LaneInput{Goal: "写周报放到桌面"}); got == LaneL4 {
		t.Fatal("写周报放到桌面 must stay office, not a desktop folder act")
	}
	if got := classifyChatLane(LaneInput{Goal: "生成 Word 周报，放到桌面。"}); got == LaneL4 {
		t.Fatal("生成周报到桌面 must stay office")
	}
	if got := classifyChatLane(LaneInput{Goal: "生成周报保存到桌面"}); got == LaneL4 {
		t.Fatal("保存到桌面 on a weekly report is still office.generate")
	}
	if got := classifyChatLane(LaneInput{Goal: "帮我删掉桌面上的周报.docx"}); got != LaneL4 {
		t.Fatalf("delete weekly-report file on desktop => %s want L4", got)
	}
}

func TestClassifyChatLaneVagueTaskIsL2Ask(t *testing.T) {
	if got := classifyChatLane(LaneInput{Goal: "帮我做个任务"}); got != LaneL2Ask {
		t.Fatalf("got %s want L2-ask", got)
	}
}

func TestClassifyChatLaneT18ResumeUsesPriorGoal(t *testing.T) {
	prior := "写周报\n- 完成登录页\n- 修复支付超时\n- 下周联调"
	if got := classifyChatLane(LaneInput{Goal: prior, HasTurnMaterials: true}); got != LaneL2 {
		t.Fatalf("prior goal => %s", got)
	}
}

func TestApplyLaneOverridesT20NoWebStaysL2Ask(t *testing.T) {
	in := LaneInput{Goal: "写周报，不要联网"}
	lane := classifyChatLane(in)
	lane, contract := applyLaneOverrides(lane, in, RouteR4, CouncilOverlay{})
	if lane != LaneL2Ask {
		t.Fatalf("lane=%s want L2-ask", lane)
	}
	if contract.AllowWebSearch {
		t.Fatal("不要联网 must drop search")
	}
}

func TestApplyLaneToolsT17StripsSearchOnWeeklyReport(t *testing.T) {
	defs := []llmadapter.ToolDefinition{
		{Name: "web.search"}, {Name: "web.fetch"}, {Name: "docx.gen"}, {Name: "user.ask"}, {Name: "todo.write"}, {Name: "workspace.read"},
	}
	ask := applyLaneTools(defs, buildLaneContract(LaneL2Ask, RouteR4, CouncilOverlay{}))
	if hasLaneTool(ask, "web.search") || hasLaneTool(ask, "web.fetch") || hasLaneTool(ask, "docx.gen") {
		t.Fatalf("L2-ask kept scrape/gen: %#v", namesOf(ask))
	}
	if !hasLaneTool(ask, "user.ask") || !hasLaneTool(ask, "todo.write") {
		t.Fatal("L2-ask dropped the typed decision card or checklist")
	}
	l2 := applyLaneTools(defs, buildLaneContract(LaneL2, RouteR4, CouncilOverlay{}))
	if hasLaneTool(l2, "web.search") || hasLaneTool(l2, "web.fetch") {
		t.Fatalf("L2 kept search: %#v", namesOf(l2))
	}
	if !hasLaneTool(l2, "docx.gen") || !hasLaneTool(l2, "user.ask") {
		t.Fatalf("L2 dropped gen: %#v", namesOf(l2))
	}
}

func TestApplyLaneToolsKeepsSpecialistGensWhenFlagged(t *testing.T) {
	defs := []llmadapter.ToolDefinition{
		{Name: "pptx.gen"}, {Name: "docx.gen"}, {Name: "excel.gen"},
		{Name: "web.search"}, {Name: "workspace.write"}, {Name: "user.ask"}, {Name: "skill.invoke"},
	}
	plain := applyLaneTools(defs, buildLaneContract(LaneL2Ask, RouteR4, CouncilOverlay{}))
	if hasLaneTool(plain, "pptx.gen") || hasLaneTool(plain, "docx.gen") {
		t.Fatalf("plain L2-ask must still strip gens: %#v", namesOf(plain))
	}
	keep := buildLaneContract(LaneL2Ask, RouteR4, CouncilOverlay{})
	keep.KeepSpecialistTools = true
	got := applyLaneTools(defs, keep)
	for _, name := range []string{"pptx.gen", "docx.gen", "excel.gen", "web.search", "workspace.write", "user.ask", "skill.invoke"} {
		if !hasLaneTool(got, name) {
			t.Fatalf("specialist L2-ask dropped %s: %#v", name, namesOf(got))
		}
	}
	talk := buildLaneContract(LaneL1, RouteR1, CouncilOverlay{})
	talk.KeepSpecialistTools = true
	talkTools := applyLaneTools(defs, talk)
	if !hasLaneTool(talkTools, "workspace.write") || !hasLaneTool(talkTools, "pptx.gen") {
		t.Fatalf("specialist L1 dropped compose tools: %#v", namesOf(talkTools))
	}
}

func TestApplyLaneToolsKeepsSkillInvokeOnAskAndTalk(t *testing.T) {
	defs := []llmadapter.ToolDefinition{
		{Name: "skill.invoke"}, {Name: "web.search"}, {Name: "user.ask"},
	}
	ask := applyLaneTools(defs, buildLaneContract(LaneL2Ask, RouteR4, CouncilOverlay{}))
	if !hasLaneTool(ask, "skill.invoke") {
		t.Fatalf("L2-ask dropped skill.invoke: %#v", namesOf(ask))
	}
	if hasLaneTool(ask, "web.search") {
		t.Fatalf("L2-ask kept search: %#v", namesOf(ask))
	}
	talk := applyLaneTools(defs, buildLaneContract(LaneL1, RouteR1, CouncilOverlay{}))
	if !hasLaneTool(talk, "skill.invoke") {
		t.Fatalf("L1 dropped skill.invoke: %#v", namesOf(talk))
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

func TestShouldStartOfficeResearchSkipsL2(t *testing.T) {
	c := buildLaneContract(LaneL2Ask, RouteR4, CouncilOverlay{})
	if shouldStartOfficeResearch(c, "", false) {
		t.Fatal("L2-ask must skip report pipeline")
	}
	c = buildLaneContract(LaneL2, RouteR4, CouncilOverlay{})
	if shouldStartOfficeResearch(c, "", false) {
		t.Fatal("L2 must skip research pipeline")
	}
	c = buildLaneContract(LaneL3, RouteR4, CouncilOverlay{})
	if !shouldStartOfficeResearch(c, "", false) {
		t.Fatal("L3 keeps the research pipeline")
	}
}

func TestBundledWorkflowWeeklyReportOmitsResearch(t *testing.T) {
	blob := bundledWorkflowInjection("写周报")
	if strings.Contains(blob, "普通资料用 web.search") || strings.Contains(blob, "两轮 web.search") {
		t.Fatal(blob)
	}
	if !strings.Contains(blob, "问缺什么") && !strings.Contains(blob, "离线") && !strings.Contains(blob, "已有材料") {
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

func TestClassifyChatLaneOfficeTaskDoesNotForceL2(t *testing.T) {
	if got := classifyChatLane(LaneInput{Goal: "写周报", OfficeTaskID: "task-1"}); got != LaneL2Ask {
		t.Fatalf("empty office 写周报 => %s want L2-ask", got)
	}
	ask := applyLaneTools([]llmadapter.ToolDefinition{{Name: "office.generate"}, {Name: "web.search"}, {Name: "docx.gen"}, {Name: "user.ask"}}, buildLaneContract(LaneL2Ask, RouteR4, CouncilOverlay{}))
	if hasLaneTool(ask, "web.search") || hasLaneTool(ask, "docx.gen") {
		t.Fatalf("L2-ask kept scrape/docx: %#v", namesOf(ask))
	}
	if !hasLaneTool(ask, "office.generate") || !hasLaneTool(ask, "user.ask") {
		t.Fatalf("office studio must keep office.generate: %#v", namesOf(ask))
	}
}

func TestClassifyChatLaneWeatherLookupKeepsSearch(t *testing.T) {
	goal := "今天合肥的天气怎么样"
	if detectTaskRoute(goal) != RouteR1 {
		t.Fatalf("weather route=%s want R1", detectTaskRoute(goal))
	}
	in := LaneInput{Goal: goal}
	lane := classifyChatLane(in)
	if lane == LaneL3 {
		t.Fatal("weather must not be promoted to research-report L3")
	}
	_, contract := applyLaneOverrides(lane, in, RouteR1, CouncilOverlay{})
	if !contract.AllowWebSearch || contract.MaxMainToolSteps < 2 {
		t.Fatalf("weather must open search with room for a result step: %#v", contract)
	}
	defs := applyLaneTools([]llmadapter.ToolDefinition{{Name: "web.search"}, {Name: "weather.get"}, {Name: "workspace.read"}}, contract)
	if !hasLaneTool(defs, "web.search") && !hasLaneTool(defs, "weather.get") {
		t.Fatalf("weather tools stripped: %#v", namesOf(defs))
	}
}

func TestClassifyChatLaneNewsKeepsSearchWithoutReportPipeline(t *testing.T) {
	in := LaneInput{Goal: "今天有什么新闻"}
	lane := classifyChatLane(in)
	if lane != LaneL1 {
		t.Fatalf("新闻 => %s want L1", lane)
	}
	_, contract := applyLaneOverrides(lane, in, RouteR1, CouncilOverlay{})
	if !contract.AllowWebSearch || !contract.SkipOfficeResearchPipeline {
		t.Fatalf("news lookup must search without the report pipeline: %#v", contract)
	}
}

func TestStartOfficeWorkflowsSkipsResearchOnL2(t *testing.T) {
	req := llmadapter.Request{Model: "m", Messages: []llmadapter.Message{{Role: llmadapter.RoleUser, Content: "写周报\n- 完成登录页\n- 修复支付超时\n- 下周联调"}}}
	turn := &chatTurnCheckpoint{Goal: "写周报\n- 完成登录页\n- 修复支付超时\n- 下周联调"}
	c := buildLaneContract(LaneL2, RouteR4, CouncilOverlay{})
	startOfficeWorkflowsIfNeeded(&req, turn, func(bridge.Event) error { return nil }, c, "", false)
	if turn.DocxActive || turn.PptActive {
		t.Fatal("L2 must not start the research pipeline")
	}
	if !strings.Contains(req.Messages[len(req.Messages)-1].Content, "已有材料") &&
		!strings.Contains(req.Messages[len(req.Messages)-1].Content, "离线") {
		t.Fatalf("L2 should inject offline office instruction: %#v", req.Messages)
	}
	for _, m := range req.Messages {
		if strings.Contains(m.Content, "报告流水线") || strings.Contains(m.Content, "两轮 web.search") {
			t.Fatal("L2 injected research pipeline")
		}
	}
}

func TestStartOfficeWorkflowsKeepsL3Pipeline(t *testing.T) {
	req := llmadapter.Request{Model: "m", Messages: []llmadapter.Message{{Role: llmadapter.RoleUser, Content: "根据网上公开资料写一份行业报告"}}}
	turn := &chatTurnCheckpoint{Goal: "根据网上公开资料写一份行业报告"}
	c := buildLaneContract(LaneL3, RouteR4, CouncilOverlay{})
	startOfficeWorkflowsIfNeeded(&req, turn, func(bridge.Event) error { return nil }, c, "", false)
	if !turn.DocxActive {
		t.Fatal("L3 must still start the report pipeline")
	}
}

func TestDocxGenBlockedSkipPipelineT16(t *testing.T) {
	turn := &chatTurnCheckpoint{DocxActive: true, DocxKind: docxKindReport, SkipOfficeResearch: true}
	if blocked, _ := docxGenBlocked(turn, "docx.gen"); blocked {
		t.Fatal("SkipOfficeResearch must let L2 call docx.gen")
	}
}

func TestLaneAllowsWebSearchT17(t *testing.T) {
	if laneAllowsWebSearch(buildLaneContract(LaneL2Ask, RouteR4, CouncilOverlay{})) {
		t.Fatal("L2-ask must not auto web.search")
	}
	if laneAllowsWebSearch(buildLaneContract(LaneL2, RouteR4, CouncilOverlay{})) {
		t.Fatal("L2 must not auto web.search")
	}
	if !laneAllowsWebSearch(buildLaneContract(LaneL3, RouteR4, CouncilOverlay{})) {
		t.Fatal("L3 keeps auto search")
	}
}

func TestCapToolLoopLimitFollowsLane(t *testing.T) {
	if got := capToolLoopLimit(24, buildLaneContract(LaneL2Ask, RouteR4, CouncilOverlay{})); got != 1 {
		t.Fatalf("L2-ask cap=%d want 1", got)
	}
	if got := capToolLoopLimit(24, buildLaneContract(LaneL2, RouteR4, CouncilOverlay{})); got != 8 {
		t.Fatalf("L2 cap=%d want 8", got)
	}
	if laneMayExtendToolLoop(buildLaneContract(LaneL1, RouteUnspecified, CouncilOverlay{})) {
		t.Fatal("L1 must not extend the tool loop")
	}
	if !laneMayExtendToolLoop(buildLaneContract(LaneL4, RouteR2, CouncilOverlay{})) {
		t.Fatal("L4 may extend")
	}
}

func TestBundledWorkflowWeeklyReportStopsAfterAsk(t *testing.T) {
	blob := bundledWorkflowInjection("写周报")
	if strings.Contains(blob, "不要在勘查后停下等待确认") {
		t.Fatal("L2-ask must not inherit execute-until-done discipline")
	}
	if !strings.Contains(blob, "问缺什么") && !strings.Contains(blob, "问完即停") {
		t.Fatal(blob)
	}
}

func TestResolveLaneGoalResumeUsesCheckpoint(t *testing.T) {
	prior := "写周报\n- 完成登录页\n- 修复支付超时\n- 下周联调"
	if got := resolveLaneGoal("继续", prior); got != prior {
		t.Fatalf("resume goal=%q", got)
	}
	goal := resolveLaneGoal("继续", prior)
	if classifyChatLane(LaneInput{Goal: goal}) != LaneL2 {
		t.Fatal("T18: 继续 + prior weekly points must be L2")
	}
	blob := bundledWorkflowInjectionForLane(goal, LaneL2)
	if strings.Contains(blob, "问缺什么") || strings.Contains(blob, "问完即停") {
		t.Fatalf("resume must not re-ask: %s", blob)
	}
	if !strings.Contains(blob, "已有材料") && !strings.Contains(blob, "离线") {
		t.Fatalf("resume L2 workflow = %s", blob)
	}
}

func TestApplyLaneOverridesOptOutWithMaterialsIsL2(t *testing.T) {
	goal := "根据网上公开资料写一份行业报告，不要联网\n- 完成登录页\n- 修复支付超时\n- 下周联调"
	in := LaneInput{Goal: goal}
	lane := classifyChatLane(in)
	lane, contract := applyLaneOverrides(lane, in, RouteR4, CouncilOverlay{})
	if lane != LaneL2 {
		t.Fatalf("opt-out + materials => %s want L2", lane)
	}
	if contract.AllowWebSearch || !contract.AllowOfficeGen {
		t.Fatalf("must drop search and keep gen: %#v", contract)
	}
}

func TestClassifyChatLaneNovelIsL3(t *testing.T) {
	if got := classifyChatLane(LaneInput{Goal: "帮我写小说"}); got != LaneL3 {
		t.Fatalf("小说 => %s want L3", got)
	}
}

func TestClassifyChatLanePlaySongIsL4(t *testing.T) {
	if detectTaskRoute("放首歌") != RouteR2 {
		t.Fatalf("放首歌 route=%s want R2", detectTaskRoute("放首歌"))
	}
	if got := classifyChatLane(LaneInput{Goal: "放首歌"}); got != LaneL4 {
		t.Fatalf("放首歌 => %s want L4", got)
	}
}

func TestClassifyChatLaneGenerateSongIsL3(t *testing.T) {
	if got := classifyChatLane(LaneInput{Goal: "帮我生成一首可以听的歌"}); got != LaneL3 {
		t.Fatalf("生成歌曲 => %s want L3", got)
	}
	lane, contract := applyLaneOverrides(LaneL1, LaneInput{Goal: "朗读这段：春风又绿江南岸"}, RouteUnspecified, CouncilOverlay{})
	if lane != LaneL3 || contract.MaxMainToolSteps < 8 {
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
	blob := bundledWorkflowInjection("做一份PPT查竞品")
	if strings.Contains(blob, "普通资料用 web.search") || strings.Contains(blob, "两轮 web.search") {
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
	chair := councilChairInstructionForLane(formatCouncilBrief("写周报", nil), false, LaneL2)
	if strings.Contains(chair, "只列出还缺什么") || strings.Contains(chair, "必须把交付做完") {
		t.Fatalf("L2 chair hijacked: %q", chair)
	}
	if !strings.Contains(chair, "已有材料") && !strings.Contains(chair, "禁止 web.search") {
		t.Fatalf("L2 chair = %q", chair)
	}
}

func TestApplyLaneOverridesFlashRouteBecomesL4(t *testing.T) {
	in := LaneInput{Goal: "帮我处理一下这个"}
	lane := classifyChatLane(in)
	lane, _ = applyLaneOverrides(lane, in, RouteR2, CouncilOverlay{})
	if lane != LaneL4 {
		t.Fatalf("flash R2 => %s want L4", lane)
	}
}

func TestClassifyChatLaneSkillCreateIsL3WithCreateTools(t *testing.T) {
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
		if lane != LaneL3 {
			t.Fatalf("%q => %s want L3", goal, lane)
		}
		lane, contract := applyLaneOverrides(lane, in, RouteUnspecified, CouncilOverlay{})
		if lane != LaneL3 || contract.MaxMainToolSteps < 8 {
			t.Fatalf("%q contract = %#v", goal, contract)
		}
		got := applyLaneTools(defs, contract)
		if !hasLaneTool(got, "skill.create") || !hasLaneTool(got, "skill.manage") {
			t.Fatalf("%q stripped create tools: %#v", goal, namesOf(got))
		}
	}
}

func TestClassifyChatLaneVideoURLIsL3KeepsUnderstand(t *testing.T) {
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
		if lane != LaneL3 {
			t.Fatalf("%q => %s want L3", goal, lane)
		}
		lane, contract := applyLaneOverrides(lane, in, detectTaskRoute(goal), CouncilOverlay{})
		if lane == LaneL1 || lane == LaneL2Ask {
			t.Fatalf("%q stayed ask/talk: %s", goal, lane)
		}
		if !contract.AllowWebSearch {
			t.Fatalf("%q must keep search: %#v", goal, contract)
		}
		got := applyLaneTools(defs, contract)
		if !hasLaneTool(got, "video.understand") || !hasLaneTool(got, "web.fetch") {
			t.Fatalf("%q stripped video tools: %#v", goal, namesOf(got))
		}
	}
}
