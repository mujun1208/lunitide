//go:build windows

package terminalruntime

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestConPTYLifecycle(t *testing.T) {
	r, e := New(Config{Workspace: t.TempDir()})
	if e != nil {
		t.Fatal(e)
	}
	defer r.Shutdown()
	if e = r.Start(context.Background(), "life", 80, 24); e != nil {
		t.Skipf("ConPTY unavailable: %v", e)
	}
	if e = r.Write("life", []byte("Write-Output LUNITIDE_MARKER\r\n")); e != nil {
		t.Fatal(e)
	}
	deadline := time.After(10 * time.Second)
	for {
		select {
		case ev := <-r.Events():
			t.Logf("event type=%s code=%d err=%v data=%q", ev.Type, ev.ExitCode, ev.Err, ev.Data)
			if ev.SessionID == "life" && strings.Contains(string(ev.Data), "LUNITIDE_MARKER") {
				if e = r.Resize("life", 100, 30); e != nil {
					t.Fatal(e)
				}
				if e = r.Close("life"); e != nil {
					t.Fatal(e)
				}
				return
			}
		case <-deadline:
			t.Fatal("no ConPTY output")
		}
	}
}

func TestJobContainsDescendant(t *testing.T) {
	r, e := New(Config{Workspace: t.TempDir()})
	if e != nil {
		t.Fatal(e)
	}
	if e = r.Start(context.Background(), "tree", 80, 24); e != nil {
		t.Skipf("ConPTY unavailable: %v", e)
	}
	// A long-lived child inherits job membership. Closing the job must terminate
	// both shell and child; this test primarily guards assignment-before-resume.
	if e = r.Write("tree", []byte("Start-Process powershell -ArgumentList '-NoProfile','-Command','Start-Sleep 60'; Write-Output CHILD_STARTED\r\n")); e != nil {
		t.Fatal(e)
	}
	time.Sleep(time.Second)
	start := time.Now()
	if e = r.Close("tree"); e != nil {
		t.Fatal(e)
	}
	if time.Since(start) > 3*time.Second {
		t.Fatal("job close blocked")
	}
}
