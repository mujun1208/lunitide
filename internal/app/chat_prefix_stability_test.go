package app

import (
	"context"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/llmadapter"
)

// chatSystemView concatenates every system message of a request (stable head
// plus trailing per-turn injection), giving tests the same single-string view
// they had when all guidance lived in the head message.
func chatSystemView(req llmadapter.Request) string {
	var b strings.Builder
	for _, m := range req.Messages {
		if m.Role == llmadapter.RoleSystem {
			b.WriteString(m.Content)
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// startPrefixStabilityChat runs one chat turn and captures the request the
// adapter received, so tests can assert where instructions actually landed.
func startPrefixStabilityChat(t *testing.T, payload string) llmadapter.Request {
	t.Helper()
	requests := make(chan llmadapter.Request, 1)
	e := NewEngineWithGateway(chatAttachmentProvider{}, "test", streamTestLease{})
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (llmadapter.Adapter, error) {
		return chatAttachmentAdapter{requests: requests}, nil
	})
	response := e.HandleStreaming(context.Background(), validRequest("chat.start", payload), func(bridge.Event) error { return nil })
	if !response.OK {
		t.Fatalf("chat.start failed: %s", response.Error.Message)
	}
	return capturedChatRequest(t, requests)
}

func prefixStabilityPayload(goal string) string {
	return `{"providerId":"` + chatAttachmentProviderID + `","modelId":"model","executionMode":"approval","messages":[{"role":"user","content":"` + goal + `"}]}`
}

// The first system message must be byte-identical across turns of the same
// session settings, whatever the user said this turn. Provider prefix caches
// key on those leading bytes; a per-turn timestamp or goal inside the head
// system message invalidates the whole history every turn.
func TestChatSystemHeadStableAcrossTurns(t *testing.T) {
	first := startPrefixStabilityChat(t, prefixStabilityPayload("今天合肥天气怎么样？"))
	second := startPrefixStabilityChat(t, prefixStabilityPayload("帮我写一个星座爱情故事"))

	if len(first.Messages) == 0 || first.Messages[0].Role != llmadapter.RoleSystem {
		t.Fatalf("first message is not system: %#v", first.Messages)
	}
	if first.Messages[0].Content != second.Messages[0].Content {
		t.Fatalf("system head differs across turns:\n--- turn1 %d bytes ---\n%s\n--- turn2 %d bytes ---\n%s",
			len(first.Messages[0].Content), first.Messages[0].Content, len(second.Messages[0].Content), second.Messages[0].Content)
	}
	if strings.Contains(first.Messages[0].Content, "[当前任务边界]") {
		t.Fatalf("per-turn boundary leaked into stable system head")
	}
}

// Per-turn injections (boundary timestamp, lane-trimmed workflows, catalog,
// persona) belong after the last user message, as a trailing system message —
// the same pattern the tool loop already uses — so the leading prefix stays
// cacheable while the dynamic guidance still reaches the model.
func TestChatTurnInjectionLandsAtTail(t *testing.T) {
	req := startPrefixStabilityChat(t, prefixStabilityPayload("帮我写一个星座爱情故事"))

	if len(req.Messages) < 2 {
		t.Fatalf("expected head system + user + tail system, got %#v", req.Messages)
	}
	last := req.Messages[len(req.Messages)-1]
	if last.Role != llmadapter.RoleSystem {
		t.Fatalf("last message is %s, want system carrying the turn injection", last.Role)
	}
	if !strings.Contains(last.Content, "[当前任务边界]") || !strings.Contains(last.Content, "星座爱情故事") {
		t.Fatalf("turn injection missing boundary or goal: %q", last.Content)
	}
	if strings.Contains(req.Messages[0].Content, "当前本地时间") {
		t.Fatalf("timestamp leaked into stable head")
	}
}

// Long-form creative generation (novel/story) does not need deep reasoning;
// leaving thinking on burns minutes before the first visible word. When the
// user did not pick a reasoning level and did not ask to think hard, the
// request must disable reasoning for these prose turns.
func TestNovelTurnDisablesReasoning(t *testing.T) {
	req := startPrefixStabilityChat(t, prefixStabilityPayload("写一篇12星座爱情长篇小说，每章1500字"))
	if !req.DisableReasoning {
		t.Fatalf("novel turn should disable reasoning")
	}
}

func TestNovelTurnKeepsReasoningWhenUserOptsIn(t *testing.T) {
	deep := startPrefixStabilityChat(t, prefixStabilityPayload("深度思考后写一篇12星座爱情长篇小说"))
	if deep.DisableReasoning {
		t.Fatalf("explicit 深度思考 must keep reasoning enabled")
	}
}

// The composer's level picker is never empty (defaults to high), so a
// non-empty reasoningLevel must not kill the prose fast path — otherwise the
// fast path is dead in every real turn.
func TestNovelTurnStreamsDirectlyDespiteDefaultLevel(t *testing.T) {
	leveled := startPrefixStabilityChat(t, `{"providerId":"`+chatAttachmentProviderID+`","modelId":"model","executionMode":"approval","reasoningLevel":"high","messages":[{"role":"user","content":"写一篇12星座爱情长篇小说"}]}`)
	if !leveled.DisableReasoning {
		t.Fatalf("default reasoning level must not kill the prose fast path")
	}
	if leveled.ReasoningLevel != "" {
		t.Fatalf("prose fast path should clear the reasoning level")
	}
}

// 长篇 reports and analytical papers are continuous prose too: the zodiac
// case (长篇分析报告论文) must stream directly instead of thinking for
// minutes before the first visible word.
func TestLongFormReportStreamsDirectly(t *testing.T) {
	req := startPrefixStabilityChat(t, prefixStabilityPayload("帮我写一个关于12星座不同星座之间的爱情匹配分析，长篇分析报告论文"))
	if !req.DisableReasoning {
		t.Fatalf("long-form report prose should stream directly")
	}
	if req.ReasoningLevel != "" {
		t.Fatalf("prose fast path should clear the reasoning level")
	}
	// Ordinary work reports are not long-form prose and keep reasoning.
	work := startPrefixStabilityChat(t, prefixStabilityPayload("帮我写一份本周测试报告"))
	if work.DisableReasoning {
		t.Fatalf("ordinary work reports must keep the lane default")
	}
}
