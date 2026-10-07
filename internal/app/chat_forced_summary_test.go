package app

import (
	"context"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/llmadapter"
	"github.com/lunitide/lunitide/internal/toolruntime"
	"github.com/oklog/ulid/v2"
)

// forcedSummaryAdapter records every request the engine sends so the tests
// can assert the shape of the no-tools closing pass (UX-05 #3).
type forcedSummaryAdapter struct {
	partialThenFailAdapter
	calls    int
	requests []llmadapter.Request
	run      func(call int, req llmadapter.Request, emit func(llmadapter.Delta) error) (llmadapter.Response, error)
}

func (a *forcedSummaryAdapter) Stream(_ context.Context, _ []byte, req llmadapter.Request, emit func(llmadapter.Delta) error) (llmadapter.Response, error) {
	a.calls++
	a.requests = append(a.requests, req)
	return a.run(a.calls, req, emit)
}

func newForcedSummaryEngine(t *testing.T) (*Engine, *appendAssistantSpy) {
	t.Helper()
	e := NewEngine(nil, "test")
	spy := &appendAssistantSpy{}
	e.messages = spy
	e.leases = streamTestLease{}
	e.SetChatTurnJournal(&budgetTestJournal{})
	runtime, err := toolruntime.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { runtime.Close() })
	e.SetToolRuntime(runtime)
	return e, spy
}

// runForcedSummaryTurn drives one chat turn pinned to the one-step L1 lane,
// the lane whose exhausted budget with a pending tool call used to end the
// turn with only the canned notice.
func runForcedSummaryTurn(t *testing.T, e *Engine, a llmadapter.Adapter, userText string) string {
	t.Helper()
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (llmadapter.Adapter, error) { return a, nil })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	id := ulid.Make().String()
	state := &streamState{cancel: cancel, state: streamRunning, sessionID: chatAttachmentSessionID}
	state.lane = buildLaneContract(LaneL1, RouteUnspecified, CouncilOverlay{})
	e.streams[id] = state
	var text strings.Builder
	e.runStream(ctx, id, state, provider.Provider{ID: chatAttachmentProviderID, Protocol: provider.ProtocolOpenAICompatible, BaseURL: "https://example.test", CredentialRef: "credential-ref"}, llmadapter.Request{Model: "model", Tools: engineToolDefinitions(), Messages: []llmadapter.Message{{Role: llmadapter.RoleUser, Content: userText}}}, func(ev bridge.Event) error {
		if ev.Type == bridge.EventDelta && ev.Delta != nil {
			text.WriteString(ev.Delta.Text)
		}
		return nil
	}, chatAttachmentSessionID, executionModeFullAccess)
	return text.String()
}

