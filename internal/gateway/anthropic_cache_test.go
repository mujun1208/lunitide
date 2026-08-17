// P0-2 prompt-caching coverage: the three ephemeral breakpoints (tools tail,
// system tail, history-prefix tail) must land on the stable prefix and stay
// within Anthropic's four-breakpoint budget; cached usage fields fold into
// the metered input so totals stay truthful.
package gateway

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func countBreakpoints(body string) int { return strings.Count(body, `"cache_control":{"type":"ephemeral"}`) }

func TestAnthropicCacheBreakpoints(t *testing.T) {
	in := Request{
		Model: "claude",
		Tools: []ToolDefinition{
			// Wire names sanitize dots to underscores; the assertion below
			// checks the on-wire tool shape verbatim.
			{Name: "workspace_read", Description: "d1", Schema: []byte(`{"type":"object"}`)},
			{Name: "workspace_search", Description: "d2", Schema: []byte(`{"type":"object"}`)},
		},
		Messages: []Message{
			{Role: RoleSystem, Content: "rules part one"},
			{Role: RoleSystem, Content: "rules part two"},
			{Role: RoleUser, Content: "question one"},
			{Role: RoleAssistant, Content: "answer one"},
			{Role: RoleUser, Content: "question two"},
		},
	}
	body, err := json.Marshal(anthropicPayload(in, false, nil))
	if err != nil {
		t.Fatal(err)
	}
	s := string(body)
	// Tools tail: only the last tool definition carries the breakpoint.
	if !strings.Contains(s, `{"name":"workspace_search","description":"d2","input_schema":{"type":"object"},"cache_control":{"type":"ephemeral"}}`) {
		t.Fatalf("tools-tail breakpoint missing: %s", s)
	}
	if strings.Contains(s, `{"name":"workspace_read","description":"d1","input_schema":{"type":"object"},"cache_control"`) {
		t.Fatalf("non-tail tool must stay unmarked: %s", s)
	}
	// System tail: block-array form with the breakpoint on the first and last blocks.
	if !strings.Contains(s, `"system":[{"text":"rules part one","cache_control":{"type":"ephemeral"}},{"text":"rules part two","cache_control":{"type":"ephemeral"}}]`) {
		t.Fatalf("system-tail breakpoint missing: %s", s)
	}
	// History prefix: the second-to-last message ("answer one") is lifted
	// from a plain string into a marked text block; the final message stays
	// plain so the next turn extends the prefix incrementally.
	if !strings.Contains(s, `{"role":"assistant","content":[{"type":"text","text":"answer one","cache_control":{"type":"ephemeral"}}]}`) {
		t.Fatalf("history-prefix breakpoint missing: %s", s)
	}
	if strings.Contains(s, `"text":"question two","cache_control"`) {
		t.Fatalf("final message must stay unmarked: %s", s)
	}
	if n := countBreakpoints(s); n != 4 {
		t.Fatalf("breakpoint count = %d, want 4 (budget 4)", n)
	}
}

func TestAnthropicCacheBreakpointOnToolResultHistory(t *testing.T) {
	// Multi-step tool loop shape: history ends with tool results. The
	// second-to-last message is a tool_result block and must take the
	// breakpoint inline (no string lifting applies).
	in := Request{Messages: []Message{
		{Role: RoleUser, Content: "fix the bug"},
		{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "call_1", Name: "workspace.read", Arguments: []byte(`{"path":"a.go"}`)}}},
		{Role: RoleTool, ToolCallID: "call_1", Content: "file body"},
		{Role: RoleUser, Content: "continue"},
	}}
	body, err := json.Marshal(anthropicPayload(in, false, nil))
	if err != nil {
		t.Fatal(err)
	}
	s := string(body)
	if !strings.Contains(s, `{"type":"tool_result","tool_use_id":"call_1","content":"file body","cache_control":{"type":"ephemeral"}}`) {
		t.Fatalf("tool_result breakpoint missing: %s", s)
	}
}

func TestAnthropicNoBreakpointsOnMinimalRequest(t *testing.T) {
	// Single-message requests and tool-less/system-less requests carry no
	// history or empty-section breakpoints.
	in := Request{Messages: []Message{{Role: RoleUser, Content: "hi"}}}
	body, err := json.Marshal(anthropicPayload(in, false, nil))
	if err != nil {
		t.Fatal(err)
	}
	if n := countBreakpoints(string(body)); n != 0 {
		t.Fatalf("breakpoint count = %d, want 0", n)
	}
}

func TestAnthropicCacheUsageFolded(t *testing.T) {
	// Cached reads/writes arrive as separate usage fields; the metered
	// input must fold all three so totals reflect real consumption.
	payload := `{"content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":10,"output_tokens":2,"cache_read_input_tokens":100,"cache_creation_input_tokens":5}}`
	f := &fakeConnector{responses: []*http.Response{response(200, payload)}}
	out, err := NewAnthropic(f, Options{}).Complete(context.Background(), nil, Request{Model: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if out.Usage.InputTokens != 115 || out.Usage.OutputTokens != 2 || out.Usage.TotalTokens != 117 {
		t.Fatalf("usage = %+v, want input 115 output 2 total 117", out.Usage)
	}
}

func TestAnthropicCacheUsageFoldedInStream(t *testing.T) {
	stream := "event: message_start\ndata: {\"content\":[],\"delta\":{\"type\":\"\",\"text\":\"\"},\"usage\":{\"input_tokens\":3,\"output_tokens\":0,\"cache_read_input_tokens\":40}}\n\nevent: content_block_delta\ndata: {\"content\":[],\"delta\":{\"type\":\"text_delta\",\"text\":\"yo\"},\"usage\":{\"input_tokens\":0,\"output_tokens\":0}}\n\nevent: message_delta\ndata: {\"content\":[],\"delta\":{\"type\":\"\",\"text\":\"\"},\"usage\":{\"input_tokens\":0,\"output_tokens\":1}}\n\nevent: message_stop\ndata: {}\n\n"
	f := &fakeConnector{responses: []*http.Response{response(200, stream)}}
	var emittedUsage []Usage
	out, err := NewAnthropic(f, Options{}).Stream(context.Background(), nil, Request{Model: "claude"}, func(d Delta) error {
		if d.Usage != nil {
			emittedUsage = append(emittedUsage, *d.Usage)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Usage.InputTokens != 43 || out.Usage.TotalTokens != 44 {
		t.Fatalf("stream usage = %+v, want input 43 total 44", out.Usage)
	}
	// The message_start frame carries the folded cached input (3 + 40);
	// later frames report incremental output only.
	if len(emittedUsage) == 0 || emittedUsage[0].InputTokens != 43 {
		t.Fatalf("first emitted usage = %+v, want input 43", emittedUsage)
	}
	// The strict decoder must accept cache fields on every SSE frame.
	if len(f.requests) != 1 {
		t.Fatalf("requests = %d", len(f.requests))
	}
	b, _ := io.ReadAll(f.requests[0].Body)
	if !strings.Contains(string(b), `"stream":true`) {
		t.Fatal("stream flag missing")
	}
	_ = http.MethodPost
}
