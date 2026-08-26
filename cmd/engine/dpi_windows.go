//go:build windows

package main

import "golang.org/x/sys/windows"

func init() {
	enableProcessDPI()
}

// enableProcessDPI opts the engine into per-monitor v2 awareness so screen
// capture and SendInput share physical desktop pixels (the WebView host
// already does this; without it the engine sees a 96-DPI virtualized screen).
func enableProcessDPI() {
	user32 := windows.NewLazySystemDLL("user32.dll")
	if r, _, _ := user32.NewProc("SetProcessDpiAwarenessContext").Call(^uintptr(3)); r != 0 {
		return
	}
	_, _, _ = user32.NewProc("SetProcessDPIAware").Call()
}
