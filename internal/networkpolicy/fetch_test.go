package networkpolicy

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// fetchPolicy allows plain HTTP so tests can exercise the fetch flow without
// TLS fixtures; the IP egress policy is independent of the scheme.
var fetchPolicy = Policy{AllowHTTP: true}

// dialTo returns a DialContext seam that routes every (already validated and
// resolved) dial to the loopback test server. URL validation and the IP
// policy still run before this is ever invoked.
func dialTo(addr string) func(ctx context.Context, network, address string) (net.Conn, error) {
	return func(ctx context.Context, network, _ string) (net.Conn, error) {
		d := &net.Dialer{Timeout: 2 * time.Second}
		return d.DialContext(ctx, network, addr)
	}
}

func serverPort(t *testing.T, srv *httptest.Server) string {
	t.Helper()
	_, port, err := net.SplitHostPort(srv.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	return port
}

func TestValidateFetchURLCorpus(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		raw  string
		ok   bool
		want string // canonical URL when ok
	}{
		{"https basic", "https://example.com/doc", true, "https://example.com/doc"},
		{"http allowed by policy", "http://example.com/doc", true, "http://example.com/doc"},
		{"query allowed", "https://example.com/s?q=1&x=2", true, "https://example.com/s?q=1&x=2"},
		{"fragment stripped", "https://example.com/doc#section", true, "https://example.com/doc"},
		{"default port folded", "https://example.com:443/doc", true, "https://example.com/doc"},
		{"non-default port kept", "https://example.com:8443/doc", true, "https://example.com:8443/doc"},
		{"trailing dot canonicalized", "https://EXAMPLE.com./doc", true, "https://example.com/doc"},
		{"ipv6 literal keeps brackets", "http://[2606:4700:4700::1111]/doc", true, "http://[2606:4700:4700::1111]/doc"},
		{"empty path rooted", "https://example.com", true, "https://example.com/"},
		{"userinfo rejected", "https://user:pass@example.com/doc", false, ""},
		{"opaque rejected", "https:example.com", false, ""},
		{"relative rejected", "/doc", false, ""},
		{"ftp rejected", "ftp://example.com/file", false, ""},
		{"file rejected", "file:///etc/passwd", false, ""},
		{"bad port rejected", "https://example.com:99999/doc", false, ""},
		{"empty host rejected", "https:///doc", false, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u, err := validateFetchURL(tt.raw, fetchPolicy)
			if (err == nil) != tt.ok {
				t.Fatalf("err=%v", err)
			}
			if tt.ok && u.String() != tt.want {
				t.Fatalf("canonical=%q want %q", u.String(), tt.want)
			}
			if tt.ok && u.Fragment != "" {
				t.Fatalf("fragment retained: %q", u.Fragment)
			}
		})
	}
}

func TestValidateFetchURLSchemePolicy(t *testing.T) {
	t.Parallel()
	if _, err := validateFetchURL("http://example.com/", Policy{}); ErrorCode(err) != CodeSSRFBlocked {
		t.Fatalf("http without AllowHTTP: code=%q", ErrorCode(err))
	}
	if _, err := validateFetchURL("http://example.com/", Policy{AllowHTTP: true}); err != nil {
		t.Fatalf("http with AllowHTTP: %v", err)
	}
}

func TestFetchBlocksPrivateLiteralBeforeDial(t *testing.T) {
	t.Parallel()
	var dials atomic.Int32
	dial := func(ctx context.Context, network, address string) (net.Conn, error) {
		dials.Add(1)
		return (&net.Dialer{}).DialContext(ctx, network, address)
	}
	for _, raw := range []string{
		"http://127.0.0.1/",
		"http://169.254.169.254/latest/meta-data",
		"http://192.168.1.1/",
		"http://[::1]/",
		"http://localhost/",
	} {
		_, err := Fetch(context.Background(), raw, FetchOptions{Policy: fetchPolicy, DialContext: dial})
		if ErrorCode(err) != CodeSSRFBlocked {
			t.Errorf("%s: code=%q err=%v", raw, ErrorCode(err), err)
		}
	}
	if dials.Load() != 0 {
		t.Fatalf("dial happened %d times for blocked targets", dials.Load())
	}
}

