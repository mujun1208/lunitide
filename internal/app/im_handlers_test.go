package app

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/imapp"
	"github.com/lunitide/lunitide/internal/messageapp"
	"github.com/lunitide/lunitide/internal/projectapp"
	"github.com/lunitide/lunitide/internal/providerapp"
	"github.com/lunitide/lunitide/internal/sessionapp"
	storage "github.com/lunitide/lunitide/internal/storage/sqlite"
)

func newIMEngine(t *testing.T) *Engine {
	t.Helper()
	store, err := storage.OpenTemplated(context.Background(), filepath.Join(t.TempDir(), "im.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	e := NewEngine(nil, "test")
	e.SetIMChannelsService(imapp.New(store))
	return e
}

func TestImChannelsGetSeedsAndSetWebhook(t *testing.T) {
	e := newIMEngine(t)
	ctx := context.Background()
	got := e.Handle(ctx, nominationRequest("im.channels.get", `{}`))
	if !got.OK {
		t.Fatalf("get %+v", got.Error)
	}
	var listed struct {
		Channels []imapp.Channel `json:"channels"`
	}
	if err := json.Unmarshal(mustJSON(got.Payload), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Channels) != 5 {
		t.Fatalf("seeded %d", len(listed.Channels))
	}

	badKind := e.Handle(ctx, nominationRequest("im.channels.set", `{"kind":"slack"}`))
	if badKind.OK {
		t.Fatal("slack must fail schema")
	}

	local := e.Handle(ctx, nominationRequest("im.channels.set", `{"kind":"feishu","enabled":true,"webhookUrl":"http://127.0.0.1/hook"}`))
	if local.OK || local.Error.Code != "BRIDGE_SCHEMA_INVALID" {
		t.Fatalf("localhost webhook = %+v", local.Error)
	}

	wechatHook := e.Handle(ctx, nominationRequest("im.channels.set", `{"kind":"wechat","enabled":true,"webhookUrl":"https://open.feishu.cn/open-apis/bot/v2/hook/aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"}`))
	if wechatHook.OK {
		t.Fatal("wechat must not accept a webhook")
	}

	ok := e.Handle(ctx, nominationRequest("im.channels.set", `{"kind":"feishu","enabled":true,"webhookUrl":"https://open.feishu.cn/open-apis/bot/v2/hook/aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"}`))
	if !ok.OK {
		t.Fatalf("feishu set %+v", ok.Error)
	}
	raw := string(mustJSON(ok.Payload))
	if !strings.Contains(raw, `"mode":"webhook"`) || !strings.Contains(raw, `"kind":"feishu"`) {
		t.Fatalf("feishu payload %s", raw)
	}

	desktop := e.Handle(ctx, nominationRequest("im.channels.set", `{"kind":"qq","enabled":true}`))
	if !desktop.OK {
		t.Fatalf("qq set %+v", desktop.Error)
	}
	if !strings.Contains(string(mustJSON(desktop.Payload)), `"mode":"desktop"`) {
		t.Fatalf("qq payload %s", desktop.Payload)
	}

	emptyInbound := e.Handle(ctx, nominationRequest("im.channels.set", `{"kind":"feishu","inboundEnabled":true}`))
	if emptyInbound.OK {
		t.Fatal("inbound without allowlist must fail")
	}
	dingInbound := e.Handle(ctx, nominationRequest("im.channels.set", `{"kind":"dingtalk","inboundEnabled":true,"inboundAllowlist":"x"}`))
	if dingInbound.OK {
		t.Fatal("dingtalk inbound must fail")
	}
	in := e.Handle(ctx, nominationRequest("im.channels.set", `{"kind":"feishu","inboundEnabled":true,"inboundAllowlist":"ou_ok"}`))
	if !in.OK {
		t.Fatalf("feishu inbound %+v", in.Error)
	}
	if !strings.Contains(string(mustJSON(in.Payload)), `"inboundEnabled":true`) {
		t.Fatalf("inbound payload %s", in.Payload)
	}
}

func TestImInboundDeliverAllowlist(t *testing.T) {
	store, err := storage.OpenTemplated(context.Background(), filepath.Join(t.TempDir(), "im-in.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	projects := projectapp.New(store, store)
	if _, err := projects.Create(context.Background(), "personal-key", "test", struct {
		Name string `json:"name"`
	}{imapp.PersonalChatProjectName}, structToProject(imapp.PersonalChatProjectName)); err != nil {
		t.Fatal(err)
	}
	sessions := sessionapp.New(store, store)
	cursorKey := []byte("0123456789abcdef0123456789abcdef")
	msgs, err := messageapp.New(store, store, cursorKey)
	if err != nil {
		t.Fatal(err)
	}
	e := NewEngineWithMessages(providerapp.New(store, store), projects, sessions, msgs, "test", nil)
	e.SetIMChannelsService(imapp.New(store))
	ctx := context.Background()

	off := e.Handle(ctx, inboundDeliverReq(`{"kind":"feishu","sender":"ou_ok","text":"打开网易云"}`))
	if off.OK || off.Error == nil || off.Error.Code != "IM_INBOUND_OFF" {
		t.Fatalf("off = %+v", off.Error)
	}

	if set := e.Handle(ctx, nominationRequest("im.channels.set", `{"kind":"feishu","inboundEnabled":true,"inboundAllowlist":"ou_ok"}`)); !set.OK {
		t.Fatalf("enable inbound %+v", set.Error)
	}

	denied := e.Handle(ctx, inboundDeliverReq(`{"kind":"feishu","sender":"ou_other","text":"打开网易云"}`))
	if denied.OK || denied.Error == nil || denied.Error.Code != "IM_INBOUND_DENIED" {
		t.Fatalf("denied = %+v", denied.Error)
	}

	ok := e.Handle(ctx, inboundDeliverReq(`{"kind":"feishu","sender":"ou_ok","text":"打开网易云"}`))
	if !ok.OK {
		t.Fatalf("deliver %+v", ok.Error)
	}
	var delivered struct {
		Accepted  bool   `json:"accepted"`
		SessionID string `json:"sessionId"`
	}
	if err := json.Unmarshal(mustJSON(ok.Payload), &delivered); err != nil {
		t.Fatal(err)
	}
	if !delivered.Accepted || delivered.SessionID == "" {
		t.Fatalf("payload %+v", delivered)
	}
	page, err := msgs.List(ctx, messageapp.PageRequest{SessionID: delivered.SessionID, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || !strings.Contains(page.Items[0].Text, "打开网易云") {
		t.Fatalf("messages %#v", page.Items)
	}
}

func TestInboundAutoRunBudget(t *testing.T) {
	if inboundAutoRunTimeout != 2*time.Minute {
		t.Fatalf("timeout = %s", inboundAutoRunTimeout)
	}
	if inboundAutoRunDeadlineMS != 120_000 {
		t.Fatalf("deadlineMs = %d", inboundAutoRunDeadlineMS)
	}
}

func inboundDeliverReq(payload string) bridge.Request {
	r := nominationRequest("im.inbound.deliver", payload)
	r.IdempotencyKey = "im-inbound-test-1"
	return r
}
