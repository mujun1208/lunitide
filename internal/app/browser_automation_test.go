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

func TestMarkBrowserNavigateFetchIsVisible(t *testing.T) {
	got := markBrowserNavigateFetch(toolruntime.Result{Output: "title\nbody"})
	if !strings.Contains(got.Output, "BROWSER_NAVIGATE_FETCH") || !strings.Contains(got.Output, "不要把这次当成浏览器已打开") || !strings.Contains(got.Output, "title") {
		t.Fatalf("%q", got.Output)
	}
}
