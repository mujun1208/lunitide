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
	long := strings.Repeat("x", 5000)
	got := clipToolSummary(toolStartedSummary("command.run", json.RawMessage(`{"argv":["echo","`+long+`"]}`)))
	if len(got) > 4096 {
		t.Fatalf("started summary must clip to 4096 bytes, got %d", len(got))
	}
}
