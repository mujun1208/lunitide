//go:build !windows

package scheduler

import "os/exec"

func hideNotificationWindow(_ *exec.Cmd) {}
