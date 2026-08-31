//go:build windows

package main

import (
	"os"
	"os/exec"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"
)

const (
	createNoWindow         = 0x08000000
	createNewProcessGroup  = 0x00000200
	createBreakawayFromJob = 0x01000000
	stillActive            = 259
)

func configureEngineProcess(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: createNoWindow | createNewProcessGroup | createBreakawayFromJob,
		HideWindow:    true,
	}
}

func processAlive(pid int) bool {
	if pid < 1 {
		return false
	}
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(handle)
	var code uint32
	if err := windows.GetExitCodeProcess(handle, &code); err != nil {
		return false
	}
	return code == stillActive
}

func processImageName(pid int) string {
	if pid < 1 {
		return ""
	}
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(handle)
	var n uint32 = 32768
	buf := make([]uint16, n)
	if err := windows.QueryFullProcessImageName(handle, 0, &buf[0], &n); err != nil {
		return ""
	}
	return windows.UTF16ToString(buf[:n])
}

func isEngineImage(pid int) bool {
	name := strings.ToLower(processImageName(pid))
	return strings.Contains(name, "lunitide-engine")
}

func stopEnginePID(pid int, requireEngineImage bool) {
	if pid < 1 {
		return
	}
	if requireEngineImage && !isEngineImage(pid) {
		return
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return
	}
	_ = proc.Kill()
}
