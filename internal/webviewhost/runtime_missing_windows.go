//go:build windows

package webviewhost

import (
	"syscall"
	"unsafe"

	"github.com/zzl/go-win32api/v2/win32"
)

// RuntimeDownloadURL is the official Evergreen Runtime acquisition page the
// installer guidance (ADR-003) and this dialog both point at.
const RuntimeDownloadURL = "https://developer.microsoft.com/microsoft-edge/webview2/#download-section"

var messageBoxW = syscall.NewLazyDLL("user32.dll").NewProc("MessageBoxW")

// showRuntimeMissingDialog is the controlled-failure UX for a missing or
// unusable WebView2 Evergreen Runtime on a console-less windowsgui process:
// without it the window would flash and vanish with no actionable message.
func showRuntimeMissingDialog(hwnd win32.HWND) {
	text, err := syscall.UTF16PtrFromString("Lunitide needs the Microsoft Edge WebView2 Runtime, but it is not installed or the installed version cannot be used.\n\nDownload the Evergreen Runtime from Microsoft, then start Lunitide again:\n" + RuntimeDownloadURL)
	if err != nil {
		return
	}
	title, err := syscall.UTF16PtrFromString("Lunitide - WebView2 Runtime missing")
	if err != nil {
		return
	}
	messageBoxW.Call(
		uintptr(hwnd),
		uintptr(unsafe.Pointer(text)),
		uintptr(unsafe.Pointer(title)),
		0x00000000|0x00000030|0x00001000|0x00010000, // MB_OK | MB_ICONWARNING | MB_SYSTEMMODAL | MB_SETFOREGROUND
	)
}
