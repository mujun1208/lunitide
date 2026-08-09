//go:build windows

package main

import (
	"os"
	"path/filepath"
	"testing"
)

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
	if err := os.Symlink(outside, filepath.Join(base, dataDirectoryName)); err != nil {
		t.Skipf("directory symlink unavailable: %v", err)
	}
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
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Skipf("directory symlink unavailable: %v", err)
	}
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
