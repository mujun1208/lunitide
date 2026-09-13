package projectroot

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProbeRejectsMissingFileAndForeignLock(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope")
	if err := Probe(missing); err == nil || !IsInvalid(err) {
		t.Fatalf("missing dir: %v", err)
	}
	file := filepath.Join(t.TempDir(), "file.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Probe(file); err == nil || !IsInvalid(err) {
		t.Fatalf("file as root: %v", err)
	}

	root := t.TempDir()
	if err := Probe(root); err != nil {
		t.Fatal(err)
	}
	if err := WriteLock(root, Lock{ProjectID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", ProjectCode: "ITM00001", Name: "A"}); err != nil {
		t.Fatal(err)
	}
	got, err := ReadLock(root)
	if err != nil || got.ProjectID != "01ARZ3NDEKTSV4RRFFQ69G5FAV" {
		t.Fatalf("read lock: %+v %v", got, err)
	}
	if err := Bind(root, Lock{ProjectID: "01ARZ3NDEKTSV4RRFFQ69G5FAW", ProjectCode: "ITM00002", Name: "B"}); err == nil || !IsBusy(err) {
		t.Fatalf("foreign lock: %v", err)
	}
	if err := Bind(root, Lock{ProjectID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", ProjectCode: "ITM00001", Name: "A"}); err != nil {
		t.Fatalf("same project rebind: %v", err)
	}
	if err := os.Remove(filepath.Join(root, ".lunitide", "project.json")); err != nil {
		t.Fatal(err)
	}
	if err := Bind(root, Lock{ProjectID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", ProjectCode: "ITM00001", Name: "A"}); err != nil {
		t.Fatalf("rebuild missing lock: %v", err)
	}
	got, err = ReadLock(root)
	if err != nil || got.ProjectID != "01ARZ3NDEKTSV4RRFFQ69G5FAV" {
		t.Fatalf("rebuilt lock: %+v %v", got, err)
	}
}
