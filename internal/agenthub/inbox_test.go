package agenthub

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInboxCopiesRegularFileLeavesSource(t *testing.T) {
	s := New(NewMemoryStore(), t.TempDir(), nil)
	srcDir := t.TempDir()
	src := filepath.Join(srcDir, "note.txt")
	if err := os.WriteFile(src, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	s.PickFiles = func() ([]string, error) { return []string{src}, nil }
	canceled, dir, files, skipped, err := s.Inbox("files", "", "")
	if err != nil || canceled || len(skipped) != 0 || len(files) != 1 {
		t.Fatalf("%v %v %v %v %v", canceled, dir, files, skipped, err)
	}
	body, err := os.ReadFile(src)
	if err != nil || string(body) != "hello" {
		t.Fatalf("source mutated: %q %v", body, err)
	}
	got, err := os.ReadFile(filepath.Join(dir, inboxDirName, "note.txt"))
	if err != nil || string(got) != "hello" {
		t.Fatalf("copy: %q %v", got, err)
	}
}

func TestInboxRenamesCollision(t *testing.T) {
	s := New(NewMemoryStore(), t.TempDir(), nil)
	src := filepath.Join(t.TempDir(), "a.txt")
	if err := os.WriteFile(src, []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}
	s.PickFiles = func() ([]string, error) { return []string{src}, nil }
	_, dir, _, _, err := s.Inbox("files", "", "")
	if err != nil {
		t.Fatal(err)
	}
	_, _, files, _, err := s.Inbox("files", dir, "")
	if err != nil || len(files) != 2 {
		t.Fatalf("%v %v", files, err)
	}
	names := files[0].Path + "," + files[1].Path
	if !strings.Contains(names, "a.txt") || !strings.Contains(names, "a (2).txt") {
		t.Fatal(names)
	}
}

func TestInboxSkipsTooLarge(t *testing.T) {
	s := New(NewMemoryStore(), t.TempDir(), nil)
	src := filepath.Join(t.TempDir(), "big.bin")
	f, err := os.Create(src)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.Truncate(maxIngestFileBytes + 1); err != nil {
		t.Fatal(err)
	}
	f.Close()
	s.PickFiles = func() ([]string, error) { return []string{src}, nil }
	_, _, files, skipped, err := s.Inbox("files", "", "")
	if err != nil || len(files) != 0 || len(skipped) != 1 || !strings.Contains(skipped[0], "100MB") {
		t.Fatalf("%v %v %v", files, skipped, err)
	}
}

func TestInboxFolderSkipsNodeModules(t *testing.T) {
	s := New(NewMemoryStore(), t.TempDir(), nil)
	src := t.TempDir()
	if err := os.MkdirAll(filepath.Join(src, "node_modules"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "node_modules", "x.js"), []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "keep.md"), []byte("k"), 0o644); err != nil {
		t.Fatal(err)
	}
	s.PickFolder = func() (string, error) { return src, nil }
	_, _, files, _, err := s.Inbox("folder", "", "")
	if err != nil || len(files) != 1 || files[0].Name != "keep.md" {
		t.Fatalf("%v %v", files, err)
	}
}

func TestInboxDropReturnsRemainingFiles(t *testing.T) {
	s := New(NewMemoryStore(), t.TempDir(), nil)
	tmp := t.TempDir()
	first := filepath.Join(tmp, "first.txt")
	second := filepath.Join(tmp, "second.txt")
	if err := os.WriteFile(first, []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte("2"), 0o644); err != nil {
		t.Fatal(err)
	}
	s.PickFiles = func() ([]string, error) { return []string{first, second}, nil }
	_, dir, _, _, err := s.Inbox("files", "", "")
	if err != nil {
		t.Fatal(err)
	}
	_, _, files, _, err := s.Inbox("drop", dir, "first.txt")
	if err != nil || len(files) != 1 || files[0].Name != "second.txt" {
		t.Fatalf("%v %v", files, err)
	}
}

func TestInboxDropOnlyInsideInbox(t *testing.T) {
	s := New(NewMemoryStore(), t.TempDir(), nil)
	src := filepath.Join(t.TempDir(), "note.txt")
	if err := os.WriteFile(src, []byte("h"), 0o644); err != nil {
		t.Fatal(err)
	}
	s.PickFiles = func() ([]string, error) { return []string{src}, nil }
	_, dir, _, _, err := s.Inbox("files", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, _, err := s.Inbox("drop", dir, "../note.txt"); err == nil {
		t.Fatal("expected escape to fail")
	}
	_, _, files, _, err := s.Inbox("drop", dir, "note.txt")
	if err != nil || len(files) != 0 {
		t.Fatalf("%v %v", files, err)
	}
	if _, err := os.Stat(src); err != nil {
		t.Fatal("source must remain")
	}
}

func TestInboxAllocatesWorkDirWhenEmpty(t *testing.T) {
	root := t.TempDir()
	s := New(NewMemoryStore(), root, nil)
	src := filepath.Join(t.TempDir(), "note.txt")
	if err := os.WriteFile(src, []byte("h"), 0o644); err != nil {
		t.Fatal(err)
	}
	s.PickFiles = func() ([]string, error) { return []string{src}, nil }
	_, dir, files, _, err := s.Inbox("files", "", "")
	if err != nil || len(files) != 1 || !strings.HasPrefix(filepath.Clean(dir), filepath.Clean(root)) {
		t.Fatalf("%s %v %v", dir, files, err)
	}
}

func TestInboxCancelEmptySelection(t *testing.T) {
	s := New(NewMemoryStore(), t.TempDir(), nil)
	s.PickFiles = func() ([]string, error) { return nil, ErrPickCanceled }
	canceled, _, files, _, err := s.Inbox("files", "", "")
	if err != nil || !canceled || len(files) != 0 {
		t.Fatalf("%v %v %v", canceled, files, err)
	}
}
