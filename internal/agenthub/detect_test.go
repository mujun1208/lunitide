package agenthub

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDetectMissing(t *testing.T) {
	st := DetectAll(func(string) (string, error) { return "", exec.ErrNotFound }, nil)
	if len(st) != 3 || st[0].State != "not_installed" {
		t.Fatalf("%+v", st)
	}
}

func TestDetectTimeoutStillAvailableWhenMatrixYes(t *testing.T) {
	prev := probeCodexAppServer
	probeCodexAppServer = func(LookPath) bool { return false }
	t.Cleanup(func() { probeCodexAppServer = prev })
	st := detectOne("codex", func(string) (string, error) { return `C:\codex.exe`, nil }, func(string, time.Duration) (string, error) {
		return "", errors.New("timeout")
	})
	if st.State != "available" || !st.NonInteractive {
		t.Fatalf("found CLI must stay runnable even if --version is slow: %+v", st)
	}
}

func TestDetectTimeoutStillAvailableForKimi(t *testing.T) {
	st := detectOne("kimi", func(string) (string, error) { return `C:\kimi.exe`, nil }, func(string, time.Duration) (string, error) {
		return "", errors.New("timeout")
	})
	if st.State != "available" || !st.NonInteractive {
		t.Fatalf("found kimi CLI must stay runnable even if --version is slow: %+v", st)
	}
}

func TestDetectAllRunsAdaptersInParallel(t *testing.T) {
	prev := probeCodexAppServer
	probeCodexAppServer = func(LookPath) bool { return false }
	t.Cleanup(func() { probeCodexAppServer = prev })
	started := time.Now()
	DetectAll(func(string) (string, error) { return `C:\x.exe`, nil }, func(string, time.Duration) (string, error) {
		time.Sleep(800 * time.Millisecond)
		return "v", nil
	})
	if time.Since(started) > 2*time.Second {
		t.Fatal("detect must probe adapters in parallel")
	}
}

func TestLookPrefersCmdShimOverBareName(t *testing.T) {
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
	t.Setenv("PATH", filepath.Join(home, "empty-path"))
	t.Setenv("APPDATA", filepath.Join(home, "roaming"))
	t.Setenv("LOCALAPPDATA", filepath.Join(home, "local"))
	npm := filepath.Join(home, "roaming", "npm")
	if err := os.MkdirAll(npm, 0o755); err != nil {
		t.Fatal(err)
	}
	name := "lunitide-hub-cmd-probe"
	bare := filepath.Join(npm, name)
	shim := bare + ".cmd"
	if err := os.WriteFile(bare, []byte("#!/usr/bin/env node\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(shim, []byte("@echo off\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := lookWithCommonPaths(name)
	if err != nil || got != shim {
		t.Fatalf("got %q err=%v want %q", got, err, shim)
	}
}

func TestLookWithCommonPathsFindsUserLocalBin(t *testing.T) {
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
	bin := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	name := "lunitide-hub-look-probe"
	exe := filepath.Join(bin, name+".exe")
	if err := os.WriteFile(exe, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := lookWithCommonPaths(name)
	if err != nil || got != exe {
		t.Fatalf("got %q err=%v want %q", got, err, exe)
	}
}

func TestLookFindsCursorAgentLocalAppData(t *testing.T) {
	home := t.TempDir()
	local := filepath.Join(home, "local")
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
	t.Setenv("PATH", filepath.Join(home, "empty-path"))
	t.Setenv("APPDATA", filepath.Join(home, "roaming"))
	t.Setenv("LOCALAPPDATA", local)
	bin := filepath.Join(local, "cursor-agent")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	shim := filepath.Join(bin, "cursor-agent.cmd")
	if err := os.WriteFile(shim, []byte("@echo off\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := lookWithCommonPaths("cursor-agent")
	if err != nil || got != shim {
		t.Fatalf("got %q err=%v want %q", got, err, shim)
	}
}

func TestLookFindsCursorAgentBesideCursorIDE(t *testing.T) {
	root := t.TempDir()
	t.Setenv("USERPROFILE", filepath.Join(root, "home"))
	t.Setenv("HOME", filepath.Join(root, "home"))
	t.Setenv("LOCALAPPDATA", filepath.Join(root, "local"))
	t.Setenv("APPDATA", filepath.Join(root, "roaming"))
	ideRoot := filepath.Join(root, "DevelopSoftware", "cursor")
	ideBin := filepath.Join(ideRoot, "resources", "app", "bin")
	if err := os.MkdirAll(ideBin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ideBin, "cursor.cmd"), []byte("@echo off\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	agent := filepath.Join(ideRoot, "cursor-agent.cmd")
	if err := os.WriteFile(agent, []byte("@echo off\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", ideBin)
	got, err := lookWithCommonPaths("cursor-agent")
	if err != nil || got != agent {
		t.Fatalf("got %q err=%v want agent beside Cursor IDE %q", got, err, agent)
	}
}

func TestDetectCursorHintWhenOnlyIDEInstalled(t *testing.T) {
	root := t.TempDir()
	t.Setenv("USERPROFILE", filepath.Join(root, "home"))
	t.Setenv("HOME", filepath.Join(root, "home"))
	t.Setenv("LOCALAPPDATA", filepath.Join(root, "local"))
	t.Setenv("APPDATA", filepath.Join(root, "roaming"))
	ideBin := filepath.Join(root, "cursor", "resources", "app", "bin")
	if err := os.MkdirAll(ideBin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ideBin, "cursor.cmd"), []byte("@echo off\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", ideBin)
	st := detectOne("cursor", lookWithCommonPaths, func(string, time.Duration) (string, error) {
		t.Fatal("version must not run when cursor-agent is missing")
		return "", nil
	})
	if st.State != "not_installed" {
		t.Fatalf("state = %s, want not_installed: %+v", st.State, st)
	}
	if !strings.Contains(st.Hint, "Cursor 软件") || !strings.Contains(st.Hint, "cursor-agent") {
		t.Fatalf("hint = %q, want IDE-found / CLI-missing", st.Hint)
	}
}

func TestLookFindsKimiCodePrefixBin(t *testing.T) {
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
	t.Setenv("PATH", filepath.Join(home, "empty-path"))
	t.Setenv("APPDATA", filepath.Join(home, "roaming"))
	t.Setenv("LOCALAPPDATA", filepath.Join(home, "local"))
	bin := filepath.Join(home, ".kimi-code", "node_modules", ".bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	shim := filepath.Join(bin, "kimi.cmd")
	if err := os.WriteFile(shim, []byte("@echo off\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := lookWithCommonPaths("kimi")
	if err != nil || got != shim {
		t.Fatalf("got %q err=%v want %q", got, err, shim)
	}
}

func TestDetectAvailableWhenMatrixAndVersionOK(t *testing.T) {
	prev := probeCodexAppServer
	probeCodexAppServer = func(LookPath) bool { return false }
	t.Cleanup(func() { probeCodexAppServer = prev })
	st := detectOne("codex", func(string) (string, error) { return `C:\codex.exe`, nil }, func(string, time.Duration) (string, error) {
		return "codex-cli 0.1.0", nil
	})
	if st.State != "available" || !st.NonInteractive {
		t.Fatalf("%+v", st)
	}
}
