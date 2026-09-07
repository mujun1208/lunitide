//go:build windows

package videounderstand

import (
	"os/exec"
	"syscall"
)

func hideMediaCommand(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
}
