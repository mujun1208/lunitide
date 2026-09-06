package browsernetwork

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func testProxy(t *testing.T, policy Policy, upstream string) (*Proxy, *http.Client, *atomic.Int32) {
	t.Helper()
	p, err := Start(policy)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close() })
	var dials atomic.Int32
	p.resolve = func(_ context.Context, host string) ([]net.IPAddr, error) {
		if strings.Contains(host, "private") || host == "127.0.0.1" {
			return []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}}, nil
		}
		return []net.IPAddr{{IP: net.ParseIP("93.184.216.34")}}, nil
	}
	p.dial = func(ctx context.Context, network, address string) (net.Conn, error) {
		if !strings.HasPrefix(address, "93.184.216.34:") {
			t.Errorf("target was not DNS pinned: %s", address)
		}
		dials.Add(1)
		return (&net.Dialer{}).DialContext(ctx, network, upstream)
	}
	u, _ := url.Parse(p.URL())
	client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(u)}, Timeout: 2 * time.Second}
	t.Cleanup(func() { client.CloseIdleConnections() })
	return p, client, &dials
}

func TestProxyRedirectAndSubresourcePrivateTargetsNeverReachUpstream(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "http://private.example/secret", http.StatusFound)
			return
		}
		_, _ = io.WriteString(w, "public document")
	}))
	defer upstream.Close()
	_, client, dials := testProxy(t, Policy{AllowHTTP: true}, strings.TrimPrefix(upstream.URL, "http://"))
	for _, raw := range []string{"http://public.example/redirect", "http://private.example/image", "http://127.0.0.1/admin"} {
		response, err := client.Get(raw)
		if err != nil {
			t.Fatal(err)
		}
		_ = response.Body.Close()
		if response.StatusCode != http.StatusBadGateway {
			t.Fatalf("private request accepted: %s %d", raw, response.StatusCode)
		}
	}
	if calls.Load() != 1 || dials.Load() != 1 {
		t.Fatalf("private network was dialed: calls=%d dials=%d", calls.Load(), dials.Load())
	}
}

func TestProxyAllowlistComparesCanonicalOrigins(t *testing.T) {
	p, err := Start(Policy{AllowHTTP: true, AllowedOrigins: []string{"https://Example.com"}})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	for _, test := range []struct {
		raw     string
		allowed bool
	}{
		{"https://example.com:443/page", true}, {"https://example.com.evil/page", false}, {"https://example.com@evil/page", false}, {"http://example.com/page", false}, {"https://example.com:8443/page", false},
	} {
		u, _ := url.Parse(test.raw)
		if allowed := p.authorize(u) == nil; allowed != test.allowed {
			t.Fatalf("%s: allowed %v", test.raw, allowed)
		}
	}
	for _, bad := range []string{"https://example.com/path", "https://example.com?x=1", "https://user@example.com"} {
		if _, err := Origin(bad); err == nil {
			t.Fatalf("invalid origin accepted: %s", bad)
		}
	}
}

func TestProxyMixedDNSAnswersAndRebindingFailClosed(t *testing.T) {
	p, _, dials := testProxy(t, Policy{}, "127.0.0.1:1")
	p.resolve = func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("93.184.216.34")}, {IP: net.ParseIP("10.0.0.1")}}, nil
	}
	if _, err := p.dialPinned(context.Background(), "tcp", "mixed.example:443"); err == nil {
		t.Fatal("mixed DNS accepted")
	}
	if dials.Load() != 0 {
		t.Fatal("dial occurred before checking all DNS answers")
	}
	var resolves atomic.Int32
	p.resolve = func(context.Context, string) ([]net.IPAddr, error) {
		if resolves.Add(1) == 1 {
			return []net.IPAddr{{IP: net.ParseIP("93.184.216.34")}}, nil
		}
		return []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}}, nil
	}
	_, _ = p.dialPinned(context.Background(), "tcp", "rebind.example:443")
	if _, err := p.dialPinned(context.Background(), "tcp", "rebind.example:443"); err == nil {
		t.Fatal("rebound private target accepted")
	}
	if dials.Load() != 1 {
		t.Fatalf("unchecked or repeated resolver dial: %d", dials.Load())
	}
}

func TestProxyCONNECTIsPinnedAndCloseReclaimsTunnel(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	upstreamDone := make(chan struct{})
	go func() {
		defer close(upstreamDone)
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_, _ = io.Copy(io.Discard, conn)
	}()
	p, _, dials := testProxy(t, Policy{}, listener.Addr().String())
	conn, err := net.Dial("tcp", strings.TrimPrefix(p.URL(), "http://"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_, _ = fmt.Fprint(conn, "CONNECT public.example:443 HTTP/1.1\r\nHost: public.example:443\r\n\r\n")
	response, err := http.ReadResponse(bufio.NewReader(conn), &http.Request{Method: http.MethodConnect})
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 200 || dials.Load() != 1 {
		t.Fatalf("CONNECT failed: %d %d", response.StatusCode, dials.Load())
	}
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-upstreamDone:
	case <-time.After(time.Second):
		t.Fatal("policy close left upstream tunnel alive")
	}
}

func TestProxyHTTPWebSocketUpgradeKeepsBidirectionalTraffic(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, buffer, err := w.(http.Hijacker).Hijack()
		if err != nil {
			return
		}
		defer conn.Close()
		_, _ = buffer.WriteString("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n\r\n")
		_ = buffer.Flush()
		_, _ = io.Copy(conn, buffer)
	}))
	defer upstream.Close()
	p, _, _ := testProxy(t, Policy{AllowHTTP: true}, strings.TrimPrefix(upstream.URL, "http://"))
	conn, err := net.Dial("tcp", strings.TrimPrefix(p.URL(), "http://"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(time.Second))
	_, _ = fmt.Fprint(conn, "GET http://public.example/ws HTTP/1.1\r\nHost: public.example\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n\r\n")
	reader := bufio.NewReader(conn)
	response, err := http.ReadResponse(reader, &http.Request{Method: http.MethodGet})
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 101 {
		t.Fatalf("upgrade failed: %d", response.StatusCode)
	}
	_, _ = fmt.Fprint(conn, "ping")
	var reply [4]byte
	if _, err := io.ReadFull(reader, reply[:]); err != nil || string(reply[:]) != "ping" {
		t.Fatalf("upgraded traffic lost: %q %v", reply, err)
	}
}
