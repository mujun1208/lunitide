package app

import (
	"context"
	"testing"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/toolruntime"
)

func TestHandlePersistRetryStartStampsStreamEnvelope(t *testing.T) {
	tools, err := toolruntime.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer tools.Close()
	spy := &appendAssistantSpy{}
	e := NewEngineWithGateway(nil, "test", streamTestLease{})
	e.SetToolRuntime(tools)
	e.messages = spy
	session := chatAttachmentSessionID
	e.saveTurnCheckpoint(session, chatTurnCheckpoint{
		Status:        turnStatusCompleted,
		PersistDraft:  "已写好但没落库",
		PersistFailed: true,
		StreamID:      "01ARZ3NDEKTSV4RRFFQ69G5FAD",
	})
	events := make(chan bridge.Event, 4)
	payload := `{"providerId":"` + chatAttachmentProviderID + `","modelId":"model","sessionId":"` + session + `","messages":[{"role":"user","content":"` + persistRetrySentinel + `"}]}`
	response := e.HandleStreaming(context.Background(), validRequest("chat.start", payload), func(event bridge.Event) error {
		events <- event
		return nil
	})
	if !response.OK {
		t.Fatalf("persist retry start: %#v", response)
	}
	payloadMap, _ := response.Payload.(map[string]any)
	streamID, _ := payloadMap["streamId"].(string)
	if streamID == "" {
		t.Fatalf("payload=%#v", response.Payload)
	}
	terminal := terminalEvent(t, events)
	if terminal.Type != bridge.EventCompleted || terminal.Completed == nil || terminal.Completed.PersistFailed || terminal.Completed.MessageID == "" {
		t.Fatalf("terminal=%#v", terminal)
	}
	if terminal.Version != bridge.Version || terminal.Kind != "event" || terminal.ID == "" || terminal.StreamID != streamID || terminal.Sequence != 1 {
		t.Fatalf("envelope=%#v streamId=%s", terminal, streamID)
	}
	if len(spy.calls) != 1 || spy.calls[0] != "已写好但没落库" {
		t.Fatalf("draft write=%v", spy.calls)
	}
}

func TestHandleChatTurnGetReturnsPersistAndResume(t *testing.T) {
	tools, err := toolruntime.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer tools.Close()
	e := NewEngine(nil, "test")
	e.SetToolRuntime(tools)
	session := chatAttachmentSessionID
	empty := e.Handle(context.Background(), validRequest("chat.turn.get", `{"sessionId":"`+session+`"}`))
	if !empty.OK {
		t.Fatalf("empty inspect: %#v", empty)
	}
	emptyMap, _ := empty.Payload.(map[string]any)
	if emptyMap["status"] != "" || emptyMap["persistFailed"] != false || emptyMap["persistDraft"] != "" {
		t.Fatalf("empty payload=%#v", empty.Payload)
	}
	e.saveTurnCheckpoint(session, chatTurnCheckpoint{
		Status:        turnStatusInterrupted,
		PersistDraft:  "已经生成但没落库",
		PersistFailed: true,
	})
	got := e.Handle(context.Background(), validRequest("chat.turn.get", `{"sessionId":"`+session+`"}`))
	if !got.OK {
		t.Fatalf("inspect: %#v", got)
	}
	payload, _ := got.Payload.(map[string]any)
	if payload["status"] != turnStatusInterrupted || payload["persistFailed"] != true || payload["persistDraft"] != "已经生成但没落库" {
		t.Fatalf("payload=%#v", got.Payload)
	}
	e.saveTurnCheckpoint(session, chatTurnCheckpoint{
		Status:       turnStatusRunning,
		PersistDraft: "流到一半还没写完",
	})
	live := e.Handle(context.Background(), validRequest("chat.turn.get", `{"sessionId":"`+session+`"}`))
	if !live.OK {
		t.Fatalf("running inspect: %#v", live)
	}
	liveMap, _ := live.Payload.(map[string]any)
	if liveMap["status"] != turnStatusRunning || liveMap["persistFailed"] != false || liveMap["persistDraft"] != "流到一半还没写完" {
		t.Fatalf("running payload=%#v", live.Payload)
	}
}
