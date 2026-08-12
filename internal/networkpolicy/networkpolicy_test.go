package networkpolicy

import (
	"compress/gzip"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type resolverFunc func(context.Context, string, string) ([]netip.Addr, error)

func (f resolverFunc) LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error) {
	return f(ctx, network, host)
}

func TestAddressPolicyTable(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		ip   string
		want bool
	}{
		{"public IPv4", "8.8.8.8", true}, {"public IPv6", "2606:4700:4700::1111", true},
		{"IPv4 private", "10.1.2.3", false}, {"IPv6 private", "fd00::1", false},
		{"IPv4 loopback", "127.0.0.1", false}, {"IPv6 loopback", "::1", false},
		{"link local", "169.254.2.3", false}, {"multicast", "224.0.0.1", false},
		{"unspecified", "0.0.0.0", false}, {"documentation v4", "192.0.2.1", false},
		{"documentation v6", "2001:db8::1", false}, {"benchmark", "198.18.0.1", false},
		{"reserved", "240.0.0.1", false}, {"mapped", "::ffff:8.8.8.8", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := allowedIP(netip.MustParseAddr(tt.ip)); got != tt.want {
				t.Fatalf("allowedIP=%v, want %v", got, tt.want)
			}
		})
	}
}

func TestEndpointPolicyTable(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, base, path string
		policy           Policy
		ok               bool
	}{
		{"https default", "https://example.com/root", "v1/chat", Policy{}, true},
		{"http default denied", "http://example.com", "v1", Policy{}, false},
		{"http explicit", "http://example.com", "v1", Policy{AllowHTTP: true}, true},
		{"userinfo", "https://secret@example.com", "v1", Policy{}, false},
		{"base query", "https://example.com?token=secret", "v1", Policy{}, false},
		{"base fragment", "https://example.com#x", "v1", Policy{}, false},
		{"path authority", "https://example.com", "//evil.example/x", Policy{}, false},
		{"path query", "https://example.com", "v1?key=secret", Policy{}, false},
		{"path fragment", "https://example.com", "v1#x", Policy{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u, err := validateAndJoin(tt.base, tt.path, tt.policy)
			if (err == nil) != tt.ok {
				t.Fatalf("err=%v", err)
			}
			if err == nil && u.Hostname() != "example.com" {
				t.Fatalf("authority changed: %q", u.Host)
			}
		})
	}
}

