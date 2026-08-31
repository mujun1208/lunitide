package main

import (
	"errors"
	"os/exec"
)

const (
	flagTakeover        = "--takeover"
	gatewayTakeoverWait = 15 // seconds; backup if the dying host forgets to drop the mutex
)

var errNoDesktopSelf = errors.New("no desktop executable path")

// engineWatchShouldRelaunch is D11 after G1: killing the tray/window must
// leave a live engine alone. Only a dead engine (RPC poison or PID gone)
// makes the desktop/tray spawn a replacement.
func engineWatchShouldRelaunch(shuttingDown, rpcBroken bool, pid int, pidAlive bool) bool {
	if shuttingDown {
		return false
	}
	if rpcBroken {
		return true
	}
	if pid > 0 && !pidAlive {
		return true
	}
	return false
}

// desktopTakeoverArgs is D11: the replacement desktop must wait for this
// process to drop the singleton mutex. Starting a sibling without --takeover
// hits "already running", activates the dying window, and exits — then this
// process stopHost() and nothing is left to respawn the engine.
func desktopTakeoverArgs(args []string) []string {
	out := make([]string, 0, len(args)+1)
	for _, arg := range args {
		if arg == flagTakeover {
			continue
		}
		out = append(out, arg)
	}
	return append(out, flagTakeover)
}

// releaseGatewayThenRelaunch is D11: drop Local\lunitide-gateway before the
// sibling starts. The 0.4.43 failure was a dying WebView2 still holding the
// mutex for longer than the child would wait, so --takeover timed out and
// nothing respawned the engine.
func releaseGatewayThenRelaunch(self string, args []string, release func(), start func(*exec.Cmd) error) error {
	if release != nil {
		release()
	}
	if self == "" {
		return errNoDesktopSelf
	}
	if start == nil {
		start = func(cmd *exec.Cmd) error { return cmd.Start() }
	}
	return start(exec.Command(self, desktopTakeoverArgs(args)...))
}
