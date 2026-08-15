// T-5.4.2 DNS rebinding defence (BRW-002). The URL policy in host.go only
// sees literals: a public hostname can still resolve to — or be re-pointed
// at — a loopback or private address between the check and the connect.
// This file adds the resolve-and-classify gate reused at three points:
// navigate, every redirect hop, and immediately before dialing
// (GuardedTransport).
package browser

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
)

// ErrTooManyRedirects caps redirect chains (BRW-002).
var ErrTooManyRedirects = errors.New("browser: too many redirects (BRW-002)")

// MaxRedirects is the hard hop budget for one navigation.
const MaxRedirects = 5

// LookupFunc resolves a hostname to addresses; swappable in tests.
type LookupFunc func(host string) ([]net.IP, error)

// defaultLookup is the production resolver (net.LookupIP); tests replace
// it temporarily and restore it with t.Cleanup.
var defaultLookup LookupFunc = net.LookupIP

// ClassifyIP exposes the shared address classifier (the same rules
// CheckURL applies to literals): loopback -> ErrLoopbackBlocked, every
// other non-public shape -> ErrPrivateAddress.
func ClassifyIP(ip net.IP) error { return classifyIP(ip) }

// ResolveAndCheck resolves host (IP literals are classified directly, no
// DNS) and refuses when ANY resolved address is non-public: a rebinding
// answer that hides one loopback/private IP behind a public one still gets
// the whole lookup refused — zero escapes (BRW-002). On success it returns
// the verified address list.
func ResolveAndCheck(host string) ([]net.IP, error) {
	if strings.EqualFold(host, "localhost") {
		return nil, fmt.Errorf("%w: localhost", ErrLoopbackBlocked)
	}
	if ip := net.ParseIP(host); ip != nil {
		if err := ClassifyIP(ip); err != nil {
			return nil, err
		}
		return []net.IP{ip}, nil
	}
	ips, err := defaultLookup(host)
	if err != nil {
		return nil, fmt.Errorf("browser: resolve %s: %w", host, err)
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("browser: %s resolved to no addresses", host)
	}
	for _, ip := range ips {
		if err := ClassifyIP(ip); err != nil {
			return nil, fmt.Errorf("%w (host %s)", err, host)
		}
	}
	return ips, nil
}

// CheckRedirectPolicy is the http.Client CheckRedirect hook: every hop
// re-runs the full URL policy plus a fresh resolve-and-classify, so a
// public origin answering 302 -> 127.0.0.1 is refused here. Chains longer
// than MaxRedirects answer ErrTooManyRedirects.
func CheckRedirectPolicy(req *http.Request, via []*http.Request) error {
	if len(via) >= MaxRedirects {
		return fmt.Errorf("%w: %d hops", ErrTooManyRedirects, len(via))
	}
	if err := CheckURL(req.URL.String()); err != nil {
		return err
	}
	if _, err := ResolveAndCheck(req.URL.Hostname()); err != nil {
		return err
	}
	return nil
}

// resolvedIPsKey is the context key GuardedTransport uses to stash the
// verified addresses.
type resolvedIPsKey struct{}

// ResolvedIPsFromRequest returns the address list GuardedTransport verified
// for the request (a pinning hint for the dial layer).
func ResolvedIPsFromRequest(ctx context.Context) []net.IP {
	ips, _ := ctx.Value(resolvedIPsKey{}).([]net.IP)
	return ips
}

// GuardedTransport re-checks the destination right before the bytes leave:
// RoundTrip resolves the request host again and refuses non-public
// answers. This closes the TOCTOU window where DNS is switched between the
// redirect check and the real connect (classic rebinding). The verified IP
// list is attached to the request context so a pinning dialer can connect
// to one of the checked addresses instead of re-resolving.
type GuardedTransport struct {
	Base http.RoundTripper
}

func (t *GuardedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	ips, err := ResolveAndCheck(req.URL.Hostname())
	if err != nil {
		return nil, err
	}
	return t.base().RoundTrip(req.WithContext(context.WithValue(req.Context(), resolvedIPsKey{}, ips)))
}

func (t *GuardedTransport) base() http.RoundTripper {
	if t.Base != nil {
		return t.Base
	}
	return http.DefaultTransport
}
