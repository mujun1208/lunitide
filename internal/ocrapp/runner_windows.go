//go:build windows

package ocrapp

import (
	"os/exec"
	"syscall"
)

func hideOCRWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
}