func TestResolveFailClosedAndRebinding(t *testing.T) {
	_, err := New(context.Background(), "https://localhost", "v1", Options{Resolver: publicResolver()})
	if ErrorCode(err) != CodeSSRFBlocked {
		t.Fatalf("localhost code=%q", ErrorCode(err))
	}

	var calls atomic.Int32
	r := resolverFunc(func(context.Context, string, string) ([]netip.Addr, error) {
		calls.Add(1)
		return []netip.Addr{netip.MustParseAddr("8.8.8.8"), netip.MustParseAddr("127.0.0.1")}, nil
	})
	_, err = New(context.Background(), "https://example.test", "v1", Options{Resolver: r})
	if ErrorCode(err) != CodeSSRFBlocked {
		t.Fatalf("mixed DNS code=%q", ErrorCode(err))
	}

	calls.Store(0)
	r = resolverFunc(func(context.Context, string, string) ([]netip.Addr, error) {
		n := calls.Add(1)
		if n > 1 {
			return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
		}
		return []netip.Addr{netip.MustParseAddr("8.8.8.8")}, nil
	})
	c, err := New(context.Background(), "https://example.test", "v1", Options{Resolver: r, ConnectTimeout: time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest(http.MethodGet, c.BaseURL, nil)
	_, _ = c.Do(req)
	if calls.Load() != 1 {
		t.Fatalf("resolver called %d times; transport performed secondary DNS", calls.Load())
	}
}

func TestTransportSecurityDefaults(t *testing.T) {
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")
	c, err := New(context.Background(), "https://example.test", "v1", Options{Resolver: publicResolver()})
	if err != nil {
		t.Fatal(err)
	}
	tr := c.Client.Transport.(*http.Transport)
	if tr.Proxy != nil {
		t.Fatal("environment proxy is enabled")
	}
	if tr.TLSClientConfig.MinVersion != tls.VersionTLS12 {
		t.Fatalf("minimum TLS=%x", tr.TLSClientConfig.MinVersion)
	}
	if tr.TLSClientConfig.ServerName != "example.test" {
		t.Fatalf("SNI=%q", tr.TLSClientConfig.ServerName)
	}
	if err := c.Client.CheckRedirect(nil, nil); ErrorCode(err) != CodeRedirectBlocked {
		t.Fatalf("redirect code=%q", ErrorCode(err))
	}
}

func TestOverallTimeoutCanBeExplicitlyDisabled(t *testing.T) {
	c, err := New(context.Background(), "https://example.test", "v1", Options{
		Resolver:              publicResolver(),
		DisableOverallTimeout: true,
		IdleReadTimeout:       90 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if c.Client.Timeout != 0 {
		t.Fatalf("client timeout=%v, want no total response deadline", c.Client.Timeout)
	}
}

func TestIdleReadConnRefreshesDeadline(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	conn := &idleReadConn{Conn: client, timeout: 200 * time.Millisecond}
	go func() {
		_, _ = server.Write([]byte("a"))
		time.Sleep(250 * time.Millisecond)
		_, _ = server.Write([]byte("b"))
	}()

	buf := make([]byte, 1)
	if _, err := conn.Read(buf); err != nil || string(buf) != "a" {
		t.Fatalf("first read=%q err=%v", buf, err)
	}
	time.Sleep(120 * time.Millisecond)
	if _, err := conn.Read(buf); err != nil || string(buf) != "b" {
		t.Fatalf("second read=%q err=%v", buf, err)
	}
}

func TestBodyLimitAfterDecompression(t *testing.T) {
	var compressed strings.Builder
	zw := gzip.NewWriter(&compressed)
	_, _ = zw.Write([]byte(strings.Repeat("x", 4096)))
	_ = zw.Close()
	zr, err := gzip.NewReader(strings.NewReader(compressed.String()))
	if err != nil {
		t.Fatal(err)
	}
	b := &limitedBody{r: zr, closer: zr, remaining: 32}
	_, err = io.ReadAll(b)
	if ErrorCode(err) != CodeResponseTooLarge {
		t.Fatalf("compression bomb code=%q err=%v", ErrorCode(err), err)
	}
}

func TestSSELimits(t *testing.T) {
	c := &Connector{maxLine: 8, maxEvent: 12}
	if _, _, err := c.ReadSSE(strings.NewReader("data: 123456789\n\n")); ErrorCode(err) != CodeResponseTooLarge {
		t.Fatalf("line code=%q", ErrorCode(err))
	}
	if _, _, err := c.ReadSSE(strings.NewReader("1234567\n1234567\n\n")); ErrorCode(err) != CodeResponseTooLarge {
		t.Fatalf("event code=%q", ErrorCode(err))
	}
}

func TestCancellationAndStableSafeErrors(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := resolverFunc(func(ctx context.Context, _, _ string) ([]netip.Addr, error) { return nil, ctx.Err() })
	_, err := New(ctx, "https://example.test?Authorization=secret", "v1", Options{Resolver: r})
	// URL validation happens first and must not echo its secret query.
	if strings.Contains(err.Error(), "secret") || strings.Contains(strings.ToLower(err.Error()), "authorization") {
		t.Fatalf("secret leaked: %v", err)
	}
	_, err = New(ctx, "https://example.test", "v1", Options{Resolver: r})
	if ErrorCode(err) != CodeCancelled {
		t.Fatalf("cancel code=%q err=%v", ErrorCode(err), err)
	}
	for _, tc := range []struct {
		err  error
		code Code
	}{
		{context.DeadlineExceeded, CodeTimeout},
		{errors.New("remote error: tls: handshake failure"), CodeTLSError},
	} {
		if got := ErrorCode(classifyError("test", tc.err)); got != tc.code {
			t.Errorf("code=%q want=%q", got, tc.code)
		}
	}
}

func TestDNSError(t *testing.T) {
	r := resolverFunc(func(context.Context, string, string) ([]netip.Addr, error) { return nil, errors.New("lookup failed") })
	_, err := New(context.Background(), "https://example.test", "v1", Options{Resolver: r})
	if ErrorCode(err) != CodeDNSError {
		t.Fatalf("code=%q", ErrorCode(err))
	}
}

func publicResolver() Resolver {
	return resolverFunc(func(context.Context, string, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("8.8.8.8")}, nil
	})
}

func TestMain(m *testing.M) { os.Exit(m.Run()) }

func TestTLSBypassConfigurationsAreRejected(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		config *tls.Config
	}{
		{"insecure skip verify", &tls.Config{InsecureSkipVerify: true}}, //nolint:gosec -- verifies rejection
		{"verify peer callback", &tls.Config{VerifyPeerCertificate: func([][]byte, [][]*x509.Certificate) error { return nil }}},
		{"verify connection callback", &tls.Config{VerifyConnection: func(tls.ConnectionState) error { return nil }}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := New(context.Background(), "https://example.test", "v1", Options{Resolver: publicResolver(), TLSConfig: tt.config})
			if ErrorCode(err) != CodeTLSError {
				t.Fatalf("code=%q err=%v", ErrorCode(err), err)
			}
		})
	}
}

func TestTLSConfigUsesFixedServerNameAndWhitelist(t *testing.T) {
	t.Parallel()
	src := &tls.Config{ServerName: "attacker.test", MinVersion: tls.VersionTLS10, NextProtos: []string{"h2"}}
	c, err := New(context.Background(), "https://EXAMPLE.test./root", "v1", Options{Resolver: publicResolver(), TLSConfig: src})
	if err != nil {
		t.Fatal(err)
	}
	tr := c.Client.Transport.(*http.Transport)
	if got := tr.TLSClientConfig.ServerName; got != "example.test" {
		t.Fatalf("SNI=%q", got)
	}
	if got := tr.TLSClientConfig.MinVersion; got != tls.VersionTLS12 {
		t.Fatalf("minimum TLS=%x", got)
	}
	if c.BaseURL != "https://example.test:443/root/v1" {
		t.Fatalf("canonical BaseURL=%q", c.BaseURL)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestDoRejectsOriginAndHostConfusion(t *testing.T) {
	t.Parallel()
	c, err := New(context.Background(), "https://example.test/root", "v1", Options{Resolver: publicResolver()})
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	c.Client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return nil, errors.New("must not be called")
	})
	tests := []struct {
		name, target, host string
	}{
		{"different host", "https://evil.test:443/root/v1", ""},
		{"different scheme", "http://example.test:443/root/v1", ""},
		{"different effective port", "https://example.test:444/root/v1", ""},
		{"userinfo authority", "https://user@example.test:443/root/v1", ""},
		{"foreign Host override", c.BaseURL, "evil.test"},
		{"same Host override", c.BaseURL, "example.test:443"},
		{"userinfo Host override", c.BaseURL, "user@example.test:443"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, reqErr := http.NewRequest(http.MethodGet, tt.target, nil)
			if reqErr != nil {
				t.Fatal(reqErr)
			}
			req.Host = tt.host
			_, doErr := c.Do(req)
			if ErrorCode(doErr) != CodeSSRFBlocked {
				t.Fatalf("code=%q err=%v", ErrorCode(doErr), doErr)
			}
		})
	}
	if calls.Load() != 0 {
		t.Fatalf("transport called %d times", calls.Load())
	}
}

