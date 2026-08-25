//go:build windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// linkDirectory creates a directory reparse point, which is what these tests
// are actually about. Creating a symlink needs a privilege most developer
// machines do not grant, and falling straight to Skip there left the reparse
// refusals covered only on CI — long enough for an unrelated bug to sit in
// the same function unnoticed. A junction is the same kind of reparse point
// and needs no privilege, so it stands in when symlinks are unavailable.
func linkDirectory(t *testing.T, link, target string) {
	t.Helper()
	if err := os.Symlink(target, link); err == nil {
		return
	}
	out, err := exec.Command("cmd.exe", "/d", "/s", "/c", "mklink", "/J", link, target).CombinedOutput()
	if err != nil {
		t.Skipf("neither symlink nor junction is available: %v: %s", err, out)
	}
}

func TestPurgeRejectsWrongTarget(t *testing.T) {
	base := t.TempDir()
	wrong := filepath.Join(base, "Other")
	if err := os.Mkdir(wrong, 0700); err != nil {
		t.Fatal(err)
	}
	if err := purge(base, wrong); err == nil {
		t.Fatal("non-Lunitide target accepted")
	}
	if _, err := os.Stat(wrong); err != nil {
		t.Fatalf("wrong target was altered: %v", err)
	}
}
func TestPurgeRejectsReparseRoot(t *testing.T) {
	base, outside := t.TempDir(), t.TempDir()
	marker := filepath.Join(outside, "keep.txt")
	if err := os.WriteFile(marker, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	linkDirectory(t, filepath.Join(base, dataDirectoryName), outside)
	if err := purge(base, filepath.Join(base, dataDirectoryName)); err == nil {
		t.Fatal("reparse root accepted")
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("reparse target was altered: %v", err)
	}
}
func TestPurgeDeletesTreeButDoesNotFollowChildReparse(t *testing.T) {
	base, outside := t.TempDir(), t.TempDir()
	root := filepath.Join(base, dataDirectoryName)
	marker := filepath.Join(outside, "keep.txt")
	if err := os.MkdirAll(filepath.Join(root, "nested"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "nested", "delete.txt"), []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(marker, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	linkDirectory(t, filepath.Join(root, "link"), outside)
	if err := purge(base, root); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(root); !os.IsNotExist(err) {
		t.Fatalf("root remains: %v", err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("child reparse target was altered: %v", err)
	}
}
