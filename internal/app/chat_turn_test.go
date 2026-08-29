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
	"github.com/lunitide/lunitide/internal/domain/queueinput"
	"github.com/lunitide/lunitide/internal/gateway"
	"github.com/lunitide/lunitide/internal/queueapp"
	"github.com/lunitide/lunitide/internal/toolruntime"
)

func TestLooksLikeResume(t *testing.T) {
	if !looksLikeResume(resumeUserPrompt) || !looksLikeResume("继续") || looksLikeResume("帮我安装技能") {
		t.Fatal("resume detector mismatch")
	}
	if !looksLikeIndependentRequest("帮我打开桌面协议的文件") || !looksLikeIndependentRequest("打开协议") {
		t.Fatal("new tasks must stay independent")
	}
	if looksLikeIndependentRequest("只要 arkcli 相关的技能") || looksLikeIndependentRequest("继续") {
		t.Fatal("clarifications and resume are not independent tasks")
	}
	if !looksLikeStatusFollowUp("做好了没有") || !looksLikeStatusFollowUp("还要多久？") || !looksLikeStatusFollowUp("进度") {
		t.Fatal("status follow-ups must attach to the in-flight task")
	}
	if looksLikeIndependentRequest("做好了没有") || looksLikeIndependentRequest("改方案用深色封面") {
		t.Fatal("status and steer must not start a new task")
	}
	if closedLoopTurnInjection("做好了没有") != "" {
		t.Fatal("status follow-ups must not close the previous loop")
	}
	if closedLoopTurnInjection("继续") != "" {
		t.Fatal("resume must not add closed-loop scope")
	}
	if !strings.Contains(closedLoopTurnInjection("帮我打开协议"), "本轮范围") {
		t.Fatal("new turns must close the previous loop")
	}
}

type memQueueStore struct {
	mu    sync.Mutex
	items []queueinput.Message
}

func (s *memQueueStore) SessionExists(context.Context, string) (bool, error) { return true, nil }
func (s *memQueueStore) EnqueueQueuedMessage(context.Context, string, string, string, string, string) (queueinput.Message, error) {
	return queueinput.Message{}, errors.New("unused")
}
func (s *memQueueStore) GetQueuedByRequest(context.Context, string, string) (queueinput.Message, error) {
	return queueinput.Message{}, nil
}
func (s *memQueueStore) CountQueued(context.Context, string) (int, error) { return 0, nil }
func (s *memQueueStore) CountQueuedSince(context.Context, string, time.Time) (int, error) {
	return 0, nil
}
func (s *memQueueStore) ListQueued(context.Context, string) ([]queueinput.Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := append([]queueinput.Message(nil), s.items...)
	return out, nil
}
func (s *memQueueStore) WithdrawQueuedMessage(context.Context, string, string) (queueinput.Message, error) {
	return queueinput.Message{}, errors.New("unused")
}
func (s *memQueueStore) ConsumeQueuedMessages(context.Context, string) ([]queueinput.Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.items
	s.items = nil
	return out, nil
}
func (s *memQueueStore) push(text string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items = append(s.items, queueinput.Message{Payload: text, Status: queueinput.StatusQueued, Seq: int64(len(s.items) + 1)})
}

type queueInjectAdapter struct {
	store          *memQueueStore
	calls          int
	sawSupplement  bool
	sawIndependent bool
}

