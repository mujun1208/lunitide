//go:build windows

package people

import (
	"fmt"
	"syscall"
	"unsafe"
)

var (
	shell32Open    = syscall.NewLazyDLL("shell32.dll")
	procShellExecW = shell32Open.NewProc("ShellExecuteW")
	openPathFn     = openLocalPath
)

func openLocalPath(path string) error {
	operation, err := syscall.UTF16PtrFromString("open")
	if err != nil {
		return err
	}
	target, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	result, _, callErr := procShellExecW.Call(0, uintptr(unsafe.Pointer(operation)), uintptr(unsafe.Pointer(target)), 0, 0, 1)
	if result <= 32 {
		return fmt.Errorf("ShellExecuteW failed: result=%d: %w", result, callErr)
	}
	return nil
}
