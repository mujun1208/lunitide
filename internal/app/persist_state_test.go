package app

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/domain/message"
	"github.com/lunitide/lunitide/internal/imapp"
	"github.com/lunitide/lunitide/internal/messageapp"
)

func TestInboundRoutePersistsAcrossEngines(t *testing.T) {
	dir := t.TempDir()
	first := NewEngine(nil, "test")
	first.SetPersistDir(dir)
	first.rememberInboundRoute("01ARZ3NDEKTSV4RRFFQ69G5FAV", imapp.Channel{Kind: imapp.KindDingTalk}, "ou_me", "cid_1")
	second := NewEngine(nil, "test")
	second.SetPersistDir(dir)
	raw, ok := second.inboundRoutes.Load("01ARZ3NDEKTSV4RRFFQ69G5FAV")
	if !ok {
		t.Fatal("inbound route missing after restart")
	}
	route, ok := raw.(inboundRoute)
	if !ok || route.Kind != imapp.KindDingTalk || route.Sender != "ou_me" || route.ConversationID != "cid_1" {
		t.Fatalf("route = %#v", raw)
	}
}

func TestInboundReplyFailureNoticeIsVisible(t *testing.T) {
	got := inboundReplyFailureNotice(imapp.KindWeCom, errors.New("wecom inbound stream is down"))
	if !strings.Contains(got, "回程失败") || !strings.Contains(got, "企业微信") || !strings.Contains(got, "stream is down") {
		t.Fatalf("notice = %q", got)
	}
}

func TestSendWeComInboundReplyRequiresWriter(t *testing.T) {
	e := NewEngine(nil, "test")
	err := e.sendWeComInboundReply("zhangsan", "", "hi")
	if err == nil || !strings.Contains(err.Error(), "down") {
		t.Fatalf("err = %v", err)
	}
}

// inboundAppendSpy is written from the background reply goroutine and read
// from the test, so its slice is guarded — otherwise the observation itself
// is a data race the -race build (correctly) rejects.
type inboundAppendSpy struct {
	mu    sync.Mutex
	texts []string
}

func (s *inboundAppendSpy) Append(_ context.Context, _, _ string, _ any, msg message.Message) (message.Message, error) {
	s.mu.Lock()
	s.texts = append(s.texts, msg.Text)
	s.mu.Unlock()
	return msg, nil
}

// snapshot returns a copy of what has been appended so far.
func (s *inboundAppendSpy) snapshot() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.texts...)
}
func (s *inboundAppendSpy) AppendAssistant(context.Context, string, string, string, string, messageapp.AssistantUsage) (message.Message, error) {
	return message.Message{}, errors.New("not used")
}
func (s *inboundAppendSpy) List(context.Context, messageapp.PageRequest) (messageapp.Page, error) {
	return messageapp.Page{}, errors.New("not used")
}

func TestPushInboundReplyWeComDownWritesSessionNotice(t *testing.T) {
	spy := &inboundAppendSpy{}
	e := NewEngine(nil, "test")
	e.messages = spy
	e.rememberInboundRoute("01ARZ3NDEKTSV4RRFFQ69G5FAV", imapp.Channel{Kind: imapp.KindWeCom}, "zhangsan", "")
	e.pushInboundReply("01ARZ3NDEKTSV4RRFFQ69G5FAV", "企微回了")
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && len(spy.snapshot()) == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	texts := spy.snapshot()
	if len(texts) != 1 || !strings.Contains(texts[0], "回程失败") || !strings.Contains(texts[0], "企业微信") {
		t.Fatalf("wecom down must write a visible session notice: %#v", texts)
	}
}

func TestPushInboundReplyWeComUsesStream(t *testing.T) {
	e := NewEngine(nil, "test")
	// The writer fires on the background reply goroutine; a channel hands the
	// bytes across without the test and that goroutine touching one variable.
	got := make(chan []byte, 1)
	e.setWeComWriter(func(raw []byte) error {
		select {
		case got <- append([]byte(nil), raw...):
		default:
		}
		return nil
	})
	e.rememberInboundRoute("01ARZ3NDEKTSV4RRFFQ69G5FAV", imapp.Channel{Kind: imapp.KindWeCom}, "zhangsan", "")
	e.pushInboundReply("01ARZ3NDEKTSV4RRFFQ69G5FAV", "企微回了")
	select {
	case raw := <-got:
		if !strings.Contains(string(raw), `"cmd":"aibot_send_msg"`) || !strings.Contains(string(raw), "企微回了") {
			t.Fatalf("wecom stream reply = %s", raw)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("wecom stream reply timed out")
	}
}

func TestMcpPresetPersistsAcrossEngines(t *testing.T) {
	dir := t.TempDir()
	first := NewEngine(nil, "test")
	first.SetPersistDir(dir)
	first.rememberMcpPreset("ep_playwright", "playwright")
	second := NewEngine(nil, "test")
	second.SetPersistDir(dir)
	if got := second.endpointPresetID("ep_playwright"); got != "playwright" {
		t.Fatalf("preset = %q", got)
	}
}
