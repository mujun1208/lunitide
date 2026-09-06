package app

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/llmadapter"
	"github.com/oklog/ulid/v2"
)

type budgetAdapter struct {
	partialThenFailAdapter
	run func(context.Context, func(llmadapter.Delta) error) (llmadapter.Response, error)
}

type budgetTestJournal struct{ countingTurnJournal }

func (*budgetTestJournal) LatestChatTurn(context.Context, string) ([]byte, error) { return nil, nil }

func (a budgetAdapter) Stream(ctx context.Context, _ []byte, _ llmadapter.Request, emit func(llmadapter.Delta) error) (llmadapter.Response, error) {
	return a.run(ctx, emit)
}

func TestGenerationBudgetSpansPassesAndRejectsUnstreamedArguments(t *testing.T) {
	b := turnGenerationBudget{}
	a := budgetAdapter{run: func(_ context.Context, emit func(llmadapter.Delta) error) (llmadapter.Response, error) {
		return llmadapter.Response{}, emit(llmadapter.Delta{Reasoning: strings.Repeat("r", turnGenerationMaxBytes/2)})
	}}
	for range 2 {
		if _, err := b.stream(context.Background(), a, nil, llmadapter.Request{}, func(llmadapter.Delta) error { return nil }); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := b.stream(context.Background(), a, nil, llmadapter.Request{}, func(llmadapter.Delta) error { t.Fatal("budget exceeded callback"); return nil }); !errors.Is(err, errTurnGenerationBudget) {
		t.Fatal(err)
	}
	b = turnGenerationBudget{bytes: turnGenerationMaxBytes - 10}
	a.run = func(context.Context, func(llmadapter.Delta) error) (llmadapter.Response, error) {
		return llmadapter.Response{Message: llmadapter.Message{ToolCalls: []llmadapter.ToolCall{{ID: "tool", Name: "large", Arguments: []byte(strings.Repeat("x", 100))}}}}, nil
	}
	if _, err := b.stream(context.Background(), a, nil, llmadapter.Request{}, func(llmadapter.Delta) error { return nil }); !errors.Is(err, errTurnGenerationBudget) {
		t.Fatal(err)
	}
}

func TestGenerationTimeBudgetCancelsProvider(t *testing.T) {
	b := turnGenerationBudget{elapsed: turnGenerationMaxTime - time.Millisecond}
	a := budgetAdapter{run: func(ctx context.Context, _ func(llmadapter.Delta) error) (llmadapter.Response, error) {
		<-ctx.Done()
		return llmadapter.Response{}, ctx.Err()
	}}
	if _, err := b.stream(context.Background(), a, nil, llmadapter.Request{}, func(llmadapter.Delta) error { return nil }); !errors.Is(err, errTurnGenerationBudget) {
		t.Fatal(err)
	}
}

func TestCouncilPassesShareConcurrentGenerationBudget(t *testing.T) {
	b := &turnGenerationBudget{}
	a := turnBudgetAdapter{budget: b, Adapter: budgetAdapter{run: func(_ context.Context, emit func(llmadapter.Delta) error) (llmadapter.Response, error) {
		return llmadapter.Response{}, emit(llmadapter.Delta{Text: strings.Repeat("x", 300000)})
	}}}
	var wg sync.WaitGroup
	results := make(chan error, 3)
	for range 3 {
		wg.Go(func() { _, err := a.Complete(context.Background(), nil, llmadapter.Request{}); results <- err })
	}
	wg.Wait()
	close(results)
	failures := 0
	for err := range results {
		if errors.Is(err, errTurnGenerationBudget) {
			failures++
		} else if err != nil {
			t.Fatal(err)
		}
	}
	if failures != 2 || b.bytes != 300000 {
		t.Fatalf("shared council budget: rejected=%d bytes=%d", failures, b.bytes)
	}
}

func TestRunStreamBudgetPreservesAcceptedTextAndExplicitTerminal(t *testing.T) {
	e := NewEngine(nil, "test")
	spy := &appendAssistantSpy{}
	e.messages = spy
	e.leases = streamTestLease{}
	e.SetChatTurnJournal(&budgetTestJournal{})
	a := budgetAdapter{run: func(_ context.Context, emit func(llmadapter.Delta) error) (llmadapter.Response, error) {
		for range turnGenerationMaxBytes/4096 + 1 {
			if err := emit(llmadapter.Delta{Text: strings.Repeat("x", 4096)}); err != nil {
				return llmadapter.Response{}, err
			}
		}
		t.Fatal("provider kept generating")
		return llmadapter.Response{}, nil
	}}
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (llmadapter.Adapter, error) { return a, nil })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	id := ulid.Make().String()
	state := &streamState{cancel: cancel, state: streamRunning, sessionID: chatAttachmentSessionID}
	e.streams[id] = state
	var terminal bridge.Event
	e.runStream(ctx, id, state, provider.Provider{ID: chatAttachmentProviderID, Protocol: provider.ProtocolOpenAICompatible, BaseURL: "https://example.test", CredentialRef: "credential-ref"}, llmadapter.Request{Model: "model", Messages: []llmadapter.Message{{Role: llmadapter.RoleUser, Content: "你好"}}}, func(ev bridge.Event) error { terminal = ev; return nil }, chatAttachmentSessionID)
	if terminal.Type != bridge.EventFailed || terminal.Error == nil || terminal.Error.Code != "TURN_GENERATION_BUDGET_EXCEEDED" {
		t.Fatalf("terminal: %#v", terminal)
	}
	text := strings.Join(spy.calls, "")
	if !strings.HasPrefix(text, strings.Repeat("x", turnGenerationMaxBytes)) || !strings.Contains(text, turnGenerationBudgetNotice) {
		t.Fatalf("accepted text or budget notice missing (%d bytes)", len(text))
	}
}
