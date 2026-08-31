package imapp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestReplyTransportFeishuAndDingTalk(t *testing.T) {
	var feishuText, dingText string
	feishu := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/tenant_access_token/internal"):
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "tenant_access_token": "t"})
		case strings.Contains(r.URL.Path, "/im/v1/messages"):
			raw, _ := io.ReadAll(r.Body)
			var body struct {
				Content string `json:"content"`
			}
			_ = json.Unmarshal(raw, &body)
			var inner struct {
				Text string `json:"text"`
			}
			_ = json.Unmarshal([]byte(body.Content), &inner)
			feishuText = inner.Text
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0})
		default:
			t.Fatalf("feishu path %s", r.URL.Path)
		}
	}))
	t.Cleanup(feishu.Close)
	dingAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "oToMessages") {
			t.Fatalf("ding path %s", r.URL.Path)
		}
		raw, _ := io.ReadAll(r.Body)
		var body struct {
			MsgParam string `json:"msgParam"`
		}
		_ = json.Unmarshal(raw, &body)
		var inner struct {
			Content string `json:"content"`
		}
		_ = json.Unmarshal([]byte(body.MsgParam), &inner)
		dingText = inner.Content
		_ = json.NewEncoder(w).Encode(map[string]any{})
	}))
	t.Cleanup(dingAPI.Close)
	dingOAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "dtoken"})
	}))
	t.Cleanup(dingOAPI.Close)
	tr := ReplyTransport{
		HTTP:           http.DefaultClient,
		FeishuDomain:   feishu.URL,
		DingTalkDomain: dingAPI.URL,
		DingTalkOAPI:   dingOAPI.URL,
	}
	if err := tr.Send(context.Background(), KindFeishu, "cli", "sec", "ou_me", "", "飞书回了"); err != nil {
		t.Fatal(err)
	}
	if feishuText != "飞书回了" {
		t.Fatalf("feishu text %q", feishuText)
	}
	if err := tr.Send(context.Background(), KindDingTalk, "app", "sec", "staff", "cid", "钉钉回了"); err != nil {
		t.Fatal(err)
	}
	if dingText != "钉钉回了" {
		t.Fatalf("ding text %q", dingText)
	}
}

func TestReplyTransportWeComRefusesHTTP(t *testing.T) {
	tr := ReplyTransport{HTTP: http.DefaultClient}
	err := tr.Send(context.Background(), KindWeCom, "corp", "sec", "user", "", "不能走 agentid=1")
	if err == nil || !strings.Contains(err.Error(), "aibot stream") {
		t.Fatalf("wecom http reply = %v", err)
	}
}
