//go:build !race

// cmd/desktop test binaries pull lunitide.syso into the PE .rsrc section.
// Under CGO+race the external linker (gcc/ld) fails with
// "relocation truncated to fit: IMAGE_REL_AMD64_ADDR32 against `.rsrc'".
// Non-race Quality still runs these tests; the race job skips the package.

package main

import (
	"os/exec"
	"slices"
	"testing"
)

func TestSelfLaunchWatchRelaunchesWhenRPCBroken(t *testing.T) {
	if !engineWatchShouldRelaunch(false, true, 4242, true) {
		t.Fatal("self-launch path must treat poisoned RPC as relaunch even when PID is alive")
	}
}

func TestEngineWatchPreferReconnectWhenPIDAlive(t *testing.T) {
	if !engineWatchPreferReconnect(true, true) {
		t.Fatal("poisoned RPC with a live PID must try in-process reconnect before --takeover")
	}
	if engineWatchPreferReconnect(true, false) {
		t.Fatal("dead PID must not pretend reconnect will work")
	}
	if engineWatchPreferReconnect(false, true) {
		t.Fatal("healthy RPC must not reconnect")
	}
}

func TestEngineWatchShouldRelaunch(t *testing.T) {
	if engineWatchShouldRelaunch(true, true, 12, false) {
		t.Fatal("shutdown must not relaunch")
	}
	if !engineWatchShouldRelaunch(false, true, 12, true) {
		t.Fatal("broken RPC must relaunch")
	}
	if !engineWatchShouldRelaunch(false, false, 12, false) {
		t.Fatal("dead engine pid must relaunch")
	}
	if engineWatchShouldRelaunch(false, false, 12, true) {
		t.Fatal("healthy reconnect must leave the engine")
	}
	if engineWatchShouldRelaunch(false, false, 0, false) {
		t.Fatal("unknown pid is not a death signal")
	}
}

func TestDesktopTakeoverArgsWaitsForDyingHost(t *testing.T) {
	got := desktopTakeoverArgs([]string{"--tray"})
	if !slices.Equal(got, []string{"--tray", flagTakeover}) {
		t.Fatalf("got %v", got)
	}
	got = desktopTakeoverArgs([]string{flagTakeover, "--tray", flagTakeover})
	if !slices.Equal(got, []string{"--tray", flagTakeover}) {
		t.Fatalf("dedupe got %v", got)
	}
	if desktopTakeoverArgs(nil)[0] != flagTakeover {
		t.Fatal("empty args must still pass takeover")
	}
}

func TestReleaseGatewayThenRelaunchDropsMutexBeforeStart(t *testing.T) {
	var order []string
	err := releaseGatewayThenRelaunch("self.exe", []string{"--tray"}, func() {
		order = append(order, "release")
	}, func(cmd *exec.Cmd) error {
		order = append(order, "start")
		if !slices.Equal(cmd.Args[1:], []string{"--tray", flagTakeover}) {
			t.Fatalf("args %v", cmd.Args)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(order, []string{"release", "start"}) {
		t.Fatalf("order %v", order)
	}
}

func TestReleaseGatewayThenRelaunchRequiresSelf(t *testing.T) {
	started := false
	err := releaseGatewayThenRelaunch("", nil, func() {}, func(*exec.Cmd) error {
		started = true
		return nil
	})
	if err == nil || started {
		t.Fatalf("empty self must fail without start, err=%v started=%v", err, started)
	}
}
