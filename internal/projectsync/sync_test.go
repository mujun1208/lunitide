package projectsync

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

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
