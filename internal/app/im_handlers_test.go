package app

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/imapp"
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
}
