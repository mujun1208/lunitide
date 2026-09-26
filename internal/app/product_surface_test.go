package app

import (
	"strings"
	"testing"
)

func TestProductSurfaceKeepsSystemLocationAndCanvas(t *testing.T) {
	cases := []struct {
		goal string
		want string
		cc   bool
	}{
		{goal: "这里的天气", want: "location.get", cc: false},
		{goal: "帮我点这个按钮", want: "system.run", cc: true},
		{goal: "写一份对比报告", want: "canvas.present", cc: false},
		{goal: "用画布展示这个对比", want: "canvas.present", cc: false},
		{goal: "写方案", want: "canvas.present", cc: false},
		{goal: "写文档", want: "canvas.present", cc: false},
		{goal: "写PRD", want: "canvas.present", cc: false},
	}
	for _, tc := range cases {
		names := productToolNames(tc.goal, tc.cc)
		if !names[tc.want] {
			t.Fatalf("%s missing %s; tools=%v", tc.goal, tc.want, names)
		}
		text := bundledWorkflowInjection(tc.goal)
		switch tc.want {
		case "location.get":
			if !strings.Contains(text, "location.get") {
				t.Fatalf("weather workflow: %s", text)
			}
		case "system.run":
			if !strings.Contains(text, "system.run") {
				t.Fatalf("computer workflow: %s", text)
			}
		case "canvas.present":
			if !strings.Contains(text, "canvas.present") {
				t.Fatalf("canvas workflow: %s", text)
			}
		}
	}
	if !chatDeliverableArtifact("canvas.present", "html", "canvas.html") {
		t.Fatal("canvas document is not kept as a chat deliverable")
	}
}

func productToolNames(goal string, cc bool) map[string]bool {
	e := &Engine{}
	catalog := e.engineToolDefinitionsFor(executionModeFullAccess)
	route, allow := classifyTaskRoute(goal, false, cc)
	defs := applyTaskRoute(catalog, route, allow)
	_, contract := applyLaneOverrides(classifyChatLane(LaneInput{Goal: goal}), LaneInput{Goal: goal}, route, CouncilOverlay{})
	defs = restoreFileLandingTools(applyLaneTools(defs, contract), catalog, goal, false, contract.Lane)
	out := map[string]bool{}
	for _, d := range defs {
		out[d.Name] = true
	}
	return out
}
