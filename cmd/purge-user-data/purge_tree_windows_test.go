//go:build windows

package main

import (
	"os"
	"path/filepath"
	"testing"
)

// The ordinary case: a data directory with files in it. Every other test
// here needs a symlink to set up and skips on machines that cannot create
// one, which left the plain non-empty tree — the only shape that ever
// happens in production — with no coverage at all.
func TestPurgeDeletesOrdinaryNonEmptyTree(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, dataDirectoryName)
	nested := filepath.Join(root, "sessions", "01ARZ3NDEKTSV4RRFFQ69G5FAV")
	if err := os.MkdirAll(nested, 0700); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{
		filepath.Join(root, "settings.json"),
		filepath.Join(root, "sessions", "index.db"),
		filepath.Join(nested, "transcript.md"),
	} {
		if err := os.WriteFile(p, []byte("x"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := purge(base, root); err != nil {
		t.Fatalf("purge of a non-empty data directory failed: %v", err)
	}
	if _, err := os.Lstat(root); !os.IsNotExist(err) {
		t.Fatalf("data root remains: %v", err)
	}
}
