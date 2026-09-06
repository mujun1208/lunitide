package browserapp

import (
	"context"
	"net"
	"net/url"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/webviewhost"
)

func TestBrowserManagerProxyClosesWithItsHost(t *testing.T) {
	var endpoint string
	m, err := New(`C:\browser`, `C:\main`, func(o webviewhost.BrowserHostOptions) (BrowserHost, error) {
		endpoint = o.ProxyURL
		return newFakeHost(), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = m.Shutdown(ctx)
	})
	if _, err := m.Open(context.Background(), "https://example.com/"); err != nil {
		t.Fatal(err)
	}
	proxy, err := url.Parse(endpoint)
	if err != nil || proxy.Host == "" {
		t.Fatalf("no private proxy: %q", endpoint)
	}
	connection, err := net.DialTimeout("tcp", proxy.Host, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	_ = connection.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := m.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if connection, err := net.DialTimeout("tcp", proxy.Host, 100*time.Millisecond); err == nil {
		connection.Close()
		t.Fatal("proxy survived browser close")
	}
}
