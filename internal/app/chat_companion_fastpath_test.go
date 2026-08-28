package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/contextapp"
	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/domain/skill"
	"github.com/lunitide/lunitide/internal/gateway"
	"github.com/lunitide/lunitide/internal/toolruntime"
)

func TestCompanionSpeakFallbackUsesGenericVoiceLine(t *testing.T) {
	out := companionSpeakFallback(gateway.Response{
		Message:   gateway.Message{Content: ""},
		Reasoning: "嗯，你好呀！我在呢。后面还有很长的内心独白…",
	})
	if out != "嗯，我在呢，稍等我一下。" {
		t.Fatalf("got %q", out)
	}
	if companionSpeakFallback(gateway.Response{Message: gateway.Message{Content: "直接回答。"}}) != "直接回答。" {
		t.Fatal("content should win")
	}
}

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
	if !strings.Contains(system, "身份记忆") || !strings.Contains(system, "月汐") || !strings.Contains(system, "私人助理") {
		t.Fatalf("companion identity memory missing: %q", system)
	}
	if !strings.Contains(system, "不要原样复读") {
		t.Fatalf("companion must not echo the user verbatim: %q", system)
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

type emptyCompanionReader struct{}

func (emptyCompanionReader) ListMessages(context.Context, string, string, int) ([]contextapp.Message, error) {
	return nil, nil
}
func (emptyCompanionReader) SumTokens(context.Context, string, string, string, string) (int64, error) {
	return 0, nil
}

type errCompanionReader struct{ err error }

func (r errCompanionReader) ListMessages(context.Context, string, string, int) ([]contextapp.Message, error) {
	return nil, r.err
}
func (r errCompanionReader) SumTokens(context.Context, string, string, string, string) (int64, error) {
	return 0, nil
}

func TestCompanionEmptySessionFallsBackToSpokenTurn(t *testing.T) {
	requests := make(chan gateway.Request, 1)
	e := NewEngineWithGateway(chatAttachmentProvider{}, "test", streamTestLease{})
	e.messageReader = emptyCompanionReader{}
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (gateway.Adapter, error) {
		return chatAttachmentAdapter{requests: requests}, nil
	})
	payload := `{"providerId":"` + chatAttachmentProviderID + `","modelId":"model","sessionId":"` + chatAttachmentSessionID + `","companion":true,"messages":[{"role":"user","content":"今晚月色如何"}]}`
	response := e.HandleStreaming(context.Background(), validRequest("chat.start", payload), func(bridge.Event) error { return nil })
	if !response.OK {
		t.Fatalf("companion empty-session chat.start failed: %#v", response)
	}
	req := capturedChatRequest(t, requests)
	var foundSpoken bool
	for _, m := range req.Messages {
		if m.Role == gateway.RoleUser && strings.Contains(m.Content, "今晚月色如何") {
			foundSpoken = true
		}
	}
	if !foundSpoken {
		t.Fatalf("spoken turn missing after empty-session fallback: %#v", req.Messages)
	}
}

func TestCompanionAssemblyReadErrorFallsBackToSpokenTurn(t *testing.T) {
	requests := make(chan gateway.Request, 1)
	e := NewEngineWithGateway(chatAttachmentProvider{}, "test", streamTestLease{})
	e.messageReader = errCompanionReader{err: errors.New("sqlite busy")}
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (gateway.Adapter, error) {
		return chatAttachmentAdapter{requests: requests}, nil
	})
	payload := `{"providerId":"` + chatAttachmentProviderID + `","modelId":"model","sessionId":"` + chatAttachmentSessionID + `","companion":true,"messages":[{"role":"user","content":"你好"}]}`
	response := e.HandleStreaming(context.Background(), validRequest("chat.start", payload), func(bridge.Event) error { return nil })
	if !response.OK {
		t.Fatalf("companion should not surface CONTEXT_ASSEMBLY_FAILED: %#v", response)
	}
	req := capturedChatRequest(t, requests)
	if lastUserChatText(req.Messages) != "你好" {
		t.Fatalf("spoken turn missing: %#v", req.Messages)
	}
}

func TestChatStartEmptyHistoryWithoutMessagesStillFailsAssembly(t *testing.T) {
	e := NewEngineWithGateway(chatAttachmentProvider{}, "test", streamTestLease{})
	e.messageReader = emptyCompanionReader{}
	payload := `{"providerId":"` + chatAttachmentProviderID + `","modelId":"model","sessionId":"` + chatAttachmentSessionID + `"}`
	response := e.HandleStreaming(context.Background(), validRequest("chat.start", payload), func(bridge.Event) error { return nil })
	if response.OK || response.Error.Code != "CONTEXT_ASSEMBLY_FAILED" {
		t.Fatalf("empty durable history without messages = %#v", response)
	}
}

func TestUseExplicitChatFallback(t *testing.T) {
	trusted := []gateway.Message{{Role: gateway.RoleUser, Content: "hi"}}
	if !useExplicitChatFallback(true, trusted, contextapp.ErrNoMessages) {
		t.Fatal("companion empty history must fall back")
	}
	if !useExplicitChatFallback(false, trusted, contextapp.ErrNoMessages) {
		t.Fatal("explicit user turn must recover empty history")
	}
	if useExplicitChatFallback(false, trusted, contextapp.ErrEnvelopeBudgetTooSmall) {
		t.Fatal("non-companion budget failure must stay fail-closed")
	}
	if !useExplicitChatFallback(true, trusted, contextapp.ErrEnvelopeBudgetTooSmall) {
		t.Fatal("companion budget failure must fall back")
	}
	if useExplicitChatFallback(true, nil, contextapp.ErrNoMessages) {
		t.Fatal("no spoken turn means no fallback")
	}
}

func TestCompanionWantsTools(t *testing.T) {
	if companionWantsTools("今晚月色如何") || companionWantsTools("你好") {
		t.Fatal("idle chat must not request tools")
	}
	if !companionWantsTools("打开网页") || !companionWantsTools("搜一下今天新闻") {
		t.Fatal("action chat must request tools")
	}
	if !companionWantsTools("把开了我把它桌面上的") || !companionWantsTools("打开桌面上的协议文档") {
		t.Fatal("garbled desktop-open ASR must still request tools")
	}
}

func TestCompanionOpeningAck(t *testing.T) {
	if got := companionOpeningAck("一场大雨淋湿了眼睛。"); got != "嗯，我听到了。" {
		t.Fatalf("got %q", got)
	}
	if got := companionOpeningAck("你好"); got != "嗨，我在呢。" {
		t.Fatalf("got %q", got)
	}
}

func TestCompanionToolLeadIn(t *testing.T) {
	if got := companionToolLeadIn("web.search"); got != "好，我帮你查一下。" {
		t.Fatalf("got %q", got)
	}
	if got := companionToolLeadIn("cc.click"); got != "好，我来操作电脑。" {
		t.Fatalf("got %q", got)
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
