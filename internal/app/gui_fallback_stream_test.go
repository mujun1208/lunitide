package app

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/gateway"
	"github.com/lunitide/lunitide/internal/toolruntime"
)

type guiStreamAdapter struct {
	mu    sync.Mutex
	round int
	failTwice bool
}

func (a *guiStreamAdapter) Complete(context.Context, []byte, gateway.Request) (gateway.Response, error) {
	return gateway.Response{}, nil
}
func (a *guiStreamAdapter) Discover(context.Context, []byte) (gateway.Discovery, error) {
	return gateway.Discovery{}, nil
}
func (a *guiStreamAdapter) Stream(_ context.Context, _ []byte, _ gateway.Request, emit func(gateway.Delta) error) (gateway.Response, error) {
	a.mu.Lock()
	a.round++
	round := a.round
	a.mu.Unlock()
	if round == 1 || (a.failTwice && round == 2) {
		return gateway.Response{Message: gateway.Message{Role: gateway.RoleAssistant, ToolCalls: []gateway.ToolCall{
			{ID: "c-act", Name: "computer.act", Arguments: json.RawMessage(`{"action":"click","id":"B1"}`)},
			{ID: "c-web", Name: "web.search", Arguments: json.RawMessage(`{"query":"x"}`)},
		}}}, nil
	}
	if err := emit(gateway.Delta{Text: "done"}); err != nil {
		return gateway.Response{}, err
	}
	return gateway.Response{Message: gateway.Message{Content: "done"}}, nil
}

func runGUIStream(t *testing.T, route TaskRoute, failTwice bool, hookResult toolruntime.Result) (hookCalls int, events []bridge.Event) {
	t.Helper()
	adapter := &guiStreamAdapter{failTwice: failTwice}
	e := NewEngineWithGateway(nil, "test", streamTestLease{})
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (gateway.Adapter, error) {
		return adapter, nil
	})
	e.toolExecHook = func(_ context.Context, _ executionMode, _, name string, _ json.RawMessage) (toolruntime.Result, error) {
		if name == "computer.act" {
			return toolruntime.Result{Output: "ok:false\n无法执行"}, nil
		}
		return toolruntime.Result{Output: "ok:true\nsearch"}, nil
	}
	var hookMu sync.Mutex
	e.guiFallbackHook = func(context.Context, executionMode, string, string, string, *streamState, []gateway.Image, bool, bool, bool) (toolruntime.Result, json.RawMessage, bool) {
		hookMu.Lock()
		hookCalls++
		hookMu.Unlock()
		return hookResult, json.RawMessage(`{"action":"click","id":"B1"}`), true
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	state := &streamState{cancel: cancel, state: streamRunning, taskRoute: route}
	id := "stream-gui"
	e.streams[id] = state
	var mu sync.Mutex
	done := make(chan struct{})
	go func() {
		e.runStream(ctx, id, state, provider.Provider{
			ID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", Protocol: provider.ProtocolOpenAICompatible,
			BaseURL: "https://api.example.com", CredentialRef: "credential-ref",
		}, gateway.Request{Model: "m"}, func(event bridge.Event) error {
			mu.Lock()
			events = append(events, event)
			mu.Unlock()
			if event.Type == bridge.EventCompleted || event.Type == bridge.EventFailed {
				close(done)
			}
			return nil
		}, "", executionModeFullAccess)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out")
	}
	mu.Lock()
	out := append([]bridge.Event(nil), events...)
	mu.Unlock()
	hookMu.Lock()
	calls := hookCalls
	hookMu.Unlock()
	return calls, out
}

func TestGUIFallbackStreamBatchKeepsDesktopFail(t *testing.T) {
	calls, events := runGUIStream(t, RouteR2, false, toolruntime.Result{Output: "clicked B1"})
	if calls != 1 {
		t.Fatalf("D-batch: hook calls=%d want 1", calls)
	}
	var sawGUI bool
	for _, ev := range events {
		if ev.Type == bridge.EventToolCompleted && ev.Tool != nil && ev.Tool.CallID != "" && strings.HasPrefix(ev.Tool.CallID, "gui-") {
			if !strings.Contains(ev.Tool.Summary, "clicked B1") {
				t.Fatalf("success writeback = %q", ev.Tool.Summary)
			}
			sawGUI = true
		}
	}
	if !sawGUI {
		t.Fatal("fallback ToolCompleted missing")
	}
}

func TestGUIFallbackStreamR1DoesNotEnter(t *testing.T) {
	calls, _ := runGUIStream(t, RouteR1, false, toolruntime.Result{Output: "clicked B1"})
	if calls != 0 {
		t.Fatalf("D-D5 R1 hook calls=%d", calls)
	}
}

func TestGUIFallbackStreamOncePerTurn(t *testing.T) {
	calls, _ := runGUIStream(t, RouteR2, true, toolruntime.Result{Output: "clicked B1"})
	if calls != 1 {
		t.Fatalf("本轮只一次: hook calls=%d", calls)
	}
}

func TestGUIFallbackStreamSomFailWritesBack(t *testing.T) {
	_, events := runGUIStream(t, RouteR2, false, guiFallbackFailResult("屏幕读号不是合法 markId/坐标"))
	var saw bool
	for _, ev := range events {
		if ev.Type == bridge.EventToolCompleted && ev.Tool != nil && strings.HasPrefix(ev.Tool.CallID, "gui-") {
			if !strings.Contains(ev.Tool.Summary, "ok:false") || !strings.Contains(ev.Tool.Summary, "无法执行") {
				t.Fatalf("fail writeback = %q", ev.Tool.Summary)
			}
			saw = true
		}
	}
	if !saw {
		t.Fatal("SoM fail must still emit ToolCompleted")
	}
}
