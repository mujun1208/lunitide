//go:build windows

package datadir

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

func TestPrepareForTestCreatesProtectedCurrentUserDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data")
	root, err := PrepareForTest(path)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if root.Path() != path {
		t.Fatalf("got path %q, want %q", root.Path(), path)
	}
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.OWNER_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	control, _, err := sd.Control()
	if err != nil {
		t.Fatal(err)
	}
	if control&windows.SE_DACL_PROTECTED == 0 {
		t.Fatal("data directory DACL inherits from its parent")
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if dacl == nil || dacl.AceCount != 2 {
		t.Fatalf("expected exactly user and SYSTEM ACEs, got %#v", dacl)
	}
}

func TestPrepareForTestRejectsRelativePathAndReparsePoint(t *testing.T) {
	if _, err := PrepareForTest("relative"); err == nil {
		t.Fatal("relative override accepted")
	}
	target := filepath.Join(t.TempDir(), "target")
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := PrepareForTest(link); err == nil {
		t.Fatal("reparse-point data directory accepted")
	}
}

func TestSecureRootCloseIsSharedAndIdempotent(t *testing.T) {
	root, err := PrepareForTest(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	copy := &SecureRoot{state: root.state} // Simulate an unsafe copied wrapper around the shared private state.
	if err := root.Close(); err != nil {
		t.Fatal(err)
	}
	if err := root.Close(); err != nil {
		t.Fatal(err)
	}
	if copy.Path() != "" {
		t.Fatal("closed copy returned a path")
	}
	if _, err := copy.FilePath("test.db"); err == nil {
		t.Fatal("closed copy remained usable")
	}
	if err := copy.ProtectRegularFile("test.db"); err == nil {
		t.Fatal("closed copy remained usable")
	}
	if err := copy.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestProtectRegularFileRejectsHardLinks(t *testing.T) {
	root, err := PrepareForTest(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	path, err := root.FilePath("test.db")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(path, filepath.Join(filepath.Dir(path), "linked.db")); err != nil {
		t.Skipf("hard links unavailable: %v", err)
	}
	if err := root.ProtectRegularFile("test.db"); err == nil {
		t.Fatal("hard-linked file accepted")
	}
}
