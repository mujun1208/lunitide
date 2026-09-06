package brapp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gorilla/websocket"
)

func TestCDPOwnedContextDisposeACKLossDoesNotTouchUserContext(t *testing.T) {
	var mu sync.Mutex
	var methods []string
	disposed := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			var request struct {
				ID     int            `json:"id"`
				Method string         `json:"method"`
				Params map[string]any `json:"params"`
			}
			if err := conn.ReadJSON(&request); err != nil {
				return
			}
			mu.Lock()
			methods = append(methods, request.Method)
			mu.Unlock()
			result := map[string]any{}
			switch request.Method {
			case "Target.createBrowserContext":
				if request.Params["disposeOnDetach"] != true || request.Params["proxyServer"] != "http://127.0.0.1:1234" || request.Params["proxyBypassList"] != "<-loopback>" {
					t.Errorf("missing private policy: %#v", request.Params)
				}
				result["browserContextId"] = "owned"
			case "Target.createTarget":
				if request.Params["browserContextId"] != "owned" {
					t.Errorf("navigated user context: %#v", request.Params)
				}
				result["targetId"] = "owned-tab"
			case "Target.disposeBrowserContext":
				if request.Params["browserContextId"] != "owned" {
					t.Errorf("disposed user context: %#v", request.Params)
				}
				mu.Lock()
				disposed = true
				mu.Unlock()
				return // operation committed, ACK lost
			case "Target.getBrowserContexts":
				mu.Lock()
				if !disposed {
					t.Error("verify before disposal")
				}
				mu.Unlock()
				result["browserContextIds"] = []string{"users-other-context"}
			default:
				t.Errorf("unexpected browser operation: %s", request.Method)
			}
			if err := conn.WriteJSON(map[string]any{"id": request.ID, "result": result}); err != nil {
				return
			}
		}
	}))
	defer server.Close()
	owned, err := createCDPContext(context.Background(), strings.Replace(server.URL, "http://", "ws://", 1), "http://127.0.0.1:1234")
	if err != nil {
		t.Fatal(err)
	}
	if err := owned.Navigate(context.Background(), "https://example.com"); err != nil {
		t.Fatal(err)
	}
	if err := owned.Close(context.Background()); err != nil {
		t.Fatalf("lost disposal ACK was not recovered: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(methods) != 4 {
		t.Fatalf("unexpected operations: %v", methods)
	}
}

func TestExternalCDPRequiresVerifiableNetworkFlags(t *testing.T) {
	for _, args := range [][]string{{"--disable-quic"}, {"--disable-quic", "--force-webrtc-ip-handling-policy=disable_non_proxied_udp"}} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
			if err != nil {
				return
			}
			defer conn.Close()
			var req struct {
				ID     int             `json:"id"`
				Method string          `json:"method"`
				Params json.RawMessage `json:"params"`
			}
			if err := conn.ReadJSON(&req); err != nil {
				return
			}
			if req.Method != "Browser.getBrowserCommandLine" {
				t.Errorf("unexpected call: %s", req.Method)
			}
			_ = conn.WriteJSON(map[string]any{"id": req.ID, "result": map[string]any{"arguments": args}})
		}))
		err := verifyExternalBrowserNetworkFlags(context.Background(), strings.Replace(server.URL, "http://", "ws://", 1))
		server.Close()
		if (err == nil) != (len(args) == 2) {
			t.Fatalf("unsafe external browser accepted: %v %v", args, err)
		}
	}
}

func TestCDPVersionCannotNominateAnotherEndpoint(t *testing.T) {
	for _, raw := range []string{"ws://10.0.0.1:9222/devtools/browser/a", "ws://localhost:9222/devtools/browser/a", "ws://user@127.0.0.1:9222/devtools/browser/a", "wss://127.0.0.1:9222/a"} {
		if _, _, err := wsHostPort(raw); err == nil {
			t.Fatalf("untrusted endpoint accepted: %s", raw)
		}
	}
}
