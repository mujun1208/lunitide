package browsernetwork

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/lunitide/lunitide/internal/commandworker"
)

// Opt-in, local-only, headless integration: no microphone or external upstream.
func TestNativeBrowserProxyCoversRedirectsAndSubresources(t *testing.T) {
	exe := os.Getenv("LUNITIDE_BROWSER_TEST_EXECUTABLE")
	if exe == "" {
		t.Skip("set LUNITIDE_BROWSER_TEST_EXECUTABLE for local hidden browser integration")
	}
	var pageRequests, redirectRequests, privateLookups, privateDials atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.Host, "private") {
			privateDials.Add(1)
		}
		if r.URL.Path == "/redirect" {
			redirectRequests.Add(1)
			http.Redirect(w, r, "http://private.example/redirected", http.StatusFound)
			return
		}
		pageRequests.Add(1)
		w.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(w, `<!doctype html><body>safe<img src="http://private.example/pixel"><iframe src="http://public.example/redirect"></iframe><script>fetch('http://private.example/fetch').catch(()=>{});fetch('file:///C:/Windows/win.ini').then(()=>document.title='UNSAFE').catch(()=>document.title='FILE_BLOCKED')</script></body>`)
	}))
	defer upstream.Close()
	p, _, _ := testProxy(t, Policy{AllowHTTP: true}, strings.TrimPrefix(upstream.URL, "http://"))
	p.resolve = func(_ context.Context, host string) ([]net.IPAddr, error) {
		if strings.Contains(host, "private") {
			privateLookups.Add(1)
			return []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}}, nil
		}
		return []net.IPAddr{{IP: net.ParseIP("93.184.216.34")}}, nil
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	root := t.TempDir()
	runCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	env := []string{}
	for _, key := range []string{"SystemRoot", "WINDIR", "TEMP", "TMP", "PATH", "HOME", "DISPLAY"} {
		if value := os.Getenv(key); value != "" {
			env = append(env, key+"="+value)
		}
	}
	go func() {
		_, err := commandworker.Run(runCtx, commandworker.Spec{Exe: exe, Dir: root, Env: env, Timeout: 30 * time.Second, MaxOutputBytes: 4096, Args: []string{
			"--headless=new", "--no-first-run", "--no-default-browser-check", "--disable-background-networking", "--disable-quic", "--force-webrtc-ip-handling-policy=disable_non_proxied_udp", "--proxy-server=" + p.URL(), "--proxy-bypass-list=<-loopback>", "--user-data-dir=" + root, "--remote-debugging-port=" + strconv.Itoa(port), "about:blank",
		}}, nil, nil)
		done <- err
	}()
	defer func() {
		cancel()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("native browser process tree did not close")
		}
	}()
	client := &http.Client{Timeout: time.Second, Transport: &http.Transport{Proxy: nil}}
	defer client.CloseIdleConnections()
	var endpoint string
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		response, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/json/version", port))
		if err == nil {
			var doc struct {
				URL string `json:"webSocketDebuggerUrl"`
			}
			err = json.NewDecoder(response.Body).Decode(&doc)
			_ = response.Body.Close()
			if err == nil {
				endpoint = doc.URL
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	if endpoint == "" {
		t.Fatal("native browser CDP did not start")
	}
	conn, _, err := websocket.DefaultDialer.Dial(endpoint, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	conn.SetReadLimit(1 << 20)
	sequence := 0
	call := func(method string, params any, session string) map[string]any {
		t.Helper()
		sequence++
		request := map[string]any{"id": sequence, "method": method, "params": params}
		if session != "" {
			request["sessionId"] = session
		}
		if err := conn.WriteJSON(request); err != nil {
			t.Fatal(err)
		}
		for {
			var response struct {
				ID     int             `json:"id"`
				Result map[string]any  `json:"result"`
				Error  json.RawMessage `json:"error"`
			}
			if err := conn.ReadJSON(&response); err != nil {
				t.Fatal(err)
			}
			if response.ID != sequence {
				continue
			}
			if len(response.Error) > 0 {
				t.Fatalf("%s: %s", method, response.Error)
			}
			return response.Result
		}
	}
	created := call("Target.createBrowserContext", map[string]any{"disposeOnDetach": true, "proxyServer": p.URL(), "proxyBypassList": "<-loopback>"}, "")
	target := call("Target.createTarget", map[string]any{"url": "http://public.example/", "browserContextId": created["browserContextId"]}, "")
	attached := call("Target.attachToTarget", map[string]any{"targetId": target["targetId"], "flatten": true}, "")
	session := attached["sessionId"].(string)
	deadline = time.Now().Add(5 * time.Second)
	for privateLookups.Load() < 3 && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	result := call("Runtime.evaluate", map[string]any{"expression": "document.title", "returnByValue": true}, session)
	raw, _ := json.Marshal(result)
	if privateLookups.Load() < 3 || privateDials.Load() != 0 || pageRequests.Load() < 1 || redirectRequests.Load() < 1 || !strings.Contains(string(raw), "FILE_BLOCKED") {
		t.Fatalf("native boundary failure: pages=%d redirects=%d privateLookups=%d privateUpstream=%d title=%s", pageRequests.Load(), redirectRequests.Load(), privateLookups.Load(), privateDials.Load(), raw)
	}
	t.Logf("native hidden browser: pages=%d redirects=%d blocked private requests=%d forbidden upstream hits=%d file fetch=blocked", pageRequests.Load(), redirectRequests.Load(), privateLookups.Load(), privateDials.Load())
}
