package app

import (
	"context"
	"errors"
	"strings"
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

type inboundAppendSpy struct{ texts []string }

func (s *inboundAppendSpy) Append(_ context.Context, _, _ string, _ any, msg message.Message) (message.Message, error) {
	s.texts = append(s.texts, msg.Text)
	return msg, nil
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
	for time.Now().Before(deadline) && len(spy.texts) == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	if len(spy.texts) != 1 || !strings.Contains(spy.texts[0], "回程失败") || !strings.Contains(spy.texts[0], "企业微信") {
		t.Fatalf("wecom down must write a visible session notice: %#v", spy.texts)
	}
}

func TestPushInboundReplyWeComUsesStream(t *testing.T) {
	e := NewEngine(nil, "test")
	var got []byte
	e.setWeComWriter(func(raw []byte) error {
		got = append([]byte(nil), raw...)
		return nil
	})
	e.rememberInboundRoute("01ARZ3NDEKTSV4RRFFQ69G5FAV", imapp.Channel{Kind: imapp.KindWeCom}, "zhangsan", "")
	e.pushInboundReply("01ARZ3NDEKTSV4RRFFQ69G5FAV", "企微回了")
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && !strings.Contains(string(got), "企微回了") {
		time.Sleep(10 * time.Millisecond)
	}
	if !strings.Contains(string(got), `"cmd":"aibot_send_msg"`) || !strings.Contains(string(got), "企微回了") {
		t.Fatalf("wecom stream reply = %s", got)
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
