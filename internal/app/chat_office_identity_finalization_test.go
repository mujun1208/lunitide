package app

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/llmadapter"
	"github.com/lunitide/lunitide/internal/messageapp"
	"github.com/lunitide/lunitide/internal/officetools"
	"github.com/lunitide/lunitide/internal/toolruntime"
)

const officeIdentityFinalReply = "Created news-top10-acceptance.docx from the supplied text. News facts remain unverified."
const officeIdentityReferenceMarker = "OFFICE-REFERENCE-READ-FIRST"

func TestWordOfficeFinalizationWithExpertIdentityDoesNotRepeat(t *testing.T) {
	const liveGoal = "\u751f\u6210\u6587\u6863\uff1a\u8bf7\u5148\u7528 workspace.read \u5b8c\u6574\u8bfb\u53d6\u4f1a\u8bdd\u76ee\u5f55\u7684 news-reference.json\uff0c\u518d\u8c03\u7528 docx.gen \u751f\u6210 Word \u6587\u6863 news-top10-acceptance.docx\u3002\u53ea\u505a\u539f\u6587\u8f6c\u5f55\uff0c\u4e0d\u65b0\u589e\u65b0\u95fb\u4e8b\u5b9e\uff0c\u4e0d\u8054\u7f51\u3002report \u768410\u6761\u65b0\u95fb\u53ca\u4e3b\u8981\u6765\u6e90\u987b\u4fdd\u7559\u539f\u6587\u4e0e\u6570\u5b57\uff1bsources \u768423\u4e2a\u539f\u59cbURL\u5168\u90e8\u9010\u5b57\u5217\u5728\u6587\u672b\uff0c\u6ce8\u660e\u53ea\u662f\u539f\u59cb\u68c0\u7d22\u8bb0\u5f55\uff0c\u53ef\u80fd\u542b\u65e0\u5173\u7ed3\u679c\uff0c\u5e76\u975e\u9010\u6761\u65b0\u95fb\u7684\u5df2\u6838\u5b9e\u8bc1\u636e\u3002\u5f00\u5934\u6ce8\u660e\uff1a\u65b0\u95fb\u4e8b\u5b9e\u672a\u7ecf\u72ec\u7acb\u6838\u5b9e\u3002\u6587\u6863\u5199\u5165\u6807\u8bb0 NEWS-DOCX-ACCEPTANCE-20260908\u3001sourceRun \u548c sourceMessage \u7684\u539f\u503c\u3002\u53ea\u4fdd\u5b58\u5230\u672c\u4f1a\u8bdd\u76ee\u5f55\uff0c\u4e0d\u6253\u5f00\u6587\u4ef6\u6216\u5e94\u7528\u3002\u6700\u540e\u56de\u8bfb\u6838\u5bf9\u3002"
	identities := []struct{ name, expert string }{
		{"ppt", "PPT\u4e13\u5bb6"},
		{"report", "\u62a5\u544a\u7f16\u5199\u4e13\u5bb6"},
		{"novel", "\u5c0f\u8bf4\u7f16\u5199\u4e13\u5bb6"},
	}
	goals := []struct{ name, text string }{
		{"exact-live-goal", liveGoal},
		{"explicit-report", liveGoal + "\u8fd9\u662f\u4e00\u4efd\u4e2d\u6587\u65b0\u95fb\u62a5\u544a\u3002"},
	}
	for _, identity := range identities {
		for _, goal := range goals {
			t.Run(identity.name+"/"+goal.name, func(t *testing.T) {
				testWordOfficeIdentityFinalization(t, identity.expert, goal.text)
			})
		}
	}
}

