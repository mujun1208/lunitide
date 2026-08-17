// Package networkpolicy provides an SSRF-resistant HTTP upstream connector.
package networkpolicy

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"path"
	"strings"
)

type Resolver interface {
	LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error)
}

type SystemResolver struct{ Resolver *net.Resolver }

func (r SystemResolver) LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error) {
	if r.Resolver == nil {
		r.Resolver = net.DefaultResolver
	}
	return r.Resolver.LookupNetIP(ctx, network, host)
}

type Policy struct {
	AllowHTTP      bool
	AllowLocalhost bool
}

var forbiddenPrefixes = mustPrefixes(
	"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16",
	"172.16.0.0/12", "192.0.0.0/24", "192.0.2.0/24", "192.168.0.0/16", "198.18.0.0/15",
	"192.88.99.0/24", "198.51.100.0/24", "203.0.113.0/24", "224.0.0.0/4", "240.0.0.0/4",
	"::/128", "::1/128", "64:ff9b:1::/48", "100::/64", "2001::/23", "2001:db8::/32",
	"2002::/16", "3fff::/20", "5f00::/16",
	"fc00::/7", "fe80::/10", "ff00::/8",
)

func mustPrefixes(values ...string) []netip.Prefix {
	r := make([]netip.Prefix, len(values))
	for i, s := range values {
		r[i] = netip.MustParsePrefix(s)
	}
	return r
}

func allowedIP(ip netip.Addr) bool {
	if !ip.IsValid() || ip.Is4In6() || ip.IsUnspecified() || ip.IsLoopback() || ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() || !ip.IsGlobalUnicast() {
		return false
	}
	for _, p := range forbiddenPrefixes {
		if p.Contains(ip) {
			return false
		}
	}
	return true
}

func validateAndJoin(rawBase, apiPath string, policy Policy) (*url.URL, error) {
	u, err := url.Parse(rawBase)
	if err != nil || !u.IsAbs() || u.Host == "" {
		return nil, &Error{Code: CodeSSRFBlocked, Op: "validate endpoint", Err: err}
	}
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
		return nil, &Error{Code: CodeSSRFBlocked, Op: "validate endpoint"}
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && policy.AllowHTTP) {
		return nil, &Error{Code: CodeSSRFBlocked, Op: "validate endpoint"}
	}
	p, err := url.Parse(apiPath)
	if err != nil || p.IsAbs() || p.Host != "" || p.User != nil || p.RawQuery != "" || p.Fragment != "" || p.Opaque != "" {
		return nil, &Error{Code: CodeSSRFBlocked, Op: "join API path", Err: err}
	}
	// Treat API path as a path, never as a URL reference capable of replacing authority.
	u.Path = path.Join("/", u.Path, p.Path)
	u.RawPath = ""
	return u, nil
}

func resolveAllowed(ctx context.Context, resolver Resolver, host string, policy Policy) ([]netip.Addr, error) {
	if !policy.AllowLocalhost {
		if strings.EqualFold(strings.TrimSuffix(host, "."), "localhost") {
			return nil, &Error{Code: CodeSSRFBlocked, Op: "resolve host"}
		}
	}
	if literal, err := netip.ParseAddr(host); err == nil {
		if !policy.AllowLocalhost && !allowedIP(literal) {
			return nil, &Error{Code: CodeSSRFBlocked, Op: "resolve host"}
		}
		return []netip.Addr{literal}, nil
	}
	ips, err := resolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		if ctx.Err() != nil {
			return nil, classifyError("resolve host", ctx.Err())
		}
		return nil, &Error{Code: CodeDNSError, Op: "resolve host", Err: err}
	}
	if len(ips) == 0 {
		return nil, &Error{Code: CodeDNSError, Op: "resolve host", Err: fmt.Errorf("no addresses")}
	}
	for _, ip := range ips { // Mixed answers fail closed.
		if !policy.AllowLocalhost && !allowedIP(ip) {
			return nil, &Error{Code: CodeSSRFBlocked, Op: "resolve host"}
		}
	}
	return append([]netip.Addr(nil), ips...), nil
}
