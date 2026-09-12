//go:build windows

package officestudio

import (
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	gdi32              = windows.NewLazySystemDLL("gdi32.dll")
	user32             = windows.NewLazySystemDLL("user32.dll")
	procGetDC          = user32.NewProc("GetDC")
	procReleaseDC      = user32.NewProc("ReleaseDC")
	procCreateFontW    = gdi32.NewProc("CreateFontW")
	procSelectObject   = gdi32.NewProc("SelectObject")
	procDeleteObject   = gdi32.NewProc("DeleteObject")
	procGetTextExtent  = gdi32.NewProc("GetTextExtentPoint32W")
	defaultGlyphExtent = windowsGlyphExtent
)

type gdiSize struct {
	cx int32
	cy int32
}

func windowsGlyphExtent(text, fontFamily string) (int, bool) {
	text = strings.TrimSpace(text)
	fontFamily = strings.TrimSpace(fontFamily)
	if text == "" || fontFamily == "" {
		return 0, false
	}
	dc, _, _ := procGetDC.Call(0)
	if dc == 0 {
		return 0, false
	}
	defer procReleaseDC.Call(0, dc)
	face, err := syscall.UTF16PtrFromString(fontFamily)
	if err != nil {
		return 0, false
	}
	font, _, _ := procCreateFontW.Call(20, 0, 0, 0, 400, 0, 0, 0, 1, 0, 0, 0, 0, uintptr(unsafe.Pointer(face)))
	if font == 0 {
		return 0, false
	}
	defer procDeleteObject.Call(font)
	prev, _, _ := procSelectObject.Call(dc, font)
	if prev == 0 {
		return 0, false
	}
	defer procSelectObject.Call(dc, prev)
	utf16, err := syscall.UTF16FromString(text)
	if err != nil || len(utf16) < 2 {
		return 0, false
	}
	var sz gdiSize
	n := len(utf16) - 1
	ok, _, _ := procGetTextExtent.Call(dc, uintptr(unsafe.Pointer(&utf16[0])), uintptr(n), uintptr(unsafe.Pointer(&sz)))
	if ok == 0 || sz.cx < 1 {
		return 0, false
	}
	return int(sz.cx), true
}
