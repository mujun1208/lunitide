package toolruntime

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestExecuteIMSendWebhookAndDesktopGuard(t *testing.T) {
	r := &Runtime{}
	r.SetIMSend(func(_ context.Context, kind, to, text string) (string, string, error) {
		if kind == "feishu" {
			return "", "sent via 飞书 webhook", nil
		}
		return "微信", "desktop:微信", nil
	})
	payload, _ := json.Marshal(map[string]any{"channel": "feishu", "text": "hello"})
	got, err := r.executeIMSend(context.Background(), "s1", payload, true, true)
	if err != nil || got.Output != "sent via 飞书 webhook" {
		t.Fatalf("webhook %v %v", got, err)
	}

	desk, _ := json.Marshal(map[string]any{"channel": "wechat", "text": "hi", "to": "张三"})
	_, err = r.executeIMSend(context.Background(), "s1", desk, true, false)
	if err == nil || !strings.Contains(err.Error(), "完整磁盘访问") {
		t.Fatalf("desktop confined %v", err)
	}

	empty, _ := json.Marshal(map[string]any{"channel": "feishu", "text": "  "})
	_, err = r.executeIMSend(context.Background(), "s1", empty, true, true)
	if err == nil || !strings.Contains(err.Error(), "没有可发送的内容") {
		t.Fatalf("empty %v", err)
	}
}
