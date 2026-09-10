package app

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/llmadapter"
	"github.com/lunitide/lunitide/internal/messageapp"
	"github.com/lunitide/lunitide/internal/toolruntime"
	"github.com/oklog/ulid/v2"
)

func TestGenerationUsageCountsEachCallOnceAndMarksMixedUnknown(t *testing.T) {
	b := turnGenerationBudget{}
	u := llmadapter.Usage{InputTokens: 100, OutputTokens: 5, TotalTokens: 105, CachedInputTokens: 80, CacheWriteInputTokens: 10, CacheUsageReported: true}
	a := budgetAdapter{run: func(_ context.Context, emit func(llmadapter.Delta) error) (llmadapter.Response, error) {
		for range 3 {
			if err := emit(llmadapter.Delta{Usage: &u}); err != nil {
				return llmadapter.Response{}, err
			}
		}
		return llmadapter.Response{Usage: u}, nil
	}}
	for range 2 {
		if _, err := b.stream(context.Background(), a, nil, llmadapter.Request{}, func(llmadapter.Delta) error { return nil }); err != nil {
			t.Fatal(err)
		}
	}
	want := llmadapter.Usage{InputTokens: 200, OutputTokens: 10, TotalTokens: 210, CachedInputTokens: 160, CacheWriteInputTokens: 20, CacheUsageReported: true}
	if got := b.usageSnapshot(); got != want {
		t.Fatalf("usage delta/result double counted: %+v", got)
	}
	a.run = func(_ context.Context, emit func(llmadapter.Delta) error) (llmadapter.Response, error) {
		u := llmadapter.Usage{InputTokens: 20, OutputTokens: 2, TotalTokens: 22}
		_ = emit(llmadapter.Delta{Usage: &u})
		return llmadapter.Response{}, errors.New("connection interrupted")
	}
	if _, err := b.stream(context.Background(), a, nil, llmadapter.Request{}, func(llmadapter.Delta) error { return nil }); err == nil {
		t.Fatal("error swallowed")
	}
	want.InputTokens += 20
	want.OutputTokens += 2
	want.TotalTokens += 22
	want.CacheUsageReported = false
	if got := b.usageSnapshot(); got != want {
		t.Fatalf("partial error or unknown cache call lost: %+v", got)
	}
}

func TestGenerationUsageAggregatesConcurrentCouncilCalls(t *testing.T) {
	b := &turnGenerationBudget{}
	u := llmadapter.Usage{InputTokens: 100, OutputTokens: 4, TotalTokens: 104, CachedInputTokens: 60, CacheWriteInputTokens: 20, CacheUsageReported: true}
	a := turnBudgetAdapter{budget: b, Adapter: budgetAdapter{run: func(_ context.Context, emit func(llmadapter.Delta) error) (llmadapter.Response, error) {
		if err := emit(llmadapter.Delta{Usage: &u}); err != nil {
			return llmadapter.Response{}, err
		}
		return llmadapter.Response{Usage: u}, nil
	}}}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			if _, err := a.Complete(context.Background(), nil, llmadapter.Request{}); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	want := llmadapter.Usage{InputTokens: 800, OutputTokens: 32, TotalTokens: 832, CachedInputTokens: 480, CacheWriteInputTokens: 160, CacheUsageReported: true}
	if got := b.usageSnapshot(); got != want {
		t.Fatalf("concurrent council lost usage: %+v", got)
	}
}

