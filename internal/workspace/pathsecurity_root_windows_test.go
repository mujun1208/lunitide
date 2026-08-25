//go:build windows

package workspace_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/lunitide/lunitide/internal/workspace"
)

// A workspace reached through a junction is an ordinary setup, not an
// attack: OneDrive redirects Documents that way, and folder redirection does
// it on managed machines. The handle layer verifies the leaf by asking the
// operating system for its real name, so unless the root is put in that same
// spelling every file inside such a workspace fails containment and the
// whole workspace reads as empty.
//
// The refusal that matters is a reparse point *below* the root, which
// TestJunctionLeafRefusedUnderJunctionRoot pins alongside this.
func TestJunctionRootStaysReadable(t *testing.T) {
	base := t.TempDir()
	realRoot := filepath.Join(base, "real")
	linkedRoot := filepath.Join(base, "linked")
	if err := os.MkdirAll(realRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("cmd.exe", "/d", "/s", "/c", "mklink", "/J", linkedRoot, realRoot).CombinedOutput(); err != nil {
		t.Skipf("directory junction unavailable: %v: %s", err, out)
	}

	root, err := workspace.NewSecureRoot(linkedRoot)
	if err != nil {
		t.Fatal(err)
	}
	if err := root.WriteAtomic("dir/file.txt", []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := root.OpenSecure("dir/file.txt")
	if err != nil {
		t.Fatalf("file written through a junctioned root must read back: %v", err)
	}
	defer f.Close()
	buf := make([]byte, 8)
	n, _ := f.Read(buf)
	if string(buf[:n]) != "v1" {
		t.Fatalf("read %q, want v1", buf[:n])
	}
	if _, err := root.StatSecure("dir/file.txt"); err != nil {
		t.Fatalf("stat through a junctioned root must succeed: %v", err)
	}
}

// Following the junction that leads *to* the workspace must not soften the
// rule about junctions found inside it.
func TestJunctionLeafRefusedUnderJunctionRoot(t *testing.T) {
	base := t.TempDir()
	realRoot := filepath.Join(base, "real")
	linkedRoot := filepath.Join(base, "linked")
	outside := filepath.Join(base, "outside")
	if err := os.MkdirAll(realRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	mklink := func(link, target string) bool {
		out, err := exec.Command("cmd.exe", "/d", "/s", "/c", "mklink", "/J", link, target).CombinedOutput()
		if err != nil {
			t.Skipf("directory junction unavailable: %v: %s", err, out)
		}
		return true
	}
	mklink(linkedRoot, realRoot)
	mklink(filepath.Join(realRoot, "escape"), outside)

	root, err := workspace.NewSecureRoot(linkedRoot)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := root.OpenSecure("escape/secret.txt"); !errors.Is(err, workspace.ErrPathEscape) {
		t.Fatalf("junction below the root must be refused WS-002, got %v", err)
	}
}
