// Package browser implements the M5 restricted browsing policy layer
// (T-5.4.x). Browsing runs in a dedicated low-privilege process: on Windows
// the host layer is responsible for actually spawning the confined browser
// process (low-integrity token / AppContainer); this package provides the
// policy gate every navigation must pass plus the process orchestration
// surface the host drives. Each session gets its own throwaway profile
// directory — created on open, wiped on close — so no cookies, cache or
// credentials survive the session.
package browser

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
)

// Wire error family BRW-001: every refusal below is a hard block, never a
// warning.
var (
	// ErrProtocolBlocked: scheme outside the http/https allowlist
	// (file:, javascript:, data:, ftp:, ws:, ... are all refused) or a
	// shape that is not a proper URL at all (UNC paths, no scheme).
	ErrProtocolBlocked = errors.New("browser: protocol blocked (BRW-001)")
	// ErrLoopbackBlocked: loopback target (127.0.0.0/8, ::1, localhost).
	ErrLoopbackBlocked = errors.New("browser: loopback address blocked (BRW-001)")
	// ErrPrivateAddress: RFC1918 / link-local / ULA / multicast / reserved.
	ErrPrivateAddress = errors.New("browser: private or reserved address blocked (BRW-001)")
	// ErrDownloadBlocked: downloads are never allowed; the product is
	// read-only browsing.
	ErrDownloadBlocked = errors.New("browser: downloads blocked (BRW-001)")
	// ErrProfileRequired: NewHost needs a profile directory.
	ErrProfileRequired = errors.New("browser: profile directory is required")
)

// allowedScheme is the protocol allowlist; anything else (file, javascript,
// data, ftp, ws, ...) answers ErrProtocolBlocked.
func allowedScheme(s string) bool { return s == "http" || s == "https" }

// reservedNets are non-routable / special-use ranges the net.IP built-ins
// do not cover: CGNAT shared space, TEST-NET blocks, benchmarking, the
// "this" network beyond plain 0.0.0.0, and class E.
var reservedNets = func() []net.IPNet {
	cidrs := []string{
		"0.0.0.0/8",       // "this" network
		"100.64.0.0/10",   // CGNAT shared address space
		"192.0.0.0/24",    // IETF protocol assignments
		"192.0.2.0/24",    // TEST-NET-1
		"198.18.0.0/15",   // benchmarking
		"198.51.100.0/24", // TEST-NET-2
		"203.0.113.0/24",  // TEST-NET-3
		"240.0.0.0/4",     // reserved (incl. broadcast)
	}
	out := make([]net.IPNet, 0, len(cidrs))
	for _, c := range cidrs {
		_, n, err := net.ParseCIDR(c)
		if err != nil {
			panic("browser: invalid reserved cidr " + c)
		}
		out = append(out, *n)
	}
	return out
}()

// classifyIP is the single address classifier shared by CheckURL (T-5.4.1),
// ResolveAndCheck, CheckRedirectPolicy and GuardedTransport (T-5.4.2).
// Loopback answers ErrLoopbackBlocked; every other non-public shape
// (RFC1918, link-local, ULA, multicast, reserved) answers ErrPrivateAddress.
func classifyIP(ip net.IP) error {
	if ip.IsLoopback() {
		return fmt.Errorf("%w: %s", ErrLoopbackBlocked, ip)
	}
	if ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() || ip.IsUnspecified() {
		return fmt.Errorf("%w: %s", ErrPrivateAddress, ip)
	}
	for _, n := range reservedNets {
		if n.Contains(ip) {
			return fmt.Errorf("%w: %s", ErrPrivateAddress, ip)
		}
	}
	return nil
}

// checkHostLiteral classifies a URL host when it is already a literal:
// "localhost" by name, anything parseable as an IP by address. Hostnames
// that need DNS are resolved (and re-classified) by the netcheck layer
// (BRW-002). Non-standard ports are fine: the port never changes the
// classification of the host beside it.
func checkHostLiteral(host string) error {
	if strings.EqualFold(host, "localhost") {
		return fmt.Errorf("%w: localhost", ErrLoopbackBlocked)
	}
	if ip := net.ParseIP(host); ip != nil {
		return classifyIP(ip)
	}
	return nil
}

// CheckURL is the policy gate every navigation (initial load and each
// redirect hop) must pass: http/https only, and a literal host must not be
// loopback/private/reserved. Domain hosts additionally go through the DNS
// gate in netcheck (ResolveAndCheck) before the driver or transport sees
// them.
func CheckURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" {
		// Not a scheme-bearing URL (UNC paths land here too): treat as a
		// protocol refusal so the corpus stays on one sentinel family.
		return fmt.Errorf("%w: %q", ErrProtocolBlocked, raw)
	}
	if !allowedScheme(strings.ToLower(u.Scheme)) {
		return fmt.Errorf("%w: %s", ErrProtocolBlocked, u.Scheme)
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("%w: no host in %q", ErrProtocolBlocked, raw)
	}
	return checkHostLiteral(host)
}

// AllowDownload answers BRW-001 for any download intent: downloads are
// never executed or written to disk — the product is read-only browsing.
func AllowDownload() error { return ErrDownloadBlocked }

// Host pins one restricted browsing session: a throwaway profile directory
// created on open and removed on Close.
type Host struct{ profileDir string }

// NewHost creates the session profile directory (MkdirAll, so callers may
// pass a nested per-session path under the run workspace).
func NewHost(profileDir string) (*Host, error) {
	if strings.TrimSpace(profileDir) == "" {
		return nil, ErrProfileRequired
	}
	if err := os.MkdirAll(profileDir, 0o700); err != nil {
		return nil, fmt.Errorf("browser: create profile dir: %w", err)
	}
	return &Host{profileDir: profileDir}, nil
}

// TempProfile returns the per-session profile directory handed to the
// browser process.
func (h *Host) TempProfile() string { return h.profileDir }

// Close wipes the session profile (cookies, cache, credentials) with
// os.RemoveAll.
func (h *Host) Close() error {
	if h == nil || h.profileDir == "" {
		return nil
	}
	return os.RemoveAll(h.profileDir)
}

// LowPrivilegeFlags returns the policy flag hints for spawning the browser
// child process. On Windows the actual token work (low-integrity level /
// AppContainer) happens in the host cmd layer; this list is the strategic
// command-line surface the host appends when assembling argv.
func LowPrivilegeFlags() ([]string, error) {
	return []string{
		"--no-first-run",
		"--no-default-browser-check",
		"--disable-sync",
		"--disable-extensions",
		"--disable-component-update",
		"--disable-background-networking",
		"--mute-audio",
	}, nil
}
