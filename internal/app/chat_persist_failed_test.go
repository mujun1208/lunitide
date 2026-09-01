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

func TestAssistantTurnPersistText(t *testing.T) {
	if got := assistantTurnPersistText("正文", "推理过程", false); got != "正文" {
		t.Fatalf("success persist = %q", got)
	}
	if got := assistantTurnPersistText("正文\n无法执行。", "推理过程", false); got != "正文\n无法执行。" {
		t.Fatalf("failed-with-body persist = %q", got)
	}
	if assistantTurnPersistText("", "只有思考", false) != "" {
		t.Fatal("success with empty body must not persist thinking")
	}
	if assistantTurnPersistText("", "只有思考", true) != "【思考过程】\n只有思考" {
		t.Fatal("thinking-only persist must survive")
	}
	if got := assistantTurnPersistText("无法执行。模型结果不完整，请重试。", "只有思考", true); got != "【思考过程】\n只有思考\n\n无法执行。模型结果不完整，请重试。" {
		t.Fatalf("thinking+notice persist = %q", got)
	}
	hello := "你好，我是月汐，可以帮你处理音乐、搜索、工作和电脑自动化。"
	if got := assistantTurnPersistText(hello, hello, true); got != hello {
		t.Fatalf("duplicate greeting persist = %q", got)
	}
}

func TestIsShortIdleGreeting(t *testing.T) {
	if !isShortIdleGreeting("你好") || !isShortIdleGreeting("在吗？") {
		t.Fatal("plain greetings must skip reasoning")
	}
	if isShortIdleGreeting("你好，帮我打开汽水") || isShortIdleGreeting("打开桌面汽水音乐") {
		t.Fatal("tool-shaped greetings must keep reasoning")
	}
}

func TestPersistFailureOnlyKeepsCompleted(t *testing.T) {
	if !persistFailureOnly(nil, errors.New("write failed"), "已写好正文") {
		t.Fatal("streamed text + persist error must be persist-only")
	}
	if persistFailureOnly(errors.New("upstream"), errors.New("write failed"), "已写好正文") {
		t.Fatal("upstream failure stays a failed turn")
	}
	if persistFailureOnly(nil, errors.New("write failed"), "") {
		t.Fatal("empty reply must not claim persist-only")
	}
	if selectPersistTerminal(false, nil, errors.New("write failed"), "已写好正文") != bridge.EventCompleted {
		t.Fatal("persist-only must complete")
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
	if strings.Contains(spy.calls[0], "【思考过程】") || strings.Contains(spy.calls[0], "先规划十二星座结构") {
		t.Fatalf("thinking must not persist when a reply body exists: %q", spy.calls[0])
	}
	if !strings.Contains(spy.calls[0], turnErrorNotice) {
		t.Fatalf("failure notice missing: %q", spy.calls[0])
	}
	waitForStreamCleanup(t, e, "stream")
}

type thinkingOnlyThenFailAdapter struct{}

func (thinkingOnlyThenFailAdapter) Complete(context.Context, []byte, gateway.Request) (gateway.Response, error) {
	return gateway.Response{}, errors.New("not used")
}
func (thinkingOnlyThenFailAdapter) Discover(context.Context, []byte) (gateway.Discovery, error) {
	return gateway.Discovery{}, errors.New("not used")
}
func (thinkingOnlyThenFailAdapter) Stream(_ context.Context, _ []byte, _ gateway.Request, emit func(gateway.Delta) error) (gateway.Response, error) {
	if err := emit(gateway.Delta{Reasoning: "先规划十二星座结构。"}); err != nil {
		return gateway.Response{}, err
	}
	return gateway.Response{}, errors.New("upstream failed")
}

func TestRunStreamFailurePersistsThinkingWhenAssistantEmpty(t *testing.T) {
	spy := &appendAssistantSpy{}
	e := NewEngineWithGateway(nil, "test", streamTestLease{})
	e.messages = spy
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (gateway.Adapter, error) {
		return thinkingOnlyThenFailAdapter{}, nil
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
	if !strings.Contains(spy.calls[0], "【思考过程】") || !strings.Contains(spy.calls[0], "先规划十二星座结构") {
		t.Fatalf("empty-reply failure must persist thinking: %q", spy.calls[0])
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
