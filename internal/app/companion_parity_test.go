package app

import (
	"testing"

	"github.com/lunitide/lunitide/internal/gateway"
)

func TestCompanionParityDesktopAllow(t *testing.T) {
	defs := []gateway.ToolDefinition{
		{Name: "desktop.open"}, {Name: "desktop.type"}, {Name: "computer.act"},
		{Name: "command.run"}, {Name: "im.send"}, {Name: "user.ask"}, {Name: "web.search"},
	}
	has := func(list []gateway.ToolDefinition, name string) bool {
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
		if has(voice, "command.run") {
			t.Fatalf("companion must strip command.run for %q", goal)
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
