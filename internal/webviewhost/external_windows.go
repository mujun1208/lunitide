//go:build windows

package webviewhost

import (
	"fmt"
	"syscall"
	"unsafe"
)

var shellExecuteW = syscall.NewLazyDLL("shell32.dll").NewProc("ShellExecuteW")

func openSystemBrowser(url string) error {
	operation, err := syscall.UTF16PtrFromString("open")
	if err != nil {
		return err
	}
	target, err := syscall.UTF16PtrFromString(url)
	if err != nil {
		return err
	}
	result, _, callErr := shellExecuteW.Call(0, uintptr(unsafe.Pointer(operation)), uintptr(unsafe.Pointer(target)), 0, 0, 1)
	if result <= 32 {
		return fmt.Errorf("ShellExecuteW failed: result=%d: %w", result, callErr)
	}
	return nil
}

// OpenMicrophonePrivacySettings opens a fixed Windows settings page. The URI
// is host-owned so renderer input can never select an arbitrary shell target.
func OpenMicrophonePrivacySettings() error {
	return openSystemBrowser("ms-settings:privacy-microphone")
}