// A one-step lane that spends its only step on a tool call must still answer
// in words: the forced summary pass runs without tools and its text reaches
// the user instead of the canned no-answer notice.
func TestForcedSummaryPassSpeaksWhenStepBudgetEndsOnToolCall(t *testing.T) {
	e, _ := newForcedSummaryEngine(t)
	a := &forcedSummaryAdapter{run: func(call int, _ llmadapter.Request, emit func(llmadapter.Delta) error) (llmadapter.Response, error) {
		if call == 1 {
			return llmadapter.Response{Message: llmadapter.Message{Role: llmadapter.RoleAssistant, ToolCalls: []llmadapter.ToolCall{{ID: "search-canvas", Name: "mcp.search", Arguments: []byte(`{"query":"canvas 画布 展示报告"}`)}}}}, nil
		}
		if err := emit(llmadapter.Delta{Text: "没有找到可用的画布工具，报告保持上一轮的原样。"}); err != nil {
			return llmadapter.Response{}, err
		}
		return llmadapter.Response{Message: llmadapter.Message{Role: llmadapter.RoleAssistant, Content: "没有找到可用的画布工具，报告保持上一轮的原样。"}}, nil
	}}
	text := runForcedSummaryTurn(t, e, a, "补充：给每个配对的总分加一个五星等级")
	if len(a.requests) != 2 {
		t.Fatalf("forced summary pass did not run: model calls=%d", len(a.requests))
	}
	if !strings.Contains(text, "报告保持上一轮的原样") || strings.Contains(text, "这一轮没有回答") {
		t.Fatalf("canned notice preempted the summary pass: %q", text)
	}
	pass := a.requests[1]
	if len(pass.Tools) != 0 {
		t.Fatalf("summary pass kept tools: %d", len(pass.Tools))
	}
	assistantToolMessages := 0
	sawToolResult := false
	for _, m := range pass.Messages {
		if m.Role == llmadapter.RoleAssistant && len(m.ToolCalls) > 0 {
			assistantToolMessages++
		}
		if m.Role == llmadapter.RoleTool && m.ToolCallID == "search-canvas" {
			sawToolResult = true
		}
	}
	if assistantToolMessages != 1 || !sawToolResult {
		t.Fatalf("summary pass request malformed: assistant tool messages=%d tool result=%t", assistantToolMessages, sawToolResult)
	}
	if last := pass.Messages[len(pass.Messages)-1]; last.Role != llmadapter.RoleSystem || last.Content != forceSummaryNudgeText {
		t.Fatalf("summary pass missing forced nudge: %+v", last)
	}
}

// When the forced summary pass also stays silent, the deferred canned notice
// is still written, so a finished turn never ends as a blank bubble.
func TestForcedSummaryPassFallsBackToCannedNoticeWhenSilent(t *testing.T) {
	e, _ := newForcedSummaryEngine(t)
	a := &forcedSummaryAdapter{run: func(call int, _ llmadapter.Request, _ func(llmadapter.Delta) error) (llmadapter.Response, error) {
		if call == 1 {
			return llmadapter.Response{Message: llmadapter.Message{Role: llmadapter.RoleAssistant, ToolCalls: []llmadapter.ToolCall{{ID: "search-canvas", Name: "mcp.search", Arguments: []byte(`{"query":"canvas 画布 展示报告"}`)}}}}, nil
		}
		return llmadapter.Response{Message: llmadapter.Message{Role: llmadapter.RoleAssistant}}, nil
	}}
	text := runForcedSummaryTurn(t, e, a, "补充：给每个配对的总分加一个五星等级")
	if len(a.requests) != 2 {
		t.Fatalf("summary pass did not run before the fallback: model calls=%d text=%q", len(a.requests), text)
	}
	if !strings.Contains(text, "这一轮没有回答") {
		t.Fatalf("canned fallback notice lost: %q", text)
	}
}

// A factual receipt (work actually landed) is still written immediately and
// must not trigger an extra model pass.
func TestFactualTurnReceiptStaysImmediateWithoutSummaryPass(t *testing.T) {
	e, _ := newForcedSummaryEngine(t)
	a := &forcedSummaryAdapter{run: func(call int, _ llmadapter.Request, _ func(llmadapter.Delta) error) (llmadapter.Response, error) {
		if call != 1 {
			t.Errorf("factual receipt must not trigger a summary pass: extra call %d", call)
			return llmadapter.Response{}, nil
		}
		return llmadapter.Response{Message: llmadapter.Message{Role: llmadapter.RoleAssistant, ToolCalls: []llmadapter.ToolCall{{ID: "write-file", Name: "workspace.write", Arguments: []byte(`{"path":"report.txt","content":"done"}`)}}}}, nil
	}}
	text := runForcedSummaryTurn(t, e, a, "补充：给每个配对的总分加一个五星等级")
	if len(a.requests) != 1 {
		t.Fatalf("factual receipt must not trigger a summary pass: model calls=%d", len(a.requests))
	}
	if !strings.Contains(text, "已经做完") {
		t.Fatalf("factual receipt lost: %q", text)
	}
}