func TestChatToolLoopEmitsOneAggregateUsageAfterBothModelCalls(t *testing.T) {
	e := NewEngine(nil, "test")
	e.messages = &appendAssistantSpy{}
	e.leases = streamTestLease{}
	e.SetChatTurnJournal(&budgetTestJournal{})
	runtime, err := toolruntime.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { runtime.Close() })
	e.SetToolRuntime(runtime)
	calls := 0
	a := budgetAdapter{run: func(_ context.Context, emit func(llmadapter.Delta) error) (llmadapter.Response, error) {
		calls++
		u := llmadapter.Usage{InputTokens: 10 * calls, OutputTokens: 2, TotalTokens: 10*calls + 2, CachedInputTokens: 5 * calls, CacheUsageReported: true}
		for range 2 {
			if err := emit(llmadapter.Delta{Usage: &u}); err != nil {
				return llmadapter.Response{Usage: u}, err
			}
		}
		if calls == 1 {
			return llmadapter.Response{Usage: u, Message: llmadapter.Message{Role: llmadapter.RoleAssistant, ToolCalls: []llmadapter.ToolCall{{ID: "inspect-local", Name: "workspace.list", Arguments: []byte(`{"path":"."}`)}}}}, nil
		}
		if calls > 2 {
			t.Fatal("unexpected extra model pass")
		}
		if err := emit(llmadapter.Delta{Text: "已检查完成。"}); err != nil {
			return llmadapter.Response{Usage: u}, err
		}
		return llmadapter.Response{Usage: u, Message: llmadapter.Message{Role: llmadapter.RoleAssistant, Content: "已检查完成。"}}, nil
	}}
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (llmadapter.Adapter, error) { return a, nil })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	id := ulid.Make().String()
	state := &streamState{cancel: cancel, state: streamRunning, sessionID: chatAttachmentSessionID}
	e.streams[id] = state
	var usages []bridge.UsageEvent
	var terminal bridge.Event
	toolSeen := false
	req := llmadapter.Request{Model: "model", Tools: engineToolDefinitions(), Messages: []llmadapter.Message{{Role: llmadapter.RoleUser, Content: "检查工作目录内容"}}}
	e.runStream(ctx, id, state, provider.Provider{ID: chatAttachmentProviderID, Protocol: provider.ProtocolOpenAICompatible, BaseURL: "https://example.test", CredentialRef: "credential-ref"}, req, func(ev bridge.Event) error {
		terminal = ev
		if ev.Usage != nil {
			usages = append(usages, *ev.Usage)
		}
		if ev.Type == bridge.EventToolCompleted {
			toolSeen = true
		}
		return nil
	}, chatAttachmentSessionID, executionModeFullAccess)
	want := bridge.UsageEvent{InputTokens: 30, OutputTokens: 4, TotalTokens: 34, CachedInputTokens: 15, CacheUsageReported: true}
	if calls != 2 || !toolSeen || len(usages) != 1 || usages[0] != want || terminal.Type != bridge.EventCompleted {
		t.Fatalf("tool loop usage: calls=%d tool=%t usages=%+v terminal=%+v", calls, toolSeen, usages, terminal)
	}
}

func TestChatToolLoopPersistsAggregateUsage(t *testing.T) {
	e := NewEngine(nil, "test")
	spy := &assistantUsageSpy{}
	e.messages = spy
	e.leases = streamTestLease{}
	e.SetChatTurnJournal(&budgetTestJournal{})
	runtime, err := toolruntime.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { runtime.Close() })
	e.SetToolRuntime(runtime)
	calls := 0
	a := budgetAdapter{run: func(_ context.Context, emit func(llmadapter.Delta) error) (llmadapter.Response, error) {
		calls++
		u := llmadapter.Usage{InputTokens: 10 * calls, OutputTokens: 2, TotalTokens: 10*calls + 2, CachedInputTokens: 5 * calls, CacheUsageReported: true}
		if calls == 1 {
			return llmadapter.Response{Usage: u, Message: llmadapter.Message{Role: llmadapter.RoleAssistant, ToolCalls: []llmadapter.ToolCall{{ID: "inspect-local", Name: "workspace.list", Arguments: []byte(`{"path":"."}`)}}}}, nil
		}
		if err := emit(llmadapter.Delta{Text: "已检查完成。"}); err != nil {
			return llmadapter.Response{Usage: u}, err
		}
		return llmadapter.Response{Usage: u, Message: llmadapter.Message{Role: llmadapter.RoleAssistant, Content: "已检查完成。"}}, nil
	}}
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (llmadapter.Adapter, error) { return a, nil })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	id := ulid.Make().String()
	state := &streamState{cancel: cancel, state: streamRunning, sessionID: chatAttachmentSessionID}
	e.streams[id] = state
	req := llmadapter.Request{Model: "model", Tools: engineToolDefinitions(), Messages: []llmadapter.Message{{Role: llmadapter.RoleUser, Content: "检查工作目录内容"}}}
	e.runStream(ctx, id, state, provider.Provider{ID: chatAttachmentProviderID, Protocol: provider.ProtocolOpenAICompatible, BaseURL: "https://example.test", CredentialRef: "credential-ref"}, req, func(bridge.Event) error { return nil }, chatAttachmentSessionID, executionModeFullAccess)
	want := messageapp.AssistantUsage{Provider: "openai_compatible", Model: "model", InputTokens: 30, OutputTokens: 4, CachedInputTokens: 15, CacheUsageReported: true}
	if spy.usage != want {
		t.Fatalf("persist used last stream frame, not turn aggregate: %+v", spy.usage)
	}
}

