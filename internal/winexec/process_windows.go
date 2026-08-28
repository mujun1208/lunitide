//go:build windows

package winexec

import (
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const processQueryLimitedInfo = 0x1000

var (
	kernel32Process          = syscall.NewLazyDLL("kernel32.dll")
	procOpenProcessImg       = kernel32Process.NewProc("OpenProcess")
	procCloseHandleImg       = kernel32Process.NewProc("CloseHandle")
	procQueryFullProcessImg  = kernel32Process.NewProc("QueryFullProcessImageNameW")
)

// LookupProcessImages returns full paths of running processes whose
// executable base name matches any of names (with or without .exe).
func LookupProcessImages(names []string) []string {
	want := map[string]bool{}
	for _, name := range names {
		base := strings.ToLower(strings.TrimSpace(filepath.Base(name)))
		if base == "" {
			continue
		}
		want[base] = true
		want[strings.TrimSuffix(base, ".exe")] = true
		if !strings.HasSuffix(base, ".exe") {
			want[base+".exe"] = true
		}
	}
	if len(want) == 0 {
		return nil
	}
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil
	}
	defer windows.CloseHandle(snap)
	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	if err := windows.Process32First(snap, &entry); err != nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for {
		exe := strings.ToLower(windows.UTF16ToString(entry.ExeFile[:]))
		stem := strings.TrimSuffix(exe, ".exe")
		if want[exe] || want[stem] {
			if path := processImagePath(entry.ProcessID); path != "" {
				key := strings.ToLower(path)
				if !seen[key] {
					seen[key] = true
					out = append(out, path)
				}
			}
		}
		if err := windows.Process32Next(snap, &entry); err != nil {
			break
		}
	}
	return out
}

func processImagePath(pid uint32) string {
	if pid == 0 {
		return ""
	}
	handle, _, _ := procOpenProcessImg.Call(processQueryLimitedInfo, 0, uintptr(pid))
	if handle == 0 {
		return ""
	}
	defer procCloseHandleImg.Call(handle)
	buf := make([]uint16, 1024)
	n := uint32(len(buf))
	if ok, _, _ := procQueryFullProcessImg.Call(handle, 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&n))); ok == 0 {
		return ""
	}
	return windows.UTF16ToString(buf[:n])
}
