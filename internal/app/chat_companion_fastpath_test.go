package app

import (
	"context"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/domain/skill"
	"github.com/lunitide/lunitide/internal/gateway"
)

func TestCompanionFastPathCapsTokensAndSkipsCatalog(t *testing.T) {
	requests := make(chan gateway.Request, 1)
	e := NewEngineWithGateway(chatAttachmentProvider{}, "test", streamTestLease{})
	e.skills = &skillCatalogStub{items: []skill.Skill{catalogTestSkill("demo", "unused catalog", `{}`)}}
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (gateway.Adapter, error) {
		return chatAttachmentAdapter{requests: requests}, nil
	})
	payload := `{"providerId":"` + chatAttachmentProviderID + `","modelId":"model","companion":true,"messages":[{"role":"user","content":"今晚天气"}]}`
	response := e.HandleStreaming(context.Background(), validRequest("chat.start", payload), func(bridge.Event) error { return nil })
	if !response.OK {
		t.Fatalf("companion chat.start failed: %#v", response)
	}
	req := capturedChatRequest(t, requests)
	if req.MaxTokens != companionMaxTokens {
		t.Fatalf("MaxTokens=%d, want %d", req.MaxTokens, companionMaxTokens)
	}
	if !req.DisableReasoning {
		t.Fatal("companion must disable reasoning for first-token latency")
	}
	if len(req.Messages) == 0 || req.Messages[0].Role != gateway.RoleSystem {
		t.Fatalf("messages = %#v", req.Messages)
	}
	system := req.Messages[0].Content
	if strings.Contains(system, "可用技能目录") || strings.Contains(system, "内置工作流") {
		t.Fatalf("companion injected skill catalog: %q", system)
	}
	if !strings.Contains(system, "第一句") {
		t.Fatalf("companion voice instruction missing: %q", system)
	}
}

func TestAdapterCacheReusesProductionConnector(t *testing.T) {
	e := NewEngineWithGateway(chatAttachmentProvider{}, "test", streamTestLease{})
	p, err := chatAttachmentProvider{}.Get(context.Background(), chatAttachmentProviderID)
	if err != nil {
		t.Fatal(err)
	}
	sentinel := chatAttachmentAdapter{requests: make(chan gateway.Request, 1)}
	key := p.ID + "\x00" + p.BaseURL + "\x00" + string(p.Protocol)
	e.adapterCache[key] = sentinel
	got, err := e.adapter(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	cached, ok := got.(chatAttachmentAdapter)
	if !ok || cached.requests != sentinel.requests {
		t.Fatalf("adapter cache miss: got %#v", got)
	}
}
