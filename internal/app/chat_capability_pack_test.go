package app

import "testing"

func TestPackSpecFromManifest(t *testing.T) {
	spec := packSpecFromManifest(map[string]any{
		"skills":        []any{"browser-automation", "e2e-browser"},
		"mcpPresetIds":  []any{"playwright"},
		"toolGates":     []any{"browser", "web-fetch"},
	})
	if len(spec.Skills) != 2 || spec.McpPresetIDs[0] != "playwright" || len(spec.ToolGates) != 2 {
		t.Fatalf("spec = %#v", spec)
	}
	if note := formatPackNotes([]string{"技能 browser-automation"}, "MCP playwright：失败"); note == "" {
		t.Fatal("expected pack notes")
	}
}
