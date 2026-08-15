package worker

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// scratch builds a real sandbox root under t.TempDir().
func scratch(t *testing.T) (root string, evidenceDir string) {
	t.Helper()
	base := t.TempDir()
	root = filepath.Join(base, "workers", "w1", "root")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	return root, filepath.Join(base, "workers", "evidence")
}

func profileAt(root string) Profile {
	return Profile{
		WorkerID: "w1",
		Root:     root,
		Mounts:   []Mount{{Source: filepath.Join(filepath.Dir(filepath.Dir(root)), "shared"), Target: "shared", ReadOnly: true}},
		NetAllowlist: []NetTarget{
			{Host: "api.example.com", Port: "443"},
			{Host: "internal.lunitide.local"},
		},
		Quotas: Quota{CPUMillis: 500, MemoryMB: 128, DiskMB: 64, DeadlineMS: 10000},
	}
}

// SBX-001 lexical escapes: ../ components are rejected before any
// filesystem call, on relative and absolute shapes alike.
func TestSandboxEscapeLexicalTraversal(t *testing.T) {
	root, _ := scratch(t)
	g := NewGuard(profileAt(root))
	for _, p := range []string{
		"../outside.txt",
		"sub/../../outside.txt",
		"..\\outside.txt",
		"a/../../../b",
	} {
		if err := g.CheckPath(p); err == nil {
			t.Errorf("want escape denied %q, got allowed", p)
		} else if !isEscape(err, EscapeFile) {
			t.Errorf("want file escape for %q, got %v", p, err)
		}
	}
}

// SBX-001 symlink escape: a link inside the root pointing outside must be
// caught by the real-path pass.
func TestSandboxEscapeSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on windows CI")
	}
	root, _ := scratch(t)
	outside := filepath.Join(filepath.Dir(filepath.Dir(root)), "secret.txt")
	if err := os.WriteFile(outside, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "innocent.txt")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	g := NewGuard(profileAt(root))
	if err := g.CheckPath("innocent.txt"); err == nil {
		t.Fatal("want symlink escape denied")
	} else if !isEscape(err, EscapeFile) {
		t.Fatalf("want file escape, got %v", err)
	}
	// The same check must also fail when the worker passes the absolute
	// link path directly.
	if err := g.CheckPath(link); !isEscape(err, EscapeFile) {
		t.Fatalf("want file escape on absolute link, got %v", err)
	}
}

// SBX-001 absolute paths outside the root are denied; inside they pass.
func TestSandboxEscapeAbsolutePaths(t *testing.T) {
	root, _ := scratch(t)
	g := NewGuard(profileAt(root))
	if err := g.CheckPath(filepath.Join(root, "file.txt")); err != nil {
		t.Fatalf("in-root absolute path must pass, got %v", err)
	}
	if err := g.CheckPath(filepath.Join(filepath.Dir(root), "file.txt")); !isEscape(err, EscapeFile) {
		t.Fatalf("want file escape for sibling path, got %v", err)
	}
	if err := g.CheckPath(`C:\Windows\System32\config.sys`); !isEscape(err, EscapeFile) {
		t.Fatalf("want file escape for system path, got %v", err)
	}
}

// SBX-002 network allowlist: only listed targets dial; metadata endpoints,
// link-local, loopback and unlisted hosts are blocked and audited.
func TestSandboxEscapeNetwork(t *testing.T) {
	root, _ := scratch(t)
	g := NewGuard(profileAt(root))
	if err := g.CheckDial("api.example.com", "443"); err != nil {
		t.Fatalf("listed host must pass, got %v", err)
	}
	if err := g.CheckDial("internal.lunitide.local", "8443"); err != nil {
		t.Fatalf("host-wide entry must pass any port, got %v", err)
	}
	denied := [][2]string{
		{"api.example.com", "80"},
		{"evil.example.net", "443"},
		{"169.254.169.254", "80"},
		{"metadata.google.internal", "80"},
		{"100.100.100.200", "80"},
		{"localhost", "8080"},
		{"127.0.0.1", "8080"},
		{"169.254.170.2", "80"},
	}
	for _, d := range denied {
		if err := g.CheckDial(d[0], d[1]); !isEscape(err, EscapeNetwork) {
			t.Errorf("want network escape for %s:%s, got %v", d[0], d[1], err)
		}
	}
}

