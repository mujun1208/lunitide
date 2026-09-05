package app

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

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
	if out != "我在呢，稍等我一下。" {
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
	if strings.Contains(system, "不要 web.fetch") {
		t.Fatalf("idle weather talk must not ship the tools weather clause: %q", system)
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
	want := map[string]bool{"web.fetch": false, "browser.act": false, "user.ask": false}
	forbidden := map[string]bool{"command.run": true, "im.send": true, "desktop.open": true, "media.play": true, "computer.act": true}
	for _, def := range req.Tools {
		if _, ok := want[def.Name]; ok {
			want[def.Name] = true
		}
		if forbidden[def.Name] {
			t.Fatalf("companion must not advertise %s", def.Name)
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
	if companionWantsTools("继续聊") || companionWantsTools("我随便说说") || companionWantsTools("后面那个更好听") {
		t.Fatal("idle fillers must not request tools")
	}
	if companionWantsTools("查一下天气") == false {
		t.Fatal("weather lookup must request tools")
	}
	if companionWantsTools("今晚天气") {
		t.Fatal("bare weather chat must stay idle")
	}
	if !companionWantsTools("帮我做手册解析") {
		t.Fatal("skill-shaped 帮我做 must request tools")
	}
	if !companionWantsTools("打开网页") || !companionWantsTools("搜一下今天新闻") {
		t.Fatal("action chat must request tools")
	}
	if !companionWantsTools("把开了我把它桌面上的") || !companionWantsTools("打开桌面上的协议文档") {
		t.Fatal("garbled desktop-open ASR must still request tools")
	}
	if !companionWantsTools("在证件号码后面填一下") || !companionWantsTools("输入完点发送") {
		t.Fatal("document fill and send must request tools")
	}
	if !companionWantsTools("随机播放") {
		t.Fatal("shuffle play must request tools")
	}
	if !companionWantsTools("继续填表") || !companionWantsTools("下一步再点一下") {
		t.Fatal("desktop follow-through must request tools")
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

func TestCompanionChatStartDropsFailedAssistantBeforeNewUser(t *testing.T) {
	requests := make(chan gateway.Request, 1)
	e := NewEngineWithGateway(chatAttachmentProvider{}, "test", streamTestLease{})
	e.messageReader = priorUserReader{msgs: []contextapp.Message{
		{ID: "u1", Role: "user", Content: "打开汽水", Sequence: 1, TokenCount: 4},
		{ID: "a1", Role: "assistant", Content: "无法执行。窗口没到前台", Sequence: 2, TokenCount: 8},
	}}
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (gateway.Adapter, error) {
		return chatAttachmentAdapter{requests: requests}, nil
	})
	payload := `{"providerId":"` + chatAttachmentProviderID + `","modelId":"model","sessionId":"` + chatAttachmentSessionID + `","companion":true,"messages":[{"role":"user","content":"再打开一次汽水"}]}`
	response := e.HandleStreaming(context.Background(), validRequest("chat.start", payload), func(bridge.Event) error { return nil })
	if !response.OK {
		t.Fatalf("C7-6 companion chat.start failed: %#v", response)
	}
	req := capturedChatRequest(t, requests)
	for _, m := range req.Messages {
		if m.Role != gateway.RoleSystem && strings.Contains(m.Content, "无法执行") {
			t.Fatalf("C7-6 assembled request must drop failed assistant: %#v", req.Messages)
		}
	}
	if lastUserChatText(req.Messages) != "再打开一次汽水" {
		t.Fatalf("C7-6 must keep the new user turn: %#v", req.Messages)
	}
}

func TestCompanionChatStartDoesNotEmitOpeningAckDelta(t *testing.T) {
	var mu sync.Mutex
	var texts []string
	done := make(chan struct{})
	requests := make(chan gateway.Request, 1)
	e := NewEngineWithGateway(chatAttachmentProvider{}, "test", streamTestLease{})
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (gateway.Adapter, error) {
		return chatAttachmentAdapter{requests: requests}, nil
	})
	payload := `{"providerId":"` + chatAttachmentProviderID + `","modelId":"model","companion":true,"messages":[{"role":"user","content":"今晚月色如何"}]}`
	response := e.HandleStreaming(context.Background(), validRequest("chat.start", payload), func(ev bridge.Event) error {
		mu.Lock()
		if ev.Delta != nil {
			texts = append(texts, ev.Delta.Text)
		}
		terminal := ev.Type == bridge.EventCompleted || ev.Type == bridge.EventFailed || ev.Type == bridge.EventCancelled
		mu.Unlock()
		if terminal {
			select {
			case <-done:
			default:
				close(done)
			}
		}
		return nil
	})
	if !response.OK {
		t.Fatalf("companion chat.start failed: %#v", response)
	}
	_ = capturedChatRequest(t, requests)
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("timed out waiting for stream terminal")
	}
	mu.Lock()
	blob := strings.Join(texts, "")
	mu.Unlock()
	for _, banned := range []string{"嗯，", "嗨，我在呢", companionOpeningAck("今晚月色如何")} {
		if banned != "" && strings.Contains(blob, banned) {
			t.Fatalf("opening ack leaked into stream: %q in %q", banned, blob)
		}
	}
}

func TestShouldInjectCompanionToolLeadIn(t *testing.T) {
	if !shouldInjectCompanionToolLeadIn("", false) {
		t.Fatal("empty first step should inject")
	}
	if shouldInjectCompanionToolLeadIn("我就帮你查一下今天的天气哈！", false) {
		t.Fatal("model already spoke — do not inject")
	}
	if shouldInjectCompanionToolLeadIn("", true) {
		t.Fatal("second tool step must not replay the pad")
	}
	if shouldInjectCompanionToolLeadIn("无法执行。桌面被拦住了", false) {
		t.Fatal("failure text must not get a lead-in")
	}
}

func TestCompanionRedundantWebSkip(t *testing.T) {
	msg, skip := companionRedundantWebSkip(true, []string{"web.search"}, "web.search", "今天天气怎么样", false)
	if !skip || !strings.Contains(msg, "已经有搜索摘要") {
		t.Fatalf("second search: %q skip=%v", msg, skip)
	}
	if _, skip := companionRedundantWebSkip(true, nil, "web.fetch", "今天天气怎么样", true); !skip {
		t.Fatal("fetch after search without a URL must skip")
	}
	if _, skip := companionRedundantWebSkip(true, []string{"web.search"}, "web.fetch", "打开 https://example.com/weather", false); skip {
		t.Fatal("user-supplied URL fetch must run")
	}
	if _, skip := companionRedundantWebSkip(true, nil, "web.search", "今天天气怎么样", false); skip {
		t.Fatal("first search must run")
	}
	if _, skip := companionRedundantWebSkip(false, []string{"web.search"}, "web.fetch", "今天天气怎么样", false); skip {
		t.Fatal("typed chat must not skip fetch")
	}
	if _, skip := companionRedundantWebSkip(true, []string{"web.search"}, "video.understand", "https://www.bilibili.com/video/BV1xx", true); skip {
		t.Fatal("D-V14: video.understand must not be treated as a second weather search")
	}
}

func TestCompanionToolLeadIn(t *testing.T) {
	if got := companionToolLeadIn("web.search"); got != "好，我帮你查一下。" {
		t.Fatalf("got %q", got)
	}
	if got := companionToolLeadIn("cc.click"); got != "好，我来操作电脑。" {
		t.Fatalf("got %q", got)
	}
	if got := companionToolLeadIn("desktop.type"); got != "好，我来输入。" {
		t.Fatalf("got %q", got)
	}
	if got := companionToolLeadIn("video.understand"); got != "好，我先看下这个链接。" {
		t.Fatalf("got %q", got)
	}
	if got := companionToolResultSpeech("desktop.type", `typed "204040" after "证件号码"`); got != "已经写入了 204040。" {
		t.Fatalf("result %q", got)
	}
	if got := companionToolResultSpeech("desktop.open", "无法执行：桌面上有多份匹配"); !strings.Contains(got, "无法执行") {
		t.Fatalf("fail %q", got)
	}
	if got := companionToolResultSpeech("computer.act", "clicked; verify capture failed"); got != "这次没有完成。" {
		t.Fatalf("verify fail speech %q", got)
	}
	if got := companionToolResultSpeech("computer.act", "COMPUTER_STALE_FRAME: echo frameId"); got != "这次没有完成。" {
		t.Fatalf("stale speech %q", got)
	}
	if got := companionToolResultSpeech("computer.act", "uac dialog — needs_user: 这是系统提权对话框"); got != "这是系统提权对话框，我不能代点「是」。请你自己确认或取消。" {
		t.Fatalf("uac speech %q", got)
	}
	if got := companionToolResultSpeech("computer.act", "ok:false\nM10-CC-012: 电脑控制未启用"); got != "电脑控制未启用。第一次控桌面请到设置里打开。" {
		t.Fatalf("disabled speech %q", got)
	}
	if got := companionToolResultSpeech("browser.act", "ok:false\nBROWSER_MCP_NOT_READY: Playwright MCP 未就绪"); got != companionBrowserMCPSpeech {
		t.Fatalf("browser unready speech %q", got)
	}
	if got := companionToolResultSpeech("computer.act", "screenshot frameId=01ARZ3NDEKTSV4RRFFQ69G5FAV"); got != "先看了一下。" {
		t.Fatalf("screenshot must not claim done: %q", got)
	}
	if got := companionToolResultSpeech("computer.act", "clicked left mouse 1 time(s)"); got != "点了一下。" {
		t.Fatalf("click mid-step must not claim done: %q", got)
	}
	if got := companionToolResultSpeech("browser.act", "ok"); got != "这次没有完成。" {
		t.Fatalf("empty browser ok must not claim done: %q", got)
	}
	if got := companionToolResultSpeech("skill.invoke", "ok"); got != "还在处理。" {
		t.Fatalf("opaque skill closeout must not claim done: %q", got)
	}
	if got := companionToolResultSpeech("filesystem.write", ""); got != "还在处理。" {
		t.Fatalf("empty unknown tool must not claim done: %q", got)
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
