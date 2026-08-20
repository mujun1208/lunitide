//go:build windows

package webviewhost

import "github.com/zzl/go-win32api/v2/win32"

const (
	nimSetVersion      = 4
	notifyIconVersion4 = 4
	ninSelect          = 0x0400 // WM_USER; NOTIFYICON_VERSION_4 left-click
	ninKeySelect       = 0x0401
	trayCmdShow        = 1001
	trayCmdExit        = 1002
	tpmLeftAlign       = 0x0000
	tpmRightButton     = 0x0002
	tpmBottomAlign     = 0x0020
	tpmReturnCmd       = 0x0100
)

// trayCallbackEvent extracts the mouse/key event from a Shell_NotifyIcon
// callback. Classic (pre-v4) packing stores the message in lParam; v4 stores
// it in the low word and the icon id in the high word.
func trayCallbackEvent(lParam win32.LPARAM) uint32 {
	return uint32(lParam) & 0xFFFF
}

func isTrayContextMenu(event uint32) bool {
	switch event {
	case win32.WM_CONTEXTMENU, win32.WM_RBUTTONUP:
		return true
	default:
		return false
	}
}

func isTrayActivate(event uint32) bool {
	switch event {
	case win32.WM_LBUTTONUP, win32.WM_LBUTTONDBLCLK, ninSelect, ninKeySelect:
		return true
	default:
		return false
	}
}
