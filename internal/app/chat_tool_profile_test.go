package app

import (
	"testing"

	"github.com/lunitide/lunitide/internal/gateway"
)

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
