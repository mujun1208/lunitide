package app

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/contextapp"
	"github.com/lunitide/lunitide/internal/domain/message"
	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/domain/token"
	"github.com/lunitide/lunitide/internal/gateway"
	"github.com/lunitide/lunitide/internal/messageapp"
)

type assistantUsageSpy struct{ usage messageapp.AssistantUsage }

func (s *assistantUsageSpy) Append(context.Context, string, string, any, message.Message) (message.Message, error) {
	return message.Message{}, errors.New("not used")
}
func (s *assistantUsageSpy) AppendAssistant(_ context.Context, _, _, sessionID, text string, usage messageapp.AssistantUsage) (message.Message, error) {
	s.usage = usage
	return message.Message{ID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", SessionID: sessionID, Role: message.RoleAssistant, Status: message.StatusCompleted, Text: text, Sequence: 1}, nil
}
func (s *assistantUsageSpy) List(context.Context, messageapp.PageRequest) (messageapp.Page, error) {
	return messageapp.Page{}, errors.New("not used")
}

type assistantUsageAdapter struct{}

func (assistantUsageAdapter) Complete(context.Context, []byte, gateway.Request) (gateway.Response, error) {
	return gateway.Response{}, errors.New("not used")
}
func (assistantUsageAdapter) Discover(context.Context, []byte) (gateway.Discovery, error) {
	return gateway.Discovery{}, errors.New("not used")
}
func (assistantUsageAdapter) Stream(_ context.Context, _ []byte, _ gateway.Request, emit func(gateway.Delta) error) (gateway.Response, error) {
	if err := emit(gateway.Delta{Text: "answer"}); err != nil {
		return gateway.Response{}, err
	}
	return gateway.Response{Usage: gateway.Usage{InputTokens: 90, OutputTokens: 10, TotalTokens: 100}}, nil
}

type finalizationGateAdapter struct {
	upstreamReady chan struct{}
	release       chan struct{}
}

func (a finalizationGateAdapter) Complete(context.Context, []byte, gateway.Request) (gateway.Response, error) {
	return gateway.Response{}, errors.New("not used")
}
func (a finalizationGateAdapter) Discover(context.Context, []byte) (gateway.Discovery, error) {
	return gateway.Discovery{}, errors.New("not used")
}
func (a finalizationGateAdapter) Stream(_ context.Context, _ []byte, _ gateway.Request, emit func(gateway.Delta) error) (gateway.Response, error) {
	if err := emit(gateway.Delta{Text: "durable answer"}); err != nil {
		return gateway.Response{}, err
	}
	close(a.upstreamReady)
	<-a.release
	return gateway.Response{Usage: gateway.Usage{OutputTokens: 3, TotalTokens: 3}}, nil
}

type blockingAssistantWriter struct {
	mu      sync.Mutex
	started chan struct{}
	release chan struct{}
	calls   int
	err     error
}

func (w *blockingAssistantWriter) Append(context.Context, string, string, any, message.Message) (message.Message, error) {
	return message.Message{}, errors.New("not used")
}
func (w *blockingAssistantWriter) AppendAssistant(_ context.Context, _, _, sessionID, text string, _ messageapp.AssistantUsage) (message.Message, error) {
	w.mu.Lock()
	w.calls++
	w.mu.Unlock()
	close(w.started)
	<-w.release
	if w.err != nil {
		return message.Message{}, w.err
	}
	return message.Message{ID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", SessionID: sessionID, Role: message.RoleAssistant, Status: message.StatusCompleted, Text: text}, nil
}
func (w *blockingAssistantWriter) List(context.Context, messageapp.PageRequest) (messageapp.Page, error) {
	return messageapp.Page{}, errors.New("not used")
}
func (w *blockingAssistantWriter) callCount() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.calls
}

func runFinalizationTestStream(t *testing.T, writer MessageService, adapter gateway.Adapter) (*Engine, string, <-chan bridge.Event) {
	t.Helper()
	e := NewEngineWithGateway(nil, "test", streamTestLease{})
	e.messages = writer
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (gateway.Adapter, error) { return adapter, nil })
	ctx, cancel := context.WithCancel(context.Background())
	state := &streamState{cancel: cancel, state: streamRunning}
	id := "stream"
	e.streams[id] = state
	events := make(chan bridge.Event, 8)
	go e.runStream(ctx, id, state, provider.Provider{
		ID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", Protocol: provider.ProtocolOpenAICompatible,
		BaseURL: "https://api.example.com", CredentialRef: "credential-ref",
	}, gateway.Request{Model: "model"}, func(event bridge.Event) error { events <- event; return nil }, "01ARZ3NDEKTSV4RRFFQ69G5FAV")
	return e, id, events
}

