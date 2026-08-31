package app

import "testing"

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
