package workspace_test

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/lunitide/lunitide/internal/workspace"
)

// T-5.2.1 corpus: every entry must be refused by the lexical layer.
func TestPathEscapeLexicalCorpus(t *testing.T) {
	cases := []string{
		// traversal
		"..", "../escape", "a/../../escape", "a\\..\\..\\escape", "...", "....",
		// absolute / UNC / device
		"/etc/passwd", `\windows\system32`, `\\server\share\file`, `\\?\C:\x`, `\\.\PhysicalDisk0`, `\\.\COM1`,
		// ADS and drive separators
		"file.txt:stream", "file.txt:$DATA", "a/b:c", "C:file", "C:\\abs",
		// reserved device names (stem, with extension, mixed case)
		"CON", "con", "NUL.txt", "PRN.log", "AUX.ini", "COM1", "com9.dat", "LPT1.out", "lpt7",
		// empty / dot components and trailing dot/space mangling
		"a//b", "a/./b", "a/.", "name.", "name..", "name ", "dir /name",
		// control bytes and oversize
		"a\x00b", string(make([]byte, 513)),
	}
	root, err := workspace.NewSecureRoot(filepath.Join(t.TempDir(), "ws"))
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		if _, err := root.Resolve(c); !errors.Is(err, workspace.ErrPathEscape) {
			t.Errorf("corpus %q must be refused WS-002, got %v", c, err)
		}
	}
}

func TestPathEscapeValidPathsResolve(t *testing.T) {
	dir := t.TempDir()
	root, err := workspace.NewSecureRoot(filepath.Join(dir, "ws"))
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []string{"README.md", "src/main.go", "docs/a.b.c.txt", ".gitignore", "a/b/c/d/e.txt", "UPPER.TXT"} {
		full, err := root.Resolve(c)
		if err != nil {
			t.Fatalf("valid path %q refused: %v", c, err)
		}
		if !filepath.IsAbs(full) {
			t.Fatalf("resolved path must stay absolute: %q", full)
		}
	}
	// Root itself must refuse UNC and relative roots.
	if _, err := workspace.NewSecureRoot(`\\server\share`); !errors.Is(err, workspace.ErrPathEscape) {
		t.Fatalf("UNC root must be refused, got %v", err)
	}
	if _, err := workspace.NewSecureRoot("relative/root"); !errors.Is(err, workspace.ErrPathEscape) {
		t.Fatalf("relative root must be refused, got %v", err)
	}
}

// TestPathEscapeSymlinkAndOutsideBytes verifies the handle layer: a symlink
// (or, without symlink privilege on Windows, a junction) pointing outside
// the root is refused at Open time and the outside bytes never change.
func TestPathEscapeSymlinkAndOutsideBytes(t *testing.T) {
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.txt")
	secretBody := []byte("DO-NOT-TOUCH")
	if err := os.WriteFile(secret, secretBody, 0o644); err != nil {
		t.Fatal(err)
	}
	ws := filepath.Join(t.TempDir(), "ws")
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatal(err)
	}
	root, err := workspace.NewSecureRoot(ws)
	if err != nil {
		t.Fatal(err)
	}
	var viaDir bool
	if err := os.Symlink(secret, filepath.Join(ws, "leak")); err != nil {
		// Windows symlink needs a privilege; fall back to a junction escape
		// (junction creation is unprivileged) and attack through a parent.
		junction := filepath.Join(ws, "leakdir")
		cmd := exec.Command("cmd", "/c", "mklink", "/J", junction, outside)
		if out, mkErr := cmd.CombinedOutput(); mkErr != nil {
			t.Skipf("symlink/junction unsupported: %v / %s", err, out)
		}
		viaDir = true
	}
	rel := "leak"
	if viaDir {
		rel = "leakdir/secret.txt"
	}
	if _, err := root.OpenSecure(rel); !errors.Is(err, workspace.ErrPathEscape) {
		t.Fatalf("%s must be refused WS-002, got %v", rel, err)
	}
	if viaDir {
		// The junction leaf itself must also be refused.
		if _, err := root.OpenSecure("leakdir"); !errors.Is(err, workspace.ErrPathEscape) {
			t.Fatalf("junction leaf must be refused WS-002, got %v", err)
		}
	}
	got, err := os.ReadFile(secret)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, secretBody) {
		t.Fatal("outside bytes changed during refused access")
	}
}

func TestWriteAtomicRoundtrip(t *testing.T) {
	ws := filepath.Join(t.TempDir(), "ws")
	root, err := workspace.NewSecureRoot(ws)
	if err != nil {
		t.Fatal(err)
	}
	if err := root.WriteAtomic("dir/file.txt", []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := root.WriteAtomic("dir/file.txt", []byte("v2"), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := root.OpenSecure("dir/file.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	buf := make([]byte, 8)
	n, _ := f.Read(buf)
	if string(buf[:n]) != "v2" {
		t.Fatalf("atomic replace must observe v2, got %q", buf[:n])
	}
	// Refused write path leaves no bytes anywhere outside.
	if err := root.WriteAtomic("../escape.txt", []byte("x"), 0o644); !errors.Is(err, workspace.ErrPathEscape) {
		t.Fatalf("escaping write must be refused WS-002, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(ws), "escape.txt")); !os.IsNotExist(err) {
		t.Fatal("escaping write must not create files")
	}
}