// SBX-003 quota ceilings.
func TestSandboxEscapeQuota(t *testing.T) {
	root, _ := scratch(t)
	g := NewGuard(profileAt(root))
	if err := g.CheckQuota(Usage{CPUMillis: 400, MemoryMB: 100, DiskMB: 32, ElapsedMS: 9000}); err != nil {
		t.Fatalf("within quota must pass, got %v", err)
	}
	for _, u := range []Usage{
		{CPUMillis: 501}, {MemoryMB: 129}, {DiskMB: 65}, {ElapsedMS: 10001},
	} {
		if err := g.CheckQuota(u); !isEscape(err, EscapeQuota) {
			t.Errorf("want quota escape for %+v, got %v", u, err)
		}
	}
}

// Every escape freezes an evidence bundle on disk.
func TestSandboxEscapeEvidenceBundle(t *testing.T) {
	root, evidenceDir := scratch(t)
	g := NewGuard(profileAt(root))
	err := g.CheckDial("evil.example.net", "443")
	if !isEscape(err, EscapeNetwork) {
		t.Fatalf("want network escape, got %v", err)
	}
	bundle := filepath.Join(evidenceDir, "w1-network.json")
	raw, readErr := os.ReadFile(bundle)
	if readErr != nil {
		t.Fatalf("evidence bundle missing: %v", readErr)
	}
	if len(raw) == 0 || !containsAll(string(raw), `"kind":"network"`, `"workerId":"w1"`) {
		t.Fatalf("evidence bundle malformed: %s", raw)
	}
}

// Output contract: only the four allowed artifact kinds.
func TestSandboxArtifactKindAllowlist(t *testing.T) {
	root, _ := scratch(t)
	g := NewGuard(profileAt(root))
	for _, k := range []string{"result_manifest", "patch", "test_report", "usage"} {
		if err := g.CheckArtifact(k, "out.json"); err != nil {
			t.Errorf("want artifact %s allowed, got %v", k, err)
		}
	}
	for _, k := range []string{"binary", "shellcode", "raw"} {
		if err := g.CheckArtifact(k, "out.bin"); !isEscape(err, EscapeFile) {
			t.Errorf("want artifact %s denied, got %v", k, err)
		}
	}
	if err := g.CheckArtifact("patch", "../escape"); !isEscape(err, EscapeFile) {
		t.Errorf("want artifact name denied, got %v", err)
	}
}

func isEscape(err error, kind string) bool {
	var e *EscapeError
	return errors.As(err, &e) && e.Kind == kind
}

func containsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		if !contains(s, sub) {
			return false
		}
	}
	return true
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}

// TestSandboxEscapeJunction covers the Windows junction blind spot found by
// the M6 slice-5A stdio POC: a directory junction inside the sandbox root
// pointing at a host directory must be resolved by the real-path pass
// (os.Readlink handles junctions; filepath.EvalSymlinks does not) and
// rejected as an escape.
func TestSandboxEscapeJunction(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("junctions are windows-only")
	}
	base := t.TempDir()
	root := filepath.Join(base, "w1", "root")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	host := filepath.Join(base, "host-secret")
	if err := os.MkdirAll(host, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(host, "secret.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	junction := filepath.Join(root, "escape-dir")
	if out, err := exec.Command("cmd.exe", "/c", "mklink", "/J", junction, host).CombinedOutput(); err != nil {
		t.Skipf("cannot create junction: %v: %s", err, out)
	}
	g := NewGuard(Profile{WorkerID: "w1", Root: root})
	// Through-junction file access must be an escape...
	if err := g.CheckPath(filepath.Join("escape-dir", "secret.txt")); err == nil {
		t.Fatal("junction escape not blocked")
	} else if _, isEscape := err.(*EscapeError); !isEscape {
		t.Fatalf("want EscapeError, got %v", err)
	}
	// ...and the junction dir itself too.
	if err := g.CheckPath("escape-dir"); err == nil {
		t.Fatal("junction dir not blocked")
	}
	// A legitimate in-root path still passes (guard not brain-dead).
	if err := g.CheckPath(filepath.Join("sub", "ok.txt")); err != nil {
		t.Fatalf("in-root path rejected: %v", err)
	}
}
