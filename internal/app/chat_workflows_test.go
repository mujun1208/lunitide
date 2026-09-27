package app

import (
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/llmadapter"
)

func TestCanvasAndHereWeatherAreNamedInTheWorkflow(t *testing.T) {
	canvas := strings.Join(selectWorkflowClauses("写一份对比报告", ""), "\n")
	if !strings.Contains(canvas, "canvas.present") {
		t.Fatal(canvas)
	}
	weather := strings.Join(selectWorkflowClauses("这里的天气", ""), "\n")
	if !strings.Contains(weather, "location.get") || !strings.Contains(weather, "weather.get") {
		t.Fatal(weather)
	}
	computer := strings.Join(selectWorkflowClauses("帮我点这个按钮", ""), "\n")
	if !strings.Contains(computer, "system.run") {
		t.Fatal(computer)
	}
	for _, goal := range []string{"写方案", "写文档", "写PRD"} {
		got := strings.Join(selectWorkflowClauses(goal, ""), "\n")
		if !strings.Contains(got, "canvas.present") {
			t.Fatalf("%s did not ask for the canvas: %s", goal, got)
		}
	}
	word := strings.Join(selectWorkflowClauses("写一份PRD Word", ""), "\n")
	if strings.Contains(word, "canvas.present") {
		t.Fatalf("a named Word document must stay off the canvas: %s", word)
	}
}

func TestZodiacReportChainShape(t *testing.T) {
	goal := "帮我写一个关于，12星座，不同星座和不同星座的爱情，哪里匹配，哪里合适，哪里不匹配，不合适，相互之间在一起能得多少分的，长篇分析报告论文。"
	if !wantsDefaultCanvas(goal) {
		t.Fatal("zodiac report left the canvas")
	}
	names := productToolNames(goal, false)
	var tools []string
	for name := range names {
		tools = append(tools, name)
	}
	flow := strings.Join(selectWorkflowClauses(goal, classifyChatLane(LaneInput{Goal: goal})), "\n")
	t.Logf("lane=%s", classifyChatLane(LaneInput{Goal: goal}))
	t.Logf("tools=%v", tools)
	t.Logf("workflow=%s", flow)
	t.Logf("assist=%s", typedAssistInstruction())
	if names["web.search"] || names["docx.gen"] || names["todo.write"] || names["workspace.write"] || !names["canvas.present"] || !names["user.ask"] {
		t.Fatalf("tools=%v", names)
	}
	if strings.Contains(flow, "web.search") || strings.Contains(flow, "docx.gen") {
		t.Fatalf("workflow=%s", flow)
	}
}

func TestUntypedPaperGoesToTheCanvas(t *testing.T) {
	paper := "帮我写一个时间空间论证，人可以穿越时空的，长篇分析论文"
	if !wantsDefaultCanvas(paper) || docxKindFromRequest(llmadapter.Request{}, paper) != "" || officeGenToolForGoal(paper) != "" {
		t.Fatal("an untyped paper must stay on the canvas")
	}
	got := strings.Join(selectWorkflowClauses(paper, ""), "\n")
	if !strings.Contains(got, "canvas.present") {
		t.Fatalf("canvas clause missing: %s", got)
	}
	names := productToolNames(paper, false)
	if !names["canvas.present"] || names["docx.gen"] || names["pptx.gen"] {
		t.Fatalf("tools=%v", names)
	}
	word := "把这篇时间空间论证论文写成 Word"
	if wantsDefaultCanvas(word) || docxKindFromRequest(llmadapter.Request{}, word) == "" {
		t.Fatal("a named Word paper stays a document")
	}
}

func TestOwnedCenterPlaybackIntentRoutesPlaybackRequests(t *testing.T) {
	// 打字对话里说播电影/放首歌/想听：默认进自带媒体中心（target=center），
	// 不会再走去控制本机播放器或打开网易云网页。
	for _, text := range []string{
		"播放电影九品芝麻官",
		"我要看《九品芝麻官》",
		"帮我播放一首歌曲 一生所爱",
		"我想听一生所爱",
		"随便放一部周星驰的电影",
		"播放大话西游的主题曲",
		"放首歌",
	} {
		got := strings.Join(selectWorkflowClauses(text, ""), "\n")
		if !strings.Contains(got, "target=center") || !strings.Contains(got, "只调用一次 media.play") {
			t.Fatalf("%q must route to the owned media center: %s", text, got)
		}
	}
	// 点名本机播放软件或只做播放控制：继续走本机播放器那条路。
	for _, text := range []string{
		"用网易云音乐播放周杰伦",
		"暂停",
		"下一首",
		"打开汽水音乐",
	} {
		got := strings.Join(selectWorkflowClauses(text, ""), "\n")
		if strings.Contains(got, "target=center") {
			t.Fatalf("%q must stay on the local player path: %s", text, got)
		}
	}
}

func TestProjectPhaseWorkflowInjectionDev(t *testing.T) {
	hint := projectPhaseWorkflowInjection(5, "开发")
	if hint == "" {
		t.Fatal("expected dev hint")
	}
	for _, skill := range []string{"implement", "tdd-loop", "code-reviewer", "pm-phase-5"} {
		if !strings.Contains(hint, skill) {
			t.Fatalf("hint missing %s: %s", skill, hint)
		}
	}
}

func TestProjectPhaseWorkflowInjectionOpsDev(t *testing.T) {
	hint := projectPhaseWorkflowInjection(4, "开发")
	if !strings.Contains(hint, "pm-phase-4") {
		t.Fatalf("expected ops dev pm-phase-4, got %q", hint)
	}
}

func TestProjectPhaseWorkflowInjectionAsksDecisions(t *testing.T) {
	hint := projectPhaseWorkflowInjection(1, "需求架构规范")
	if !strings.Contains(hint, "user.ask") || !strings.Contains(hint, "能自行决定的不要弹卡") {
		t.Fatalf("spec phase must keep Claude-style ask-only-if-needed, got %q", hint)
	}
	if strings.Contains(hint, "拍板必须调用") {
		t.Fatal("spec phase must not force a decision card")
	}
	ops := projectPhaseWorkflowInjection(8, "运维")
	if !strings.Contains(ops, "user.ask") || !strings.Contains(ops, "先自行判断") {
		t.Fatalf("default phase must keep ask-only-if-needed, got %q", ops)
	}
	voice := projectPhaseWorkflowInjectionMode(1, "需求架构规范", false)
	if strings.Contains(voice, "user.ask") || strings.Contains(voice, "决策卡") {
		t.Fatalf("voice phase must not open a decision card: %q", voice)
	}
	if !strings.Contains(voice, "grill-me") {
		t.Fatalf("voice phase still needs its skills: %q", voice)
	}
}