func (a *queueInjectAdapter) Complete(context.Context, []byte, gateway.Request) (gateway.Response, error) {
	return gateway.Response{}, errors.New("not used")
}
func (a *queueInjectAdapter) Discover(context.Context, []byte) (gateway.Discovery, error) {
	return gateway.Discovery{}, errors.New("not used")
}
func (a *queueInjectAdapter) Stream(_ context.Context, _ []byte, req gateway.Request, emit func(gateway.Delta) error) (gateway.Response, error) {
	a.calls++
	for _, m := range req.Messages {
		if strings.Contains(m.Content, "任务进行中补充") && strings.Contains(m.Content, "只要 arkcli") {
			a.sawSupplement = true
		}
		if strings.Contains(m.Content, "任务进行中补充") && strings.Contains(m.Content, "打开") {
			a.sawIndependent = true
		}
	}
	if a.calls == 1 {
		a.store.push("只要 arkcli 相关的技能")
		return gateway.Response{Message: gateway.Message{Role: gateway.RoleAssistant, ToolCalls: []gateway.ToolCall{
			{ID: "call-search", Name: "mcp.search", Arguments: []byte(`{"query":"skills"}`)},
		}}}, nil
	}
	if err := emit(gateway.Delta{Text: "已结合补充说明继续安装。"}); err != nil {
		return gateway.Response{}, err
	}
	return gateway.Response{Message: gateway.Message{Role: gateway.RoleAssistant, Content: "已结合补充说明继续安装。"}}, nil
}

func TestRunStreamInjectsQueuedSupplementsMidTurn(t *testing.T) {
	store := &memQueueStore{}
	adapter := &queueInjectAdapter{store: store}
	e := NewEngineWithGateway(nil, "test", streamTestLease{})
	e.SetQueueService(queueapp.New(store))
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (gateway.Adapter, error) { return adapter, nil })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	state := &streamState{cancel: cancel, state: streamRunning}
	id := "stream-queue-inject"
	e.streams[id] = state
	e.runStream(ctx, id, state, provider.Provider{ID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", Protocol: provider.ProtocolOpenAICompatible, BaseURL: "https://api.example.com", CredentialRef: "credential-ref"}, gateway.Request{Model: "m"}, func(bridge.Event) error { return nil }, "01ARZ3NDEKTSV4RRFFQ69G5FAV")
	if adapter.calls < 2 {
		t.Fatalf("calls = %d, want at least 2", adapter.calls)
	}
	if !adapter.sawSupplement {
		t.Fatal("queued supplement was not injected into the in-flight turn")
	}
}

func TestRunStreamInjectsStatusFollowUp(t *testing.T) {
	store := &memQueueStore{}
	adapter := &dropUIAdapter{}
	e := NewEngineWithGateway(nil, "test", streamTestLease{})
	e.SetQueueService(queueapp.New(store))
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (gateway.Adapter, error) { return adapter, nil })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	state := &streamState{cancel: cancel, state: streamRunning}
	id := "stream-queue-status"
	e.streams[id] = state
	store.push("做好了没有")
	var sawMerge, sawThinking bool
	e.runStream(ctx, id, state, provider.Provider{ID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", Protocol: provider.ProtocolOpenAICompatible, BaseURL: "https://api.example.com", CredentialRef: "credential-ref"}, gateway.Request{Model: "m"}, func(event bridge.Event) error {
		if event.Delta != nil && strings.Contains(event.Delta.Text, "已并入你刚才补充的说明") {
			sawMerge = true
		}
		if event.Thinking != nil && strings.Contains(event.Thinking.Text, "继续当前任务") {
			sawThinking = true
		}
		return nil
	}, "01ARZ3NDEKTSV4RRFFQ69G5FAV")
	if !sawMerge || !sawThinking {
		t.Fatalf("status follow-up must attach without wiping thinking: merge=%v thinking=%v", sawMerge, sawThinking)
	}
	left, err := store.ListQueued(context.Background(), "01ARZ3NDEKTSV4RRFFQ69G5FAV")
	if err != nil || len(left) != 0 {
		t.Fatalf("status follow-up should be consumed: %+v err=%v", left, err)
	}
}

