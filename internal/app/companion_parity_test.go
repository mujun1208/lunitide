package app

import (
	"reflect"
	"testing"

	"github.com/lunitide/lunitide/internal/llmadapter"
)

func TestVoiceAndTypedCompoundTaskTools(t *testing.T) {
	defs := append(engineToolDefinitions(), llmadapter.ToolDefinition{Name: "computer.act"})
	for _, tc := range []struct {
		goal   string
		needed []string
	}{
		{"查询合肥天气并发给微信里的小王", []string{"weather.get", "web.search", "im.send", "desktop.open", "computer.act"}},
		{"查一下股价写一份报告", []string{"web.search", "workspace.write", "docx.gen"}},
		{"打开记事本然后运行测试命令", []string{"desktop.open", "command.run", "workspace.read"}},
		{"给小王发送消息", []string{"im.send"}},
	} {
		t.Run(tc.goal, func(t *testing.T) {
			voice := assembleRoutedTools(defs, tc.goal, true, true)
			typed := assembleRoutedTools(defs, tc.goal, false, true)
			// Typed chat retains choice cards. Voice asks clarifications aloud,
			// so user.ask is the only deliberate difference; every work tool,
			// schema and description must remain identical.
			var typedWorkTools []llmadapter.ToolDefinition
			typedHasAsk := false
			for _, tool := range typed {
				if tool.Name == "user.ask" {
					typedHasAsk = true
					continue
				}
				typedWorkTools = append(typedWorkTools, tool)
			}
			if !typedHasAsk {
				t.Fatal("typed chat lost its existing clarification card")
			}
			if !reflect.DeepEqual(voice, typedWorkTools) {
				t.Fatal("voice and typed work tools differ beyond the spoken user.ask exception")
			}
			seen := map[string]bool{}
			for _, tool := range voice {
				seen[tool.Name] = true
			}
			if seen["user.ask"] {
				t.Fatal("voice must clarify aloud instead of parking a choice card")
			}
			for _, name := range tc.needed {
				if !seen[name] {
					t.Errorf("missing %s", name)
				}
			}
			if !companionWantsTools(tc.goal) {
				t.Fatal("voice fast path suppressed an explicit task")
			}
		})
	}
}

func TestCompanionParityDesktopAllow(t *testing.T) {
	defs := []llmadapter.ToolDefinition{
		{Name: "desktop.open"}, {Name: "desktop.type"}, {Name: "computer.act"},
		{Name: "command.run"}, {Name: "im.send"}, {Name: "user.ask"}, {Name: "web.search"},
	}
	has := func(list []llmadapter.ToolDefinition, name string) bool {
		for _, d := range list {
			if d.Name == name {
				return true
			}
		}
		return false
	}
	for _, goal := range []string{"帮我打开桌面道具", "打开记事本"} {
		voice := assembleRoutedTools(defs, goal, true, false)
		typed := assembleRoutedTools(defs, goal, false, false)
		if !has(voice, "desktop.open") || !has(typed, "desktop.open") {
			t.Fatalf("%q must allow desktop.open on both lanes", goal)
		}
		if has(voice, "command.run") != has(typed, "command.run") {
			t.Fatalf("voice and typed routes differ for %q", goal)
		}
	}
	offV := assembleRoutedTools(defs, "打开记事本", true, false)
	offT := assembleRoutedTools(defs, "打开记事本", false, false)
	if has(offV, "computer.act") || has(offT, "computer.act") {
		t.Fatal("ccOff must not ship computer.act")
	}
	onV := assembleRoutedTools(defs, "打开记事本", true, true)
	onT := assembleRoutedTools(defs, "打开记事本", false, true)
	if !has(onV, "computer.act") || !has(onT, "computer.act") {
		t.Fatal("ccOn R2 must ship computer.act on both lanes")
	}
	if companionGoalIsOpenOnly("打开记事本并写你好") {
		t.Fatal("open+type is not open-only")
	}
	if !companionGoalIsOpenOnly("打开记事本") {
		t.Fatal("plain open is open-only")
	}
}
