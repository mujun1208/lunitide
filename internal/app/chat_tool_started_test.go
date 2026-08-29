package app

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestToolStartedSummary(t *testing.T) {
	t.Parallel()
	if got := toolStartedSummary("command.run", json.RawMessage(`{"argv":["go","test","./..."]}`)); got != "$ go test ./..." {
		t.Fatalf("command: %q", got)
	}
	if got := toolStartedSummary("web.search", json.RawMessage(`{"query":"jay"}`)); got != "搜索：jay" {
		t.Fatalf("search: %q", got)
	}
	if got := toolStartedSummary("web.fetch", json.RawMessage(`{"url":"https://ex.test"}`)); got != "https://ex.test" {
		t.Fatalf("fetch: %q", got)
	}
	if got := toolStartedSummary("workspace.read", json.RawMessage(`{}`)); got != "" {
		t.Fatalf("other: %q", got)
	}
	if got := toolStartedSummary("user.ask", json.RawMessage(`{"title":"需求边界","questions":[{"prompt":"部署","options":[{"label":"A"},{"label":"B"}]}]}`)); got != "需要你决策：需求边界" {
		t.Fatalf("user.ask started: %q", got)
	}
	args := json.RawMessage(`{"title":"需求边界","questions":[{"prompt":"部署","options":[{"label":"容器化"},{"label":"虚拟机"}]}]}`)
	if got := approvalRequiredSummary("user.ask", args); !strings.Contains(got, `"questions"`) || !strings.Contains(got, "需求边界") {
		t.Fatalf("user.ask approval summary: %q", got)
	}
	if got := approvalRequiredSummary("command.run", args); got != "approval required" {
		t.Fatalf("other approval summary: %q", got)
	}
	long := strings.Repeat("x", 5000)
	got := clipToolSummary(toolStartedSummary("command.run", json.RawMessage(`{"argv":["echo","`+long+`"]}`)))
	if len(got) > 4096 {
		t.Fatalf("started summary must clip to 4096 bytes, got %d", len(got))
	}
}