func terminalEvent(t *testing.T, events <-chan bridge.Event) bridge.Event {
	t.Helper()
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	for {
		select {
		case event := <-events:
			if event.Type == bridge.EventCompleted || event.Type == bridge.EventCancelled || event.Type == bridge.EventFailed {
				return event
			}
		case <-timer.C:
			t.Fatal("timed out waiting for terminal event")
		}
	}
}

func waitForStreamCleanup(t *testing.T, e *Engine, id string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for {
		e.streamsMu.Lock()
		_, ok := e.streams[id]
		e.streamsMu.Unlock()
		if !ok {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("terminal stream retained after cleanup deadline")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestRunStreamCancelBeforeFinalizationClaimSkipsDurableAppend(t *testing.T) {
	writer := &blockingAssistantWriter{started: make(chan struct{}), release: make(chan struct{})}
	adapter := finalizationGateAdapter{upstreamReady: make(chan struct{}), release: make(chan struct{})}
	e, id, events := runFinalizationTestStream(t, writer, adapter)
	<-adapter.upstreamReady
	if !e.cancelStream(id) {
		t.Fatal("cancellation did not win before finalization claim")
	}
	close(adapter.release)
	if terminal := terminalEvent(t, events); terminal.Type != bridge.EventCancelled {
		t.Fatalf("terminal=%s want cancelled", terminal.Type)
	}
	if calls := writer.callCount(); calls != 0 {
		t.Fatalf("durable appends=%d want 0", calls)
	}
	waitForStreamCleanup(t, e, id)
}

func TestRunStreamCancelAfterFinalizationClaimAgreesWithDurableAppend(t *testing.T) {
	writer := &blockingAssistantWriter{started: make(chan struct{}), release: make(chan struct{})}
	adapter := finalizationGateAdapter{upstreamReady: make(chan struct{}), release: make(chan struct{})}
	e, id, events := runFinalizationTestStream(t, writer, adapter)
	<-adapter.upstreamReady
	close(adapter.release)
	<-writer.started // AppendAssistant starts only after the finalization claim.
	if e.cancelStream(id) {
		t.Fatal("cancellation won while durable append was in flight")
	}
	close(writer.release)
	terminal := terminalEvent(t, events)
	if terminal.Type != bridge.EventCompleted || terminal.Completed == nil || terminal.Completed.MessageID == "" {
		t.Fatalf("terminal=%#v want completed with durable message id", terminal)
	}
	if calls := writer.callCount(); calls != 1 {
		t.Fatalf("durable appends=%d want 1", calls)
	}
	waitForStreamCleanup(t, e, id)
}

func TestRunStreamAppendFailureEmitsFailedAfterFinalizationClaim(t *testing.T) {
	writer := &blockingAssistantWriter{started: make(chan struct{}), release: make(chan struct{}), err: errors.New("write failed")}
	adapter := finalizationGateAdapter{upstreamReady: make(chan struct{}), release: make(chan struct{})}
	e, id, events := runFinalizationTestStream(t, writer, adapter)
	<-adapter.upstreamReady
	close(adapter.release)
	<-writer.started
	if e.cancelStream(id) {
		t.Fatal("cancellation won while failing durable append was in flight")
	}
	close(writer.release)
	terminal := terminalEvent(t, events)
	if terminal.Type != bridge.EventCompleted || terminal.Completed == nil || !terminal.Completed.PersistFailed {
		t.Fatalf("terminal=%#v want completed persistFailed", terminal)
	}
	waitForStreamCleanup(t, e, id)
}

func TestCombineDurableProviderMessagesOrdersAndValidatesFinalSequence(t *testing.T) {
	history := []contextapp.Message{
		{Role: "user", Content: "old question", TokenCount: 3},
		{Role: "assistant", Content: "old answer", TokenCount: 3},
	}
	explicit := []gateway.Message{
		{Role: gateway.RoleSystem, Content: "authoritative rules"},
		{Role: gateway.RoleUser, Content: "current request"},
	}
	got, err := combineDurableProviderMessages(history, explicit, contextapp.ProviderInfo{ContextWindow: 100, SafetyCeiling: 100})
	if err != nil {
		t.Fatal(err)
	}
	want := []gateway.Role{gateway.RoleSystem, gateway.RoleUser, gateway.RoleAssistant, gateway.RoleUser}
	if len(got) != len(want) {
		t.Fatalf("messages=%#v", got)
	}
	for i := range want {
		if got[i].Role != want[i] {
			t.Fatalf("role[%d]=%s want %s; messages=%#v", i, got[i].Role, want[i], got)
		}
	}
	if got[len(got)-1].Content != "current request" {
		t.Fatalf("current request not final: %#v", got)
	}

	_, err = combineDurableProviderMessages(
		[]contextapp.Message{{Role: "user", Content: "history", TokenCount: 1}, {Role: "assistant", Content: "restored answer", TokenCount: 1}},
		[]gateway.Message{{Role: gateway.RoleAssistant, Content: "invalid current assistant"}},
		contextapp.ProviderInfo{ContextWindow: 100},
	)
	if err == nil {
		t.Fatal("final combined sequence was not validated")
	}
}

func TestCombineDurableProviderMessagesBudgetsCurrentRequest(t *testing.T) {
	_, err := combineDurableProviderMessages(
		[]contextapp.Message{{Role: "user", Content: "12345678901234567890123456789012", TokenCount: 1}}, // exact 8; stale count must not be trusted
		[]gateway.Message{{Role: gateway.RoleUser, Content: "123456789012"}},                             // 3 tokens
		contextapp.ProviderInfo{ContextWindow: 10, SafetyCeiling: 10},
	)
	if !errors.Is(err, errCombinedContextOverBudget) {
		t.Fatalf("err=%v, want combined budget failure", err)
	}
}

func TestCombineDurableProviderMessagesReestimatesNormalizedFinalContent(t *testing.T) {
	history := "cafe\u0301\r\n12345678"
	explicit := "abcde"
	exact := token.EstimateTokens(history) + token.EstimateTokens(explicit)
	_, err := combineDurableProviderMessages(
		[]contextapp.Message{{Role: "user", Content: history, TokenCount: 1}},
		[]gateway.Message{{Role: gateway.RoleUser, Content: explicit}},
		contextapp.ProviderInfo{ContextWindow: exact - 1, SafetyCeiling: exact - 1},
	)
	if !errors.Is(err, errCombinedContextOverBudget) {
		t.Fatalf("err=%v, want exact normalized-content budget failure (exact=%d)", err, exact)
	}
}

func TestRunStreamAttributesOnlyOutputTokensToAssistant(t *testing.T) {
	spy := &assistantUsageSpy{}
	e := NewEngineWithGateway(nil, "test", streamTestLease{})
	e.messages = spy
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (gateway.Adapter, error) {
		return assistantUsageAdapter{}, nil
	})
	_, cancel := context.WithCancel(context.Background())
	state := &streamState{cancel: cancel, state: streamRunning}
	e.streams["stream"] = state
	e.runStream(context.Background(), "stream", state, provider.Provider{
		ID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", Protocol: provider.ProtocolOpenAICompatible,
		BaseURL: "https://api.example.com", CredentialRef: "credential-ref",
	}, gateway.Request{Model: "model"}, func(bridge.Event) error { return nil }, "01ARZ3NDEKTSV4RRFFQ69G5FAV")
	if spy.usage.OutputTokens != 10 {
		t.Fatalf("assistant usage=%+v, want outputTokens=10 (not request total 100)", spy.usage)
	}
}
