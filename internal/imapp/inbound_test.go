package imapp

import (
	"testing"
)

func TestParseAllowlistAndSenderMatch(t *testing.T) {
	items := ParseAllowlist("ou_aaa\nOU_BBB,  user@corp.com ; ou_aaa")
	if len(items) != 3 {
		t.Fatalf("items=%v", items)
	}
	if !SenderAllowed("ou_aaa\nuser@corp.com", "OU_AAA") {
		t.Fatal("open_id should match case-insensitively")
	}
	if SenderAllowed("ou_aaa", "ou_zzz") {
		t.Fatal("unknown sender must fail closed")
	}
	if SenderAllowed("ou_aaa", " ") {
		t.Fatal("empty sender must fail closed")
	}
}

func TestAdmitInboundFailClosed(t *testing.T) {
	ch := Normalize(Channel{Kind: KindFeishu, InboundEnabled: true, InboundAllowlist: "ou_ok"})
	if err := AdmitInbound(ch, "ou_ok", "打开网易云"); err != nil {
		t.Fatal(err)
	}
	off := ch
	off.InboundEnabled = false
	if err := AdmitInbound(off, "ou_ok", "x"); err != ErrInboundOff {
		t.Fatalf("off=%v", err)
	}
	if err := AdmitInbound(ch, "ou_other", "x"); err != ErrInboundDenied {
		t.Fatalf("denied=%v", err)
	}
	qq := Normalize(Channel{Kind: KindQQ, InboundEnabled: true, InboundAllowlist: "x"})
	if err := AdmitInbound(qq, "x", "hi"); err != ErrInboundKind {
		t.Fatalf("qq=%v", err)
	}
}

func TestAdmitInboundPairsFirstSender(t *testing.T) {
	ch := Normalize(Channel{Kind: KindFeishu, InboundEnabled: true, InboundAppID: "cli_x"})
	if err := AdmitInbound(ch, "ou_first", "你好"); err != nil {
		t.Fatalf("empty allowlist should pair: %v", err)
	}
}

func TestInboundShouldAutoRunOnlyWhenPaired(t *testing.T) {
	waiting := Normalize(Channel{Kind: KindFeishu, InboundEnabled: true, InboundAutoRun: true, InboundAppID: "cli_x"})
	if InboundShouldAutoRun(waiting) || waiting.InboundAutoRun {
		t.Fatal("unpaired inbound must not auto-run")
	}
	paired := Normalize(Channel{Kind: KindFeishu, InboundEnabled: true, InboundAutoRun: true, InboundAllowlist: "ou_ok"})
	if !InboundShouldAutoRun(paired) {
		t.Fatal("paired inbound should auto-run when enabled")
	}
}

func TestValidateInboundRequiresAllowlist(t *testing.T) {
	if err := validateInboundFields(KindFeishu, true, "", "", ""); err != ErrInboundAllowlist {
		t.Fatalf("empty allowlist=%v", err)
	}
	if err := validateInboundFields(KindFeishu, true, "", "cli_x", ""); err != nil {
		t.Fatalf("app id pairing=%v", err)
	}
	if err := validateInboundFields(KindDingTalk, true, "x", "", ""); err != ErrInboundKind {
		t.Fatalf("dingtalk=%v", err)
	}
	if err := validateInboundFields(KindFeishu, false, "", "", ""); err != nil {
		t.Fatal(err)
	}
}

func TestParseFeishuMessageEvent(t *testing.T) {
	raw := []byte(`{"schema":"2.0","header":{"event_type":"im.message.receive_v1"},"event":{"sender":{"sender_id":{"open_id":"ou_me"}},"message":{"message_id":"om_1","message_type":"text","content":"{\"text\":\"打开网易云\"}"}}}`)
	got, ok := ParseFeishuMessageEvent(raw)
	if !ok || got.Sender != "ou_me" || got.Text != "打开网易云" || got.MessageID != "om_1" {
		t.Fatalf("got=%+v ok=%v", got, ok)
	}
	wrapped := append([]byte{0x0a, 0x04, 'x'}, raw...)
	got, ok = ParseFeishuMessageEvent(wrapped)
	if !ok || got.Text != "打开网易云" {
		t.Fatalf("binary prefix got=%+v ok=%v", got, ok)
	}
	if _, ok := ParseFeishuMessageEvent([]byte(`{"header":{"event_type":"im.chat.updated_v1"}}`)); ok {
		t.Fatal("non-message events must drop")
	}
}
