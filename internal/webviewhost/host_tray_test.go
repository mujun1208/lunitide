//go:build windows

package webviewhost

import (
	"testing"

	"github.com/zzl/go-win32api/v2/win32"
)

func TestTrayCallbackEventUsesLowWord(t *testing.T) {
	// NOTIFYICON_VERSION_4 packs the mouse message in LOWORD and the icon
	// id in HIWORD. Classic packing stores the message in the full lParam.
	tests := []struct {
		name   string
		lParam win32.LPARAM
		want   uint32
	}{
		{name: "classic right-up", lParam: win32.LPARAM(win32.WM_RBUTTONUP), want: uint32(win32.WM_RBUTTONUP)},
		{name: "classic context", lParam: win32.LPARAM(win32.WM_CONTEXTMENU), want: uint32(win32.WM_CONTEXTMENU)},
		{name: "v4 right-click", lParam: win32.LPARAM(win32.WM_CONTEXTMENU) | (trayIconID << 16), want: uint32(win32.WM_CONTEXTMENU)},
		{name: "v4 left-select", lParam: win32.LPARAM(ninSelect) | (trayIconID << 16), want: uint32(ninSelect)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := trayCallbackEvent(tc.lParam); got != tc.want {
				t.Fatalf("trayCallbackEvent()=%#x want %#x", got, tc.want)
			}
		})
	}
}

func TestIsTrayContextMenu(t *testing.T) {
	if !isTrayContextMenu(uint32(win32.WM_CONTEXTMENU)) {
		t.Fatal("WM_CONTEXTMENU should open the tray menu")
	}
	if !isTrayContextMenu(uint32(win32.WM_RBUTTONUP)) {
		t.Fatal("WM_RBUTTONUP should open the tray menu")
	}
	if isTrayContextMenu(uint32(win32.WM_LBUTTONUP)) {
		t.Fatal("left-click must not open the tray menu")
	}
}

func TestIsTrayActivate(t *testing.T) {
	if !isTrayActivate(uint32(win32.WM_LBUTTONUP)) {
		t.Fatal("WM_LBUTTONUP should restore the window")
	}
	if !isTrayActivate(ninSelect) {
		t.Fatal("NIN_SELECT should restore the window")
	}
	if isTrayActivate(uint32(win32.WM_RBUTTONUP)) {
		t.Fatal("right-click must not restore the window")
	}
}
