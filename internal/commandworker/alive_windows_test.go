//go:build windows

package commandworker

import "golang.org/x/sys/windows"

// processAlive reports whether a pid currently refers to a running process.
// After the worker closes its process handles, OpenProcess on a reaped pid
// fails, which is exactly the post-kill state the tree test asserts.
func processAlive(pid int) bool {
	const processQueryLimitedInformation = 0x1000
	h, err := windows.OpenProcess(processQueryLimitedInformation, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	var code uint32
	if err := windows.GetExitCodeProcess(h, &code); err != nil {
		return false
	}
	const stillActive = 259
	return code == stillActive
}
