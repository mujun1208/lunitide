//go:build windows

package toolruntime

import (
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	desktopReadObjects = 0x0001
	uoiName            = 2
	errorAccessDenied  = 5
)

var (
	user32                       = windows.NewLazySystemDLL("user32.dll")
	procOpenInputDesktop         = user32.NewProc("OpenInputDesktop")
	procCloseDesktop             = user32.NewProc("CloseDesktop")
	procGetUserObjectInformation = user32.NewProc("GetUserObjectInformationW")
)

func workstationLocked() bool {
	h, _, err := procOpenInputDesktop.Call(0, 0, desktopReadObjects)
	if h == 0 {
		if errno, ok := err.(syscall.Errno); ok && errno == errorAccessDenied {
			return true
		}
		return false
	}
	defer procCloseDesktop.Call(h)
	var buf [128]uint16
	var needed uint32
	ok, _, _ := procGetUserObjectInformation.Call(h, uoiName, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)*2), uintptr(unsafe.Pointer(&needed)))
	if ok == 0 {
		return false
	}
	name := strings.TrimSpace(windows.UTF16ToString(buf[:]))
	return name != "" && !strings.EqualFold(name, "Default")
}
