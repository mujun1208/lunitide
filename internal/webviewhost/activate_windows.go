//go:build windows

package webviewhost

import (
	"github.com/zzl/go-win32api/v2/win32"
)

// ActivateExistingMainWindow shows a running workbench host if one exists.
func ActivateExistingMainWindow() bool {
	hwnd, _ := win32.FindWindow(win32.StrToPwstr(windowClass), win32.StrToPwstr("Lunitide"))
	if hwnd == 0 {
		return false
	}
	win32.ShowWindow(hwnd, win32.SW_SHOW)
	win32.SetForegroundWindow(hwnd)
	return true
}
