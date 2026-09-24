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
	"github.com/lunitide/lunitide/internal/llmadapter"
	"github.com/lunitide/lunitide/internal/toolruntime"
)

// glm-5.3 rejects each fresh screenshot with HTTP 400 and accepts the same
// turn once the pixels are removed. A second capture in the same turn must
// get that recovery again.
type screenshotRejectAdapter struct {
	mu           sync.Mutex
	imageRejects int
	bareCalls    int
}

func (a *screenshotRejectAdapter) Complete(context.Context, []byte, llmadapter.Request) (llmadapter.Response, error) {
	return llmadapter.Response{}, nil
}
func (a *screenshotRejectAdapter) Discover(context.Context, []byte) (llmadapter.Discovery, error) {
	return llmadapter.Discovery{}, nil
}
func (a *screenshotRejectAdapter) Stream(_ context.Context, _ []byte, req llmadapter.Request, emit func(llmadapter.Delta) error) (llmadapter.Response, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(req.Images) > 0 {
		a.imageRejects++
		return llmadapter.Response{}, &llmadapter.Error{Code: "HTTP_400", Stage: llmadapter.StageHTTP, HTTPStatus: 400, Message: "Bad Request: image rejected"}
	}
	a.bareCalls++
	switch a.bareCalls {
	case 1:
		return llmadapter.Response{Message: llmadapter.Message{Role: llmadapter.RoleAssistant, ToolCalls: []llmadapter.ToolCall{{
			ID: "shot-1", Name: "computer.act", Arguments: json.RawMessage(`{"action":"screenshot"}`),
		}}}}, nil
	case 2:
		return llmadapter.Response{Message: llmadapter.Message{Role: llmadapter.RoleAssistant, ToolCalls: []llmadapter.ToolCall{{
			ID: "shot-2", Name: "computer.act", Arguments: json.RawMessage(`{"action":"screenshot","target":"foreground"}`),
		}}}}, nil
	default:
		if err := emit(llmadapter.Delta{Text: "页面已确认，继续补齐"}); err != nil {
			return llmadapter.Response{}, err
		}
		return llmadapter.Response{Message: llmadapter.Message{Role: llmadapter.RoleAssistant, Content: "页面已确认，继续补齐"}}, nil
	}
}

func TestSecondScreenshotHTTP400StillContinues(t *testing.T) {
	adapter := &screenshotRejectAdapter{}
	e := NewEngineWithGateway(nil, "test", streamTestLease{})
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (llmadapter.Adapter, error) {
		return adapter, nil
	})
	shots := 0
	e.toolExecHook = func(_ context.Context, _ executionMode, _, name string, _ json.RawMessage) (toolruntime.Result, error) {
		if name != "computer.act" {
			return toolruntime.Result{Output: "ok:true"}, nil
		}
		shots++
		return toolruntime.Result{
			Output:     "captured foreground",
			VisionMIME: "image/png",
			VisionData: []byte("png-frame"),
		}, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	state := &streamState{cancel: cancel, state: streamRunning}
	id := "stream-second-screenshot"
	e.streams[id] = state
	var mu sync.Mutex
	var text strings.Builder
	var terminal bridge.EventType
	done := make(chan struct{})
	go func() {
		e.runStream(ctx, id, state, provider.Provider{
			ID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", Protocol: provider.ProtocolOpenAICompatible,
			BaseURL: "https://api.example.com", CredentialRef: "credential-ref",
		}, llmadapter.Request{
			Model:    "glm-5.3",
			Messages: []llmadapter.Message{{Role: llmadapter.RoleUser, Content: "继续执行完成把少的缺的补上"}},
		}, func(event bridge.Event) error {
			mu.Lock()
			defer mu.Unlock()
			if event.Type == bridge.EventDelta && event.Delta != nil {
				text.WriteString(event.Delta.Text)
			}
			if event.Type == bridge.EventFailed && event.Error != nil {
				text.WriteString(event.Error.Message)
			}
			if event.Type == bridge.EventCompleted || event.Type == bridge.EventFailed {
				terminal = event.Type
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
	got := text.String()
	kind := terminal
	mu.Unlock()
	adapter.mu.Lock()
	rejects := adapter.imageRejects
	adapter.mu.Unlock()
	if shots < 2 || rejects < 2 {
		t.Fatalf("shots=%d imageRejects=%d, second capture never reached the model", shots, rejects)
	}
	if kind != bridge.EventCompleted || !strings.Contains(got, "页面已确认，继续补齐") || strings.Contains(got, "供应商拒绝了请求") {
		t.Fatalf("terminal=%s text=%q", kind, got)
	}
}
