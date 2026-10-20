package app

import (
	"sort"
	"strings"
	"testing"
	"time"
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
	// v2 FR-100: the starting surface is the profile full set for every goal;
	// the route/lane may only shift prompts and budget.
	e := &Engine{}
	catalog := e.engineToolDefinitionsFor(executionModeFullAccess)
	if cc {
		catalog = append(catalog, e.ccToolDefinitions()...)
	}
	route, _ := classifyTaskRoute(goal, false, cc)
	_, contract := applyLaneOverrides(classifyChatLane(LaneInput{Goal: goal}), LaneInput{Goal: goal}, route, CouncilOverlay{})
	defs := applyLaneTools(catalog, contract)
	out := map[string]bool{}
	for _, d := range defs {
		out[d.Name] = true
	}
	return out
}

// v2 FR-100 replaces the v1.4 "ordinary tasks skip the tool pile" contract:
// every task — weather, playback, a click, a page edit, a weekly report, a
// greeting — sees the same profile full surface. Misrouting may now only cost
// budget, never capability. Prompt-level narrowing still applies.
func TestEveryTaskSeesTheProfileFullSurface(t *testing.T) {
	pageEdit := "帮我新增一个商机\n\n" + openPageFileMark + "E:/poc/index.html\n" + openPageFileInstruction
	cases := []struct {
		goal string
		cc   bool
		must []string
	}{
		{
			goal: "帮我写一个关于，12星座，不同星座和不同星座的爱情，哪里匹配，哪里合适，哪里不匹配，不合适，相互之间在一起能得多少分的，长篇分析报告论文。",
			must: []string{"canvas.present", "user.ask", "web.search", "docx.gen"},
		},
		{
			goal: "这里的天气",
			must: []string{"location.get", "weather.get", "user.ask", "web.search"},
		},
		{
			goal: "帮我点这个按钮",
			cc:   true,
			must: []string{"system.run", "user.ask", "web.search"},
		},
		{
			goal: "在媒体中心播放《大都会》",
			must: []string{"media.play", "user.ask", "web.search"},
		},
		{
			goal: "把这篇时间空间论证论文写成 Word",
			must: []string{"docx.gen", "user.ask", "web.search"},
		},
		{
			goal: "写周报",
			must: []string{"workspace.write", "docx.gen", "user.ask", "web.search"},
		},
		{
			goal: pageEdit,
			must: []string{"workspace.read", "workspace.edit", "user.ask", "web.search"},
		},
		{
			goal: "把这段话润色得更顺：今天开会很顺利",
			must: []string{"user.ask", "docx.gen", "web.search", "workspace.write"},
		},
		{
			goal: "修一下支付超时的 bug，然后跑测试",
			must: []string{"workspace.edit", "command.run", "todo.write", "user.ask", "web.search"},
		},
		{
			goal: "根据网上公开资料写一份行业报告",
			must: []string{"web.search", "docx.gen", "user.ask"},
		},
		{
			goal: "打开记事本",
			must: []string{"desktop.open", "user.ask", "web.search"},
		},
		{
			goal: "查明天上海到北京火车票",
			must: []string{"web.search", "user.ask", "todo.write"},
		},
		{
			goal: "生成一张图片",
			must: []string{"image.generate", "user.ask", "web.search"},
		},
		{
			goal: "你好",
			must: []string{"web.search", "docx.gen", "canvas.present", "media.play", "todo.write"},
		},
		{
			goal: "用浏览器打开 https://example.com",
			must: []string{"browser.act", "user.ask", "web.search"},
		},
		{
			goal: "我要看《九品芝麻官》",
			must: []string{"media.play", "user.ask", "web.search"},
		},
		{
			goal: "用网易云音乐播放周杰伦",
			must: []string{"desktop.open", "user.ask", "web.search"},
		},
	}
	var baseline map[string]bool
	for _, tc := range cases {
		names := productToolNames(tc.goal, tc.cc)
		got := sortedToolNames(names)
		for _, name := range tc.must {
			if !names[name] {
				t.Errorf("%q missing %s; tools=%v", tc.goal, name, got)
			}
		}
		if baseline == nil {
			baseline = names
			continue
		}
		if len(baseline) != len(names) {
			t.Errorf("%q surface size drifted (%d vs %d)", tc.goal, len(names), len(baseline))
		}
	}
	// Prompt-level narrowing survives: the media-center turn carries the media
	// clause, not the office pipeline clause.
	media := bundledWorkflowInjection("在媒体中心播放《大都会》")
	if !strings.Contains(media, "媒体中心") || strings.Contains(media, "pptx.gen") {
		t.Errorf("prompt narrowing drifted for media: %.200s", media)
	}
}

func TestSingleDeliverableDoesNotAskForAPlanFirst(t *testing.T) {
	now := time.Date(2026, 9, 27, 2, 40, 0, 0, time.Local)
	paper := currentTurnInstruction("帮我写一个关于12星座的长篇分析报告论文", now)
	if !strings.Contains(paper, "不要先写") {
		t.Fatalf("a single paper still invites a plan first: %s", paper)
	}
	weather := currentTurnInstruction("这里的天气", now)
	if !strings.Contains(weather, "不要先写") {
		t.Fatalf("weather still invites a plan first: %s", weather)
	}
	steps := currentTurnInstruction("修一下支付超时的 bug，然后跑测试", now)
	if strings.Contains(steps, "不要先写") {
		t.Fatalf("a real multi-step task lost its checklist: %s", steps)
	}
	assist := typedAssistInstruction()
	if !strings.Contains(assist, "一件事直接做完") || !strings.Contains(assist, "todo.write") || !strings.Contains(assist, "user.ask") || !strings.Contains(assist, "completed") || !strings.Contains(assist, "下一步建议") {
		t.Fatalf("typed assist: %s", assist)
	}
	if !strings.Contains(steps, "下一步建议") {
		t.Fatalf("a finished multi-step turn lost the next-step suggestion: %s", steps)
	}
}

func sortedToolNames(names map[string]bool) []string {
	out := make([]string, 0, len(names))
	for name := range names {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}
