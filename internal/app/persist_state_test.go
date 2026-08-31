package app

import (
	"strings"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/imapp"
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
