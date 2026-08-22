//go:build windows

package winexec

import (
	"context"
	"os/exec"
	"syscall"
)

const createNoWindow = 0x08000000

// HiddenPowerShell runs powershell.exe without spawning a visible console window.
func HiddenPowerShell(ctx context.Context, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "powershell.exe", args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: createNoWindow,
		HideWindow:    true,
	}
	return cmd
}
