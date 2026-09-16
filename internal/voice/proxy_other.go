//go:build !windows

package voice

import (
	"net/http"
	"net/url"

	"github.com/lunitide/lunitide/internal/egressproxy"
)

func proxyResolver() func(*http.Request) (*url.URL, error) {
	return egressproxy.Resolver()
}
