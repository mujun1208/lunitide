//go:build !windows

package egressproxy

import (
	"net/http"
	"net/url"
)

// Everywhere but Windows the environment is where a proxy is configured, so
// the standard resolution is already the right one. This file exists so the
// package still builds for a developer running `go build ./...` on a Mac.
func Resolver() func(*http.Request) (*url.URL, error) {
	return http.ProxyFromEnvironment
}