func TestFetchBlocksMixedDNSAnswer(t *testing.T) {
	t.Parallel()
	r := resolverFunc(func(context.Context, string, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("93.184.216.34"), netip.MustParseAddr("10.0.0.1")}, nil
	})
	_, err := Fetch(context.Background(), "http://example.test/", FetchOptions{Policy: fetchPolicy, Resolver: r})
	if ErrorCode(err) != CodeSSRFBlocked {
		t.Fatalf("mixed answer code=%q", ErrorCode(err))
	}
}

func TestFetchFollowsRedirectWithPerHopRevalidation(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/start":
			http.Redirect(w, r, "/final", http.StatusFound)
		case "/final":
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte("done"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	var resolutions atomic.Int32
	r := resolverFunc(func(context.Context, string, string) ([]netip.Addr, error) {
		resolutions.Add(1)
		return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
	})
	result, err := Fetch(context.Background(), "http://example.test:"+serverPort(t, srv)+"/start", FetchOptions{
		Policy:      fetchPolicy,
		Resolver:    r,
		DialContext: dialTo(srv.Listener.Addr().String()),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != http.StatusOK || string(result.Body) != "done" || result.Truncated {
		t.Fatalf("result=%+v", result)
	}
	if want := "http://example.test:" + serverPort(t, srv) + "/final"; result.FinalURL != want {
		t.Fatalf("FinalURL=%q want %q", result.FinalURL, want)
	}
	// Each hop re-resolves the host: start + final = 2 resolutions.
	if resolutions.Load() != 2 {
		t.Fatalf("resolutions=%d, want per-hop re-resolution", resolutions.Load())
	}
}

func TestFetchBlocksRedirectToPrivateIP(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://internal.test:8080/secret", http.StatusFound)
	}))
	defer srv.Close()

	resolverCalls := 0
	r := resolverFunc(func(_ context.Context, _, host string) ([]netip.Addr, error) {
		resolverCalls++
		if host == "internal.test" {
			return []netip.Addr{netip.MustParseAddr("192.168.0.1")}, nil
		}
		return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
	})
	_, err := Fetch(context.Background(), "http://example.test:"+serverPort(t, srv)+"/start", FetchOptions{
		Policy:      fetchPolicy,
		Resolver:    r,
		DialContext: dialTo(srv.Listener.Addr().String()),
	})
	if ErrorCode(err) != CodeSSRFBlocked {
		t.Fatalf("redirect to private code=%q err=%v", ErrorCode(err), err)
	}
	if resolverCalls != 2 {
		t.Fatalf("resolver calls=%d; redirect target must be re-resolved", resolverCalls)
	}
}

func TestFetchBlocksDNSRebindingAcrossRedirect(t *testing.T) {
	t.Parallel()
	// Same host, relative redirect: the second hop re-resolves the name and the
	// resolver flips to a private answer — the classic DNS rebinding sequence.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/start" {
			http.Redirect(w, r, "/final", http.StatusFound)
			return
		}
		_, _ = w.Write([]byte("must never be reached"))
	}))
	defer srv.Close()

	var calls atomic.Int32
	r := resolverFunc(func(context.Context, string, string) ([]netip.Addr, error) {
		if calls.Add(1) == 1 {
			return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
		}
		return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
	})
	_, err := Fetch(context.Background(), "http://example.test:"+serverPort(t, srv)+"/start", FetchOptions{
		Policy:      fetchPolicy,
		Resolver:    r,
		DialContext: dialTo(srv.Listener.Addr().String()),
	})
	if ErrorCode(err) != CodeSSRFBlocked {
		t.Fatalf("rebinding code=%q err=%v", ErrorCode(err), err)
	}
	if calls.Load() != 2 {
		t.Fatalf("resolver calls=%d", calls.Load())
	}
}

