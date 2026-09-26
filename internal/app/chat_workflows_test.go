package app

import (
	"strings"
	"testing"
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
