package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/llmadapter"
)

type subagentStreamAdapter struct {
	parallelFakeAdapter
	turn  int
	calls []llmadapter.ToolCall
}

func (a *subagentStreamAdapter) Stream(_ context.Context, _ []byte, _ llmadapter.Request, emit func(llmadapter.Delta) error) (llmadapter.Response, error) {
	if a.turn == 0 {
		a.turn++
		return llmadapter.Response{Message: llmadapter.Message{Role: llmadapter.RoleAssistant, ToolCalls: a.calls}}, nil
	}
	if err := emit(llmadapter.Delta{Text: "两个来源均已核对，独立子任务报告已经收集。"}); err != nil {
		return llmadapter.Response{}, err
	}
	return llmadapter.Response{Message: llmadapter.Message{Role: llmadapter.RoleAssistant, Content: "两个来源均已核对。"}, Usage: llmadapter.Usage{TotalTokens: 10}}, nil
}

func TestSubagentRunStreamEmitsBothLiveRowsBeforeFirstResult(t *testing.T) {
	e := newSubagentChatEngine(t)
	e.leases = streamTestLease{}
	adapter := &subagentStreamAdapter{parallelFakeAdapter: parallelFakeAdapter{entered: make(chan struct{}, 2), release: make(chan struct{})}, calls: []llmadapter.ToolCall{
		{ID: "source-a", Name: "subagent.spawn", Arguments: json.RawMessage(`{"purpose":"来源一资料查询","profile":"research"}`)},
		{ID: "source-b", Name: "subagent.spawn", Arguments: json.RawMessage(`{"purpose":"来源二资料查询","profile":"research"}`)},
	}}
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (llmadapter.Adapter, error) { return adapter, nil })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	streamID := "subagent-stream-progress"
	state := &streamState{cancel: cancel, state: streamRunning, subagentPolicy: subTestPolicy()}
	e.streams[streamID] = state
	events := make(chan bridge.Event, 128)
	done := make(chan struct{})
	go func() {
		defer close(done)
		e.runStream(ctx, streamID, state, provider.Provider{ID: subTestSession, Protocol: provider.ProtocolOpenAICompatible, BaseURL: "https://api.example.com", CredentialRef: "fake-test-reference"}, llmadapter.Request{Model: "m", Messages: []llmadapter.Message{{Role: llmadapter.RoleUser, Content: "请并行查询两个独立资料来源"}}, Tools: e.subagentToolDefinitions(executionModeFullAccess, subTestPolicy())}, func(ev bridge.Event) error { events <- ev; return nil }, subTestSession, executionModeFullAccess)
	}()
	released := false
	defer func() {
		if !released {
			close(adapter.release)
		}
		cancel()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Error("stream cleanup did not end")
		}
	}()
	seen := map[string]string{}
	for len(seen) < 2 {
		select {
		case ev := <-events:
			if ev.Type == bridge.EventFailed {
				t.Fatalf("stream failed before live status: %+v", ev.Error)
			}
			if ev.Type != bridge.EventToolOutput || ev.Tool == nil {
				continue
			}
			var progress subagentProgress
			if err := json.Unmarshal([]byte(ev.Tool.Summary), &progress); err != nil {
				t.Fatal(err)
			}
			if progress.Stage != "thinking" {
				continue
			}
			for _, call := range adapter.calls {
				if call.ID == ev.Tool.CallID && argsDigestOrFallback(call.Name, call.Arguments) != ev.Tool.ArgsDigest {
					t.Fatal("progress args digest does not match original tool call")
				}
			}
			seen[ev.Tool.CallID] = progress.ID
		case <-time.After(10 * time.Second):
			t.Fatal("two live progress rows were not emitted while first result was pending")
		}
	}
	if seen["source-a"] == "" || seen["source-a"] == seen["source-b"] {
		t.Fatalf("actual IDs not separate: %v", seen)
	}
	close(adapter.release)
	released = true
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("stream did not finish after both reports")
	}
	completed := map[string]bool{}
	for len(events) > 0 {
		ev := <-events
		if ev.Type == bridge.EventToolCompleted && ev.Tool != nil && ev.Tool.Name == "subagent.spawn" {
			if !json.Valid([]byte(ev.Tool.Summary)) || !strings.Contains(ev.Tool.Summary, seen[ev.Tool.CallID]) {
				t.Fatalf("terminal display lost identity: %s", ev.Tool.Summary)
			}
			completed[ev.Tool.CallID] = true
		}
	}
	if len(completed) != 2 {
		t.Fatalf("terminal rows=%v", completed)
	}
}
