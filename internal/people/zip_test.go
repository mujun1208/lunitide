package people

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

func TestZipDirectoryPacksFilesUnderLimit(t *testing.T) {
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "note.txt"), []byte("hello-folder"), 0o600); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(src, "sub")
	if err := os.Mkdir(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, "inner.txt"), []byte("inner"), 0o600); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(t.TempDir(), "folder.zip")
	if err := zipDirectory(src, dest, maxFileBytes); err != nil {
		t.Fatal(err)
	}
	r, err := zip.OpenReader(dest)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	found := map[string]int64{}
	for _, f := range r.File {
		found[f.Name] = int64(f.UncompressedSize64)
	}
	if found["note.txt"] != 12 || found["sub/inner.txt"] != 5 {
		t.Fatalf("zip entries = %#v", found)
	}
}

func TestZipDirectoryRejectsOversize(t *testing.T) {
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "big.bin"), make([]byte, 64), 0o600); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(t.TempDir(), "over.zip")
	if err := zipDirectory(src, dest, 8); err != ErrTooLarge {
		t.Fatalf("oversize zip = %v", err)
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatalf("failed zip must be removed, stat=%v", err)
	}
}
