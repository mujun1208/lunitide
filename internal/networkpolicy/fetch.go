package networkpolicy

import (
	"context"
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// FetchOptions configures one Fetch call. Unlike Connector (fixed API base
// URL for a provider), Fetch targets arbitrary web URLs, so it allows query
// strings and follows redirects — but every hop is validated, re-resolved and
// dialed through the pinned transport again, so a redirect can never smuggle
// the connection to a private address (PRD M4: 防 DNS rebinding/内网/
// metadata/重定向绕过).
type FetchOptions struct {
	Policy                Policy
	Resolver              Resolver
	ConnectTimeout        time.Duration
	ResponseHeaderTimeout time.Duration
	// OverallTimeout bounds the whole redirect chain, not a single hop.
	OverallTimeout time.Duration
	// MaxRedirects bounds the hop count (default 5, hard cap 10).
	MaxRedirects int
	// MaxBodyBytes caps the (decompressed) response body (default 1 MiB).
	// The body is truncated at the cap and FetchResult.Truncated is set;
	// oversized responses are data, not errors.
	MaxBodyBytes int64
	TLSConfig    *tls.Config
	// DialContext replaces the pinned dialer. It is a hermetic-test seam for
	// loopback fixtures and MUST be nil in production; URL validation and the
	// IP egress policy still run before any dial.
	DialContext func(ctx context.Context, network, address string) (net.Conn, error)
}

// FetchResult is the captured page. Body holds at most MaxBodyBytes bytes of
// the decompressed response; Truncated reports clipping.
type FetchResult struct {
	FinalURL    string
	Status      int
	ContentType string
	Body        []byte
	Truncated   bool
}

const (
	fetchDefaultMaxRedirects = 5
	fetchMaxRedirectsCap     = 10
	fetchDefaultMaxBody      = 1 << 20
	fetchDefaultOverall      = 30 * time.Second
)

// validateFetchURL parses and policy-checks one fetch target. Query strings
// are allowed (unlike provider base URLs); userinfo is rejected and fragments
// are stripped (they never reach the wire). The returned URL is canonicalized
// to the exact authority the transport is permitted to dial.
func validateFetchURL(raw string, policy Policy) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || !u.IsAbs() || u.Host == "" {
		return nil, &Error{Code: CodeSSRFBlocked, Op: "validate fetch URL", Err: err}
	}
	// Userinfo can smuggle credentials; fragments never reach the wire and are
	// stripped rather than rejected (web URLs carry them routinely).
	if u.User != nil || u.Opaque != "" {
		return nil, &Error{Code: CodeSSRFBlocked, Op: "validate fetch URL"}
	}
	u.Fragment = ""
	u.RawFragment = ""
	scheme := strings.ToLower(u.Scheme)
	if scheme != "https" && !(scheme == "http" && policy.AllowHTTP) {
		return nil, &Error{Code: CodeSSRFBlocked, Op: "validate fetch URL"}
	}
	host := canonicalHost(u.Hostname())
	if _, err := effectivePort(scheme, u.Port()); err != nil {
		return nil, &Error{Code: CodeSSRFBlocked, Op: "validate fetch URL", Err: err}
	}
	// Canonical authority: bare host for the default port, host:port otherwise.
	// IPv6 literals must keep their brackets or the authority is ambiguous.
	authority := host
	if strings.Contains(host, ":") {
		authority = "[" + host + "]"
	}
	if p := u.Port(); p != "" && p != defaultPort(scheme) {
		authority = net.JoinHostPort(host, p)
	}
	if u.Path == "" {
		u.Path = "/"
	}
	return &url.URL{Scheme: scheme, Host: authority, Path: u.Path, RawPath: u.RawPath, RawQuery: u.RawQuery}, nil
}

func defaultPort(scheme string) string {
	switch scheme {
	case "https":
		return "443"
	case "http":
		return "80"
	}
	return ""
}

