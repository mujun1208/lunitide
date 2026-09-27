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

// Ordinary tasks must not inherit the whole catalog. A long paper was one
// example; weather, playback, a click, a page edit and a weekly report are
// the same leak.
func TestEveryOrdinaryTaskSkipsTheUnusedToolPile(t *testing.T) {
	pageEdit := "帮我新增一个商机\n\n" + openPageFileMark + "E:/poc/index.html\n" + openPageFileInstruction
	pageQuestion := "这个表里有多少客户\n\n" + openPageFileMark + "E:/poc/index.html\n" + openPageReadInstruction
	cases := []struct {
		goal   string
		cc     bool
		must   []string
		forbid []string
		max    int
	}{
		{
			goal:   "帮我写一个关于，12星座，不同星座和不同星座的爱情，哪里匹配，哪里合适，哪里不匹配，不合适，相互之间在一起能得多少分的，长篇分析报告论文。",
			must:   []string{"canvas.present", "user.ask"},
			forbid: []string{"web.search", "todo.write", "media.play", "desktop.open", "docx.gen", "system.run", "skill.invoke"},
			max:    2,
		},
		{
			goal:   "这里的天气",
			must:   []string{"location.get", "weather.get", "user.ask"},
			forbid: []string{"media.play", "docx.gen", "desktop.open", "canvas.present", "workspace.write", "command.run", "todo.write", "office.generate", "web.search", "skill.invoke"},
			max:    4,
		},
		{
			goal:   "帮我点这个按钮",
			cc:     true,
			must:   []string{"system.run", "user.ask"},
			forbid: []string{"media.play", "docx.gen", "canvas.present", "office.generate", "web.search", "todo.write", "workspace.write"},
			max:    8,
		},
		{
			goal:   "在媒体中心播放《大都会》",
			must:   []string{"media.play", "user.ask"},
			forbid: []string{"web.search", "docx.gen", "desktop.open", "canvas.present", "workspace.write", "todo.write", "office.generate", "command.run", "skill.invoke"},
			max:    3,
		},
		{
			goal:   "把这篇时间空间论证论文写成 Word",
			must:   []string{"docx.gen", "user.ask"},
			forbid: []string{"canvas.present", "media.play", "desktop.open", "web.search", "video.generate", "system.run"},
			max:    10,
		},
		{
			goal:   "写周报",
			must:   []string{"workspace.write", "docx.gen", "user.ask"},
			forbid: []string{"web.search", "media.play", "desktop.open", "canvas.present", "command.run", "video.generate", "system.run"},
			max:    12,
		},
		{
			goal:   pageEdit,
			must:   []string{"workspace.read", "workspace.edit", "user.ask"},
			forbid: []string{"web.search", "media.play", "docx.gen", "canvas.present", "desktop.open", "todo.write", "office.generate"},
			max:    6,
		},
		{
			goal:   pageQuestion,
			must:   []string{"workspace.read", "user.ask"},
			forbid: []string{"workspace.edit", "web.search", "docx.gen", "media.play", "desktop.open", "todo.write"},
			max:    3,
		},
		{
			goal:   "把这段话润色得更顺：今天开会很顺利",
			must:   []string{"user.ask"},
			forbid: []string{"docx.gen", "web.search", "media.play", "workspace.write", "todo.write", "office.generate", "canvas.present"},
			max:    2,
		},
		{
			goal:   "修一下支付超时的 bug，然后跑测试",
			must:   []string{"workspace.edit", "command.run", "todo.write", "user.ask"},
			forbid: []string{"media.play", "canvas.present", "docx.gen", "desktop.open", "web.search", "office.generate"},
			max:    10,
		},
		{
			goal:   "根据网上公开资料写一份行业报告",
			must:   []string{"web.search", "docx.gen", "user.ask"},
			forbid: []string{"media.play", "desktop.open", "video.generate", "system.run", "canvas.present"},
			max:    12,
		},
		{
			goal:   "打开记事本",
			must:   []string{"desktop.open", "user.ask"},
			forbid: []string{"docx.gen", "media.play", "canvas.present", "web.search", "workspace.write", "office.generate"},
			max:    8,
		},
		{
			goal:   "查明天上海到北京火车票",
			must:   []string{"web.search", "user.ask"},
			forbid: []string{"docx.gen", "media.play", "canvas.present", "desktop.open", "workspace.write", "office.generate", "todo.write"},
			max:    5,
		},
		{
			goal:   "生成一张图片",
			must:   []string{"image.generate", "user.ask"},
			forbid: []string{"docx.gen", "media.play", "web.search", "desktop.open", "workspace.write", "todo.write"},
			max:    4,
		},
		{
			goal:   "你好",
			forbid: []string{"web.search", "docx.gen", "canvas.present", "media.play", "todo.write"},
			max:    0,
		},
		{
			goal:   "用浏览器打开 https://example.com",
			must:   []string{"browser.act", "user.ask"},
			forbid: []string{"docx.gen", "media.play", "canvas.present", "desktop.open", "workspace.write", "todo.write"},
			max:    4,
		},
		{
			goal:   "我要看《九品芝麻官》",
			must:   []string{"media.play", "user.ask"},
			forbid: []string{"desktop.open", "docx.gen", "web.search", "canvas.present", "todo.write"},
			max:    3,
		},
		{
			goal:   "用网易云音乐播放周杰伦",
			must:   []string{"desktop.open", "user.ask"},
			forbid: []string{"docx.gen", "canvas.present", "workspace.write", "office.generate", "web.search"},
			max:    8,
		},
	}
	for _, tc := range cases {
		names := productToolNames(tc.goal, tc.cc)
		got := sortedToolNames(names)
		if len(got) > tc.max {
			t.Errorf("%q has %d tools %v", tc.goal, len(got), got)
		}
		for _, name := range tc.must {
			if !names[name] {
				t.Errorf("%q missing %s; tools=%v", tc.goal, name, got)
			}
		}
		for _, name := range tc.forbid {
			if names[name] {
				t.Errorf("%q still carries %s; tools=%v", tc.goal, name, got)
			}
		}
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