func TestChatUsageSurvivesFailedAndCancelledTurnsWithoutChangingTerminal(t *testing.T) {
	for _, kind := range []string{"failed", "cancelled", "unreported"} {
		t.Run(kind, func(t *testing.T) {
			e := NewEngine(nil, "test")
			e.messages = &appendAssistantSpy{}
			e.leases = streamTestLease{}
			e.SetChatTurnJournal(&budgetTestJournal{})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			id := ulid.Make().String()
			state := &streamState{cancel: cancel, state: streamRunning, sessionID: chatAttachmentSessionID}
			e.streams[id] = state
			u := llmadapter.Usage{InputTokens: 50, OutputTokens: 4, TotalTokens: 54, CachedInputTokens: 30, CacheUsageReported: true}
			a := budgetAdapter{run: func(_ context.Context, emit func(llmadapter.Delta) error) (llmadapter.Response, error) {
				if kind != "unreported" {
					if err := emit(llmadapter.Delta{Usage: &u}); err != nil {
						return llmadapter.Response{}, err
					}
				}
				if kind == "cancelled" {
					if !e.cancelStream(id) {
						t.Error("cancellation refused")
					}
					return llmadapter.Response{}, context.Canceled
				}
				return llmadapter.Response{}, errors.New("upstream failed")
			}}
			e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (llmadapter.Adapter, error) { return a, nil })
			var events []bridge.Event
			e.runStream(ctx, id, state, provider.Provider{ID: chatAttachmentProviderID, Protocol: provider.ProtocolOpenAICompatible, BaseURL: "https://example.test", CredentialRef: "credential-ref"}, llmadapter.Request{Model: "model", Messages: []llmadapter.Message{{Role: llmadapter.RoleUser, Content: "你好"}}}, func(ev bridge.Event) error { events = append(events, ev); return nil }, chatAttachmentSessionID)
			wantTerminal := bridge.EventFailed
			if kind == "cancelled" {
				wantTerminal = bridge.EventCancelled
			}
			if len(events) == 0 || events[len(events)-1].Type != wantTerminal {
				t.Fatalf("terminal changed: %+v", events)
			}
			usages := 0
			for i, ev := range events {
				if ev.Usage != nil {
					usages++
					if i == len(events)-1 || ev.Usage.TotalTokens != 54 || ev.Usage.CachedInputTokens != 30 || !ev.Usage.CacheUsageReported {
						t.Fatalf("usage lost or emitted after terminal: %+v", ev)
					}
				}
			}
			if kind == "unreported" {
				if usages != 0 {
					t.Fatal("fabricated usage after provider omitted metadata")
				}
			} else if usages != 1 {
				t.Fatalf("want one received usage before %s: %+v", kind, events)
			}
		})
	}
}
