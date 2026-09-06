//go:build windows

package terminalruntime

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestTerminalAssignmentFailureReapsSuspendedProcess(t *testing.T) {
	previous := assignTerminalProcess
	defer func() { assignTerminalProcess = previous }()
	var pid uint32
	assignTerminalProcess = func(_, process windows.Handle) (bool, error) {
		pid, _ = windows.GetProcessId(process)
		return false, windows.ERROR_ACCESS_DENIED
	}
	_, err := startPlatform(t.TempDir(), 80, 24, func([]byte) {}, func(uint32, error) {})
	if !errors.Is(err, windows.ERROR_ACCESS_DENIED) || pid == 0 {
		t.Fatalf("assignment not rejected: pid=%d err=%v", pid, err)
	}
	h, err := windows.OpenProcess(windows.SYNCHRONIZE, false, pid)
	if err == nil {
		defer windows.CloseHandle(h)
		state, err := windows.WaitForSingleObject(h, 1000)
		if err != nil || state != windows.WAIT_OBJECT_0 {
			t.Fatalf("suspended orphan survived: %d %v", state, err)
		}
	}
}

type blockingInputPlatform struct {
	done chan struct{}
	once sync.Once
}

func (p *blockingInputPlatform) write([]byte) error          { <-p.done; return ErrClosed }
func (p *blockingInputPlatform) resize(uint16, uint16) error { return nil }
func (p *blockingInputPlatform) close() error                { p.once.Do(func() { close(p.done) }); return nil }

func TestTerminalInputCancellationReturnsAndStopsBlockedWriter(t *testing.T) {
	r, err := New(Config{Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	p := &blockingInputPlatform{done: make(chan struct{})}
	s := &session{id: "blocked", p: p}
	r.sessions[s.id] = s
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := r.WriteContext(ctx, s.id, []byte("input")); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("blocked write reported accepted: %v", err)
	}
	select {
	case <-p.done:
	case <-time.After(time.Second):
		t.Fatal("cancelled writer retained shell")
	}
	newPlatform := &lifecyclePlatform{}
	r.mu.Lock()
	r.sessions[s.id] = &session{id: s.id, p: newPlatform}
	r.mu.Unlock()
	if err := r.closeSession(s); !errors.Is(err, ErrNotFound) || newPlatform.closes.Load() != 0 {
		t.Fatal("old timeout closed replacement session")
	}
}

func TestTerminalCloseCanInterruptBlockedInput(t *testing.T) {
	p, err := startPlatform(t.TempDir(), 80, 24, func([]byte) {}, func(uint32, error) {})
	if err != nil {
		t.Fatal(err)
	}
	defer p.close()
	if err := p.write([]byte("Start-Sleep 60\r\n")); err != nil {
		t.Fatal(err)
	}
	written := make(chan struct{})
	go func() { defer close(written); _ = p.write(bytes.Repeat([]byte("x"), 1<<20)) }()
	time.Sleep(30 * time.Millisecond)
	closed := make(chan struct{})
	go func() { defer close(closed); _ = p.close() }()
	select {
	case <-closed:
	case <-time.After(3 * time.Second):
		t.Fatal("close blocked behind input write")
	}
	select {
	case <-written:
	case <-time.After(3 * time.Second):
		t.Fatal("input writer remained blocked after close")
	}
}

type lifecyclePlatform struct{ closes atomic.Int32 }

func (p *lifecyclePlatform) write([]byte) error          { return nil }
func (p *lifecyclePlatform) resize(uint16, uint16) error { return nil }
func (p *lifecyclePlatform) close() error                { p.closes.Add(1); return nil }

func TestTerminalNaturalExitClosesResourcesAndIgnoresLateExit(t *testing.T) {
	r, err := New(Config{Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	p := &lifecyclePlatform{}
	s := &session{id: "test", p: p}
	r.sessions[s.id] = s
	r.exited(s, 0, nil)
	if p.closes.Load() != 1 {
		t.Fatal("natural exit leaked platform")
	}
	r.exited(s, 0, nil)
	if p.closes.Load() != 1 || len(r.events) != 1 {
		t.Fatal("late exit changed finalized session")
	}
}

func TestTerminalOutputLimitReportsFailureAndReclaimsResources(t *testing.T) {
	r, err := New(Config{Workspace: t.TempDir(), MaxOutputBytes: 4})
	if err != nil {
		t.Fatal(err)
	}
	p := &lifecyclePlatform{}
	s := &session{id: "limited", p: p}
	r.sessions[s.id] = s
	r.output(s, []byte("too much output"))
	deadline := time.Now().Add(time.Second)
	for p.closes.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if p.closes.Load() != 1 {
		t.Fatal("output flood kept terminal process running")
	}
	found := false
	for len(r.events) > 0 {
		ev := <-r.events
		if ev.Type == EventError && errors.Is(ev.Err, ErrLimit) {
			found = true
		}
	}
	if !found {
		t.Fatal("output was silently truncated without terminal failure")
	}
}
