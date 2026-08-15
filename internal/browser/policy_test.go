package browser

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestBrowserPolicy drives the T-5.4.1 URL corpus: public http/https passes
// (non-standard ports included), and every local / private / UNC /
// non-allowlisted protocol shape is refused with a distinguishable
// sentinel (ErrProtocolBlocked vs ErrPrivateAddress vs ErrLoopbackBlocked).
func TestBrowserPolicy(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want error
	}{
		{"https public", "https://example.com", nil},
		{"http custom port", "http://example.com:8080", nil},
		{"file local disk", "file:///C:/Windows/system32/config.sys", ErrProtocolBlocked},
		{"file admin share", "file://localhost/c$", ErrProtocolBlocked},
		{"unc loopback share", `\\127.0.0.1\c$`, ErrProtocolBlocked},
		{"loopback v4", "http://127.0.0.1", ErrLoopbackBlocked},
		{"loopback v4 high octet", "http://127.255.0.1", ErrLoopbackBlocked},
		{"localhost with port", "http://localhost:8080", ErrLoopbackBlocked},
		{"rfc1918 192.168", "https://192.168.1.1", ErrPrivateAddress},
		{"rfc1918 10", "http://10.0.0.1", ErrPrivateAddress},
		{"rfc1918 172.16", "http://172.16.0.1", ErrPrivateAddress},
		{"cgnat", "http://100.64.0.1", ErrPrivateAddress},
		{"testnet1", "http://192.0.2.1", ErrPrivateAddress},
		{"this network", "http://0.0.0.1", ErrPrivateAddress},
		{"link local v4", "http://169.254.1.1", ErrPrivateAddress},
		{"loopback v6", "http://[::1]/", ErrLoopbackBlocked},
		{"link local v6", "http://[fe80::1]/", ErrPrivateAddress},
		{"ula v6", "http://[fc00::1]/", ErrPrivateAddress},
		{"javascript", "javascript:alert(1)", ErrProtocolBlocked},
		{"data uri", "data:text/html;base64,PHNjcmlwdD5hbGVydCgxKTwvc2NyaXB0Pg==", ErrProtocolBlocked},
		{"ftp", "ftp://example.com", ErrProtocolBlocked},
		{"websocket", "ws://example.com", ErrProtocolBlocked},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := CheckURL(tc.raw)
			if tc.want == nil {
				if err != nil {
					t.Fatalf("CheckURL(%q) = %v, want accepted", tc.raw, err)
				}
				return
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("CheckURL(%q) = %v, want %v", tc.raw, err, tc.want)
			}
		})
	}
}

// TestAllowDownloadBlocked: BRW-001 — any download intent is refused.
func TestAllowDownloadBlocked(t *testing.T) {
	if err := AllowDownload(); !errors.Is(err, ErrDownloadBlocked) {
		t.Fatalf("AllowDownload() = %v, want %v", err, ErrDownloadBlocked)
	}
}

// TestHostProfileLifecycle: the per-session profile directory is created by
// NewHost and wiped by Close.
func TestHostProfileLifecycle(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "profile-a")
	h, err := NewHost(dir)
	if err != nil {
		t.Fatal(err)
	}
	if fi, statErr := os.Stat(h.TempProfile()); statErr != nil || !fi.IsDir() {
		t.Fatalf("profile dir missing after NewHost: %v", statErr)
	}
	if err := h.Close(); err != nil {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(dir); !os.IsNotExist(statErr) {
		t.Fatalf("profile dir survived Close: %v", statErr)
	}
	if _, err := NewHost(""); !errors.Is(err, ErrProfileRequired) {
		t.Fatalf(`NewHost("") = %v, want ErrProfileRequired`, err)
	}
}

// TestLowPrivilegeFlags sanity: the host layer gets a non-empty hint list.
func TestLowPrivilegeFlags(t *testing.T) {
	flags, err := LowPrivilegeFlags()
	if err != nil {
		t.Fatal(err)
	}
	if len(flags) == 0 {
		t.Fatal("LowPrivilegeFlags returned no flags")
	}
}
