package app

import (
	"context"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/contextapp"
	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/llmadapter"
)

const (
	chatMessageRefID   = "01ARZ3NDEKTSV4RRFFQ69G5FB1"
	chatMessageOtherID = "01ARZ3NDEKTSV4RRFFQ69G5FB2"
	chatMessageMissing = "01ARZ3NDEKTSV4RRFFQ69G5FB3"
)

// startMessageRefChat wires an engine whose durable history contains two
// messages with canonical ULIDs so @message refs can be resolved by id.
func startMessageRefChat(t *testing.T, contextRefs string) (bridge.Response, <-chan llmadapter.Request) {
	t.Helper()
	requests := make(chan llmadapter.Request, 1)
	reader := priorUserReader{msgs: []contextapp.Message{
		{ID: chatMessageRefID, Role: "user", Content: "SELECTED MESSAGE CONTENT", Sequence: 1, TokenCount: 4},
		{ID: chatMessageOtherID, Role: "assistant", Content: "UNSELECTED MESSAGE CONTENT", Sequence: 2, TokenCount: 4},
	}}
	e := NewEngineWithContextReader(chatAttachmentProvider{}, nil, nil, nil, reader, nil, "test", streamTestLease{})
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (llmadapter.Adapter, error) {
		return chatAttachmentAdapter{requests: requests}, nil
	})
	payload := `{"providerId":"` + chatAttachmentProviderID + `","modelId":"model","sessionId":"` + chatAttachmentSessionID + `","messages":[{"role":"user","content":"current question"}]` + contextRefs + `}`
	response := e.HandleStreaming(context.Background(), validRequest("chat.start", payload), func(bridge.Event) error { return nil })
	return response, requests
}

func TestChatStartExplicitMessageRefInjectsOnlySelectedMessage(t *testing.T) {
	response, requests := startMessageRefChat(t, `,"contextRefs":[{"type":"message","id":"`+chatMessageRefID+`"}]`)
	if !response.OK {
		t.Fatalf("chat.start failed: %#v", response)
	}
	var combined strings.Builder
	for _, message := range capturedChatRequest(t, requests).Messages {
		combined.WriteString(message.Content)
	}
	// The referenced message is injected as quoted, untrusted evidence with a
	// role label. Both messages also appear as ordinary session history, so we
	// assert the role-labeled quoted-evidence form (the label only appears on
	// the injected ref) rather than mere substring presence.
	if !strings.Contains(combined.String(), "用户消息") {
		t.Fatalf("selected message role label not injected: %q", combined.String())
	}
	if strings.Contains(combined.String(), "助手消息") {
		t.Fatalf("unselected message injected as quoted evidence: %q", combined.String())
	}
}

func TestChatStartExplicitMessageRefMissingIsNotFound(t *testing.T) {
	response, _ := startMessageRefChat(t, `,"contextRefs":[{"type":"message","id":"`+chatMessageMissing+`"}]`)
	if response.OK || response.Error == nil || response.Error.Code != "CONTEXT_REF_NOT_FOUND" {
		t.Fatalf("response = %#v, want CONTEXT_REF_NOT_FOUND", response)
	}
}

func TestChatStartMessageRefRejectsNonCanonicalID(t *testing.T) {
	response, _ := startMessageRefChat(t, `,"contextRefs":[{"type":"message","id":"not-a-ulid"}]`)
	if response.OK || response.Error == nil || response.Error.Code != "BRIDGE_SCHEMA_INVALID" {
		t.Fatalf("response = %#v, want BRIDGE_SCHEMA_INVALID", response)
	}
}
