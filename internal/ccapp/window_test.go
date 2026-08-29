package ccapp

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestMatchWindowPrefersExactTitle(t *testing.T) {
	wins := []WindowInfo{
		{ID: "0x1", Title: "Notes", Process: "notepad.exe"},
		{ID: "0x2", Title: "Untitled - Notepad", Process: "notepad.exe", Foreground: true},
	}
	got, ok := MatchWindow(wins, "Untitled - Notepad")
	if !ok || got.ID != "0x2" {
		t.Fatalf("got %+v ok=%v", got, ok)
	}
	byProc, ok := MatchWindow(wins, "notepad")
	if !ok || byProc.Process != "notepad.exe" {
		t.Fatalf("process match %+v ok=%v", byProc, ok)
	}
	fg, ok := MatchWindow(wins, "foreground")
	if !ok || !fg.Foreground {
		t.Fatalf("foreground %+v ok=%v", fg, ok)
	}
}

func TestMatchWindowsCollectsProcessHits(t *testing.T) {
	wins := []WindowInfo{
		{Title: "A", Process: "app.exe"},
		{Title: "B", Process: "app.exe"},
		{Title: "C", Process: "other.exe"},
	}
	got := MatchWindows(wins, "app.exe")
	if len(got) != 2 {
		t.Fatalf("got %d want 2", len(got))
	}
}

func TestProtectedDesktopProcess(t *testing.T) {
	for _, name := range []string{"explorer.exe", "CONSENT.EXE", "lunitide", "dwm.exe"} {
		if !ProtectedDesktopProcess(name) {
			t.Fatalf("%s should be protected", name)
		}
	}
	if ProtectedDesktopProcess("notepad.exe") {
		t.Fatal("notepad should not be protected")
	}
}

func TestChromeCloseControlAndDocumentEditor(t *testing.T) {
	if !ChromeCloseControl("关闭", 8, 28, 28) {
		t.Fatal("title-bar close")
	}
	if !ChromeCloseControl("关闭文档", 80, 120, 32) {
		t.Fatal("close-document command")
	}
	if ChromeCloseControl("发送", 8, 64, 28) {
		t.Fatal("send is not close")
	}
	if !documentEditorProcess("WINWORD.EXE") || !documentEditorProcess("wps.exe") || !documentEditorProcess("notepad.exe") {
		t.Fatal("document editors")
	}
	if documentEditorProcess("cloudmusic.exe") {
		t.Fatal("music player is not a document editor")
	}
}

func TestSplitMenuPath(t *testing.T) {
	got := SplitMenuPath("File > Save As")
	if len(got) != 2 || got[0] != "File" || got[1] != "Save As" {
		t.Fatalf("%v", got)
	}
	got = SplitMenuPath("文件/保存")
	if len(got) != 2 || got[0] != "文件" || got[1] != "保存" {
		t.Fatalf("%v", got)
	}
}

func TestWindowFocusQueryPrefersTitle(t *testing.T) {
	if got := windowFocusQuery("Notes", "notepad.exe"); got != "Notes" {
		t.Fatalf("got %q", got)
	}
	if got := windowFocusQuery("", "notepad.exe"); got != "notepad.exe" {
		t.Fatalf("got %q", got)
	}
	if windowFocusQuery("  ", "  ") != "" {
		t.Fatal("empty query")
	}
}

func TestClampClipboardCapsRunes(t *testing.T) {
	long := strings.Repeat("月", CcMaxClipboardRunes+50)
	got := clampClipboard(long)
	if utf8.RuneCountInString(got) != CcMaxClipboardRunes {
		t.Fatalf("runes = %d want %d", utf8.RuneCountInString(got), CcMaxClipboardRunes)
	}
	if got == long {
		t.Fatal("should truncate")
	}
	short := "hello"
	if clampClipboard(short) != short {
		t.Fatal("short text unchanged")
	}
}
