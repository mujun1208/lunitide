// refhost_test.go covers the auto-host state machine without touching a
// real GPT-SoVITS install: not_configured when no launcher exists, the
// online fast path against an httptest stub, and the launching →
// ErrRefEngineStarting → Stop() tree-kill path driven by a sleeping
// .bat stand-in.
package tts

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRefHostNotConfiguredWhenScriptMissing(t *testing.T) {
	h := NewRefHost(filepath.Join(t.TempDir(), "missing.bat"))
	if state, script := h.Status("http://127.0.0.1:1"); state != RefHostNotConfigured || script != "" {
		t.Fatalf("Status = %q, %q; want not_configured, empty", state, script)
	}
	if err := h.EnsureRunning("http://127.0.0.1:1", 50*time.Millisecond); err == nil {
		t.Fatal("EnsureRunning without a launcher must fail")
	}
	if state, _ := h.Status("http://127.0.0.1:1"); state != RefHostNotConfigured {
		t.Fatalf("state after failed ensure = %q, want not_configured", state)
	}
}

func TestRefHostOnlineFastPathAgainstLiveService(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/docs" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	// A live /docs wins even without any launcher script.
	h := NewRefHost()
	if err := h.EnsureRunning(srv.URL, 50*time.Millisecond); err != nil {
		t.Fatalf("EnsureRunning against live service: %v", err)
	}
	if state, _ := h.Status(srv.URL); state != RefHostOnline {
		t.Fatalf("Status = %q, want online", state)
	}
}

func TestRefHostLaunchingThenStopKillsTree(t *testing.T) {
	if _, err := os.Stat(`C:\Windows\System32\cmd.exe`); err != nil {
		t.Skip("windows-only spawn test")
	}
	dir := t.TempDir()
	// A sleeping stand-in for the model server: ~20s of quiet life that
	// only taskkill /T can end early.
	bat := filepath.Join(dir, "slow-start.bat")
	if err := os.WriteFile(bat, []byte("@echo off\r\nping -n 20 127.0.0.1 > nul\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Dead endpoint → probe never flips online on its own.
	h := NewRefHost(bat)
	err := h.EnsureRunning("http://127.0.0.1:1", 100*time.Millisecond)
	if err == nil || !errors.Is(err, ErrRefEngineStarting) {
		t.Fatalf("EnsureRunning = %v, want ErrRefEngineStarting", err)
	}
	if state, script := h.Status("http://127.0.0.1:1"); state != RefHostLaunching || !strings.EqualFold(script, bat) {
		t.Fatalf("Status = %q, %q; want launching + script", state, script)
	}
	h.Stop()
	if state, _ := h.Status("http://127.0.0.1:1"); state == RefHostLaunching {
		t.Fatal("Stop must clear the launching state")
	}
}

func TestRefHostAdoptsInFlightLaunch(t *testing.T) {
	if _, err := os.Stat(`C:\Windows\System32\cmd.exe`); err != nil {
		t.Skip("windows-only spawn test")
	}
	dir := t.TempDir()
	bat := filepath.Join(dir, "slow-start.bat")
	if err := os.WriteFile(bat, []byte("@echo off\r\nping -n 20 127.0.0.1 > nul\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	h := NewRefHost(bat)
	// First caller starts the launch and times out; second caller must
	// adopt the same tree instead of spawning a second process.
	if err := h.EnsureRunning("http://127.0.0.1:1", 100*time.Millisecond); !errors.Is(err, ErrRefEngineStarting) {
		t.Fatalf("first EnsureRunning = %v", err)
	}
	if err := h.EnsureRunning("http://127.0.0.1:1", 100*time.Millisecond); !errors.Is(err, ErrRefEngineStarting) {
		t.Fatalf("second EnsureRunning = %v, want adopt-in-flight", err)
	}
	h.mu.Lock()
	cmd := h.cmd
	h.mu.Unlock()
	if cmd == nil || cmd.Process == nil {
		t.Fatal("expected an adopted child process")
	}
	h.Stop()
}

func TestRefHostIsLaunchingSkipsDocsOnline(t *testing.T) {
	if _, err := os.Stat(`C:\Windows\System32\cmd.exe`); err != nil {
		t.Skip("windows-only spawn test")
	}
	dir := t.TempDir()
	bat := filepath.Join(dir, "slow-start.bat")
	if err := os.WriteFile(bat, []byte("@echo off\r\nping -n 20 127.0.0.1 > nul\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	h := NewRefHost(bat)
	if h.IsLaunching("http://127.0.0.1:1") {
		t.Fatal("idle host must not report launching")
	}
	if err := h.EnsureRunning("http://127.0.0.1:1", 80*time.Millisecond); !errors.Is(err, ErrRefEngineStarting) {
		t.Fatalf("EnsureRunning = %v", err)
	}
	if !h.IsLaunching("http://127.0.0.1:1") {
		t.Fatal("in-flight spawn must report launching")
	}
	h.Stop()
}

func TestRefHostProcessExitLeavesOfflineWithError(t *testing.T) {
	if _, err := os.Stat(`C:\Windows\System32\cmd.exe`); err != nil {
		t.Skip("windows-only spawn test")
	}
	dir := t.TempDir()
	bat := filepath.Join(dir, "die.bat")
	if err := os.WriteFile(bat, []byte("@echo off\r\nexit /b 1\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	h := NewRefHost(bat)
	err := h.EnsureRunning("http://127.0.0.1:1", 100*time.Millisecond)
	if err == nil {
		t.Fatal("dead launcher must not look ready")
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		state, _ := h.Status("http://127.0.0.1:1")
		if state == RefHostOffline && h.LastErr() != "" {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	state, _ := h.Status("http://127.0.0.1:1")
	t.Fatalf("state=%s lastErr=%q, want offline with a reason", state, h.LastErr())
}

func TestRefMetaCarriesHostState(t *testing.T) {
	// RefPackMeta must embed the host state without panicking on a
	// not_configured host (no launcher, dead endpoint).
	meta := RefPackMeta("http://127.0.0.1:1")
	if meta.HostState == "" {
		t.Fatal("RefMeta.HostState must be populated")
	}
}
