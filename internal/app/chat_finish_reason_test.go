package app

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/llmadapter"
	"github.com/lunitide/lunitide/internal/messageapp"
	"github.com/lunitide/lunitide/internal/toolruntime"
)

type finishReasonFixtureAdapter struct {
	draftTrialAdapter
	reason   llmadapter.FinishReason
	partial  string
	finished bool
}

func (a *finishReasonFixtureAdapter) Stream(_ context.Context, _ []byte, req llmadapter.Request, emit func(llmadapter.Delta) error) (llmadapter.Response, error) {
	a.requests = append(a.requests, req)
	call := llmadapter.ToolCall{ID: "pending-call", Name: "workspace.write", Arguments: []byte(`{"path":"must-not-write.txt","content":"not authorized by an incomplete response"}`)}
	usage := llmadapter.Usage{InputTokens: 3, OutputTokens: 2, TotalTokens: 5}
	for _, delta := range []llmadapter.Delta{{Text: a.partial, Usage: &usage}, {ToolCall: &call}} {
		if err := emit(delta); err != nil {
			return llmadapter.Response{}, err
		}
	}
	a.finished = true
	return llmadapter.Response{Message: llmadapter.Message{Role: llmadapter.RoleAssistant, Content: a.partial, ToolCalls: []llmadapter.ToolCall{call}}, Usage: usage, FinishReason: a.reason}, nil
}

func TestTurnBudgetHoldsToolsUntilNormalFinish(t *testing.T) {
	for _, reason := range []llmadapter.FinishReason{llmadapter.FinishReasonLength, llmadapter.FinishReasonContentFilter, llmadapter.FinishReasonStop, ""} {
		t.Run(string(reason), func(t *testing.T) {
			adapter := &finishReasonFixtureAdapter{reason: reason, partial: "partial text"}
			budget := &turnGenerationBudget{}
			tools := 0
			var text string
			out, err := budget.stream(context.Background(), adapter, nil, llmadapter.Request{MaxTokens: 32}, func(delta llmadapter.Delta) error {
				text += delta.Text
				if delta.ToolCall != nil {
					if !adapter.finished {
						t.Fatal("tool delta released before finish reason was known")
					}
					tools++
				}
				return nil
			})
			if chatModelFinishError(reason) != nil {
				if !chatModelCompletionFailed(err) || tools != 0 || len(out.Message.ToolCalls) != 0 {
					t.Fatalf("incomplete response released tools: err=%v callbacks=%d calls=%d", err, tools, len(out.Message.ToolCalls))
				}
			} else if err != nil || tools != 1 || len(out.Message.ToolCalls) != 1 {
				t.Fatalf("normal tool response changed: err=%v callbacks=%d", err, tools)
			}
			if text != adapter.partial || out.Message.Content != adapter.partial || budget.usageSnapshot().TotalTokens != 5 || len(adapter.requests) != 1 {
				t.Fatal("partial content/usage lost or model request retried")
			}
		})
	}
}

func TestChatIncompleteFinishKeepsPartialAndNeverGeneratesArtifact(t *testing.T) {
	for _, tc := range []struct {
		reason llmadapter.FinishReason
		code   string
	}{
		{llmadapter.FinishReasonLength, "UPSTREAM_RESPONSE_TRUNCATED"},
		{llmadapter.FinishReasonContentFilter, "UPSTREAM_RESPONSE_FILTERED"},
	} {
		for _, buffered := range []bool{false, true} {
			name := string(tc.reason)
			if buffered {
				name += "/buffered"
			}
			t.Run(name, func(t *testing.T) {
				storeEngine, _, sid, _ := messageEngine(t)
				e := NewEngineWithGateway(nil, "test", streamTestLease{})
				e.messages, e.sessions = storeEngine.messages, storeEngine.sessions
				runtime, err := toolruntime.New(t.TempDir())
				if err != nil {
					t.Fatal(err)
				}
				defer runtime.Close()
				e.tools = runtime
				defer e.StopChatMemoryWorkers()
				adapter := &finishReasonFixtureAdapter{reason: tc.reason, partial: officeFallbackProse}
				e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (llmadapter.Adapter, error) { return adapter, nil })
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				state := &streamState{cancel: cancel, state: streamRunning, companion: buffered}
				var events []bridge.Event
				e.runStream(ctx, "01ARZ3NDEKTSV4RRFFQ69G5FAV", state,
					provider.Provider{ID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", Protocol: provider.ProtocolOpenAICompatible, BaseURL: "https://api.example.com", CredentialRef: "credential-ref"},
					llmadapter.Request{Model: "model", Messages: []llmadapter.Message{{Role: llmadapter.RoleUser, Content: "生成文档：将提供的内容转为 Word"}}, Tools: []llmadapter.ToolDefinition{{Name: "workspace.write", Schema: []byte(`{"type":"object"}`)}}},
					func(event bridge.Event) error { events = append(events, event); return nil }, sid, executionModeFullAccess)
				terminal := events[len(events)-1]
				if terminal.Type != bridge.EventFailed || terminal.Error.Code != tc.code || len(adapter.requests) != 1 {
					t.Fatalf("terminal=%+v requests=%d", terminal, len(adapter.requests))
				}
				page, err := e.messages.List(ctx, messageapp.PageRequest{SessionID: sid})
				if err != nil || len(page.Items) != 1 || !strings.Contains(page.Items[0].Text, adapter.partial) || !strings.Contains(page.Items[0].Text, "任务未完成") {
					t.Fatalf("partial or failure notice not persisted: %+v err=%v", page, err)
				}
				if artifacts := e.loadSessionArtifactsByMessage(sid)[page.Items[0].ID]; len(artifacts) != 0 {
					t.Fatalf("incomplete response recovered as an artifact: %+v", artifacts)
				}
				for _, event := range events {
					if event.Type == bridge.EventToolStarted || (event.Tool != nil && event.Tool.Artifact != nil) {
						t.Fatal("incomplete response executed a tool or generated an artifact")
					}
				}
			})
		}
	}
}
