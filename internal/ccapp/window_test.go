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

func TestMatchWindowWeChatByProcessWhenTitleIsTheContact(t *testing.T) {
	wins := []WindowInfo{
		{ID: "0x1", Title: "_穆_", Process: "Weixin.exe"},
		{ID: "0x2", Title: "问候 - 豆包", Process: "Doubao.exe"},
	}
	got, ok := MatchWindow(wins, "微信")
	if !ok || got.Process != "Weixin.exe" {
		t.Fatalf("wechat %+v ok=%v", got, ok)
	}
	got, ok = MatchWindow(wins, "豆包")
	if !ok || got.Process != "Doubao.exe" {
		t.Fatalf("doubao %+v ok=%v", got, ok)
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

func TestPickUserFacingWindowSkipsCompanion(t *testing.T) {
	wins := []WindowInfo{
		{ID: "app", Title: "月伴对话 - Lunitide", Process: "lunitide.exe", Foreground: true},
		{ID: "wv", Title: "Lunitide", Process: "msedgewebview2.exe"},
		{ID: "edge", Title: "新闻 - Microsoft Edge", Process: "msedge.exe"},
		{ID: "word", Title: "周报.docx - Word", Process: "winword.exe"},
	}
	browser, ok := pickUserFacingWindow(wins, true)
	if !ok || browser.ID != "edge" {
		t.Fatalf("browser = %+v ok=%v", browser, ok)
	}
	doc, ok := pickUserFacingWindow(wins, false)
	if !ok || doc.ID != "word" {
		t.Fatalf("document = %+v ok=%v", doc, ok)
	}
	if _, ok := pickUserFacingWindow([]WindowInfo{{ID: "app", Title: "Lunitide", Process: "lunitide.exe", Foreground: true}}, true); ok {
		t.Fatal("companion window must not count as a browser")
	}
}

func TestUniqueWindowForGoal(t *testing.T) {
	wins := []WindowInfo{
		{ID: "0x1", Title: "无标题 - 记事本", Process: "notepad.exe"},
		{ID: "0x2", Title: "微信", Process: "Weixin.exe"},
		{ID: "0x3", Title: "月伴", Process: "lunitide.exe"},
	}
	one, hits := UniqueWindowForGoal(wins, "在记事本里输入你好")
	if len(hits) != 1 || one.ID != "0x1" {
		t.Fatalf("notepad goal = %+v hits=%d", one, len(hits))
	}
	_, hits = UniqueWindowForGoal(wins, "在记事本和微信里各发一句")
	if len(hits) != 2 {
		t.Fatalf("two named apps = %d hits", len(hits))
	}
	_, hits = UniqueWindowForGoal(wins, "点一下")
	if len(hits) != 0 {
		t.Fatalf("unnamed goal must not lock a window, hits=%d", len(hits))
	}
	byProc, hits := UniqueWindowForGoal(wins, "type into notepad")
	if len(hits) != 1 || byProc.ID != "0x1" {
		t.Fatalf("process goal = %+v hits=%d", byProc, len(hits))
	}
}

func TestRefuseTypingWithoutFocus(t *testing.T) {
	s := New(nil)
	s.noteTypingFocus("button", true)
	if err := s.refuseTypingWithoutFocus(); err == nil || !strings.Contains(err.Error(), "输入框") {
		t.Fatalf("button focus = %v", err)
	}
	s.noteTypingFocus("edit", true)
	if err := s.refuseTypingWithoutFocus(); err != nil {
		t.Fatal(err)
	}
	s.noteTypingFocus("", false)
	if err := s.refuseTypingWithoutFocus(); err != nil {
		t.Fatal(err)
	}
}

func TestFocusRoleAllowsType(t *testing.T) {
	for _, role := range []string{"edit", "document", "combobox"} {
		if !focusRoleAllowsType(role) {
			t.Fatalf("%s must accept typing", role)
		}
	}
	for _, role := range []string{"", "button", "pane", "other"} {
		if focusRoleAllowsType(role) {
			t.Fatalf("%s must refuse typing", role)
		}
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
