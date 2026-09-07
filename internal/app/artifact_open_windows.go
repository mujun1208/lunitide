//go:build windows

package app

import (
	"fmt"
	"syscall"
	"unsafe"
)

var artifactShellExecute = syscall.NewLazyDLL("shell32.dll").NewProc("ShellExecuteW")

func openLocalArtifactPath(path string) error {
	operation, err := syscall.UTF16PtrFromString("open")
	if err != nil {
		return err
	}
	target, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	result, _, callErr := artifactShellExecute.Call(0, uintptr(unsafe.Pointer(operation)), uintptr(unsafe.Pointer(target)), 0, 0, 1)
	if result <= 32 {
		return fmt.Errorf("open artifact failed: result=%d: %w", result, callErr)
	}
	return nil
}
