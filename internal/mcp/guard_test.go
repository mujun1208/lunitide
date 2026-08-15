package mcp

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/tls"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"
)

// hostOf extracts host:port from a test server URL for the allowlist
// (private-IP classification is out of scope for this package).
func hostOf(t *testing.T, raw string) string {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse %q: %v", raw, err)
	}
	return u.Host
}

// tlsTestClient builds a client for srv with the test TLS bypass so guard
// behaviour above the TLS layer can be exercised. tune runs before the
// HTTP stack is rebuilt so shortened timeout fields take effect.
func tlsTestClient(t *testing.T, srv *httptest.Server, tune ...func(*Client)) *Client {
	t.Helper()
	c, err := NewClient(RemoteEndpoint{ID: "test", BaseURL: srv.URL}, []string{hostOf(t, srv.URL)})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	for _, f := range tune {
		f(c)
	}
	c.SetTLSConfig(&tls.Config{InsecureSkipVerify: true}) // test-only: httptest self-signed cert
	return c
}

func TestMcpGuard(t *testing.T) {
	t.Run("plain http endpoint rejected", func(t *testing.T) {
		if _, err := NewClient(RemoteEndpoint{ID: "x", BaseURL: "http://127.0.0.1:9"}, []string{"127.0.0.1:9"}); !errors.Is(err, ErrNotHttps) {
			t.Fatalf("http endpoint: want ErrNotHttps, got %v", err)
		}
		if err := ValidateBaseURL("https://"); !errors.Is(err, ErrNotHttps) {
			t.Fatalf("empty host: want ErrNotHttps, got %v", err)
		}
	})

	t.Run("host outside allowlist rejected", func(t *testing.T) {
		if _, err := NewClient(RemoteEndpoint{ID: "x", BaseURL: "https://example.com"}, []string{"other.example"}); !errors.Is(err, ErrHostNotAllowed) {
			t.Fatalf("want ErrHostNotAllowed, got %v", err)
		}
	})

	t.Run("self-signed certificate rejected by system trust", func(t *testing.T) {
		srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer srv.Close()
		// No TLS injection here: the httptest certificate is self-signed,
		// so real system-chain verification must fail (MCP-001).
		c, err := NewClient(RemoteEndpoint{ID: "x", BaseURL: srv.URL}, []string{hostOf(t, srv.URL)})
		if err != nil {
			t.Fatalf("NewClient: %v", err)
		}
		if _, err := c.Invoke(context.Background(), InvokeInput{Tool: "ping"}); !errors.Is(err, ErrInvokeFailed) {
			t.Fatalf("self-signed cert must fail verification, got %v", err)
		}
	})

	t.Run("response above cap refused without truncation", func(t *testing.T) {
		var hits atomic.Int32
		srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			hits.Add(1)
			_, _ = w.Write(bytes.Repeat([]byte("x"), MaxResponseBytes+1))
		}))
		defer srv.Close()
		c := tlsTestClient(t, srv)
		res, err := c.Invoke(context.Background(), InvokeInput{Tool: "big"})
		if !errors.Is(err, ErrResponseTooLarge) {
			t.Fatalf("want ErrResponseTooLarge, got %v", err)
		}
		if len(res.Data) != 0 {
			t.Fatalf("no partial data may escape, got %d bytes", len(res.Data))
		}
		if hits.Load() != 1 {
			t.Fatalf("policy answers must not retry: %d requests", hits.Load())
		}
	})

	t.Run("header timeout retries exactly once", func(t *testing.T) {
		var hits atomic.Int32
		srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			hits.Add(1)
			time.Sleep(2 * time.Second)
			_, _ = w.Write([]byte(`{"late":true}`))
		}))
		defer srv.Close()
		c := tlsTestClient(t, srv, func(c *Client) { c.HeaderTimeout = 200 * time.Millisecond })
		start := time.Now()
		_, err := c.Invoke(context.Background(), InvokeInput{Tool: "slow"})
		if !errors.Is(err, ErrInvokeFailed) {
			t.Fatalf("want ErrInvokeFailed after two timeouts, got %v", err)
		}
		if hits.Load() != 2 {
			t.Fatalf("want exactly one retry (2 requests), got %d", hits.Load())
		}
		if elapsed := time.Since(start); elapsed >= time.Second {
			t.Fatalf("first-byte timeout must fire early, took %s", elapsed)
		}
	})

	t.Run("redirect blocked", func(t *testing.T) {
		var hits atomic.Int32
		srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			hits.Add(1)
			http.Redirect(w, r, "https://unauthorised.example/elsewhere", http.StatusFound)
		}))
		defer srv.Close()
		c := tlsTestClient(t, srv)
		if _, err := c.Invoke(context.Background(), InvokeInput{Tool: "bounce"}); !errors.Is(err, ErrRedirectBlocked) {
			t.Fatalf("want ErrRedirectBlocked, got %v", err)
		}
		if hits.Load() != 1 {
			t.Fatalf("redirect block must not retry: %d requests", hits.Load())
		}
	})

	t.Run("gzip encoding blocked", func(t *testing.T) {
		var buf bytes.Buffer
		zw := gzip.NewWriter(&buf)
		_, _ = zw.Write([]byte(`{"result":"ok"}`))
		_ = zw.Close()
		srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Encoding", "gzip")
			_, _ = w.Write(buf.Bytes())
		}))
		defer srv.Close()
		c := tlsTestClient(t, srv)
		if _, err := c.Invoke(context.Background(), InvokeInput{Tool: "zipped"}); !errors.Is(err, ErrEncodingBlocked) {
			t.Fatalf("want ErrEncodingBlocked, got %v", err)
		}
	})

	t.Run("healthy json returned", func(t *testing.T) {
		var gotPath, gotArgs string
		srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.Path
			gotArgs = r.URL.Query().Get("args")
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"result":"ok"}`))
		}))
		defer srv.Close()
		c := tlsTestClient(t, srv)
		res, err := c.Invoke(context.Background(), InvokeInput{Tool: "echo", ArgsJSON: []byte(`{"q":1}`)})
		if err != nil {
			t.Fatalf("Invoke: %v", err)
		}
		if string(res.Data) != `{"result":"ok"}` {
			t.Fatalf("body mismatch: %q", res.Data)
		}
		if res.Tool != "echo" || res.Truncated {
			t.Fatalf("result metadata mismatch: %+v", res)
		}
		if gotPath != "/tools/echo" || gotArgs != `{"q":1}` {
			t.Fatalf("request shape mismatch: path %q args %q", gotPath, gotArgs)
		}
	})
}
