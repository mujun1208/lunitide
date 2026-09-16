package agenthub

import (
	"context"
	"errors"
	"os/exec"
	"testing"
	"time"
)

func TestInstallDoesNotRunUntilConfirmed(t *testing.T) {
	ran := false
	got, err := InstallAndConnect(context.Background(), "cursor", false, func(string) (string, error) { return "", exec.ErrNotFound }, nil, func(context.Context, string, ...string) (string, error) {
		ran = true
		return "", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if ran {
		t.Fatal("must not install before confirm")
	}
	if got.Connected || got.Installed {
		t.Fatalf("%+v", got)
	}
}

func TestInstallRunsLocalRecipeThenConnects(t *testing.T) {
	prev := probeCodexAppServer
	probeCodexAppServer = func(LookPath) bool { return false }
	t.Cleanup(func() { probeCodexAppServer = prev })
	installed := false
	look := func(name string) (string, error) {
		if installed && name == "cursor-agent" {
			return `C:\cursor-agent.exe`, nil
		}
		return "", exec.ErrNotFound
	}
	got, err := InstallAndConnect(context.Background(), "cursor", true, look, func(string, time.Duration) (string, error) {
		return "v1", nil
	}, func(context.Context, string, ...string) (string, error) {
		installed = true
		return "", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Installed || !got.Connected {
		t.Fatalf("%+v", got)
	}
}

func TestInstallLogsInWhenUnsigned(t *testing.T) {
	prev := probeCodexAppServer
	probeCodexAppServer = func(LookPath) bool { return false }
	t.Cleanup(func() { probeCodexAppServer = prev })
	logged := false
	look := func(name string) (string, error) {
		if name == "cursor-agent" {
			return `C:\cursor-agent.exe`, nil
		}
		return "", exec.ErrNotFound
	}
	got, err := InstallAndConnect(context.Background(), "cursor", true, look, func(string, time.Duration) (string, error) {
		if logged {
			return "v1", nil
		}
		return "please login", nil
	}, func(_ context.Context, name string, args ...string) (string, error) {
		if name == `C:\cursor-agent.exe` && len(args) > 0 && args[0] == "login" {
			logged = true
		}
		return "", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Connected {
		t.Fatalf("%+v logged=%v", got, logged)
	}
}

func TestInstallStopsWhenContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ran := false
	_, err := InstallAndConnect(ctx, "cursor", true, func(string) (string, error) { return "", exec.ErrNotFound }, nil, func(context.Context, string, ...string) (string, error) {
		ran = true
		return "", nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
	if ran {
		t.Fatal("canceled install must not start a recipe")
	}
}
