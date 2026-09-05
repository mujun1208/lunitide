package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/toolruntime"
)

func TestAppendPostActSnapshot(t *testing.T) {
	if got := appendPostActSnapshot("snapshot", "tree", "ignored"); got != "tree" {
		t.Fatalf("snapshot op should not wrap itself: %q", got)
	}
	if got := appendPostActSnapshot("click", "clicked", ""); got != "clicked" {
		t.Fatalf("empty follow-up: %q", got)
	}
	got := appendPostActSnapshot("click", "clicked B1", `{"refs":["B2"]}`)
	if got != "clicked B1\n\n[snapshot after click]\n{\"refs\":[\"B2\"]}" {
		t.Fatalf("got %q", got)
	}
	if got := appendPostActSnapshot("navigate", "", "tree-only"); got != "tree-only" {
		t.Fatalf("empty primary: %q", got)
	}
	if got := appendPostActSnapshot("read", "body", "tree"); got != "body\n\n[snapshot after read]\ntree" {
		t.Fatalf("read follow-up: %q", got)
	}
}

func TestBrowserActNeedsSnapshotRing(t *testing.T) {
	for _, op := range []string{"click", "type", "navigate", "read", "wait", "dialog", "scroll", "press"} {
		if !browserActNeedsSnapshot(browserActCall{Op: op}) {
			t.Fatalf("%s must resnapshot after act", op)
		}
	}
	if browserActNeedsSnapshot(browserActCall{Op: "snapshot"}) {
		t.Fatal("snapshot must not follow itself")
	}
	if browserActNeedsSnapshot(browserActCall{Op: "tabs", Tab: "list"}) {
		t.Fatal("tabs list is read-only")
	}
	if !browserActNeedsSnapshot(browserActCall{Op: "tabs", Tab: "new"}) {
		t.Fatal("tab switch must resnapshot")
	}
}

func TestInvokeBrowserActViaPlaywrightRefusesEmptySuccess(t *testing.T) {
	e := NewEngine(nil, "test")
	out, err := e.invokeBrowserActViaPlaywright(context.Background(), browserActCall{Op: "click", Selector: "e12"})
	if !errors.Is(err, errBrowserMCPNotReady) && (err == nil || !strings.Contains(err.Error(), "BROWSER_MCP_NOT_READY")) {
		t.Fatalf("unready act must be a typed error, got %v", err)
	}
	if strings.TrimSpace(out.Output) != "" {
		t.Fatalf("click must not empty-succeed: %+v", out)
	}
	if browserActMayEnsureMCP("click") || browserActMayEnsureMCP("type") || !browserActMayEnsureMCP("navigate") {
		t.Fatal("only navigate may auto-install Playwright")
	}
}

func TestBrowserHumanGate(t *testing.T) {
	if browserHumanGate("https://example.com/login", "") != "login" {
		t.Fatal("D-G1 login URL")
	}
	if browserHumanGate("https://pay.example.com/checkout", "") != "pay" {
		t.Fatal("D-G3 pay URL")
	}
	if browserHumanGate("https://example.com", "please complete captcha") != "captcha" {
		t.Fatal("captcha snapshot")
	}
	if browserHumanGate("https://example.com/about", "hello") != "" {
		t.Fatal("clean page")
	}
}

func TestFinishBrowserActLoginWallAndL0(t *testing.T) {
	_, err := finishBrowserAct("https://example.com", "password login form", toolruntime.Result{Output: "tree"})
	if err == nil || browserWallReason(err.Error()) != "login" {
		t.Fatalf("D-G1 navigate snapshot wall: %v", err)
	}
	out, err := finishBrowserAct("https://example.com/about", "hello", toolruntime.Result{Output: "tree-only"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.Output, `"l0"`) || !strings.Contains(out.Output, "tree-only") {
		t.Fatalf("navigate success must keep snapshot and attach l0: %q", out.Output)
	}
}

func TestMarkBrowserNavigateFetchIsVisible(t *testing.T) {
	got := markBrowserNavigateFetch(toolruntime.Result{Output: "title\nbody"})
	if !strings.Contains(got.Output, "BROWSER_NAVIGATE_FETCH") || !strings.Contains(got.Output, "不要把这次当成浏览器已打开") || !strings.Contains(got.Output, "title") {
		t.Fatalf("%q", got.Output)
	}
}
