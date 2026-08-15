//go:build windows

package stdioworker

import (
	"fmt"
	"os/exec"

	"golang.org/x/sys/windows"
)

// makeJunction creates a directory junction (needs no privilege on NTFS).
func makeJunction(link, target string) error {
	cmd := exec.Command("cmd.exe", "/c", "mklink", "/J", link, target)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("mklink /J: %v: %s", err, out)
	}
	return nil
}

// pidsAlive returns the subset of pids that are still running.
func pidsAlive(pids []int) []int {
	var alive []int
	for _, pid := range pids {
		if pid <= 0 {
			continue
		}
		h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
		if err != nil {
			continue
		}
		var code uint32
		err = windows.GetExitCodeProcess(h, &code)
		windows.CloseHandle(h)
		if err == nil && code == uint32(windows.STATUS_PENDING) {
			alive = append(alive, pid)
		}
	}
	return alive
}

var swVirtualAlloc = windows.NewLazySystemDLL("kernel32.dll").NewProc("VirtualAlloc")

// rtAllocProbe commits memory in 16 MiB chunks until want bytes or the OS
// refuses (the Job Object commit cap). MEM_RESERVE|MEM_COMMIT charges the
// job commit at call time, so the refusal is observable as a NULL return
// and the child can report through the RESULT frame instead of dying to
// an OOM kill; the mappings stay alive until the process exits.
func rtAllocProbe(want int64) (committed int64, failed bool, detail string) {
	const chunk = 16 << 20
	for committed < want {
		addr, _, e := swVirtualAlloc.Call(0, uintptr(chunk), windows.MEM_RESERVE|windows.MEM_COMMIT, windows.PAGE_READWRITE)
		if addr == 0 {
			return committed, true, fmt.Sprintf("virtual alloc refused after %d MiB: %v", committed>>20, e)
		}
		committed += chunk
	}
	return committed, false, fmt.Sprintf("committed %d MiB as asked", committed>>20)
}
