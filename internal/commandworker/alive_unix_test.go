//go:build darwin || linux

package commandworker

import "syscall"

// processAlive reports whether a pid currently refers to a running process
// using the kill(pid, 0) probe: nil means alive, ESRCH means gone.
func processAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}
