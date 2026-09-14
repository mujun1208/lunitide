//go:build !windows

package main

import (
	"os"
	"os/exec"
)

func configureEngineProcess(_ *exec.Cmd) {}

func processAlive(pid int) bool {
	// Unix FindProcess always succeeds for any pid; without a Wait/signal
	// probe we cannot know liveness. Refuse to claim "alive" so D11 does
	// not skip relaunch on stubs.
	return false
}

func isEngineImage(pid int) bool {
	return pid > 0
}

func stopEnginePID(pid int, requireEngineImage bool) {
	if pid < 1 {
		return
	}
	if requireEngineImage && !isEngineImage(pid) {
		return
	}
	if proc, err := os.FindProcess(pid); err == nil {
		_ = proc.Kill()
	}
}
