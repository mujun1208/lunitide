//go:build windows

package winexec

import (
	"fmt"
	"strings"
	"sync"
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
	procGetForegroundWin   = user32Window.NewProc("GetForegroundWindow")
	kernel32Window         = syscall.NewLazyDLL("kernel32.dll")
	procOpenProcessWin     = kernel32Window.NewProc("OpenProcess")
	procCloseHandleWin     = kernel32Window.NewProc("CloseHandle")
	procQueryFullImageWin  = kernel32Window.NewProc("QueryFullProcessImageNameW")
)

const (
	swRestore                = 9
	processQueryLimitedInfoW = 0x1000
)

type windowMatch struct {
	fragment string
	hwnd     uintptr
}

// EnumWindows carries exactly one uintptr of caller state through to the
// callback. Handing it a Go pointer is not sound: the garbage collector does
// not trace a uintptr, so nothing keeps the target alive for the duration of
// the enumeration, and converting it back is the round trip go vet refuses.
// A token into a registry the collector can see costs a map lookup per window
// and removes the hazard.
var (
	windowMatchMu  sync.Mutex
	windowMatchSeq uintptr
	windowMatches  = map[uintptr]*windowMatch{}
)

// enumWindowsCallbackPtr is built once. syscall.NewCallback draws from a
// fixed-size table that is never reclaimed, so minting a callback per
// enumeration exhausts it after a couple of thousand window activations and
// panics — in a process that stays open all day, which this one does.
var enumWindowsCallbackPtr = sync.OnceValue(func() uintptr {
	return syscall.NewCallback(enumWindowsCallback)
})

// enumerateWindows runs one EnumWindows pass against m, which the callback
// fills in on a hit.
func enumerateWindows(m *windowMatch) {
	windowMatchMu.Lock()
	windowMatchSeq++
	token := windowMatchSeq
	windowMatches[token] = m
	windowMatchMu.Unlock()
	defer func() {
		windowMatchMu.Lock()
		delete(windowMatches, token)
		windowMatchMu.Unlock()
	}()
	_, _, _ = procEnumWindows.Call(enumWindowsCallbackPtr(), token)
}

func lookupWindowMatch(token uintptr) *windowMatch {
	windowMatchMu.Lock()
	defer windowMatchMu.Unlock()
	return windowMatches[token]
}

func enumWindowsCallback(hwnd uintptr, lParam uintptr) uintptr {
	if hwnd == 0 {
		return 1
	}
	// Resolved before any per-window work: an unknown token has nowhere to
	// record a hit, so continuing would only burn syscalls.
	match := lookupWindowMatch(lParam)
	if match == nil {
		return 0
	}
	frag := match.fragment
	if frag == "" {
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
	enumerateWindows(&match)
	if match.hwnd == 0 {
		stem := strings.TrimSuffix(frag, ".lnk")
		stem = strings.TrimSuffix(stem, ".exe")
		if stem != frag {
			match = windowMatch{fragment: stem}
			enumerateWindows(&match)
		}
	}
	if match.hwnd == 0 {
		return fmt.Errorf("no visible window matching %q", fragment)
	}
	_, _, _ = procShowWindowWin.Call(match.hwnd, swRestore)
	if ok, _, _ := procSetForegroundWin.Call(match.hwnd); ok == 0 {
		// SetForeground often fails without an input lock; callers must
		// confirm the target via ForegroundWindow / ListVisibleWindows.
		return nil
	}
	return nil
}

type windowList struct {
	items []WindowHint
}

var (
	windowListMu  sync.Mutex
	windowListSeq uintptr
	windowLists   = map[uintptr]*windowList{}
)

var enumWindowsListCallbackPtr = sync.OnceValue(func() uintptr {
	return syscall.NewCallback(enumWindowsListCallback)
})

func enumWindowsListCallback(hwnd uintptr, lParam uintptr) uintptr {
	if hwnd == 0 {
		return 1
	}
	windowListMu.Lock()
	list := windowLists[lParam]
	windowListMu.Unlock()
	if list == nil {
		return 0
	}
	visible, _, _ := procIsWindowVisible.Call(hwnd)
	if visible == 0 {
		return 1
	}
	buf := make([]uint16, 512)
	n, _, _ := procGetWindowTextWWin.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	title := windows.UTF16ToString(buf[:n])
	process := windowProcessName(hwnd)
	if title == "" && process == "" {
		return 1
	}
	list.items = append(list.items, WindowHint{Title: title, Process: process})
	return 1
}

func ForegroundWindow() (title, process string, err error) {
	hwnd, _, _ := procGetForegroundWin.Call()
	if hwnd == 0 {
		return "", "", fmt.Errorf("no foreground window")
	}
	buf := make([]uint16, 512)
	n, _, _ := procGetWindowTextWWin.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return windows.UTF16ToString(buf[:n]), windowProcessName(hwnd), nil
}

func ListVisibleWindows() []WindowHint {
	list := &windowList{}
	windowListMu.Lock()
	windowListSeq++
	token := windowListSeq
	windowLists[token] = list
	windowListMu.Unlock()
	defer func() {
		windowListMu.Lock()
		delete(windowLists, token)
		windowListMu.Unlock()
	}()
	_, _, _ = procEnumWindows.Call(enumWindowsListCallbackPtr(), token)
	return list.items
}
