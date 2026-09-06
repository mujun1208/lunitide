package browserapp

import (
	"context"
	"testing"

	"github.com/lunitide/lunitide/internal/webviewhost"
)

// S-06: Manager.Open must run the SSRF policy gate (browser.CheckURL) in
// addition to NormalizeBrowserURL. Loopback, RFC1918, link-local (cloud
// metadata) and other reserved literals must be refused before a browser
// host is ever created.
func TestOpenRejectsSSRFTargets(t *testing.T) {
	blocked := []string{
		"https://127.0.0.1/",           // loopback
		"https://[::1]/",               // loopback v6
		"https://10.0.0.5/admin",       // RFC1918
		"https://192.168.1.1/",         // RFC1918
		"https://172.16.0.1/",          // RFC1918
		"https://169.254.169.254/",     // link-local / cloud metadata
	}
	for _, raw := range blocked {
		t.Run(raw, func(t *testing.T) {
			created := 0
			m, err := New(`C:\browser`, `C:\main`, func(webviewhost.BrowserHostOptions) (BrowserHost, error) {
				created++
				return newFakeHost(), nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := m.Open(context.Background(), raw); err == nil {
				t.Fatalf("Open(%q) accepted an SSRF target", raw)
			}
			if created != 0 {
				t.Fatalf("Open(%q) spawned a browser host despite SSRF refusal", raw)
			}
		})
	}
}

// A normal public HTTPS URL still passes the gate.
func TestOpenAllowsPublicHTTPS(t *testing.T) {
	m, err := New(`C:\browser`, `C:\main`, func(webviewhost.BrowserHostOptions) (BrowserHost, error) {
		return newFakeHost(), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Open(context.Background(), "https://example.com/"); err != nil {
		t.Fatalf("Open(public https) rejected: %v", err)
	}
}