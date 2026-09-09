package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/llmadapter"
	"github.com/lunitide/lunitide/internal/toolruntime"
)

func TestCompanionLookupStreamingRequiresCurrentQueryEvidence(t *testing.T) {
	query := "今天合肥天气怎么样？"
	messages := []llmadapter.Message{
		{Role: llmadapter.RoleUser, Content: query},
		{Role: llmadapter.RoleAssistant, ToolCalls: []llmadapter.ToolCall{{ID: "weather", Name: "weather.get"}}},
		{Role: llmadapter.RoleTool, ToolCallID: "weather", Content: `{"kind":"weather_forecast","days":[{"date":"2026-09-08"}]}`},
	}
	if !companionLookupCanStream(query, messages) {
		t.Fatal("verified read-only response unnecessarily buffered")
	}
	for _, goal := range []string{
		"打开桌面浏览器，搜索新闻，然后查询内容给我",
		"查天气后写入文档", "播放汽水音乐", "查询新闻并发送给同事",
	} {
		if companionLookupCanStream(goal, messages) {
			t.Errorf("compound or mutating goal bypassed result guard: %s", goal)
		}
	}
	if companionLookupCanStream(query, messages[:2]) {
		t.Fatal("tool request alone was treated as evidence")
	}
	if companionLookupCanStream(query, append(messages, llmadapter.Message{Role: llmadapter.RoleUser, Content: query})) {
		t.Fatal("previous-turn evidence enabled live speech")
	}
	for _, out := range []string{"", "ok:false timeout", "无法执行：网络不可用"} {
		messages[2].Content = out
		if companionLookupCanStream(query, messages) {
			t.Fatalf("failed lookup enabled live speech: %q", out)
		}
	}
}

func TestLookupStreamingDoesNotReuseEvidenceWhenLatestLookupFailsOrIsPending(t *testing.T) {
	messages := receiptMessages("web.search", `{}`, "query: news results: fixture")
	messages = append(messages, llmadapter.Message{Role: llmadapter.RoleAssistant, ToolCalls: []llmadapter.ToolCall{{ID: "latest", Name: "web.fetch"}}})
	if currentLookupEvidence(messages) {
		t.Fatal("pending lookup reused old evidence")
	}
	messages = append(messages, llmadapter.Message{Role: llmadapter.RoleTool, ToolCallID: "latest", Content: "ok:false timeout"})
	if currentLookupEvidence(messages) {
		t.Fatal("failed latest lookup reused old evidence")
	}
}

type lookupLatencyAdapter struct {
	continueAdapter
	stream func(llmadapter.Request, func(llmadapter.Delta) error) (llmadapter.Response, error)
}

func (a *lookupLatencyAdapter) Stream(_ context.Context, _ []byte, req llmadapter.Request, emit func(llmadapter.Delta) error) (llmadapter.Response, error) {
	return a.stream(req, emit)
}

func runCompanionLatencyStream(t *testing.T, req llmadapter.Request, adapter llmadapter.Adapter, output func(string) string, emit func(bridge.Event) error, companionMode ...bool) {
	t.Helper()
	companion := true
	if len(companionMode) > 0 {
		companion = companionMode[0]
	}
	runExecutionContractStream(t, req, adapter, companion, func(name string, _ json.RawMessage) (toolruntime.Result, error) {
		return toolruntime.Result{Output: output(name)}, nil
	}, emit)
}

func runExecutionContractStream(t *testing.T, req llmadapter.Request, adapter llmadapter.Adapter, companion bool, execute func(string, json.RawMessage) (toolruntime.Result, error), emit func(bridge.Event) error) {
	t.Helper()
	e := NewEngineWithGateway(nil, "test", streamTestLease{})
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (llmadapter.Adapter, error) { return adapter, nil })
	e.toolExecHook = func(_ context.Context, _ executionMode, _, name string, args json.RawMessage) (toolruntime.Result, error) {
		return execute(name, args)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	state := &streamState{cancel: cancel, state: streamRunning, companion: companion}
	e.streams["latency"] = state
	e.runStream(ctx, "latency", state, provider.Provider{Protocol: provider.ProtocolOpenAICompatible, BaseURL: "https://api.example.com"}, req, emit, "", executionModeFullAccess)
}

