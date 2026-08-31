package imapp

import (
	"strings"
	"testing"
)

func TestParseWeComMessageEventText(t *testing.T) {
	raw := []byte(`{"cmd":"aibot_msg_callback","headers":{"req_id":"1"},"body":{"msgid":"m1","msgtype":"text","from":{"userid":"zhangsan"},"text":{"content":"帮我查天气"}}}`)
	got, ok := ParseWeComMessageEvent(raw)
	if !ok {
		t.Fatal("expected text callback")
	}
	if got.Sender != "zhangsan" || got.Text != "帮我查天气" || got.MessageID != "m1" {
		t.Fatalf("got %+v", got)
	}
}

func TestParseWeComMessageEventIgnoresPingAndNonText(t *testing.T) {
	if _, ok := ParseWeComMessageEvent([]byte(`{"cmd":"pong"}`)); ok {
		t.Fatal("pong must be ignored")
	}
	if _, ok := ParseWeComMessageEvent([]byte(`{"cmd":"aibot_msg_callback","body":{"msgtype":"image","from":{"userid":"a"},"text":{"content":"x"}}}`)); ok {
		t.Fatal("non-text must be ignored")
	}
	if _, ok := ParseWeComMessageEvent([]byte(`{"cmd":"aibot_msg_callback","body":{"msgtype":"text","from":{"userid":""},"text":{"content":"hi"}}}`)); ok {
		t.Fatal("empty sender must be ignored")
	}
}

func TestWeComSubscribePayloadShape(t *testing.T) {
	raw := string(WeComSubscribePayload("bot-1", "sec"))
	for _, part := range []string{`"cmd":"aibot_subscribe"`, `"bot_id":"bot-1"`, `"secret":"sec"`} {
		if !strings.Contains(raw, part) {
			t.Fatalf("subscribe payload missing %s: %s", part, raw)
		}
	}
	if !strings.Contains(string(WeComPingPayload()), `"cmd":"ping"`) {
		t.Fatal("ping payload")
	}
}

func TestWeComSendMsgPayloadShape(t *testing.T) {
	dm := string(WeComSendMsgPayload("zhangsan", false, "回了"))
	for _, part := range []string{`"cmd":"aibot_send_msg"`, `"chatid":"zhangsan"`, `"chat_type":1`, `"msgtype":"markdown"`, `"content":"回了"`} {
		if !strings.Contains(dm, part) {
			t.Fatalf("dm payload missing %s: %s", part, dm)
		}
	}
	group := string(WeComSendMsgPayload("wr_chat", true, "群回"))
	if !strings.Contains(group, `"chat_type":2`) || !strings.Contains(group, `"chatid":"wr_chat"`) {
		t.Fatalf("group payload = %s", group)
	}
}
