package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/llmadapter"
	"github.com/lunitide/lunitide/internal/toolruntime"
)

type spokenQuestionAdapter struct {
	typed  bool
	answer bool
	plain  bool
}

func (a *spokenQuestionAdapter) Complete(context.Context, []byte, llmadapter.Request) (llmadapter.Response, error) {
	return llmadapter.Response{}, errors.New("not used")
}
func (a *spokenQuestionAdapter) Discover(context.Context, []byte) (llmadapter.Discovery, error) {
	return llmadapter.Discovery{}, errors.New("not used")
}
func (a *spokenQuestionAdapter) Stream(_ context.Context, _ []byte, req llmadapter.Request, emit func(llmadapter.Delta) error) (llmadapter.Response, error) {
	for _, tool := range req.Tools {
		if tool.Name == "user.ask" {
			a.typed = true
		}
	}
	if a.answer {
		text := "收到，你说的是年度总结文档。"
		return llmadapter.Response{Message: llmadapter.Message{Role: llmadapter.RoleAssistant, Content: text}}, emit(llmadapter.Delta{Text: text})
	}
	if a.plain {
		text := "请说出要打开的文件名。"
		return llmadapter.Response{Message: llmadapter.Message{Role: llmadapter.RoleAssistant, Content: text}}, emit(llmadapter.Delta{Text: text})
	}
	return llmadapter.Response{Message: llmadapter.Message{Role: llmadapter.RoleAssistant, ToolCalls: []llmadapter.ToolCall{{ID: "ask-file", Name: "user.ask", Arguments: json.RawMessage(`{"questions":[{"prompt":"请说出要打开的文件名。","options":[{"label":"年度总结"},{"label":"预算"}]}]}`)}, {ID: "premature-action", Name: "workspace.write", Arguments: json.RawMessage(`{"path":"must-not-write.txt","content":"no answer yet"}`)}}}}, nil
}

func TestCompanionClarificationEndsWithoutApprovalAndAcceptsNextRound(t *testing.T) {
	for _, plain := range []bool{false, true} {
		t.Run(map[bool]string{false: "legacy-tool", true: "spoken-text"}[plain], func(t *testing.T) {
			e := NewEngineWithGateway(chatAttachmentProvider{}, "test", streamTestLease{})
			runtime, err := toolruntime.New(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { runtime.Close() })
			e.SetToolRuntime(runtime)
			adapter := &spokenQuestionAdapter{plain: plain}
			e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (llmadapter.Adapter, error) { return adapter, nil })
			for round, prompt := range []string{"帮我打开一个文档", "年度总结文档"} {
				adapter.answer = round == 1
				events := make(chan bridge.Event, 128)
				payload, _ := json.Marshal(map[string]any{"providerId": chatAttachmentProviderID, "modelId": "model", "companion": true, "executionMode": "full-access", "messages": []map[string]string{{"role": "user", "content": prompt}}})
				response := e.HandleStreaming(context.Background(), validRequest("chat.start", string(payload)), func(ev bridge.Event) error { events <- ev; return nil })
				if !response.OK {
					t.Fatalf("start: %#v", response)
				}
				frames := collectFramedChatEvents(t, response, events)
				var spoken strings.Builder
				for _, ev := range frames {
					if ev.Type == bridge.EventApprovalRequired || ev.Type == bridge.EventToolStarted {
						t.Fatalf("voice clarification created an approval/action: %#v", ev)
					}
					if ev.Delta != nil {
						spoken.WriteString(ev.Delta.Text)
					}
				}
				if frames[len(frames)-1].Type != bridge.EventCompleted {
					t.Fatalf("terminal: %#v", frames[len(frames)-1])
				}
				want := "请说出要打开的文件名。"
				if !plain {
					want = "请说出要打开的文件名。可以说年度总结，预算，或者你自己说。"
				}
				if round == 1 {
					want = "收到，你说的是年度总结文档。"
				}
				if strings.TrimSpace(spoken.String()) != want {
					t.Fatalf("spoken = %q, want %q", spoken.String(), want)
				}
			}
			if adapter.typed {
				t.Fatal("voice advertised user.ask")
			}
		})
	}
}

func TestCompanionSpokenQuestionAndPrompt(t *testing.T) {
	if got := companionSpokenQuestion(json.RawMessage(`{"questions":[{"prompt":"发给谁？","options":[{"label":"小王"},{"label":"小组群"}]},{"prompt":"内容是什么？"}]}`)); got != "发给谁？可以说小王，小组群，或者你自己说。" {
		t.Fatal(got)
	}
	if got := companionSpokenQuestion(json.RawMessage(`{"questions":[{"prompt":"发给谁？"}]}`)); got != "发给谁？" {
		t.Fatal(got)
	}
	if got := companionSpokenQuestion(json.RawMessage(`{}`)); got == "" {
		t.Fatal("empty clarification")
	}
	for _, text := range []string{"请说出文件名。", "要发给谁？"} {
		if !companionNeedsSpokenInput(text) {
			t.Fatal(text)
		}
	}
	if companionNeedsSpokenInput("今天合肥晴天，最高二十八度。") {
		t.Fatal("answer mistaken for clarification")
	}
	for _, rule := range []string{"默认只答 1–2 句", "不使用 user.ask", "先从上下文推断并直接做", "同一结果本轮只说一次"} {
		if !strings.Contains(companionPersonaChatInstruction(), rule) {
			t.Fatal(rule)
		}
	}
}

func TestCompanionVerifiedCloseDoesNotLoop(t *testing.T) {
	text := "汽水音乐已经关闭了。"
	if got := pickTurnContinueKind(text, text, "closed 汽水音乐", []string{"computer.act"}, true, true, true, true, 0, "关闭汽水音乐", true); got != "" {
		t.Fatalf("repeated close: %q", got)
	}
	if companionCloseResultSettled(text, "关闭汽水音乐", "ok:false closed verification failed") {
		t.Fatal("failure claimed success")
	}
	if companionCloseResultSettled(text, "关闭汽水音乐", "screenshot frameId=1") {
		t.Fatal("screenshot claimed success")
	}
	if companionCloseResultSettled(text, "打开汽水音乐", "closed 汽水音乐") {
		t.Fatal("wrong goal claimed success")
	}
	if companionCloseResultSettled(text, "关闭汽水音乐并打开记事本", "closed 汽水音乐") {
		t.Fatal("later action dropped")
	}
	if !companionCloseResultSettled(text, "关闭汽水音乐", "window close 汽水音乐; screen updated 100x100 frameId=1") {
		t.Fatal("real computer.act close did not settle")
	}
	if companionCloseResultSettled(text, "关闭汽水音乐", "window close 汽水音乐; screen unchanged after wait (mutation unverified)") {
		t.Fatal("unverified close claimed success")
	}
}
