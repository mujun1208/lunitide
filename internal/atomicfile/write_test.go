package atomicfile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReplaceFailurePreservesPreviousFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "document.txt")
	if err := Write(path, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := Replace(filepath.Join(t.TempDir(), "missing"), path); err == nil {
		t.Fatal("missing source accepted")
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "original" {
		t.Fatalf("previous file lost: %q %v", data, err)
	}
	if err := Write(path, []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(path)
	if err != nil || string(data) != "new" {
		t.Fatalf("replacement failed: %q %v", data, err)
	}
}
