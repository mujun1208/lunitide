package ccapp

import (
	"strings"
	"testing"
	"time"
)

type stepClock struct{ t time.Time }

func (c *stepClock) Now() time.Time { return c.t }

func TestIsBrowserProcess(t *testing.T) {
	for _, p := range []string{"msedge.exe", `C:\Program Files\Google\Chrome\Application\chrome.exe`, "FIREFOX.EXE", "brave", "360chrome.exe"} {
		if !isBrowserProcess(p) {
			t.Fatalf("%q should be a browser", p)
		}
	}
	for _, p := range []string{"WeChat.exe", "explorer.exe", "notepad.exe", "", "chromedriver.exe", "SystemSettings.exe"} {
		if isBrowserProcess(p) {
			t.Fatalf("%q should not be a browser", p)
		}
	}
}

func TestAppendBrowserHintOnlyForPageToolsAndRateLimited(t *testing.T) {
	clk := &stepClock{t: time.Date(2026, 9, 17, 10, 0, 0, 0, time.UTC)}
	s := &Service{clock: clk}
	s.noteForeground("百度一下 - Microsoft Edge", "msedge.exe")

	got := s.appendBrowserHint("clicked B3", ToolMouseClick)
	if !strings.Contains(got, "browser.act") {
		t.Fatalf("first page click in a browser must point at browser.act: %q", got)
	}
	if again := s.appendBrowserHint("typed 5 character(s)", ToolKeyboardType); strings.Contains(again, "browser.act") {
		t.Fatalf("hint must not repeat within a minute: %q", again)
	}
	clk.t = clk.t.Add(61 * time.Second)
	if later := s.appendBrowserHint("pasted 40 character(s)", ToolPaste); !strings.Contains(later, "browser.act") {
		t.Fatalf("hint returns after the cooldown: %q", later)
	}

	// Window lifecycle and screenshots are legitimately computer.act work.
	clk.t = clk.t.Add(61 * time.Second)
	for _, tool := range []string{ToolScreenCapture, ToolWindowFocus, ToolWindowAction, ToolGetActiveWindow, ToolKeyboardShortcut} {
		if out := s.appendBrowserHint("ok", tool); out != "ok" {
			t.Fatalf("%s must not carry the browser hint: %q", tool, out)
		}
	}

	// Not a browser: never hint.
	s2 := &Service{clock: clk}
	s2.noteForeground("微信", "WeChat.exe")
	if out := s2.appendBrowserHint("clicked B1", ToolMouseClick); out != "clicked B1" {
		t.Fatalf("non-browser foreground must stay quiet: %q", out)
	}
}
