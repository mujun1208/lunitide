//go:build windows

package command

import (
	"runtime"
	"strings"
	"testing"
	"time"
)

func skipNonWindows(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "windows" {
		t.Skip("command job objects are Windows-only")
	}
}

func TestJobObjectCancel(t *testing.T) {
	skipNonWindows(t)
	// Long-running tree; Cancel must reap it (P95 <= 2s) via the Job
	// Object instead of waiting out the 30-second ping.
	j, err := StartJob([]string{"cmd.exe", "/c", "ping -n 30 127.0.0.1 >nul"}, "", nil, JobOptions{})
	if err != nil {
		t.Fatalf("StartJob: %v", err)
	}
	if j.PidToken() == "" {
		t.Fatal("PidToken must not be empty")
	}
	time.Sleep(200 * time.Millisecond)
	if err := j.Cancel(); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	waited := make(chan struct{})
	go func() {
		_, _ = j.Wait()
		close(waited)
	}()
	select {
	case <-waited:
	case <-time.After(2 * time.Second):
		t.Fatal("Cancel did not reap the tree within 2s")
	}
	// Repeated Cancel is idempotent and reports nil.
	if err := j.Cancel(); err != nil {
		t.Fatalf("second Cancel must be nil, got %v", err)
	}
}

func TestBackgrounding(t *testing.T) {
	skipNonWindows(t)
	j, err := StartJob([]string{"cmd.exe", "/c", "ping -n 5 127.0.0.1 >nul"}, "", nil,
		JobOptions{BackgroundAfterMs: 300})
	if err != nil {
		t.Fatalf("StartJob: %v", err)
	}
	time.Sleep(1 * time.Second)
	bg, reason := j.Backgrounded()
	if !bg || reason != ReasonElapsed {
		t.Fatalf("want backgrounded (true, %q), got (%v, %q)", ReasonElapsed, bg, reason)
	}
	// Manual MarkBackground must not overwrite the recorded reason.
	j.MarkBackground(ReasonOutput)
	if bg, reason = j.Backgrounded(); bg != true || reason != ReasonElapsed {
		t.Fatalf("MarkBackground must be idempotent, got (%v, %q)", bg, reason)
	}
	if _, err := j.Wait(); err != nil {
		t.Fatalf("Wait: %v", err)
	}
}

func TestBackgroundOutput(t *testing.T) {
	skipNonWindows(t)
	// ~22 bytes of output must cross the 16-byte threshold.
	j, err := StartJob([]string{"cmd.exe", "/c", "echo aaaaaaaaaaaaaaaaaaaa"}, "", nil,
		JobOptions{BackgroundAfterBytes: 16})
	if err != nil {
		t.Fatalf("StartJob: %v", err)
	}
	if _, err := j.Wait(); err != nil {
		t.Fatalf("Wait: %v", err)
	}
	bg, reason := j.Backgrounded()
	if !bg || reason != ReasonOutput {
		t.Fatalf("want backgrounded (true, %q), got (%v, %q)", ReasonOutput, bg, reason)
	}
	if !strings.Contains(j.Logs(), "aaaaaaaaaaaaaaaaaaaa") {
		t.Fatalf("logs must capture the echoed output, got %q", j.Logs())
	}
}