func TestRunStreamDoesNotMergeIndependentQueuedRequest(t *testing.T) {
	store := &memQueueStore{}
	adapter := &dropUIAdapter{}
	e := NewEngineWithGateway(nil, "test", streamTestLease{})
	e.SetQueueService(queueapp.New(store))
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (gateway.Adapter, error) { return adapter, nil })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	state := &streamState{cancel: cancel, state: streamRunning}
	id := "stream-queue-independent"
	e.streams[id] = state
	store.push("帮我打开桌面协议的文件我要查看")
	var sawMerge bool
	e.runStream(ctx, id, state, provider.Provider{ID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", Protocol: provider.ProtocolOpenAICompatible, BaseURL: "https://api.example.com", CredentialRef: "credential-ref"}, gateway.Request{Model: "m"}, func(event bridge.Event) error {
		if event.Delta != nil && strings.Contains(event.Delta.Text, "已并入你刚才补充的说明") {
			sawMerge = true
		}
		return nil
	}, "01ARZ3NDEKTSV4RRFFQ69G5FAV")
	if sawMerge {
		t.Fatal("independent queued task must not merge into the current turn")
	}
	left, err := store.ListQueued(context.Background(), "01ARZ3NDEKTSV4RRFFQ69G5FAV")
	if err != nil || len(left) != 1 || !strings.Contains(left[0].Payload, "协议") {
		t.Fatalf("independent request should remain queued: %+v err=%v", left, err)
	}
}

type dropUIAdapter struct{ calls int }

func (a *dropUIAdapter) Complete(context.Context, []byte, gateway.Request) (gateway.Response, error) {
	return gateway.Response{}, errors.New("not used")
}
func (a *dropUIAdapter) Discover(context.Context, []byte) (gateway.Discovery, error) {
	return gateway.Discovery{}, errors.New("not used")
}
func (a *dropUIAdapter) Stream(_ context.Context, _ []byte, _ gateway.Request, emit func(gateway.Delta) error) (gateway.Response, error) {
	a.calls++
	if a.calls == 1 {
		return gateway.Response{Message: gateway.Message{Role: gateway.RoleAssistant, ToolCalls: []gateway.ToolCall{
			{ID: "call-search", Name: "mcp.search", Arguments: []byte(`{"query":"skills"}`)},
		}}}, nil
	}
	_ = emit(gateway.Delta{Text: "工具已跑完。"})
	return gateway.Response{Message: gateway.Message{Role: gateway.RoleAssistant, Content: "工具已跑完。"}}, nil
}

func TestRunStreamKeepsWorkingWhenUIDisconnects(t *testing.T) {
	adapter := &dropUIAdapter{}
	e := NewEngineWithGateway(nil, "test", streamTestLease{})
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (gateway.Adapter, error) { return adapter, nil })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	state := &streamState{cancel: cancel, state: streamRunning}
	id := "stream-ui-drop"
	e.streams[id] = state
	e.runStream(ctx, id, state, provider.Provider{ID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", Protocol: provider.ProtocolOpenAICompatible, BaseURL: "https://api.example.com", CredentialRef: "credential-ref"}, gateway.Request{Model: "m"}, func(bridge.Event) error {
		return errors.New("ui gone")
	}, "01ARZ3NDEKTSV4RRFFQ69G5FAV")
	if adapter.calls < 2 {
		t.Fatalf("disconnected UI aborted the turn: calls=%d", adapter.calls)
	}
}

func TestUnfinishedTurnInjectionUsesCheckpoint(t *testing.T) {
	tools, err := toolruntime.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer tools.Close()
	e := NewEngineWithGateway(nil, "test", streamTestLease{})
	e.SetToolRuntime(tools)
	session := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	e.saveTurnCheckpoint(session, chatTurnCheckpoint{Status: turnStatusInterrupted, Goal: "把目录里的技能都装上", Injected: []string{"只要 arkcli"}})
	got := e.unfinishedTurnInjection(session, resumeUserPrompt)
	if !strings.Contains(got, "把目录里的技能都装上") || !strings.Contains(got, "只要 arkcli") {
		t.Fatalf("injection missing checkpoint: %q", got)
	}
	if e.unfinishedTurnInjection(session, "新开一个话题") != "" {
		t.Fatal("non-resume turns must not force the old task")
	}
}
