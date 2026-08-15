//go:build windows

package stdiopoc

import (
	"fmt"
	"os"
	"os/exec"

	"golang.org/x/sys/windows"
)

const stillActive = 259 // NTSTATUS STATUS_PENDING / STILL_ACTIVE

// pidAlive reports whether a process id still has a live kernel object.
// Used by the proctree cross-check: after TerminateJobObject every pid the
// probe spawned must be gone.
func pidAlive(pid int) bool {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	var code uint32
	if err := windows.GetExitCodeProcess(h, &code); err != nil {
		return false
	}
	return code == stillActive
}

// makeJunction creates a directory junction pointing at target (fallback
// when os.Symlink is unavailable without developer mode).
func makeJunction(link, target string) error {
	cmd := exec.Command("cmd.exe", "/c", "mklink", "/J", link, target)
	cmd.Env = MinimalEnv(os.Environ(), []string{"SystemRoot", "PATH", "PATHEXT", "COMSPEC", "SYSTEMDRIVE", "WINDIR"}, nil)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("mklink /J: %v: %s", err, out)
	}
	return nil
}
