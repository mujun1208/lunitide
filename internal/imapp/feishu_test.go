package imapp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchFeishuEndpointRequiresWSS(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != FeishuEndpointPath {
			t.Fatalf("path %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]string{"URL": "wss://msg-frontier.feishu.cn/ws/v2?device_id=1"},
		})
	}))
	t.Cleanup(srv.Close)
	client := srv.Client()
	got, err := FetchFeishuEndpoint(context.Background(), client, srv.URL, "cli_a", "secret")
	if err != nil {
		t.Fatal(err)
	}
	if got.URL == "" {
		t.Fatal("empty url")
	}
}

func TestFetchFeishuEndpointRejectsHTTPWebsocket(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]string{"URL": "ws://evil.example/ws"},
		})
	}))
	t.Cleanup(srv.Close)
	if _, err := FetchFeishuEndpoint(context.Background(), srv.Client(), srv.URL, "cli_a", "secret"); err == nil {
		t.Fatal("plain ws must fail")
	}
}
