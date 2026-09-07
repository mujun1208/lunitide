package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/llmadapter"
	"github.com/lunitide/lunitide/internal/messageapp"
	"github.com/lunitide/lunitide/internal/toolruntime"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMessageProcessRestartScopeAndDeletion(t *testing.T) {
	e, _, sid, dbPath := messageEngine(t)
	ctx := context.Background()
	tools, err := toolruntime.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer tools.Close()
	e.tools = tools
	msg, err := e.messages.AppendAssistant(ctx, "process-run", "test", sid, "最终回答", messageapp.AssistantUsage{})
	if err != nil {
		t.Fatal(err)
	}
	process := messageProcess{}
	process.capture(bridge.Event{Type: bridge.EventThinking, Thinking: &bridge.ThinkingEvent{Text: "先核对工具返回结果"}})
	process.capture(bridge.Event{Type: bridge.EventEquip, Equip: &bridge.EquipEvent{Experts: []string{"专家A"}, Skills: []string{"技能A"}}})
	process.capture(bridge.Event{Type: bridge.EventToolStarted, Tool: &bridge.ToolEvent{CallID: "call-1", Name: "workspace.write", ArgsDigest: strings.Repeat("a", 64), Summary: "开始写入"}})
	process.capture(bridge.Event{Type: bridge.EventToolCompleted, Tool: &bridge.ToolEvent{CallID: "call-1", Name: "workspace.write", ArgsDigest: strings.Repeat("a", 64), Summary: "文件已保存", Artifact: &bridge.ArtifactEvent{Content: "not-for-process"}}})
	e.saveMessageProcess(sid, msg.ID, process)
	sessionFolder, err := tools.SessionFolder(sid)
	if err != nil {
		t.Fatal(err)
	}
	rel, _ := filepath.Rel(sessionFolder, e.messageProcessPath(sid, msg.ID))
	if !strings.HasPrefix(rel, "..") {
		t.Fatalf("process changed workspace digest: %s", rel)
	}
	fresh := NewEngine(nil, "test")
	fresh.tools = tools
	fresh.messages = e.messages
	fresh.sessions = e.sessions
	req := validRequest("message.process", `{"sessionId":"`+sid+`","messageId":"`+msg.ID+`"}`)
	got := fresh.Handle(ctx, req)
	if !got.OK {
		t.Fatalf("reload=%+v", got)
	}
	raw, _ := json.Marshal(got.Payload)
	var out messageProcess
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if out.Thinking != "先核对工具返回结果" || len(out.Tools) != 1 || out.Tools[0].Status != "tool_completed" || strings.Contains(string(raw), "not-for-process") {
		t.Fatalf("process=%s", raw)
	}
	list := fresh.Handle(ctx, validRequest("message.list", `{"sessionId":"`+sid+`"}`))
	b, _ := json.Marshal(list.Payload)
	if !list.OK || !strings.Contains(string(b), `"hasProcess":true`) || strings.Contains(string(b), out.Thinking) {
		t.Fatalf("message listing=%s", b)
	}
	bad := fresh.Handle(ctx, validRequest("message.process", `{"sessionId":"01ARZ3NDEKTSV4RRFFQ69G5FAX","messageId":"`+msg.ID+`"}`))
	if bad.OK {
		t.Fatal("foreign session exposed process")
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`DELETE FROM messages WHERE id=?`, msg.ID); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if got = fresh.Handle(ctx, req); got.OK {
		t.Fatal("deleted message still exposes process")
	}
	if _, err = os.Stat(e.messageProcessPath(sid, msg.ID)); err != nil {
		t.Fatal(err)
	} // retained metadata cannot bypass message ownership
}
func TestMessageProcessBoundsEscapedJSONAndUpdatesTool(t *testing.T) {
	p := messageProcess{MessageID: "01ARZ3NDEKTSV4RRFFQ69G5FAV"}
	p.capture(bridge.Event{Type: bridge.EventThinking, Thinking: &bridge.ThinkingEvent{Text: strings.Repeat("<汉\"", 50000)}})
	for i := 0; i < 70; i++ {
		p.capture(bridge.Event{Type: bridge.EventToolStarted, Tool: &bridge.ToolEvent{CallID: strings.Repeat("a", i+1), Name: "tool", Summary: strings.Repeat("<", 512)}})
	}
	raw, err := p.boundedJSON()
	if err != nil || len(raw) > messageProcessMaxBytes {
		t.Fatalf("bytes=%d err=%v", len(raw), err)
	}
	var out messageProcess
	if err = json.Unmarshal(raw, &out); err != nil || !out.Truncated {
		t.Fatalf("trace %v err=%v", out, err)
	}
	if len(out.Tools) > 64 || len(out.Thinking) > messageProcessThinkingBytes {
		t.Fatal("unbounded content")
	}
}