// Fetch retrieves rawURL under the egress policy. Redirects are followed one
// hop at a time with full re-validation: each Location is resolved against
// the current URL, re-parsed, re-resolved through DNS and re-checked against
// the IP policy before the next dial, so DNS rebinding across a redirect is
// blocked like a direct request.
func Fetch(ctx context.Context, rawURL string, o FetchOptions) (FetchResult, error) {
	if o.Resolver == nil {
		o.Resolver = SystemResolver{}
	}
	if o.ConnectTimeout <= 0 {
		o.ConnectTimeout = 10 * time.Second
	}
	if o.ResponseHeaderTimeout <= 0 {
		o.ResponseHeaderTimeout = 15 * time.Second
	}
	if o.OverallTimeout <= 0 {
		o.OverallTimeout = fetchDefaultOverall
	}
	if o.MaxRedirects <= 0 {
		o.MaxRedirects = fetchDefaultMaxRedirects
	}
	if o.MaxRedirects > fetchMaxRedirectsCap {
		o.MaxRedirects = fetchMaxRedirectsCap
	}
	if o.MaxBodyBytes <= 0 {
		o.MaxBodyBytes = fetchDefaultMaxBody
	}
	ctx, cancel := context.WithTimeout(ctx, o.OverallTimeout)
	defer cancel()

	current := rawURL
	for hop := 0; ; hop++ {
		u, err := validateFetchURL(current, o.Policy)
		if err != nil {
			return FetchResult{}, err
		}
		ips, err := resolveAllowed(ctx, o.Resolver, u.Hostname(), o.Policy)
		if err != nil {
			return FetchResult{}, err
		}
		tlsConfig, err := secureTLSConfig(o.TLSConfig, u.Hostname())
		if err != nil {
			return FetchResult{}, err
		}
		authority := net.JoinHostPort(canonicalHost(u.Hostname()), effectivePortOrDefault(u))
		dial := o.DialContext
		if dial == nil {
			dialer := &net.Dialer{Timeout: o.ConnectTimeout}
			dial = pinnedDial(dialer, ips, authority, 0)
		}
		tr := &http.Transport{
			Proxy:                 nil,
			TLSClientConfig:       tlsConfig,
			ResponseHeaderTimeout: o.ResponseHeaderTimeout,
			DialContext:           dial,
			DisableKeepAlives:     true,
		}
		client := &http.Client{
			Transport: tr,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
		if err != nil {
			return FetchResult{}, &Error{Code: CodeSSRFBlocked, Op: "build fetch request", Err: err}
		}
		req.Host = ""
		req.Header.Set("User-Agent", "Lunitide/0.3 (local agent evidence fetch)")
		req.Header.Set("Accept", "text/html,application/xhtml+xml,text/plain,application/json;q=0.9,*/*;q=0.5")
		resp, err := client.Do(req)
		if err != nil {
			return FetchResult{}, classifyError("fetch", err)
		}
		if isRedirect(resp.StatusCode) {
			location := resp.Header.Get("Location")
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
			_ = resp.Body.Close()
			next, err := u.Parse(location)
			if err != nil || location == "" {
				return FetchResult{}, &Error{Code: CodeRedirectBlocked, Op: "resolve redirect", Err: err}
			}
			if hop >= o.MaxRedirects {
				return FetchResult{}, &Error{Code: CodeRedirectBlocked, Op: "redirect limit"}
			}
			current = next.String()
			continue
		}
		body, truncated, err := readCapped(resp.Body, o.MaxBodyBytes)
		_ = resp.Body.Close()
		if err != nil {
			return FetchResult{}, classifyError("read fetch body", err)
		}
		return FetchResult{
			FinalURL:    u.String(),
			Status:      resp.StatusCode,
			ContentType: resp.Header.Get("Content-Type"),
			Body:        body,
			Truncated:   truncated,
		}, nil
	}
}

func effectivePortOrDefault(u *url.URL) string {
	if p := u.Port(); p != "" {
		return p
	}
	return defaultPort(strings.ToLower(u.Scheme))
}

func isRedirect(status int) bool {
	switch status {
	case http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther, http.StatusTemporaryRedirect, http.StatusPermanentRedirect:
		return true
	}
	return false
}

// readCapped reads up to limit bytes; a longer body is clipped and reported.
func readCapped(r io.Reader, limit int64) ([]byte, bool, error) {
	body, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, false, err
	}
	if int64(len(body)) > limit {
		return body[:limit], true, nil
	}
	return body, false, nil
}
