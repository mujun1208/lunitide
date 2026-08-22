package toolruntime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPickDesktopNamedFileExactWins(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"云枢ERP产品介绍.pptx", "周报.xlsx", "协议.docx", "会议纪要.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	path, names, err := pickDesktopNamedFile(dir, "协议")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(path) != "协议.docx" {
		t.Fatalf("opened %q from %v", path, names)
	}
}

func TestPickDesktopNamedFileTieDoesNotOpen(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"会议协议.pdf", "项目协议说明.docx"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	path, names, err := pickDesktopNamedFile(dir, "协议")
	if err != nil || path != "" || len(names) != 2 {
		t.Fatalf("tie = %q %v err=%v", path, names, err)
	}
}

func TestPickDesktopNamedFileIncludesShortcutFolder(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "汽水音乐"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "汽水音乐.lnk"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	path, _, err := pickDesktopNamedFile(dir, "汽水音乐")
	if err != nil {
		t.Fatal(err)
	}
	base := filepath.Base(path)
	if base != "汽水音乐.lnk" && base != "汽水音乐" {
		t.Fatalf("opened %q", path)
	}
}

func TestDesktopNameScoreSkipsLockfiles(t *testing.T) {
	if desktopNameScore("~$协议.docx", "协议") != 0 {
		t.Fatal("Word lock file must not match")
	}
	if desktopNameScore("协议.docx", "协议") != 100 {
		t.Fatal("exact stem should score 100")
	}
	if !strings.Contains("协议.docx", "协议") {
		t.Fatal("sanity")
	}
}
