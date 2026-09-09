package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/llmadapter"
	"github.com/lunitide/lunitide/internal/messageapp"
	"github.com/lunitide/lunitide/internal/toolruntime"
)

func TestRunStreamOfficeFallbackSuccessPersistsMessageArtifactAndProcess(t *testing.T) {
	testOfficeFinalizationWithoutLookup(t, "制作一份产品介绍PPT")
}

func TestOfflineNewsOfficeFinalizationDoesNotSearch(t *testing.T) {
	for _, goal := range []string{"将已有新闻整理成PPT，不联网，保留原文。", "将已完成的新闻报告转换成PPT。"} {
		t.Run(goal, func(t *testing.T) { testOfficeFinalizationWithoutLookup(t, goal) })
	}
}

func testOfficeFinalizationWithoutLookup(t *testing.T, goal string) {
	t.Helper()
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
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (llmadapter.Adapter, error) {
		// The model ends normally without issuing a generation tool. The real
		// Office fallback must finish the file and retain the successful turn.
		return officeFallbackReplyAdapter{}, nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	state := &streamState{cancel: cancel, state: streamRunning}
	var events []bridge.Event
	e.runStream(ctx, "01ARZ3NDEKTSV4RRFFQ69G5FAV", state,
		provider.Provider{ID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", Protocol: provider.ProtocolOpenAICompatible, BaseURL: "https://api.example.com", CredentialRef: "credential-ref"},
		llmadapter.Request{Model: "model", Messages: []llmadapter.Message{{Role: llmadapter.RoleUser, Content: goal}}, Tools: []llmadapter.ToolDefinition{{Name: "web.search", Schema: json.RawMessage(`{"type":"object"}`)}}},
		func(event bridge.Event) error { events = append(events, event); return nil }, sid, executionModeFullAccess)
	if len(events) == 0 {
		t.Fatal("stream emitted no events")
	}
	terminal := events[len(events)-1]
	if terminal.Type != bridge.EventCompleted || terminal.Completed == nil || terminal.Completed.PersistFailed || terminal.Completed.MessageID == "" {
		t.Fatalf("successful fallback lost its durable message: %+v", terminal)
	}
	var artifact *bridge.ToolEvent
	for _, event := range events {
		if event.Tool != nil && event.Tool.Name == "web.search" {
			t.Fatal("document finalization injected an unrequested lookup")
		}
		if event.Type == bridge.EventToolCompleted && event.Tool != nil && event.Tool.Name == "pptx.gen" {
			if artifact != nil {
				t.Fatal("fallback generated the file more than once")
			}
			artifact = event.Tool
		}
	}
	if artifact == nil || artifact.Artifact == nil || !strings.HasPrefix(artifact.CallID, "auto-") {
		t.Fatalf("real fallback artifact missing: %+v", artifact)
	}
	folder, err := runtime.SessionFolder(sid)
	if err != nil {
		t.Fatal(err)
	}
	if info, statErr := os.Stat(filepath.Join(folder, artifact.Artifact.Path)); statErr != nil || info.Size() == 0 {
		t.Fatalf("generated file missing: %v", statErr)
	}
	page, err := e.messages.List(ctx, messageapp.PageRequest{SessionID: sid})
	if err != nil || len(page.Items) != 1 || page.Items[0].ID != terminal.Completed.MessageID || !strings.Contains(page.Items[0].Text, "项目介绍") {
		t.Fatalf("saved assistant message: %+v err=%v", page, err)
	}
	savedArtifacts := e.loadSessionArtifactsByMessage(sid)[terminal.Completed.MessageID]
	if len(savedArtifacts) != 1 || savedArtifacts[0].Path != artifact.Artifact.Path {
		t.Fatalf("artifact not linked to saved reply: %+v", savedArtifacts)
	}
	result := handleMessageProcess(e, ctx, validRequest("message.process", `{"sessionId":"`+sid+`","messageId":"`+terminal.Completed.MessageID+`"}`))
	if !result.OK {
		t.Fatalf("process lookup: %+v", result)
	}
	raw, _ := json.Marshal(result.Payload)
	var process messageProcess
	if err := json.Unmarshal(raw, &process); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(process.Thinking, "先核对输入") || len(process.Tools) != 1 || process.Tools[0].CallID != artifact.CallID || process.Tools[0].Status != "tool_completed" {
		t.Fatalf("saved process lost generation outcome: %s", raw)
	}
}

// This adapter supplies actual authored text without issuing a generator call.
type officeFallbackReplyAdapter struct{ processReplyAdapter }

func (officeFallbackReplyAdapter) Stream(_ context.Context, _ []byte, _ llmadapter.Request, emit func(llmadapter.Delta) error) (llmadapter.Response, error) {
	if err := emit(llmadapter.Delta{Reasoning: "先核对输入", Text: officeFallbackProse}); err != nil {
		return llmadapter.Response{}, err
	}
	return llmadapter.Response{}, nil
}
