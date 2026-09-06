//go:build windows

package scheduler

import (
	"os/exec"
	"syscall"
)

func hideNotificationWindow(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true} }
