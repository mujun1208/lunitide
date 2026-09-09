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
	// Hosted Windows runners often emit the PowerShell banner several seconds
	// after Start returns. Bytes written before that are dropped, so wait for
	// the first real output and then give the command a longer window.
	if !waitConPTYOutput(t, r, "life", "", 20*time.Second) {
		t.Fatal("ConPTY produced no output")
	}
	if e = r.Write("life", []byte("Write-Output LUNITIDE_MARKER\r\n")); e != nil {
		t.Fatal(e)
	}
	if !waitConPTYOutput(t, r, "life", "LUNITIDE_MARKER", 45*time.Second) {
		t.Fatal("no ConPTY output")
	}
	if e = r.Resize("life", 100, 30); e != nil {
		t.Fatal(e)
	}
	if e = r.Close("life"); e != nil {
		t.Fatal(e)
	}
}

func waitConPTYOutput(t *testing.T, r *Runtime, sessionID, needle string, timeout time.Duration) bool {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case ev := <-r.Events():
			t.Logf("event type=%s code=%d err=%v data=%q", ev.Type, ev.ExitCode, ev.Err, ev.Data)
			if ev.SessionID != sessionID {
				continue
			}
			if needle == "" {
				if ev.Type == EventOutput && len(ev.Data) > 0 {
					return true
				}
				continue
			}
			if strings.Contains(string(ev.Data), needle) {
				return true
			}
		case <-deadline:
			return false
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
