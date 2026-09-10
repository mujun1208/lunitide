package toolruntime

import (
	"context"
	"encoding/json"
	"errors"
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
	_, err = r.executeIMSend(context.Background(), "s1", desk, false, true)
	if err == nil || !strings.Contains(err.Error(), "完整权限") {
		t.Fatalf("desktop unapproved %v", err)
	}

	empty, _ := json.Marshal(map[string]any{"channel": "feishu", "text": "  "})
	_, err = r.executeIMSend(context.Background(), "s1", empty, true, true)
	if err == nil || !strings.Contains(err.Error(), "没有可发送的内容") {
		t.Fatalf("empty %v", err)
	}
}

func TestExecuteIMSendAttachmentStaysPendingExternal(t *testing.T) {
	sent := 0
	r := &Runtime{}
	r.SetIMSend(func(_ context.Context, kind, to, text string) (string, string, error) {
		sent++
		if strings.Contains(text, "invoice.pdf") {
			t.Fatal("attachment path must not be treated as sent text")
		}
		return "", "sent via 飞书 webhook", nil
	})
	payload, _ := json.Marshal(map[string]any{"channel": "feishu", "text": "催办", "attachment": "invoice.pdf"})
	got, err := r.executeIMSend(context.Background(), "s1", payload, true, true)
	if err != nil {
		t.Fatal(err)
	}
	if sent != 1 {
		t.Fatalf("text may still send, sent=%d", sent)
	}
	if !strings.Contains(got.Output, "pending_external") || strings.Contains(got.Output, "已发送附件") {
		t.Fatalf("attachment must stay pending_external: %q", got.Output)
	}
}

func TestExecuteIMSendFailsWhenChannelRecipePaused(t *testing.T) {
	sent := 0
	r := &Runtime{}
	r.SetIMSend(func(context.Context, string, string, string) (string, string, error) {
		sent++
		return "", "sent via 飞书 webhook", nil
	})
	r.SetIMAllowed(func(channel string) bool { return channel != "feishu" })
	payload, _ := json.Marshal(map[string]any{"channel": "feishu", "text": "催办"})
	_, err := r.executeIMSend(context.Background(), "s1", payload, true, true)
	if err == nil || !strings.Contains(err.Error(), "已暂停") {
		t.Fatalf("paused channel must not send: %v", err)
	}
	if sent != 0 {
		t.Fatal("paused channel must not touch the transport")
	}
}

func TestExecuteIMSendDesktopTypeFailureIsError(t *testing.T) {
	origPick, origOpen, origType := imPickLaunch, imOpenApp, imTypeIntoChat
	t.Cleanup(func() {
		imPickLaunch, imOpenApp, imTypeIntoChat = origPick, origOpen, origType
	})
	imPickLaunch = func(string) (string, []string, error) { return `C:\WeChat.exe`, nil, nil }
	imOpenApp = func(string) error { return nil }
	imTypeIntoChat = func(context.Context, ccInvoker, string, json.RawMessage, bool, bool) (Result, error) {
		return Result{}, errors.New("找不到输入框")
	}
	r := &Runtime{}
	r.SetIMSend(func(context.Context, string, string, string) (string, string, error) {
		return "微信", "desktop:微信", nil
	})
	desk, _ := json.Marshal(map[string]any{"channel": "wechat", "text": "hi", "to": "张三"})
	got, err := r.executeIMSend(context.Background(), "s1", desk, true, true)
	if err == nil || !strings.Contains(err.Error(), "没打进会话") {
		t.Fatalf("type fail %v %#v", err, got)
	}
	if strings.Contains(err.Error(), "opened") || got.Output != "" {
		t.Fatalf("must not look like success: %v %#v", err, got)
	}
}
