//go:build windows

package commandworker

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestPinWorkingDirectoryBlocksRenameUntilClosed(t *testing.T) {
	root := t.TempDir()
	cwd := filepath.Join(root, "safe", "project")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	guard, err := PinWorkingDirectory(root, cwd)
	if err != nil {
		t.Fatal(err)
	}
	moved := filepath.Join(root, "safe-moved")
	if err := os.Rename(filepath.Join(root, "safe"), moved); err == nil {
		_ = guard.Close()
		t.Fatal("rename succeeded while cwd parent was pinned")
	}
	if err := guard.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(root, "safe"), moved); err != nil {
		t.Fatalf("rename remained blocked after guard close: %v", err)
	}
}

func TestPinWorkingDirectoryRejectsJunctionComponent(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	junction := filepath.Join(root, "junction")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("cmd.exe", "/d", "/s", "/c", "mklink", "/J", junction, target)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("directory junction unavailable: %v: %s", err, output)
	}
	guard, err := PinWorkingDirectory(root, junction)
	if guard != nil {
		_ = guard.Close()
	}
	if err == nil {
		t.Fatal("junction cwd was accepted")
	}
}
