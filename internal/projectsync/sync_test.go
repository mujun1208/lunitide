package projectsync

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/projectapp"
)

func TestSyncCopiesAndRejectsRoot(t *testing.T) {
	root := t.TempDir()
	dest := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "readme.txt"), []byte("hello factory"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateDest(root, root); err != projectapp.ErrSyncInvalid {
		t.Fatalf("same: %v", err)
	}
	rec, err := SyncTree(root, dest, "01ARZ3NDEKTSV4RRFFQ69G5FAV")
	if err != nil || len(rec.Files) == 0 {
		t.Fatalf("%+v %v", rec, err)
	}
	if _, err = os.Stat(filepath.Join(dest, ".lunitide-sync.json")); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(dest, "readme.txt")); err != nil {
		t.Fatal(err)
	}
	if _, err = LoadReceipt(root); err != nil {
		t.Fatal(err)
	}
}

func TestInventoryTreeSkipsGitAndDigestOnlyLargeFiles(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".git", "HEAD"), []byte("ref"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".lunitide"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".lunitide", "project-tree.json"), []byte(`{"version":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".lunitide", "sync-receipt.json"), []byte(`{"version":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "readme.txt"), []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "huge.bin"), make([]byte, MaxCopyBytes+1), 0o644); err != nil {
		t.Fatal(err)
	}
	inv, err := InventoryTree(root)
	if err != nil || inv.Version != 1 {
		t.Fatalf("%+v %v", inv, err)
	}
	var huge, readme, tree, git, receipt bool
	for _, f := range inv.Files {
		switch f.Rel {
		case "huge.bin":
			huge = f.Skip == "skipped-too-large" && f.Digest != "" && !f.Copied
		case "readme.txt":
			readme = f.Digest != "" && f.Skip == ""
		case ".lunitide/project-tree.json":
			tree = true
		case ".lunitide/sync-receipt.json":
			receipt = true
		case ".git/HEAD":
			git = true
		}
	}
	if !huge || !readme || !tree || git || receipt {
		t.Fatalf("inventory %+v", inv.Files)
	}
}

func TestInventoryTreeIsDeterministicAcrossSeconds(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "app.go"), []byte("package app"), 0o644); err != nil {
		t.Fatal(err)
	}
	first, err := InventoryTree(root)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(1100 * time.Millisecond)
	second, err := InventoryTree(root)
	if err != nil {
		t.Fatal(err)
	}
	a, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(second)
	if err != nil {
		t.Fatal(err)
	}
	if string(a) != string(b) {
		t.Fatalf("inventory must be clock-stable for release verify\n%s\n%s", a, b)
	}
	if first.At != "" {
		t.Fatalf("packed inventory must not carry a wall clock: %q", first.At)
	}
}

func TestValidateDestRejectsNestedAndCaseFold(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "out", "pkg")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateDest(root, nested); err != projectapp.ErrSyncInvalid {
		t.Fatalf("nested dest allowed: %v", err)
	}
	parent := filepath.Dir(root)
	if _, err := ValidateDest(root, parent); err != projectapp.ErrSyncInvalid {
		t.Fatalf("parent dest allowed: %v", err)
	}
	if runtime.GOOS == "windows" {
		if _, err := ValidateDest(root, strings.ToUpper(root)); err != projectapp.ErrSyncInvalid {
			t.Fatalf("case-folded same dest allowed: %v", err)
		}
	}
}
