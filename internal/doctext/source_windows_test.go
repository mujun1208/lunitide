//go:build windows

package doctext

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestReadSourceRejectsUNCDevicesAndAlternateStreams(t *testing.T) {
	for _, path := range []string{`\\127.0.0.1\share\file.txt`, `\\.\pipe\not-a-file`, `\\?\C:\file.txt`, `C:\file.txt:stream`} {
		_, err := ReadSource(path)
		if err == nil {
			t.Fatalf("unsupported path accepted: %s", path)
		}
		if !strings.Contains(err.Error(), "本地磁盘") || strings.Contains(err.Error(), "local drive file") {
			t.Fatalf("unsupported source must stay Chinese: %s %v", path, err)
		}
	}
}

func TestSourceSnapshotPinsLeafAgainstWriteAndReplacement(t *testing.T) {
	path := filepath.Join(t.TempDir(), "source.txt")
	if err := os.WriteFile(path, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	file, err := openSource(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if err := os.WriteFile(path, []byte("replacement"), 0600); err == nil {
		t.Fatal("source mutated during snapshot read")
	}
	if err := os.Rename(path, path+".moved"); err == nil {
		t.Fatal("source replaced during snapshot read")
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("next version"), 0600); err != nil {
		t.Fatalf("source remained locked: %v", err)
	}
}

func TestReadSourceRejectsActualDirectoryJunction(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	link := filepath.Join(root, "link")
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "file.txt"), []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("cmd.exe", "/d", "/s", "/c", "mklink", "/J", link, target)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("junction fixture: %v %s", err, out)
	}
	if _, err := ReadSource(filepath.Join(link, "file.txt")); err == nil {
		t.Fatal("junction source accepted")
	}
}