func TestCompanionReadOnlyAnswerStreamsBeforeModelCompletes(t *testing.T) {
	for _, mode := range []string{"local", "volc", "cloud"} {
		t.Run(mode, func(t *testing.T) {
			var spoken strings.Builder
			calls, lookups, terminals := 0, 0, 0
			adapter := &lookupLatencyAdapter{stream: func(req llmadapter.Request, emit func(llmadapter.Delta) error) (llmadapter.Response, error) {
				calls++
				if calls == 1 {
					if err := emit(llmadapter.Delta{Text: "已经查到了，今天晴。"}); err != nil {
						return llmadapter.Response{}, err
					}
					if spoken.Len() != 0 {
						t.Error("speculation escaped before lookup")
					}
					return llmadapter.Response{Message: llmadapter.Message{Role: llmadapter.RoleAssistant, ToolCalls: []llmadapter.ToolCall{{ID: "weather", Name: "weather.get", Arguments: json.RawMessage(`{"place":"合肥","days":1}`)}}}}, nil
				}
				if lookups != 1 || !currentLookupEvidence(req.Messages) {
					t.Error("missing current lookup evidence")
				}
				const first = "合肥今天多云。"
				if err := emit(llmadapter.Delta{Text: first}); err != nil {
					return llmadapter.Response{}, err
				}
				// Assert delivery while Stream is still running, not just at its return.
				if !strings.Contains(spoken.String(), first) {
					t.Error("first sentence held until completion")
				}
				const tail = "气温24到30度。"
				if err := emit(llmadapter.Delta{Text: tail}); err != nil {
					return llmadapter.Response{}, err
				}
				return llmadapter.Response{Message: llmadapter.Message{Role: llmadapter.RoleAssistant, Content: first + tail}}, nil
			}}
			req := llmadapter.Request{Model: "m", DisableReasoning: true, Tools: []llmadapter.ToolDefinition{{Name: "weather.get"}}, Messages: []llmadapter.Message{{Role: llmadapter.RoleUser, Content: "今天合肥的天气怎么样？"}}}
			runCompanionLatencyStream(t, req, adapter, func(string) string {
				lookups++
				return `{"kind":"weather_forecast","city":"合肥","days":[{"condition":"多云","min":24,"max":30}]}`
			}, func(ev bridge.Event) error {
				if ev.Delta != nil {
					spoken.WriteString(ev.Delta.Text)
				}
				if ev.Type == bridge.EventFailed {
					t.Errorf("stream failed: %+v", ev)
				}
				if ev.Type == bridge.EventCompleted {
					terminals++
				}
				return nil
			})
			if calls != 2 || lookups != 1 || terminals != 1 || strings.Contains(spoken.String(), "已经查到了") {
				t.Fatalf("calls=%d lookups=%d terminal=%d spoken=%s", calls, lookups, terminals, spoken.String())
			}
		})
	}
}

func TestCompanionBrowserNewsKeepsSummaryWithoutExtraDesktopNudges(t *testing.T) {
	const goal = "打开桌面浏览器，搜索新闻，然后查询内容给我"
	const summary = "今天的新闻：测试展会在合肥开幕，主办方公布了三项议程。"
	const browse = "已向系统默认桌面浏览器发送打开请求：https://example.com；页面加载结果需通过 computer.act 核对"
	var spoken strings.Builder
	calls, terminals := 0, 0
	adapter := &lookupLatencyAdapter{stream: func(req llmadapter.Request, emit func(llmadapter.Delta) error) (llmadapter.Response, error) {
		calls++
		switch calls {
		case 1:
			return llmadapter.Response{Message: llmadapter.Message{Role: llmadapter.RoleAssistant, ToolCalls: []llmadapter.ToolCall{{ID: "browse", Name: "desktop.browse", Arguments: json.RawMessage(`{"url":"https://example.com"}`)}}}}, nil
		case 2:
			return llmadapter.Response{Message: llmadapter.Message{Role: llmadapter.RoleAssistant, ToolCalls: []llmadapter.ToolCall{{ID: "news", Name: "web.search", Arguments: json.RawMessage(`{"query":"today news"}`)}}}}, nil
		}
		if err := emit(llmadapter.Delta{Text: summary}); err != nil {
			return llmadapter.Response{}, err
		}
		if strings.Contains(spoken.String(), summary) {
			t.Error("compound desktop response bypassed final evidence guard")
		}
		return llmadapter.Response{Message: llmadapter.Message{Role: llmadapter.RoleAssistant, Content: summary}}, nil
	}}
	req := llmadapter.Request{Model: "m", DisableReasoning: true, Tools: []llmadapter.ToolDefinition{{Name: "desktop.browse"}, {Name: "desktop.open"}, {Name: "computer.act"}, {Name: "web.search"}}, Messages: []llmadapter.Message{{Role: llmadapter.RoleUser, Content: goal}}}
	runCompanionLatencyStream(t, req, adapter, func(name string) string {
		if name == "desktop.browse" {
			return browse
		}
		if name != "web.search" {
			t.Errorf("unexpected fallback after browser was opened: %s", name)
		}
		return "query: today news source: fixture results: 测试展会在合肥开幕，主办方公布了三项议程。"
	}, func(ev bridge.Event) error {
		if ev.Delta != nil {
			spoken.WriteString(ev.Delta.Text)
		}
		if ev.Type == bridge.EventFailed {
			t.Errorf("stream failed: %+v", ev)
		}
		if ev.Type == bridge.EventCompleted {
			terminals++
		}
		return nil
	})
	if calls != 3 || terminals != 1 || !strings.Contains(spoken.String(), summary) || !strings.Contains(spoken.String(), "尚未核验") {
		t.Fatalf("calls=%d terminal=%d spoken=%s", calls, terminals, spoken.String())
	}
	messages := receiptMessages("desktop.browse", `{}`, browse)
	messages = append(messages, receiptMessages("web.search", `{}`, "query: news results: fixture")[1:]...)
	for _, pendingGoal := range []string{goal + "并保存文档", goal + "并发送给同事"} {
		if companionBrowserLookupSettled(pendingGoal, summary, messages) {
			t.Error("compound write/send was marked complete", pendingGoal)
		}
	}
	if companionBrowserLookupSettled(goal, "稍等，我去查", messages) || companionBrowserLookupSettled(goal, summary, messages[:3]) {
		t.Error("promise or launch receipt alone was marked complete")
	}
}
