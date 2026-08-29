package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/message"
	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/gateway"
	"github.com/lunitide/lunitide/internal/messageapp"
)

type appendAssistantSpy struct {
	calls []string
}

func (s *appendAssistantSpy) Append(context.Context, string, string, any, message.Message) (message.Message, error) {
	return message.Message{}, errors.New("not used")
}
func (s *appendAssistantSpy) AppendAssistant(_ context.Context, _, _, sessionID, text string, _ messageapp.AssistantUsage) (message.Message, error) {
	s.calls = append(s.calls, text)
	return message.Message{ID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", SessionID: sessionID, Role: message.RoleAssistant, Status: message.StatusCompleted, Text: text, Sequence: 1}, nil
}
func (s *appendAssistantSpy) List(context.Context, messageapp.PageRequest) (messageapp.Page, error) {
	return messageapp.Page{}, errors.New("not used")
}

type partialThenFailAdapter struct{}

func (partialThenFailAdapter) Complete(context.Context, []byte, gateway.Request) (gateway.Response, error) {
	return gateway.Response{}, errors.New("not used")
}
func (partialThenFailAdapter) Discover(context.Context, []byte) (gateway.Discovery, error) {
	return gateway.Discovery{}, errors.New("not used")
}
func (partialThenFailAdapter) Stream(_ context.Context, _ []byte, _ gateway.Request, emit func(gateway.Delta) error) (gateway.Response, error) {
	if err := emit(gateway.Delta{Text: "已写好白羊座段落。"}); err != nil {
		return gateway.Response{}, err
	}
	if err := emit(gateway.Delta{Reasoning: "先规划十二星座结构。"}); err != nil {
		return gateway.Response{}, err
	}
	return gateway.Response{}, errors.New("upstream failed")
}

func TestAssistantTurnPersistTextIncludesThinking(t *testing.T) {
	got := assistantTurnPersistText("正文", "推理过程")
	if !strings.Contains(got, "【思考过程】") || !strings.Contains(got, "推理过程") || !strings.Contains(got, "正文") {
		t.Fatalf("persist text = %q", got)
	}
	if assistantTurnPersistText("", "只有思考") != "【思考过程】\n只有思考" {
		t.Fatal("thinking-only persist must survive")
	}
}

func TestShouldPersistAssistantTurn(t *testing.T) {
	if !shouldPersistAssistantTurn(nil, true, false) {
		t.Fatal("completed turn must persist when finalization claimed")
	}
	if shouldPersistAssistantTurn(errors.New("fail"), false, true) {
		t.Fatal("early cancel must not persist")
	}
	if !shouldPersistAssistantTurn(errors.New("fail"), false, false) {
		t.Fatal("failed turn with partial work must persist")
	}
}

func TestRunStreamFailurePersistsPartialAssistantText(t *testing.T) {
	spy := &appendAssistantSpy{}
	e := NewEngineWithGateway(nil, "test", streamTestLease{})
	e.messages = spy
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (gateway.Adapter, error) {
		return partialThenFailAdapter{}, nil
	})
	_, cancel := context.WithCancel(context.Background())
	state := &streamState{cancel: cancel, state: streamRunning}
	events := make(chan bridge.Event, 8)
	go e.runStream(context.Background(), "stream", state, provider.Provider{
		ID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", Protocol: provider.ProtocolOpenAICompatible,
		BaseURL: "https://api.example.com", CredentialRef: "credential-ref",
	}, gateway.Request{Model: "model"}, func(event bridge.Event) error { events <- event; return nil }, "01ARZ3NDEKTSV4RRFFQ69G5FAV")

	terminal := terminalEvent(t, events)
	if terminal.Type != bridge.EventFailed {
		t.Fatalf("terminal=%s want failed", terminal.Type)
	}
	if len(spy.calls) != 1 {
		t.Fatalf("durable appends=%d want 1", len(spy.calls))
	}
	if !strings.Contains(spy.calls[0], "已写好白羊座段落") {
		t.Fatalf("assistant body missing: %q", spy.calls[0])
	}
	if !strings.Contains(spy.calls[0], "【思考过程】") || !strings.Contains(spy.calls[0], "先规划十二星座结构") {
		t.Fatalf("thinking missing: %q", spy.calls[0])
	}
	if !strings.Contains(spy.calls[0], turnErrorNotice) {
		t.Fatalf("failure notice missing: %q", spy.calls[0])
	}
	waitForStreamCleanup(t, e, "stream")
}

func TestRunStreamCancelBeforeFinalizationStillSkipsPersist(t *testing.T) {
	spy := &appendAssistantSpy{}
	adapter := finalizationGateAdapter{upstreamReady: make(chan struct{}), release: make(chan struct{})}
	e, id, events := runFinalizationTestStream(t, spy, adapter)
	<-adapter.upstreamReady
	if !e.cancelStream(id) {
		t.Fatal("cancellation did not win before finalization claim")
	}
	close(adapter.release)
	if terminal := terminalEvent(t, events); terminal.Type != bridge.EventCancelled {
		t.Fatalf("terminal=%s want cancelled", terminal.Type)
	}
	if len(spy.calls) != 0 {
		t.Fatalf("durable appends=%d want 0", len(spy.calls))
	}
	waitForStreamCleanup(t, e, id)
}
