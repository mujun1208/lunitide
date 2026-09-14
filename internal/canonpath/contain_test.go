package canonpath_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/lunitide/lunitide/internal/canonpath"
)

func TestContainedFoldsAliasAndTargetToOneTree(t *testing.T) {
	real := t.TempDir()
	if err := os.WriteFile(filepath.Join(real, "hello.txt"), []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "ws")
	if err := os.Symlink(real, alias); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if !canonpath.Contained(alias, filepath.Join(alias, "hello.txt")) {
		t.Fatal("file under an alias of the root must be contained")
	}
	if !canonpath.Contained(alias, filepath.Join(real, "hello.txt")) {
		t.Fatal("same file through the target must be contained")
	}
	if !canonpath.Contained(alias, filepath.Join(alias, "missing.txt")) {
		t.Fatal("a not-yet-created child under an alias root must be contained")
	}
	outside := filepath.Join(t.TempDir(), "away.txt")
	if err := os.WriteFile(outside, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if canonpath.Contained(alias, outside) {
		t.Fatal("file outside the root must not be contained")
	}
}

func TestContainedRejectsTraversal(t *testing.T) {
	root := t.TempDir()
	if canonpath.Contained(root, filepath.Join(root, "..", "escape.txt")) {
		t.Fatal("parent traversal must not be contained")
	}
}

func TestResolveMissingChildKeepsExistingPrefix(t *testing.T) {
	root := t.TempDir()
	missing := filepath.Join(root, "new", "file.txt")
	got := canonpath.Resolve(missing)
	wantPrefix := canonpath.Resolve(root)
	rel, err := filepath.Rel(wantPrefix, got)
	if err != nil || rel != filepath.Join("new", "file.txt") {
		t.Fatalf("Resolve(%q) = %q, want under %q", missing, got, wantPrefix)
	}
}
