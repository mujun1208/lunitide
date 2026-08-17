// Local-host helpers kept separate so service.go stays focused on the
// state machine and the storage surface.
package brapp

import (
	"context"
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

func removeFile(path string) error { return os.Remove(path) }

// DefaultHTTPClient bounds every host HTTP exchange.
var DefaultHTTPClient = &http.Client{Timeout: 4 * time.Second}

// NewHTTPRequestContext builds one request with the shared client's
// deadline semantics.
func NewHTTPRequestContext(ctx context.Context, method, url string, body io.Reader) (*http.Request, error) {
	return http.NewRequestWithContext(ctx, method, url, body)
}
