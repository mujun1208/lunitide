//go:build windows

package webviewhost

import (
	"fmt"
	"syscall"
	"unsafe"
)

var shellExecuteW = syscall.NewLazyDLL("shell32.dll").NewProc("ShellExecuteW")

func openSystemBrowser(url string) error {
	operation, target := syscall.StringToUTF16Ptr("open"), syscall.StringToUTF16Ptr(url)
	result, _, callErr := shellExecuteW.Call(0, uintptr(unsafe.Pointer(operation)), uintptr(unsafe.Pointer(target)), 0, 0, 1)
	if result <= 32 {
		return fmt.Errorf("ShellExecuteW failed: result=%d: %w", result, callErr)
	}
	return nil
}
