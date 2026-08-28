package toolruntime

import (
	"os"
	"os/exec"
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
	if desktopNameScore("协议.docx", "协议文档") < 70 {
		t.Fatal("协议文档 must still match 协议.docx")
	}
	if desktopNameScore("网易云音乐.lnk", "网") != 0 {
		t.Fatal("single-rune query must not substring-match")
	}
	if !strings.Contains("协议.docx", "协议") {
		t.Fatal("sanity")
	}
}

func TestCanonicalMusicAppResolvesNetease(t *testing.T) {
	if got := CanonicalMusicApp("网易云"); got != "网易云音乐" {
		t.Fatalf("got %q", got)
	}
	if got := CanonicalMusicApp("打开网易云音乐"); got != "网易云音乐" {
		t.Fatalf("got %q", got)
	}
	if got := CanonicalMusicAppFromText("帮我打开网易云音乐播放周杰伦"); got != "网易云音乐" {
		t.Fatalf("got %q", got)
	}
	if CanonicalMusicApp("网") != "" {
		t.Fatal("bare 网 must not resolve to 网易云音乐")
	}
	if got := CanonicalMusicAppFromText("打开桌面网易云音乐软件，搜索周杰伦歌曲，放一首"); got != "网易云音乐" {
		t.Fatalf("exact user utterance app = %q", got)
	}
}

func TestDesktopQueryCandidatesFromOpenUtterance(t *testing.T) {
	got := desktopQueryCandidates("打开桌面上的协议文档")
	joined := strings.Join(got, ",")
	if !strings.Contains(joined, "协议") {
		t.Fatalf("candidates %v must include 协议", got)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "协议.docx"), []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	var opened string
	for _, q := range got {
		path, _, err := pickDesktopNamedFile(dir, q)
		if err != nil {
			continue
		}
		if path != "" {
			opened = filepath.Base(path)
			break
		}
	}
	if opened != "协议.docx" {
		t.Fatalf("opened %q from %v", opened, got)
	}
}

func TestDesktopQueryCandidatesRecoversBaKai(t *testing.T) {
	got := desktopQueryCandidates("把开了我把它桌面上的协议文档")
	joined := strings.Join(got, ",")
	if !strings.Contains(joined, "协议") {
		t.Fatalf("把开 utterance candidates %v", got)
	}
}

func TestStartOpenedPathRunsExeFromItsDirectory(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "cloudmusic.exe")
	if err := os.WriteFile(exe, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	var gotDir, gotPath string
	var gotArgs []string
	err := startOpenedPath(exe, func(cmd *exec.Cmd) error {
		gotDir = cmd.Dir
		gotPath = cmd.Path
		gotArgs = append([]string{}, cmd.Args...)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotDir != dir {
		t.Fatalf("dir=%q want %q", gotDir, dir)
	}
	if !strings.EqualFold(filepath.Base(gotPath), "cloudmusic.exe") && !strings.EqualFold(filepath.Base(gotArgs[0]), "cloudmusic.exe") {
		t.Fatalf("path=%q args=%v", gotPath, gotArgs)
	}
}

func TestStartOpenedPathUsesShellStartForShortcuts(t *testing.T) {
	lnk := filepath.Join(t.TempDir(), "网易云音乐.lnk")
	var gotArgs []string
	err := startOpenedPath(lnk, func(cmd *exec.Cmd) error {
		gotArgs = append([]string{}, cmd.Args...)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(gotArgs, " ")
	if !strings.Contains(joined, "start") || !strings.Contains(joined, lnk) {
		t.Fatalf("args=%v", gotArgs)
	}
}

func TestPickKnownAppExecutableUsesInstallPath(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "cloudmusic.exe")
	if err := os.WriteFile(exe, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	orig := lookupKnownAppExecutables
	lookupKnownAppExecutables = func(app knownLaunchApp) []string {
		if app.Canonical == "网易云音乐" {
			return []string{exe}
		}
		return nil
	}
	t.Cleanup(func() { lookupKnownAppExecutables = orig })
	path, ok := pickKnownAppExecutable("网易云音乐")
	if !ok || path != exe {
		t.Fatalf("path=%q ok=%v", path, ok)
	}
}

func TestFirstInstalledMusicAppUsesLookup(t *testing.T) {
	orig := lookupKnownAppExecutables
	lookupKnownAppExecutables = func(app knownLaunchApp) []string {
		if app.Canonical == "网易云音乐" {
			return []string{`C:\fake\cloudmusic.exe`}
		}
		return nil
	}
	t.Cleanup(func() { lookupKnownAppExecutables = orig })
	if got := FirstInstalledMusicApp(); got != "网易云音乐" {
		t.Fatalf("got %q", got)
	}
}

func TestWalkForProcessFindsCloudMusic(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "Netease", "CloudMusic")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(nested, "cloudmusic.exe")
	if err := os.WriteFile(exe, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	found := walkForProcess(root, []string{"cloudmusic.exe", "cloudmusic"}, 4)
	if len(found) != 1 || !strings.EqualFold(found[0], exe) {
		t.Fatalf("found %v want %q", found, exe)
	}
}
