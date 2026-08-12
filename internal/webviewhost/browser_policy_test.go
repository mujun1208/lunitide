package webviewhost

import (
	"path/filepath"
	"testing"
)

func TestBrowserNavigationPolicy(t *testing.T) {
	allowed := map[string]string{
		"https://example.com/":            "https://example.com/",
		"HTTPS://EXAMPLE.COM:443/a?q=1#x": "https://example.com/a?q=1#x",
		"https://example.com:8443/a":      "https://example.com:8443/a",
	}
	for raw, want := range allowed {
		got, err := NormalizeBrowserURL(raw)
		if err != nil || got != want || !BrowserNavigationAllowed(raw) {
			t.Errorf("NormalizeBrowserURL(%q)=(%q,%v), want %q", raw, got, err, want)
		}
	}
	denied := []string{
		"http://example.com/", "file:///tmp/x", "data:text/html,x",
		"javascript:alert(1)", "https://user:pass@example.com/", "https://",
		"https://example.com:bad/", "//example.com/", " https://example.com/",
		"https://example.com/\nnext", "not a url", "://bad",
	}
	for _, raw := range denied {
		if normalized, err := NormalizeBrowserURL(raw); err == nil || BrowserNavigationAllowed(raw) {
			t.Errorf("unsafe browser URL %q allowed as %q", raw, normalized)
		}
	}
}

func TestIsolatedBrowserProfileMustDifferFromMainProfile(t *testing.T) {
	root := t.TempDir()
	main := filepath.Join(root, "WebView2")
	browser := filepath.Join(root, "BrowserWebView2")
	got, err := ValidateIsolatedBrowserProfile(browser, main)
	if err != nil || got != filepath.Clean(browser) {
		t.Fatalf("separate profile rejected: got=%q err=%v", got, err)
	}
	if _, err := ValidateIsolatedBrowserProfile(main, main); err == nil {
		t.Fatal("shared browser/main profile was accepted")
	}
	if _, err := ValidateIsolatedBrowserProfile("relative", main); err == nil {
		t.Fatal("relative browser profile was accepted")
	}
}
