package app

import (
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/gateway"
)

func TestAutoToolProfile(t *testing.T) {
	// Pure chat with no task hint narrows to minimal.
	chat := []string{"你好呀", "在吗？", "谢谢你", "hi", "讲个笑话", "你是谁", "good morning"}
	for _, s := range chat {
		if got := autoToolProfile(s); got != toolProfileMinimal {
			t.Fatalf("chat %q -> %q, want minimal", s, got)
		}
	}
	// Any actionable intent keeps the full default surface.
	task := []string{
		"帮我打开记事本", "运行测试", "改一下这个文件", "search the web for X",
		"fix the bug in main.go", "打开浏览器", "查一下今天天气", "git commit",
	}
	for _, s := range task {
		if got := autoToolProfile(s); got != toolProfileDefault {
			t.Fatalf("task %q -> %q, want default", s, got)
		}
	}
	// Empty and long/ambiguous turns stay on default.
	if autoToolProfile("") != toolProfileDefault {
		t.Fatal("empty should be default")
	}
	// >40 runes: the length guard keeps the full surface even with a chat hint,
	// because a long turn is more likely to carry an actionable request.
	long := strings.Repeat("你好", 21)
	if autoToolProfile(long) != toolProfileDefault {
		t.Fatal("long turn should stay default (may carry a task)")
	}
}

func TestApplyToolProfileKeepsDefaultAndFilters(t *testing.T) {
	all := engineToolDefinitions()
	if got := applyToolProfile(all, toolProfileDefault); len(got) != len(all) {
		t.Fatalf("default must keep current tools: %d vs %d", len(got), len(all))
	}
	minimal := applyToolProfile(all, toolProfileMinimal)
	if len(minimal) != 5 {
		t.Fatalf("minimal=%d", len(minimal))
	}
	for _, d := range minimal {
		if d.Name == "command.run" || d.Name == "computer.act" {
			t.Fatalf("minimal leaked %s", d.Name)
		}
	}
	coding := applyToolProfile(all, toolProfileCoding)
	seen := map[string]bool{}
	for _, d := range coding {
		seen[d.Name] = true
	}
	if !seen["workspace.write"] || !seen["command.run"] || seen["im.send"] {
		t.Fatalf("coding=%v", seen)
	}
	colleague := applyToolProfile(append(all, specialistToolDefinitions(all)...), toolProfileColleague)
	for _, d := range colleague {
		if d.Name == "computer.act" {
			t.Fatal("colleague must not gain computer.act")
		}
	}
}

func TestFilterCompanionDefaultToolsOmitsShellAndIM(t *testing.T) {
	all := append(engineToolDefinitions(), gateway.ToolDefinition{Name: "computer.act"}, gateway.ToolDefinition{Name: "cc.mouse_click"})
	got := filterCompanionDefaultTools(all)
	seen := map[string]bool{}
	for _, d := range got {
		seen[d.Name] = true
	}
	if seen["command.run"] || seen["im.send"] || seen["cc.mouse_click"] {
		t.Fatalf("companion leaked high-risk tools: %v", seen)
	}
	if !seen["desktop.open"] || !seen["media.play"] || !seen["computer.act"] || !seen["web.search"] {
		t.Fatalf("companion dropped desktop/media tools: %v", seen)
	}
	if !companionDefaultDeniedTool("command.run") || !companionDefaultDeniedTool("im.send") {
		t.Fatal("denied set incomplete")
	}
}
