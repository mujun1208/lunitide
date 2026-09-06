package webviewhost

import (
	"errors"
	"net"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
)

var ErrBrowserRuntimeUnsupported = errors.New("isolated WebView2 browser host is unsupported on this platform")

// NormalizeBrowserURL validates an untrusted browser destination and returns a
// canonical URL. Browser documents are HTTPS-only and credentials are never
// accepted in a URL.
func NormalizeBrowserURL(raw string) (string, error) {
	if raw == "" || strings.TrimSpace(raw) != raw || strings.ContainsAny(raw, "\x00\r\n\t") {
		return "", errors.New("browser URL is empty or contains invalid whitespace")
	}
	u, err := url.ParseRequestURI(raw)
	if err != nil || u == nil || !u.IsAbs() || u.Opaque != "" {
		return "", errors.New("browser URL must be an absolute hierarchical URL")
	}
	if !strings.EqualFold(u.Scheme, "https") {
		return "", errors.New("browser URL must use HTTPS")
	}
	if u.User != nil {
		return "", errors.New("browser URL must not contain user information")
	}
	host := u.Hostname()
	if host == "" {
		return "", errors.New("browser URL must contain a host")
	}
	if strings.Contains(host, "%") {
		return "", errors.New("browser URL host must not contain a zone identifier")
	}
	port := u.Port()
	if port != "" {
		if _, err := net.LookupPort("tcp", port); err != nil {
			return "", errors.New("browser URL contains an invalid port")
		}
	}
	host = strings.ToLower(host)
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	if port != "" && port != "443" {
		host += ":" + port
	}
	u.Scheme = "https"
	u.Host = host
	return u.String(), nil
}

func BrowserNavigationAllowed(raw string) bool {
	_, err := NormalizeBrowserURL(raw)
	return err == nil
}

// ValidateIsolatedBrowserProfile prevents the isolated browser from sharing a
// WebView2 user-data folder with the main renderer host.
func ValidateIsolatedBrowserProfile(browserProfile, mainProfile string) (string, error) {
	if browserProfile == "" || !filepath.IsAbs(browserProfile) {
		return "", errors.New("an absolute isolated browser profile is required")
	}
	browser := filepath.Clean(browserProfile)
	if mainProfile != "" {
		if !filepath.IsAbs(mainProfile) {
			return "", errors.New("main WebView2 profile must be absolute when provided")
		}
		main := filepath.Clean(mainProfile)
		same := browser == main
		if filepath.Separator == '\\' {
			same = strings.EqualFold(browser, main)
		}
		if same {
			return "", errors.New("isolated browser profile must differ from the main WebView2 profile")
		}
	}
	return browser, nil
}

// IsolatedBrowserArguments accepts only an owned numeric loopback proxy. No
// shell or arbitrary browser arguments may cross this boundary.
func IsolatedBrowserArguments(raw string) (string, error) {
	if raw == "" {
		return "", nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "http" || u.Hostname() != "127.0.0.1" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" || strings.ContainsAny(raw, " \t\r\n\x00") {
		return "", errors.New("isolated browser proxy must be an owned loopback endpoint")
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil || port < 1 || port > 65535 {
		return "", errors.New("isolated browser proxy port is invalid")
	}
	return "--enable-automation --proxy-server=" + raw + " --proxy-bypass-list=<-loopback> --disable-quic --force-webrtc-ip-handling-policy=disable_non_proxied_udp", nil
}

func browserArgumentsEnforced(actual []string, proxyURL string) bool {
	expected, err := IsolatedBrowserArguments(proxyURL)
	if err != nil || expected == "" {
		return false
	}
	switches := map[string]string{}
	for _, arg := range actual {
		key, value, _ := strings.Cut(arg, "=")
		switches[key] = value
	}
	for _, arg := range strings.Fields(expected) {
		key, value, _ := strings.Cut(arg, "=")
		got, ok := switches[key]
		if !ok || got != value {
			return false
		}
	}
	return true
}
