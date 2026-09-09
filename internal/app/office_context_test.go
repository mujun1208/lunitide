package app

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/llmadapter"
	"strings"
	"testing"

	domain "github.com/lunitide/lunitide/internal/domain/officestudio"
	content "github.com/lunitide/lunitide/internal/officestudio"
)

func TestOfficeChatBindingKeepsOlderTaskAndPendingApprovalDestination(t *testing.T) {
	e, store := officeEngineFixture(t)
	a := officeCreatedTask(t, e, "task-a")
	b, err := store.CreateOfficeTask(context.Background(), domain.Task{SessionID: a.SessionID, Title: "Task B"}, "task-b")
	if err != nil {
		t.Fatal(err)
	}
	ctx := withOfficeTask(context.Background(), a.ID)
	if err = e.validateOfficeChatTask(ctx, a.SessionID, a.ID); err != nil {
		t.Fatal(err)
	}
	args, _ := json.Marshal(map[string]any{"name": "当前任务.docx", "spec": content.Spec{SchemaVersion: 1, Kind: content.DOCX, Title: "A", Blocks: []content.Block{{Type: "paragraph", Text: "归属旧任务A"}}}})
	if _, _, _, err = e.executeOfficeTool(ctx, a.SessionID, "office.generate", args); err != nil {
		t.Fatal(err)
	}
	va, _ := store.ListOfficeVersions(ctx, a.ID, "")
	vb, _ := store.ListOfficeVersions(ctx, b.ID, "")
	if len(va) != 1 || len(vb) != 0 {
		t.Fatalf("bound task drifted: A=%d B=%d", len(va), len(vb))
	}
	// Pending approval persists the bound destination; later decisions do not
	// require the original stream context or the same visible route.
	bound := officeBoundToolArgs(ctx, "office.generate", args)
	var p map[string]any
	_ = json.Unmarshal(bound, &p)
	if p["taskId"] != a.ID {
		t.Fatal("approval arguments lost task", p)
	}
	if _, _, _, err = e.executeOfficeTool(context.Background(), a.SessionID, "office.generate", bound); err != nil {
		t.Fatal(err)
	}
	va, _ = store.ListOfficeVersions(ctx, a.ID, "")
	vb, _ = store.ListOfficeVersions(ctx, b.ID, "")
	if len(va) != 2 || len(vb) != 0 {
		t.Fatalf("approval destination drifted: A=%d B=%d", len(va), len(vb))
	}
	p["taskId"] = b.ID
	wrong, _ := json.Marshal(p)
	if _, _, _, err = e.executeOfficeTool(ctx, a.SessionID, "office.generate", wrong); !errors.Is(err, domain.ErrScope) {
		t.Fatalf("cross-bound task accepted: %v", err)
	}
	other := officeCreatedTask(t, e, "another-session")
	if err = e.validateOfficeChatTask(ctx, other.SessionID, a.ID); !errors.Is(err, domain.ErrScope) {
		t.Fatalf("cross-session binding accepted: %v", err)
	}
}

func TestOfficeChatStartStreamsAndPersistsRealBoundDelivery(t *testing.T) {
	e, store := officeEngineFixture(t)
	a := officeCreatedTask(t, e, "stream-a")
	b, err := store.CreateOfficeTask(context.Background(), domain.Task{SessionID: a.SessionID, Title: "Another task"}, "stream-b")
	if err != nil {
		t.Fatal(err)
	}
	e.providers = chatAttachmentProvider{}
	e.leases = streamTestLease{}
	sourceBytes, err := content.Generate(content.Spec{SchemaVersion: 1, Kind: content.DOCX, Title: "Reference", Blocks: []content.Block{{Type: "paragraph", Text: "Original source"}}})
	if err != nil {
		t.Fatal(err)
	}
	source, err := e.officeStudio.Import(context.Background(), a.ID, "", "bound-reference.docx", sourceBytes, "", 0, "reference-a")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = e.officeStudio.Import(context.Background(), b.ID, "", "other-task-secret.docx", sourceBytes, "", 0, "reference-b"); err != nil {
		t.Fatal(err)
	}
	calls := 0
	adapter := &routedExecutionAdapter{stream: func(req llmadapter.Request) (llmadapter.Response, error) {
		calls++
		if calls == 1 {
			found := false
			for _, message := range req.Messages {
				if strings.Contains(message.Content, "other-task-secret") {
					t.Error("other task source leaked")
				}
				if strings.Contains(message.Content, "bound-reference.docx") {
					found = strings.Contains(message.Content, source.ID)
					if message.Role == llmadapter.RoleSystem {
						t.Error("source promoted to system authority")
					}
				}
				if message.Role == llmadapter.RoleSystem && strings.Contains(message.Content, "九步流水线") {
					t.Error("legacy Office pipeline injected")
				}
			}
			if !found {
				t.Error("bound source catalog missing from real model request")
			}
			if !routedRequestHasTool(req, "office.generate") {
				t.Error("Office generator missing in real chat tools")
			}
			return llmadapter.Response{Message: llmadapter.Message{Role: llmadapter.RoleAssistant, ToolCalls: []llmadapter.ToolCall{{ID: "bound-office-call", Name: "office.generate", Arguments: json.RawMessage(`{"name":"当前任务.docx","spec":{"schemaVersion":1,"kind":"docx","title":"任务A","blocks":[{"type":"paragraph","text":"真实工具生成的文件"}]}}`)}}}}, nil
		}
		return llmadapter.Response{Message: llmadapter.Message{Role: llmadapter.RoleAssistant, Content: "文件已生成，可以查看。"}}, nil
	}}
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (llmadapter.Adapter, error) { return adapter, nil })
	payload, _ := json.Marshal(map[string]any{"providerId": chatAttachmentProviderID, "modelId": "model", "sessionId": a.SessionID, "officeTaskId": a.ID, "executionMode": "full-access", "messages": []map[string]string{{"role": "user", "content": "生成一份包含任务A说明的文档。"}}})
	events := make(chan bridge.Event, 256)
	response := e.HandleStreaming(context.Background(), validRequest("chat.start", string(payload)), func(event bridge.Event) error { events <- event; return nil })
	if !response.OK {
		t.Fatalf("start: %+v", response.Error)
	}
	frames := collectFramedChatEvents(t, response, events)
	if frames[len(frames)-1].Type != bridge.EventCompleted {
		t.Fatalf("terminal: %+v", frames[len(frames)-1])
	}
	delivered := false
	for _, frame := range frames {
		if frame.Tool != nil && frame.Tool.Name == "office.generate" && frame.Tool.Artifact != nil {
			delivered = true
		}
	}
	if !delivered {
		t.Fatal("real file card missing from stream")
	}
	va, _ := store.ListOfficeVersions(context.Background(), a.ID, "")
	vb, _ := store.ListOfficeVersions(context.Background(), b.ID, "")
	if len(va) != 2 || len(vb) != 1 {
		t.Fatalf("stream task drifted: A=%d B=%d", len(va), len(vb))
	}
	index := e.loadSessionArtifactsByMessage(a.SessionID)
	bound := false
	for _, files := range index {
		for _, file := range files {
			if strings.HasSuffix(file.Path, ".docx") {
				bound = bound || file.OfficeTaskID == a.ID
			}
		}
	}
	if !bound {
		t.Fatal("durable chat file lost Office task binding")
	}
}