type processReplyAdapter struct{ fail bool }

func (processReplyAdapter) Complete(context.Context, []byte, llmadapter.Request) (llmadapter.Response, error) {
	return llmadapter.Response{}, nil
}
func (processReplyAdapter) Discover(context.Context, []byte) (llmadapter.Discovery, error) {
	return llmadapter.Discovery{}, nil
}
func (a processReplyAdapter) Stream(ctx context.Context, key []byte, req llmadapter.Request, emit func(llmadapter.Delta) error) (llmadapter.Response, error) {
	if a.fail {
		return thinkingOnlyThenFailAdapter{}.Stream(ctx, key, req, emit)
	}
	if err := emit(llmadapter.Delta{Reasoning: "先核对输入", Text: "最终回答"}); err != nil {
		return llmadapter.Response{}, err
	}
	return llmadapter.Response{}, nil
}
func TestRunStreamPersistsProcessBeforeTerminalWithoutWaitingForMemory(t *testing.T) {
	for _, scenario := range []struct {
		name                 string
		failed, dropThinking bool
	}{{name: "success"}, {name: "failure", failed: true}, {name: "thinking-transport-failed", dropThinking: true}} {
		failed := scenario.failed
		t.Run(scenario.name, func(t *testing.T) {
			storeEngine, _, sid, _ := messageEngine(t)
			e := NewEngineWithGateway(nil, "test", streamTestLease{})
			e.messages = storeEngine.messages
			e.sessions = storeEngine.sessions
			tools, err := toolruntime.New(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer tools.Close()
			e.tools = tools
			blocked := make(chan struct{})
			e.chatMemoryWorkers.enqueue(func(ctx context.Context) { close(blocked); <-ctx.Done() })
			<-blocked
			defer e.StopChatMemoryWorkers()
			e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (llmadapter.Adapter, error) {
				return processReplyAdapter{fail: failed}, nil
			})
			_, cancel := context.WithCancel(context.Background())
			defer cancel()
			state := &streamState{cancel: cancel, state: streamRunning}
			events := make(chan bridge.Event, 32)
			go e.runStream(context.Background(), "01ARZ3NDEKTSV4RRFFQ69G5FAV", state, provider.Provider{ID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", Protocol: provider.ProtocolOpenAICompatible, BaseURL: "https://api.example.com", CredentialRef: "credential-ref"}, llmadapter.Request{Model: "model"}, func(event bridge.Event) error {
				if scenario.dropThinking && event.Type == bridge.EventThinking {
					return errors.New("renderer temporarily disconnected")
				}
				events <- event
				return nil
			}, sid)
			terminal := terminalEvent(t, events)
			if failed && terminal.Type != bridge.EventFailed {
				t.Fatal(terminal.Type)
			}
			if !failed && terminal.Type != bridge.EventCompleted {
				t.Fatal(terminal.Type)
			}
			page, err := e.messages.List(context.Background(), messageapp.PageRequest{SessionID: sid})
			if err != nil || len(page.Items) != 1 {
				t.Fatalf("page=%+v %v", page, err)
			}
			msg := page.Items[0]
			if strings.Contains(msg.Text, "先核对") || strings.Contains(msg.Text, "先规划") || strings.Contains(msg.Text, "【思考过程】") {
				t.Fatalf("reasoning in history: %s", msg.Text)
			}
			res := handleMessageProcess(e, context.Background(), validRequest("message.process", `{"sessionId":"`+sid+`","messageId":"`+msg.ID+`"}`))
			if !res.OK {
				t.Fatalf("process=%+v", res)
			}
			raw, _ := json.Marshal(res.Payload)
			if !strings.Contains(string(raw), "thinking") || (!strings.Contains(string(raw), "先核对") && !strings.Contains(string(raw), "先规划")) {
				t.Fatalf("reasoning lost: %s", raw)
			}
			var saved messageProcess
			if err := json.Unmarshal(raw, &saved); err != nil {
				t.Fatal(err)
			}
			if scenario.dropThinking && strings.Count(saved.Thinking, "先核对输入") != 1 {
				t.Fatalf("thinking re-recorded after failed emit: %s", saved.Thinking)
			}
		})
	}
}
