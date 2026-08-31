//go:build !windows

package main

import (
	"os"
	"os/exec"
)

func configureEngineProcess(_ *exec.Cmd) {}

func processAlive(pid int) bool {
	if pid < 1 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc != nil
}

func stopEnginePID(pid int, _ bool) {
	if pid < 1 {
		return
	}
	if proc, err := os.FindProcess(pid); err == nil {
		_ = proc.Kill()
	}
}
