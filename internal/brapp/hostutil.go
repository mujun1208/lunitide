// Local-host helpers kept separate so service.go stays focused on the
// state machine and the storage surface.
package brapp

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"time"
)

func osGetenv(key string) string { return os.Getenv(key) }

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func firstExisting(configured string, candidates []string) string {
	if configured != "" && fileExists(configured) {
		return configured
	}
	for _, c := range candidates {
		if fileExists(c) {
			return c
		}
	}
	return ""
}

// DefaultHTTPClient bounds every host HTTP exchange.
var DefaultHTTPClient = &http.Client{Timeout: 4 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("CDP redirects are not allowed") }}

// NewHTTPRequestContext builds one request with the shared client's
// deadline semantics.
func NewHTTPRequestContext(ctx context.Context, method, url string, body io.Reader) (*http.Request, error) {
	return http.NewRequestWithContext(ctx, method, url, body)
}
