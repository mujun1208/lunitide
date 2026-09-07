package app

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/llmadapter"
)

func TestChatSuggestionsUseExistingTextRequestOnly(t *testing.T) {
	for _, companion := range []bool{false, true} {
		t.Run(fmt.Sprint(companion), func(t *testing.T) {
			requests := make(chan llmadapter.Request, 1)
			e := NewEngineWithGateway(chatAttachmentProvider{}, "test", streamTestLease{})
			e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (llmadapter.Adapter, error) {
				return chatAttachmentAdapter{requests: requests}, nil
			})
			payload := fmt.Sprintf(`{"providerId":"%s","modelId":"model","companion":%t,"messages":[{"role":"user","content":"帮我分析一个会议软件的需求"}]}`, chatAttachmentProviderID, companion)
			resp := e.HandleStreaming(context.Background(), validRequest("chat.start", payload), func(bridge.Event) error { return nil })
			if !resp.OK {
				t.Fatalf("start: %+v", resp)
			}
			req := capturedChatRequest(t, requests)
			present := strings.Contains(req.Messages[0].Content, chatSuggestionsInstruction)
			if present == companion {
				t.Fatalf("suggestions instruction present=%v companion=%v", present, companion)
			}
		})
	}
}