func testWordOfficeIdentityFinalization(t *testing.T, expert, goal string) {
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
	blocks := officetools.SampleStyledDocxBlocks()
	blocks[1].Text = officeIdentityReferenceMarker + " https://example.test/original-source " + blocks[1].Text
	args, err := json.Marshal(map[string]any{
		"path": "news-top10-acceptance.docx", "title": "Offline news conversion",
		"blocks": blocks,
	})
	if err != nil {
		t.Fatal(err)
	}
	folder, err := runtime.SessionFolder(sid)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folder, "news-reference.json"), args, 0600); err != nil {
		t.Fatal(err)
	}
	adapter := &officeIdentityFinalizationAdapter{args: args}
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (llmadapter.Adapter, error) {
		return adapter, nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	state := &streamState{cancel: cancel, state: streamRunning}
	var events []bridge.Event
	// A real reference read must not reset offline generation readiness.
	identity := "[\u7a33\u5b9a\u8eab\u4efd] \u4f60\u5c31\u662f\u300c" + expert + "\u300d\u3002"
	e.runStream(ctx, "01ARZ3NDEKTSV4RRFFQ69G5FAV", state,
		provider.Provider{ID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", Protocol: provider.ProtocolOpenAICompatible, BaseURL: "https://api.example.com", CredentialRef: "credential-ref"},
		llmadapter.Request{Model: "model", Messages: []llmadapter.Message{
			{Role: llmadapter.RoleSystem, Content: identity},
			{Role: llmadapter.RoleUser, Content: goal},
		}, Tools: engineToolDefinitions()},
		func(event bridge.Event) error { events = append(events, event); return nil }, sid, executionModeFullAccess)
	if adapter.calls != 3 {
		t.Fatalf("model calls = %d, want one reference read, one generation and one final reply", adapter.calls)
	}
	if len(events) == 0 {
		t.Fatal("stream emitted no events")
	}
	terminal := events[len(events)-1]
	if terminal.Type != bridge.EventCompleted || terminal.Completed == nil || terminal.Completed.PersistFailed || terminal.Completed.MessageID == "" {
		t.Fatalf("expected durable completion: %+v", terminal)
	}
	var artifact *bridge.ToolEvent
	var reference *bridge.ToolEvent
	var reply strings.Builder
	for _, event := range events {
		if event.Thinking != nil && strings.Contains(event.Thinking.Text, "PPT \u6d41\u7a0b") {
			t.Fatal("explicit Word conversion activated the PPT workflow")
		}
		if event.Delta != nil {
			reply.WriteString(event.Delta.Text)
		}
		if event.Tool == nil {
			continue
		}
		if event.Tool.Name != "docx.gen" && event.Tool.Name != "workspace.read" {
			t.Fatalf("unexpected tool during offline Word conversion: %s", event.Tool.Name)
		}
		if event.Type == bridge.EventToolCompleted {
			if event.Tool.Name == "workspace.read" {
				if reference != nil || artifact != nil {
					t.Fatal("reference must be read exactly once before generation")
				}
				reference = event.Tool
				continue
			}
			if reference == nil {
				t.Fatal("generation ran before reading the reference")
			}
			if artifact != nil {
				t.Fatal("generated the artifact more than once")
			}
			artifact = event.Tool
		}
	}
	if reply.String() != officeIdentityFinalReply {
		t.Fatalf("streamed closeout repeated or changed: %q", reply.String())
	}
	if reference == nil || reference.CallID != "reference-read" || !strings.Contains(reference.Summary, "complete=true") || !strings.Contains(reference.Summary, officeIdentityReferenceMarker) {
		t.Fatalf("real complete reference read missing: %+v", reference)
	}
	if artifact == nil || artifact.Artifact == nil || artifact.CallID != "word-generation" || artifact.Artifact.Path != "news-top10-acceptance.docx" {
		t.Fatalf("real model-requested DOCX receipt missing: %+v", artifact)
	}
	doc, err := zip.OpenReader(filepath.Join(folder, artifact.Artifact.Path))
	if err != nil {
		t.Fatalf("generated DOCX is not a real ZIP: %v", err)
	}
	defer doc.Close()
	body, err := doc.Open("word/document.xml")
	if err != nil {
		t.Fatal(err)
	}
	content, readErr := io.ReadAll(body)
	closeErr := body.Close()
	if readErr != nil || closeErr != nil || !strings.Contains(string(content), "Offline news conversion") || !strings.Contains(string(content), officeIdentityReferenceMarker) || !strings.Contains(string(content), "https://example.test/original-source") {
		t.Fatalf("generated DOCX content missing: read=%v close=%v", readErr, closeErr)
	}
	page, err := e.messages.List(ctx, messageapp.PageRequest{SessionID: sid})
	if err != nil || len(page.Items) != 1 || page.Items[0].ID != terminal.Completed.MessageID || page.Items[0].Text != officeIdentityFinalReply {
		t.Fatalf("expected one concise persisted reply: %+v err=%v", page, err)
	}
	savedArtifacts := e.loadSessionArtifactsByMessage(sid)[terminal.Completed.MessageID]
	if len(savedArtifacts) != 1 || savedArtifacts[0].Path != artifact.Artifact.Path {
		t.Fatalf("expected one persisted artifact receipt: %+v", savedArtifacts)
	}
	result := handleMessageProcess(e, ctx, validRequest("message.process", `{"sessionId":"`+sid+`","messageId":"`+terminal.Completed.MessageID+`"}`))
	if !result.OK {
		t.Fatalf("process lookup: %+v", result)
	}
	raw, err := json.Marshal(result.Payload)
	if err != nil {
		t.Fatal(err)
	}
	var process messageProcess
	if err := json.Unmarshal(raw, &process); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(process.Thinking, "PPT 流程") || len(process.Tools) != 2 ||
		process.Tools[0].Name != "workspace.read" || process.Tools[0].CallID != reference.CallID || process.Tools[0].Status != "tool_completed" ||
		process.Tools[1].Name != "docx.gen" || process.Tools[1].CallID != artifact.CallID || process.Tools[1].Status != "tool_completed" {
		t.Fatalf("unexpected persisted workflow: %s", raw)
	}
}

type officeIdentityFinalizationAdapter struct {
	processReplyAdapter
	args  json.RawMessage
	calls int
}

func (a *officeIdentityFinalizationAdapter) Stream(_ context.Context, _ []byte, req llmadapter.Request, emit func(llmadapter.Delta) error) (llmadapter.Response, error) {
	a.calls++
	if a.calls <= 2 {
		call := llmadapter.ToolCall{ID: "reference-read", Name: "workspace.read", Arguments: json.RawMessage(`{"path":"news-reference.json","offset":0}`)}
		if a.calls == 2 {
			if output := lastToolOutput(req.Messages); !strings.Contains(output, officeIdentityReferenceMarker) || !strings.Contains(output, "complete=true") {
				return llmadapter.Response{}, errors.New("reference read did not reach the model")
			}
			call = llmadapter.ToolCall{ID: "word-generation", Name: "docx.gen", Arguments: a.args}
		}
		if err := emit(llmadapter.Delta{ToolCall: &call}); err != nil {
			return llmadapter.Response{}, err
		}
		return llmadapter.Response{Message: llmadapter.Message{Role: llmadapter.RoleAssistant, ToolCalls: []llmadapter.ToolCall{call}}, FinishReason: "tool_calls"}, nil
	}
	if a.calls > 3 {
		return llmadapter.Response{}, errors.New("unexpected model call after final Word closeout")
	}
	if err := emit(llmadapter.Delta{Text: officeIdentityFinalReply}); err != nil {
		return llmadapter.Response{}, err
	}
	return llmadapter.Response{Message: llmadapter.Message{Role: llmadapter.RoleAssistant, Content: officeIdentityFinalReply}, FinishReason: "stop"}, nil
}
