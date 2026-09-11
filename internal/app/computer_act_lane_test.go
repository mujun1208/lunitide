package app

import (
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/llmadapter"
)

func TestComputerActSameChainVoiceAndTyped(t *testing.T) {
	t.Parallel()
	defs := append(engineToolDefinitions(), llmadapter.ToolDefinition{
		Name:        "computer.act",
		Description: "Unified desktop action (OpenClaw-shaped). do not call cc.* yourself.",
		Schema:      []byte(`{"type":"object","required":["action"]}`),
	}, llmadapter.ToolDefinition{Name: "cc.mouse_click"}, llmadapter.ToolDefinition{Name: "cc.screen_capture"})

	goals := []string{
		"点一下确认",
		"帮我点保存",
		"截图看看当前窗口",
		"在记事本里输入hello",
		"按一下回车",
		"按ctrl+s保存",
		"帮我粘贴刚才的内容",
		"打开记事本然后点保存",
	}
	for _, goal := range goals {
		voice := assembleRoutedTools(defs, goal, true, true)
		typed := assembleRoutedTools(defs, goal, false, true)
		voiceHas, typedHas := false, false
		for _, d := range voice {
			if strings.HasPrefix(d.Name, "cc.") {
				t.Fatalf("voice leaked %s for %q", d.Name, goal)
			}
			if d.Name == "computer.act" {
				voiceHas = true
				if !strings.Contains(d.Description, "do not call cc.*") {
					t.Fatalf("voice computer.act schema drifted for %q", goal)
				}
			}
		}
		for _, d := range typed {
			if strings.HasPrefix(d.Name, "cc.") {
				t.Fatalf("typed leaked %s for %q", d.Name, goal)
			}
			if d.Name == "computer.act" {
				typedHas = true
			}
		}
		if !voiceHas || !typedHas {
			t.Fatalf("%q must ship computer.act on both lanes (voice=%v typed=%v)", goal, voiceHas, typedHas)
		}
		if !companionWantsTools(goal) {
			t.Fatalf("3-chain voice must attach tools for %q", goal)
		}
		if !companionWantsDesktopControl(goal) && !computerExecutionTurn(goal) {
			t.Fatalf("desktop execution contract must cover %q", goal)
		}
	}
}

func TestComputerActIdleVoiceStaysToolLess(t *testing.T) {
	t.Parallel()
	for _, idle := range []string{"你好", "今晚月色如何", "我随便说说", "继续聊"} {
		if companionWantsTools(idle) || companionWantsDesktopControl(idle) {
			t.Fatalf("idle %q must not open the desktop chain", idle)
		}
	}
}

func TestComputerActSharedExecutionInstruction(t *testing.T) {
	t.Parallel()
	inst := desktopExecutionInstruction()
	if !strings.Contains(inst, "语音与文字共用") {
		t.Fatal("typed and 3-chain voice must share one execution contract")
	}
	if !strings.Contains(inst, "computer.act") {
		t.Fatal("shared contract must name computer.act")
	}
	wf := workflowComputerClause
	if !strings.Contains(wf, "computer.act") || !strings.Contains(wf, "frameId") {
		t.Fatal("desktop workflow must stay on computer.act")
	}
	if !strings.Contains(wf, "name=") || !strings.Contains(wf, "observe") {
		t.Fatal("named observe-then-act must be in the live instruction")
	}
}

func TestComputerActVoiceKeyboardAndClickNeedles(t *testing.T) {
	t.Parallel()
	for _, goal := range []string{"按一下回车", "按回车", "帮我粘贴", "按一下快捷键", "点确定", "点保存", "点开第一条新闻"} {
		if !companionWantsTools(goal) {
			t.Fatalf("3-chain must keep the computer.act chain for %q", goal)
		}
		if !companionWantsDesktopControl(goal) {
			t.Fatalf("desktop-control follow-through must keep %q", goal)
		}
	}
}