func TestFetchRedirectLimit(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n, _ := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/hop"))
		http.Redirect(w, r, "/hop"+strconv.Itoa(n+1), http.StatusFound)
	}))
	defer srv.Close()

	r := resolverFunc(func(context.Context, string, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
	})
	_, err := Fetch(context.Background(), "http://example.test:"+serverPort(t, srv)+"/hop0", FetchOptions{
		Policy:       fetchPolicy,
		Resolver:     r,
		DialContext:  dialTo(srv.Listener.Addr().String()),
		MaxRedirects: 3,
	})
	if ErrorCode(err) != CodeRedirectBlocked {
		t.Fatalf("limit code=%q err=%v", ErrorCode(err), err)
	}
}

func TestFetchBodyCapTruncates(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte(strings.Repeat("x", 4096)))
	}))
	defer srv.Close()

	r := resolverFunc(func(context.Context, string, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
	})
	result, err := Fetch(context.Background(), "http://example.test:"+serverPort(t, srv)+"/", FetchOptions{
		Policy:       fetchPolicy,
		Resolver:     r,
		DialContext:  dialTo(srv.Listener.Addr().String()),
		MaxBodyBytes: 1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Truncated || len(result.Body) != 1024 {
		t.Fatalf("truncated=%v len=%d", result.Truncated, len(result.Body))
	}
}

func TestCopyWritesBodyAndTruncatesAtCap(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte(strings.Repeat("y", 4096)))
	}))
	defer srv.Close()

	r := resolverFunc(func(context.Context, string, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
	})
	opts := FetchOptions{
		Policy:       fetchPolicy,
		Resolver:     r,
		DialContext:  dialTo(srv.Listener.Addr().String()),
		MaxBodyBytes: 1024,
	}
	raw := "http://example.test:" + serverPort(t, srv) + "/"
	var buf strings.Builder
	written, result, err := Copy(context.Background(), raw, &buf, opts)
	if err != nil {
		t.Fatal(err)
	}
	if written != 1024 || !result.Truncated || buf.Len() != 1024 || result.Status != http.StatusOK {
		t.Fatalf("written=%d truncated=%v len=%d status=%d", written, result.Truncated, buf.Len(), result.Status)
	}
	if result.Body != nil {
		t.Fatalf("Copy must stream, not buffer Body")
	}

	var full strings.Builder
	written, result, err = Copy(context.Background(), raw, &full, FetchOptions{
		Policy:      fetchPolicy,
		Resolver:    r,
		DialContext: dialTo(srv.Listener.Addr().String()),
	})
	if err != nil {
		t.Fatal(err)
	}
	if written != 4096 || result.Truncated || full.String() != strings.Repeat("y", 4096) {
		t.Fatalf("full written=%d truncated=%v len=%d", written, result.Truncated, full.Len())
	}
}

func TestFetchUsesHTTPProxyInsteadOfPinnedDestinationDial(t *testing.T) {
	t.Parallel()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("via-proxy"))
	}))
	defer origin.Close()

	var proxyHits atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyHits.Add(1)
		resp, err := http.Get(origin.URL + "/")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, resp.Body)
	}))
	defer proxy.Close()

	proxyURL, err := url.Parse(proxy.URL)
	if err != nil {
		t.Fatal(err)
	}
	r := resolverFunc(func(context.Context, string, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
	})
	target := "http://example.test:" + serverPort(t, origin) + "/update.json"
	result, err := Fetch(context.Background(), target, FetchOptions{
		Policy:   fetchPolicy,
		Resolver: r,
		Proxy:    http.ProxyURL(proxyURL),
	})
	if err != nil {
		t.Fatal(err)
	}
	if proxyHits.Load() != 1 {
		t.Fatalf("proxy hits=%d; want 1", proxyHits.Load())
	}
	if string(result.Body) != "via-proxy" {
		t.Fatalf("body=%q", result.Body)
	}
}

