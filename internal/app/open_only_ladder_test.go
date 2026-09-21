package app

import (
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/llmadapter"
)

// The reported failure wall on an open-only turn was four refusals in a row:
//
//	desktop.open  · 失败 无法执行：启动了但未确认目标进程
//	computer.act  · 失败 本轮只要打开桌面文件、应用或页面，不得浏览工作区或跑命令。
//	computer.act  · 失败 无法执行：未能从屏幕读出下一步
//	command.run   · 失败 本轮只要打开桌面文件、应用或页面，不得浏览工作区或跑命令。
//
// The second and fourth came from the turn guard, which forbids screen work and
// shell commands on an open-only turn. Forbidding the shell is right — opening a
// file never needs it. Forbidding the screen after the dedicated tool has
// already failed leaves the request with nowhere to go, which is the wall the
// user saw. These pin the asymmetry.

func openOnlyHistory(tool, output string) []llmadapter.Message {
	return []llmadapter.Message{
		{Role: llmadapter.RoleUser, Content: "打开桌面的网易云音乐"},
		{Role: llmadapter.RoleAssistant, ToolCalls: []llmadapter.ToolCall{{ID: "call-1", Name: tool, Arguments: []byte(`{"name":"网易云音乐"}`)}}},
		{Role: llmadapter.RoleTool, ToolCallID: "call-1", Content: output},
	}
}

func TestOpenOnlyTurnKeepsScreenWorkOutUntilTheDedicatedToolHasFailed(t *testing.T) {
	const goal = "打开桌面的网易云音乐"
	if !companionGoalIsOpenOnly(goal) {
		t.Fatalf("fixture drifted: %q is no longer an open-only goal", goal)
	}
	// Nothing tried yet: the screen is still off limits, so the model reaches
	// for the dedicated tool first instead of pixel-clicking its way there.
	if err := guardCurrentTurnToolHistory(goal, "computer.act", nil); err == nil {
		t.Fatal("an untouched open-only turn must not start with screen control")
	}
	// The dedicated tool has now failed. Screen control is rung 3 of the
	// ladder and the only path left, so the guard must stand aside.
	failed := openOnlyHistory("desktop.open", "ok:false 无法执行：打不开（exit status 1）")
	if err := guardCurrentTurnToolHistory(goal, "computer.act", failed); err != nil {
		t.Fatalf("after desktop.open failed, screen control is the only path left: %v", err)
	}
	// The shell is never the way to open a desktop file, so it stays blocked
	// no matter what came before.
	if err := guardCurrentTurnToolHistory(goal, "command.run", failed); err == nil {
		t.Fatal("running shell commands is never how an open-only turn recovers")
	}
	for _, tool := range []string{"workspace.list", "workspace.search", "workspace.read", "workspace.write"} {
		if err := guardCurrentTurnToolHistory(goal, tool, failed); err == nil {
			t.Fatal("browsing the workspace is never how an open-only turn recovers: " + tool)
		}
	}
}

func TestOpenOnlyFallbackStaysShutWhenTheRefusalCameFromPermissions(t *testing.T) {
	const goal = "打开桌面的网易云音乐"
	// A capability denial is not "the tool tried and could not do it" — the
	// answer is to tell the user to enable computer control, not to escalate
	// to the very subsystem that is switched off.
	denied := openOnlyHistory("desktop.open", "ok:false 电脑控制未启用")
	if err := guardCurrentTurnToolHistory(goal, "computer.act", denied); err == nil {
		t.Fatal("a capability denial must not unlock the screen fallback")
	}
}

func TestOpenOnlySucceedingToolDoesNotUnlockTheFallback(t *testing.T) {
	const goal = "打开桌面的网易云音乐"
	// Success must end the turn. If a success unlocked screen control, the
	// model could keep working past the point the user asked about.
	opened := openOnlyHistory("desktop.open", `opened D:\\Desktop\\网易云音乐.lnk {"l0":{"passed":true}}`)
	if err := guardCurrentTurnToolHistory(goal, "computer.act", opened); err == nil {
		t.Fatal("a successful open must not unlock further screen work")
	}
	if !desktopLadderSettled(opened, goal) {
		t.Fatal("a successful open must settle the turn so the reply can be spoken")
	}
}

func TestUnverifiedOpenReadsAsSuccessNotFailure(t *testing.T) {
	// desktop.open reports "unverified" when the launch went through but the
	// window probe timed out. That is the case the user hit, and it must read
	// as done rather than sending the model up the ladder.
	const out = `opened D:\Desktop\网易云音乐.lnk {"l0":{"passed":true,"proof":"unverified"}}`
	if companionToolResultFailed(out) {
		t.Fatal("an unverified-but-launched open must not read as a failure")
	}
	if strings.Contains(out, "ok:false") {
		t.Fatal("fixture drifted")
	}
	goal := "打开桌面的网易云音乐"
	if !desktopLadderSettled(openOnlyHistory("desktop.open", out), goal) {
		t.Fatal("an unverified open must still settle the turn")
	}
}
