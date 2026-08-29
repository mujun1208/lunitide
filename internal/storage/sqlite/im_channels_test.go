package sqlite

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/lunitide/lunitide/internal/imapp"
	"github.com/lunitide/lunitide/internal/scheduler"
)

func TestIMChannelsSeedAndWebhookGuard(t *testing.T) {
	ctx := context.Background()
	store, err := OpenTemplated(ctx, filepath.Join(t.TempDir(), "im.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	svc := imapp.New(store)
	items, err := svc.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 5 {
		t.Fatalf("seeded %d channels", len(items))
	}
	on := true
	url := "http://127.0.0.1/hook"
	if _, err := svc.Set(ctx, imapp.KindFeishu, &on, &url); err == nil {
		t.Fatal("localhost webhook must fail")
	}
	good := "https://open.feishu.cn/open-apis/bot/v2/hook/aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	items, err = svc.Set(ctx, imapp.KindFeishu, &on, &good)
	if err != nil {
		t.Fatal(err)
	}
	var feishu imapp.Channel
	for _, ch := range items {
		if ch.Kind == imapp.KindFeishu {
			feishu = ch
		}
	}
	if !feishu.Enabled || feishu.Mode != "webhook" {
		t.Fatalf("feishu %+v", feishu)
	}
	if _, err := svc.Set(ctx, imapp.KindWeChat, &on, &good); err == nil {
		t.Fatal("wechat must not accept a webhook")
	}
	items, err = svc.Set(ctx, imapp.KindWeChat, &on, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, ch := range items {
		if ch.Kind == imapp.KindWeChat && (ch.Mode != "desktop" || !ch.Enabled) {
			t.Fatalf("wechat %+v", ch)
		}
	}
	if err := scheduler.ValidateWebhookURL(good); err != nil {
		t.Fatal(err)
	}
}
