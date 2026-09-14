//go:build windows

package desktopupdate

import (
	"os/exec"
	"syscall"
)

const (
	createNoWindow         = 0x08000000
	createNewProcessGroup  = 0x00000200
	createBreakawayFromJob = 0x01000000
)

func startDetached(cmd *exec.Cmd) error {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= createNoWindow | createNewProcessGroup | createBreakawayFromJob
	cmd.SysProcAttr.HideWindow = true
	return cmd.Start()
}
