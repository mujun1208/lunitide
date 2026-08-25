package voice

import (
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"golang.org/x/sys/windows/registry"
)

// Finding the proxy the rest of the machine is already using.
//
// Go resolves proxies from HTTP_PROXY and HTTPS_PROXY. On Windows those are
// almost never set: a proxy is configured once in Internet Options, or by
// whatever client the user runs, and every ordinary application reads it from
// the registry. An installer that only consults the environment therefore
// behaves, on a proxied machine, as though there were no proxy at all — which
// on the network this product's users are actually on means a download that
// crawls or never connects.

// proxyResolver returns a proxy function for an http.Transport.
func proxyResolver() func(*http.Request) (*url.URL, error) {
	return resolveProxy(http.ProxyFromEnvironment, systemProxy)
}

// resolveProxy composes the two sources: the environment wins when it says
// anything, because a user who exported HTTPS_PROXY for this process meant
// it, and Windows is asked otherwise.
//
// Taking both as arguments is what makes the precedence testable. Go resolves
// the environment once per process and caches it, so a test cannot set a
// variable and observe the difference — it has to supply the lookup.
func resolveProxy(
	fromEnv func(*http.Request) (*url.URL, error),
	fromSystem func(*url.URL) (*url.URL, error),
) func(*http.Request) (*url.URL, error) {
	return func(req *http.Request) (*url.URL, error) {
		if proxy, err := fromEnv(req); err == nil && proxy != nil {
			return proxy, nil
		}
		return fromSystem(req.URL)
	}
}

// The registry is read once. Proxy settings do change — a user toggles their
// client mid-session — but re-reading per request would put a registry open
// on every connection, and an install that started on the old setting is
// better finished on it than switched halfway.
var readSystemProxy = sync.OnceValue(loadSystemProxy)

type proxyConfig struct {
	// byScheme maps "http"/"https" to a proxy, from the scheme-qualified
	// form of ProxyServer. A bare host:port lands under both.
	byScheme map[string]*url.URL
	// bypass holds the ProxyOverride entries, lowercased.
	bypass []string
}

// systemProxy resolves the proxy Windows has configured for one target.
func systemProxy(target *url.URL) (*url.URL, error) {
	config := readSystemProxy()
	if config == nil || target == nil {
		return nil, nil
	}
	if config.bypassed(target.Hostname()) {
		return nil, nil
	}
	if proxy, ok := config.byScheme[target.Scheme]; ok {
		return proxy, nil
	}
	return nil, nil
}

// bypassed reports whether ProxyOverride exempts a host.
//
// Windows writes a semicolon-separated list where `*` may appear at either
// end, and a real machine's list looks like
// `localhost;127.*;10.*;192.168.*` — prefix wildcards, not the suffix ones a
// browser-style rule set would suggest. Both directions are handled, along
// with the `<local>` token and a bare leading dot.
//
// An entry that matches none of those shapes matches nothing, which errs
// towards using the proxy. That is the safer direction: a proxy that cannot
// reach a host fails quickly and clearly, while a bypass that should not have
// applied fails as a timeout with no explanation.
func (c *proxyConfig) bypassed(host string) bool {
	host = strings.ToLower(host)
	if host == "" {
		return false
	}
	for _, entry := range c.bypass {
		switch {
		case entry == "<local>":
			// Windows means "a name with no dots in it" by this, not the
			// loopback address.
			if !strings.Contains(host, ".") {
				return true
			}
		case strings.HasPrefix(entry, "*"):
			if strings.HasSuffix(host, strings.TrimPrefix(entry, "*")) {
				return true
			}
		case strings.HasPrefix(entry, "."):
			if strings.HasSuffix(host, entry) {
				return true
			}
		case strings.HasSuffix(entry, "*"):
			if strings.HasPrefix(host, strings.TrimSuffix(entry, "*")) {
				return true
			}
		case entry == host:
			return true
		}
	}
	return false
}

func loadSystemProxy() *proxyConfig {
	key, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Internet Settings`, registry.QUERY_VALUE)
	if err != nil {
		return nil
	}
	defer key.Close()

	if enabled, _, err := key.GetIntegerValue("ProxyEnable"); err != nil || enabled == 0 {
		return nil
	}
	server, _, err := key.GetStringValue("ProxyServer")
	if err != nil || strings.TrimSpace(server) == "" {
		return nil
	}
	config := &proxyConfig{byScheme: parseProxyServer(server)}
	if len(config.byScheme) == 0 {
		return nil
	}
	if override, _, err := key.GetStringValue("ProxyOverride"); err == nil {
		for _, entry := range strings.Split(override, ";") {
			if entry = strings.ToLower(strings.TrimSpace(entry)); entry != "" {
				config.bypass = append(config.bypass, entry)
			}
		}
	}
	return config
}

// parseProxyServer reads the two shapes ProxyServer takes: a bare `host:port`
// meaning every scheme, or a semicolon-separated `scheme=host:port` list.
func parseProxyServer(server string) map[string]*url.URL {
	out := map[string]*url.URL{}
	if !strings.Contains(server, "=") {
		if proxy := proxyURL(strings.TrimSpace(server)); proxy != nil {
			out["http"], out["https"] = proxy, proxy
		}
		return out
	}
	for _, part := range strings.Split(server, ";") {
		scheme, address, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			continue
		}
		// "socks=host:port" appears alongside the others and is not
		// something http.Transport can use for CONNECT, so it is skipped.
		scheme = strings.ToLower(strings.TrimSpace(scheme))
		if scheme != "http" && scheme != "https" {
			continue
		}
		if proxy := proxyURL(strings.TrimSpace(address)); proxy != nil {
			out[scheme] = proxy
		}
	}
	return out
}

// proxyURL turns a registry address into a URL. The registry stores a bare
// host:port; url.Parse needs a scheme, and http is the one a local proxy
// listener speaks even when it is forwarding https.
func proxyURL(address string) *url.URL {
	address = strings.TrimSpace(address)
	if address == "" {
		return nil
	}
	if !strings.Contains(address, "://") {
		address = "http://" + address
	}
	parsed, err := url.Parse(address)
	if err != nil || parsed.Host == "" || !plausibleHost(parsed.Hostname()) {
		return nil
	}
	return parsed
}

// plausibleHost keeps registry rubbish out of the transport.
//
// url.Parse is content to report ";;;" as a host, and a transport handed that
// produces a puzzling dial error on every request instead of one refusal
// here. A proxy address is a hostname or an IP literal; nothing else.
func plausibleHost(host string) bool {
	if host == "" {
		return false
	}
	if net.ParseIP(host) != nil {
		return true
	}
	for _, r := range host {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '.', r == '-', r == '_':
		default:
			return false
		}
	}
	return true
}
