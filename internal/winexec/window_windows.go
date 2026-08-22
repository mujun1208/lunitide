//go:build windows

package winexec

import (
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32Window           = syscall.NewLazyDLL("user32.dll")
	procEnumWindows        = user32Window.NewProc("EnumWindows")
	procIsWindowVisible    = user32Window.NewProc("IsWindowVisible")
	procGetWindowTextWWin  = user32Window.NewProc("GetWindowTextW")
	procSetForegroundWin   = user32Window.NewProc("SetForegroundWindow")
	procShowWindowWin      = user32Window.NewProc("ShowWindow")
	procGetWindowThreadPID = user32Window.NewProc("GetWindowThreadProcessId")
	kernel32Window         = syscall.NewLazyDLL("kernel32.dll")
	procOpenProcessWin     = kernel32Window.NewProc("OpenProcess")
	procCloseHandleWin     = kernel32Window.NewProc("CloseHandle")
	procQueryFullImageWin  = kernel32Window.NewProc("QueryFullProcessImageNameW")
)

const (
	swRestore                 = 9
	processQueryLimitedInfoW  = 0x1000
)

type windowMatch struct {
	fragment string
	hwnd     uintptr
}

func enumWindowsCallback(hwnd uintptr, lParam uintptr) uintptr {
	if hwnd == 0 {
		return 1
	}
	visible, _, _ := procIsWindowVisible.Call(hwnd)
	if visible == 0 {
		return 1
	}
	buf := make([]uint16, 512)
	n, _, _ := procGetWindowTextWWin.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	title := strings.ToLower(windows.UTF16ToString(buf[:n]))
	process := strings.ToLower(windowProcessName(hwnd))
	match := (*windowMatch)(unsafe.Pointer(lParam))
	frag := match.fragment
	if frag == "" {
		return 1
	}
	if strings.Contains(title, frag) || strings.Contains(process, frag) {
		match.hwnd = hwnd
		return 0
	}
	return 1
}

func windowProcessName(hwnd uintptr) string {
	var pid uint32
	_, _, _ = procGetWindowThreadPID.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	if pid == 0 {
		return ""
	}
	handle, _, _ := procOpenProcessWin.Call(processQueryLimitedInfoW, 0, uintptr(pid))
	if handle == 0 {
		return ""
	}
	defer procCloseHandleWin.Call(handle)
	pbuf := make([]uint16, 1024)
	pn := uint32(len(pbuf))
	if ok, _, _ := procQueryFullImageWin.Call(handle, 0, uintptr(unsafe.Pointer(&pbuf[0])), uintptr(unsafe.Pointer(&pn))); ok == 0 {
		return ""
	}
	full := windows.UTF16ToString(pbuf[:pn])
	for i := len(full) - 1; i >= 0; i-- {
		if full[i] == '\\' || full[i] == '/' {
			return full[i+1:]
		}
	}
	return full
}

// ActivateWindowMatching brings the first visible window whose title or
// process name contains fragment (case-insensitive) to the foreground.
func ActivateWindowMatching(fragment string) error {
	frag := strings.ToLower(strings.TrimSpace(fragment))
	if frag == "" {
		return nil
	}
	match := windowMatch{fragment: frag}
	procEnumWindows.Call(
		syscall.NewCallback(enumWindowsCallback),
		uintptr(unsafe.Pointer(&match)),
	)
	if match.hwnd == 0 {
		stem := strings.TrimSuffix(frag, ".lnk")
		stem = strings.TrimSuffix(stem, ".exe")
		if stem != frag {
			match = windowMatch{fragment: stem}
			procEnumWindows.Call(
				syscall.NewCallback(enumWindowsCallback),
				uintptr(unsafe.Pointer(&match)),
			)
		}
	}
	if match.hwnd == 0 {
		return nil
	}
	_, _, _ = procShowWindowWin.Call(match.hwnd, swRestore)
	if ok, _, _ := procSetForegroundWin.Call(match.hwnd); ok == 0 {
		return nil
	}
	return nil
}
