package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/contextapp"
	"github.com/lunitide/lunitide/internal/domain/message"
	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/hostbridge"
	"github.com/lunitide/lunitide/internal/llmadapter"
)

func TestChatLongInputFullDescriptionReachesProvider(t *testing.T) {
	for _, reader := range []contextapp.Reader{nil, explicitChatReader{}} {
		requests := make(chan llmadapter.Request, 1)
		e := NewEngineWithContextReader(chatAttachmentProvider{}, nil, nil, nil, reader, nil, "test", streamTestLease{})
		e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (llmadapter.Adapter, error) {
			return chatAttachmentAdapter{requests: requests}, nil
		})
		text := strings.Repeat("能力描述必须完整传递。", 2500) + "最终约束：原有功能必须保留。"
		raw, _ := json.Marshal(map[string]any{"providerId": chatAttachmentProviderID, "modelId": "model", "sessionId": chatAttachmentSessionID, "messages": []map[string]string{{"role": "user", "content": text}}})
		request := validRequest("chat.start", string(raw))
		wire, _ := json.Marshal(request)
		if len(wire) > hostbridge.MaxMessageBytes {
			t.Fatal("legal long input exceeds host transport")
		}
		ctx, cancel := context.WithCancel(context.Background())
		t.Cleanup(cancel)
		response := e.HandleStreaming(ctx, request, func(bridge.Event) error { return nil })
		if !response.OK {
			t.Fatalf("long input rejected by chat.start: %+v", response.Error)
		}
		var got llmadapter.Request
		select {
		case got = <-requests:
		case <-time.After(30 * time.Second):
			t.Fatal("long description did not reach provider")
		}
		found := false
		for _, m := range got.Messages {
			if m.Role == llmadapter.RoleUser && strings.Contains(m.Content, text) {
				found = true
			}
		}
		if !found {
			t.Fatal("provider lost complete description or final constraint")
		}
	}
}

func TestChatTypedLongInputKeepsRoleAndByteLimits(t *testing.T) {
	for _, text := range []string{strings.Repeat("描述", 16000) + "TAIL_CONSTRAINT", strings.Repeat("😀", message.MaxRunes)} {
		if !validChatMessages("model", []llmadapter.Message{{Role: llmadapter.RoleUser, Content: text}}) {
			t.Fatal("valid long description rejected")
		}
	}
	for _, m := range []llmadapter.Message{
		{Role: llmadapter.RoleUser, Content: strings.Repeat("a", message.MaxRunes+1)},
		{Role: llmadapter.RoleUser, Content: "a\x00b"},
		{Role: llmadapter.RoleUser, Content: string([]byte{255})},
		{Role: llmadapter.RoleAssistant, Content: strings.Repeat("a", message.MaxRunesAssistant+1)},
		{Role: llmadapter.RoleTool, Content: "untrusted tool"},
		{Role: llmadapter.RoleUser, Content: "text", ToolCallID: "forged"},
	} {
		if validChatMessages("model", []llmadapter.Message{m}) {
			t.Fatalf("invalid role/text accepted: %s, %d bytes", m.Role, len(m.Content))
		}
	}
}
