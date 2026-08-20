package app

import (
	"context"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/domain/skill"
	"github.com/lunitide/lunitide/internal/gateway"
	"github.com/lunitide/lunitide/internal/toolruntime"
)

func TestCompanionFastPathCapsTokensAndKeepsVoice(t *testing.T) {
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
	if strings.Contains(system, "内置工作流") {
		t.Fatalf("companion injected bundled workflows: %q", system)
	}
	if !strings.Contains(system, "第一句") || !strings.Contains(system, "闲聊立刻回答") || !strings.Contains(system, "调用对应工具") {
		t.Fatalf("companion voice instruction missing: %q", system)
	}
	if strings.Contains(system, "可用技能目录") {
		t.Fatalf("idle companion injected skill catalog: %q", system)
	}
	if len(req.Tools) != 0 {
		t.Fatalf("idle companion attached tools: %#v", req.Tools)
	}
}

func TestCompanionAttachesFullToolset(t *testing.T) {
	requests := make(chan gateway.Request, 1)
	e := NewEngineWithGateway(chatAttachmentProvider{}, "test", streamTestLease{})
	tools, err := toolruntime.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { tools.Close() })
	e.SetToolRuntime(tools)
	e.skills = &skillCatalogStub{items: []skill.Skill{catalogTestSkill("demo", "unused", `{}`)}}
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (gateway.Adapter, error) {
		return chatAttachmentAdapter{requests: requests}, nil
	})
	payload := `{"providerId":"` + chatAttachmentProviderID + `","modelId":"model","companion":true,"executionMode":"full-access","messages":[{"role":"user","content":"打开网页"}]}`
	response := e.HandleStreaming(context.Background(), validRequest("chat.start", payload), func(bridge.Event) error { return nil })
	if !response.OK {
		t.Fatalf("companion chat.start failed: %#v", response)
	}
	req := capturedChatRequest(t, requests)
	want := map[string]bool{"web.search": false, "web.fetch": false, "workspace.write": false, "command.run": false, "browser.act": false, "skill.invoke": false}
	for _, def := range req.Tools {
		if _, ok := want[def.Name]; ok {
			want[def.Name] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Fatalf("companion tools missing %s: %#v", name, req.Tools)
		}
	}
}

func TestCompanionWantsTools(t *testing.T) {
	if companionWantsTools("今晚月色如何") || companionWantsTools("你好") {
		t.Fatal("idle chat must not request tools")
	}
	if !companionWantsTools("打开网页") || !companionWantsTools("搜一下今天新闻") {
		t.Fatal("action chat must request tools")
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
