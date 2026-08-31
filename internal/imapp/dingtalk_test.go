package imapp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFetchDingTalkEndpointRequiresWSS(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != DingTalkConnectionPath {
			t.Fatalf("path %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"endpoint": "wss://wss-open-connection.dingtalk.com/connect",
			"ticket":   "tick",
		})
	}))
	t.Cleanup(srv.Close)
	got, err := FetchDingTalkEndpoint(context.Background(), srv.Client(), srv.URL, "app", "secret")
	if err != nil {
		t.Fatal(err)
	}
	if got.URL == "" || got.Ticket != "tick" {
		t.Fatalf("got=%+v", got)
	}
	if !strings.Contains(DingTalkStreamURL(got), "ticket=tick") {
		t.Fatalf("stream url %s", DingTalkStreamURL(got))
	}
}

func TestFetchDingTalkEndpointRejectsHTTPWebsocket(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"endpoint": "ws://evil.example/ws",
			"ticket":   "tick",
		})
	}))
	t.Cleanup(srv.Close)
	if _, err := FetchDingTalkEndpoint(context.Background(), srv.Client(), srv.URL, "app", "secret"); err == nil {
		t.Fatal("plain ws must fail")
	}
}

func TestParseDingTalkMessageEvent(t *testing.T) {
	raw := []byte(`{"type":"CALLBACK","headers":{"topic":"/v1.0/im/bot/messages/get","messageId":"m1"},"data":"{\"msgtype\":\"text\",\"text\":{\"content\":\"打开网易云\"},\"senderStaffId\":\"staff_1\",\"conversationId\":\"cid_1\",\"msgId\":\"om_1\"}"}`)
	got, ok := ParseDingTalkMessageEvent(raw)
	if !ok || got.Sender != "staff_1" || got.Text != "打开网易云" || got.ConversationID != "cid_1" || got.MessageID != "om_1" {
		t.Fatalf("got=%+v ok=%v", got, ok)
	}
	if _, ok := ParseDingTalkMessageEvent([]byte(`{"type":"SYSTEM","headers":{"topic":"ping"}}`)); ok {
		t.Fatal("system frames must drop")
	}
}