func TestPinnedDialRequiresExpectedAuthority(t *testing.T) {
	t.Parallel()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	port := listener.Addr().(*net.TCPAddr).Port
	authority := net.JoinHostPort("example.test", strconv.Itoa(port))
	dial := pinnedDial(&net.Dialer{Timeout: time.Second}, []netip.Addr{netip.MustParseAddr("127.0.0.1")}, authority, 0)
	if conn, dialErr := dial(context.Background(), "tcp", net.JoinHostPort("evil.test", strconv.Itoa(port))); conn != nil || ErrorCode(dialErr) != CodeSSRFBlocked {
		t.Fatalf("unexpected authority: conn=%v code=%q err=%v", conn, ErrorCode(dialErr), dialErr)
	}
	accepted := make(chan error, 1)
	go func() {
		conn, acceptErr := listener.Accept()
		if conn != nil {
			_ = conn.Close()
		}
		accepted <- acceptErr
	}()
	conn, err := dial(context.Background(), "tcp", authority)
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.Close()
	if err := <-accepted; err != nil {
		t.Fatal(err)
	}
}

func TestNewRequestStaysBelowBasePath(t *testing.T) {
	t.Parallel()
	c, err := New(context.Background(), "https://example.test/root", "v1", Options{Resolver: publicResolver()})
	if err != nil {
		t.Fatal(err)
	}
	req, err := c.NewRequest(context.Background(), http.MethodPost, "chat/completions", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	if got := req.URL.String(); got != "https://example.test:443/root/v1/chat/completions" {
		t.Fatalf("URL=%q", got)
	}
	if req.Host != "" {
		t.Fatalf("safe request retained redundant Host override %q", req.Host)
	}
	if err := c.validateRequest(req); err != nil {
		t.Fatalf("connector rejected its own request: %v", err)
	}
	for _, unsafe := range []string{"../admin", "%2e%2e/admin", "//evil.test/x", "https://evil.test/x", "x?redirect=evil"} {
		if _, err := c.NewRequest(context.Background(), http.MethodGet, unsafe, nil); ErrorCode(err) != CodeSSRFBlocked {
			t.Errorf("path %q: code=%q err=%v", unsafe, ErrorCode(err), err)
		}
	}
}

func TestDoRejectsPathMutationAndEncodedTraversal(t *testing.T) {
	c, err := New(context.Background(), "https://example.test/root", "v1", Options{Resolver: publicResolver()})
	if err != nil {
		t.Fatal(err)
	}
	c.Client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) { t.Fatal("transport called"); return nil, nil })
	for _, mutate := range []func(*http.Request){
		func(r *http.Request) { r.URL.Path = "/admin" },
		func(r *http.Request) { r.URL.RawPath = "/root/v1/%2e%2e/admin"; r.URL.Path = "/root/v1/../admin" },
		func(r *http.Request) { r.URL.Path = "/root/v1//chat" },
	} {
		r, _ := c.NewRequest(context.Background(), http.MethodGet, "chat", nil)
		mutate(r)
		if _, err := c.Do(r); ErrorCode(err) != CodeSSRFBlocked {
			t.Fatalf("code=%q err=%v url=%s", ErrorCode(err), err, r.URL)
		}
	}
}

func TestSSESkipsKeepaliveAndClassifiesEOF(t *testing.T) {
	c := &Connector{maxLine: 128, maxEvent: 256}
	event, eof, err := c.ReadSSE(strings.NewReader(": ping\n\n\n\ndata: hello\n\n"))
	if err != nil || eof || !strings.Contains(string(event), "hello") {
		t.Fatalf("event=%q eof=%v err=%v", event, eof, err)
	}
	_, eof, err = c.ReadSSE(strings.NewReader(": ping\n\n"))
	if err != nil || !eof {
		t.Fatalf("eof=%v err=%v", eof, err)
	}
}
