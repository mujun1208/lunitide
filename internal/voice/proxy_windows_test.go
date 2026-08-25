package voice

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestParseProxyServerBareAddressCoversBothSchemes(t *testing.T) {
	// The shape a local proxy client writes: one listener for everything.
	got := parseProxyServer("127.0.0.1:7890")
	for _, scheme := range []string{"http", "https"} {
		proxy, ok := got[scheme]
		if !ok {
			t.Fatalf("%s has no proxy", scheme)
		}
		if proxy.Host != "127.0.0.1:7890" || proxy.Scheme != "http" {
			t.Errorf("%s proxy = %s; want http://127.0.0.1:7890", scheme, proxy)
		}
	}
}

func TestParseProxyServerSchemeQualifiedList(t *testing.T) {
	got := parseProxyServer("http=proxy-a:8080;https=proxy-b:8443;ftp=proxy-c:21;socks=proxy-d:1080")
	if got["http"] == nil || got["http"].Host != "proxy-a:8080" {
		t.Errorf("http proxy = %v; want proxy-a:8080", got["http"])
	}
	if got["https"] == nil || got["https"].Host != "proxy-b:8443" {
		t.Errorf("https proxy = %v; want proxy-b:8443", got["https"])
	}
	// ftp is not something this client speaks, and socks cannot be used for
	// the CONNECT that an http.Transport issues.
	if _, ok := got["ftp"]; ok {
		t.Error("ftp proxy should be ignored")
	}
	if _, ok := got["socks"]; ok {
		t.Error("socks proxy should be ignored")
	}
}

func TestParseProxyServerIgnoresRubbish(t *testing.T) {
	// url.Parse will happily report ";;;" as a host. Handing that to a
	// transport turns one bad registry value into a confusing dial failure
	// on every request.
	for _, input := range []string{"", "   ", ";;;", "http=", "=1.2.3.4:80", "http://[::", "a b c"} {
		if got := parseProxyServer(input); len(got) != 0 {
			t.Errorf("parseProxyServer(%q) = %v; want nothing usable", input, got)
		}
	}
}

func TestPlausibleHost(t *testing.T) {
	for host, want := range map[string]bool{
		"127.0.0.1":   true,
		"::1":         true,
		"proxy.local": true,
		"my-proxy_01": true,
		";;;":         false,
		"":            false,
		"a b":         false,
		"host/path":   false,
	} {
		if got := plausibleHost(host); got != want {
			t.Errorf("plausibleHost(%q) = %v; want %v", host, got, want)
		}
	}
}

func TestProxyBypassHandlesARealOverrideList(t *testing.T) {
	// Copied from an actual machine. Note the shape: prefix wildcards, which
	// an implementation written from a browser's suffix rules would miss.
	const override = "localhost;127.*;10.*;172.16.*;192.168.*"
	config := &proxyConfig{}
	for _, entry := range strings.Split(override, ";") {
		config.bypass = append(config.bypass, entry)
	}
	for host, want := range map[string]bool{
		"localhost":      true,
		"127.0.0.1":      true,
		"10.2.3.4":       true,
		"172.16.9.9":     true,
		"192.168.1.10":   true,
		"huggingface.co": false, // the case that must keep the proxy
		"github.com":     false,
		"192.169.1.10":   false, // one digit off, not in the private range
	} {
		if got := config.bypassed(host); got != want {
			t.Errorf("bypassed(%q) = %v; want %v", host, got, want)
		}
	}
}

func TestProxyBypassHandlesTheOtherWildcardShapes(t *testing.T) {
	config := &proxyConfig{bypass: []string{"<local>", "*.internal.example", ".corp.example", "exact.example"}}
	for host, want := range map[string]bool{
		"intranet":                true,  // no dots, matched by <local>
		"api.internal.example":    true,  // *. prefix wildcard
		"deep.a.internal.example": true,  // suffix match is not depth limited
		"files.corp.example":      true,  // bare leading dot
		"exact.example":           true,  // literal
		"notexact.example":        false, // must not match by accident
		"huggingface.co":          false,
	} {
		if got := config.bypassed(host); got != want {
			t.Errorf("bypassed(%q) = %v; want %v", host, got, want)
		}
	}
	if (&proxyConfig{}).bypassed("") {
		t.Error("an empty host should not be treated as bypassed")
	}
}

func TestResolveProxyPrefersTheEnvironment(t *testing.T) {
	// Go resolves the environment once per process and caches it, so this
	// cannot be driven with t.Setenv; the two sources are supplied instead.
	fromEnv := staticEnvProxy("http://from-env:3128")
	fromSystem := staticSystemProxy("http://from-registry:15715")

	req, err := http.NewRequest(http.MethodGet, "https://huggingface.co/x", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	proxy, err := resolveProxy(fromEnv, fromSystem)(req)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if proxy == nil || proxy.Host != "from-env:3128" {
		t.Fatalf("proxy = %v; want the environment's from-env:3128", proxy)
	}
}

func TestResolveProxyFallsBackToWindowsWhenTheEnvironmentIsSilent(t *testing.T) {
	// The configuration this was written for: nothing exported, a proxy
	// configured in Internet Options. The default transport would go direct.
	fromEnv := staticEnvProxy("")
	fromSystem := staticSystemProxy("http://127.0.0.1:15715")

	req, err := http.NewRequest(http.MethodGet, "https://huggingface.co/x", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	proxy, err := resolveProxy(fromEnv, fromSystem)(req)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if proxy == nil || proxy.Host != "127.0.0.1:15715" {
		t.Fatalf("proxy = %v; want the registry's 127.0.0.1:15715", proxy)
	}
}

func staticEnvProxy(raw string) func(*http.Request) (*url.URL, error) {
	return func(*http.Request) (*url.URL, error) {
		if raw == "" {
			return nil, nil
		}
		return url.Parse(raw)
	}
}

func staticSystemProxy(raw string) func(*url.URL) (*url.URL, error) {
	return func(*url.URL) (*url.URL, error) {
		if raw == "" {
			return nil, nil
		}
		return url.Parse(raw)
	}
}

func TestSystemProxyIsSkippedForBypassedHosts(t *testing.T) {
	direct := &url.URL{Scheme: "http", Host: "127.0.0.1:7890"}
	config := &proxyConfig{
		byScheme: map[string]*url.URL{"http": direct, "https": direct},
		bypass:   []string{".corp.example"},
	}
	if got := resolveWith(config, "https://files.corp.example/a"); got != nil {
		t.Errorf("a bypassed host resolved to %v; want direct", got)
	}
	if got := resolveWith(config, "https://huggingface.co/a"); got == nil || got.Host != "127.0.0.1:7890" {
		t.Errorf("a proxied host resolved to %v; want 127.0.0.1:7890", got)
	}
	if got := resolveWith(config, "ftp://files.example/a"); got != nil {
		t.Errorf("an unproxied scheme resolved to %v; want direct", got)
	}
}

// resolveWith exercises the lookup against a config the test supplies rather
// than the machine's registry, which a test must not depend on or change.
func resolveWith(config *proxyConfig, raw string) *url.URL {
	target, err := url.Parse(raw)
	if err != nil {
		return nil
	}
	if config.bypassed(target.Hostname()) {
		return nil
	}
	return config.byScheme[target.Scheme]
}
