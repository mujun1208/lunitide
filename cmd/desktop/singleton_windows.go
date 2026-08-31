//go:build windows

package main

import (
	"syscall"
	"time"
	"unsafe"
)

var (
	kernel32           = syscall.NewLazyDLL("kernel32.dll")
	procCreateMutexW   = kernel32.NewProc("CreateMutexW")
	procCloseHandle    = kernel32.NewProc("CloseHandle")
	procReleaseMutex   = kernel32.NewProc("ReleaseMutex")
	errAlreadyExists   = syscall.Errno(183)
)

func claimGatewayInstance() (already bool, release func()) {
	name, err := syscall.UTF16PtrFromString("Local\\lunitide-gateway")
	if err != nil {
		return false, func() {}
	}
	handle, _, callErr := procCreateMutexW.Call(0, 0, uintptr(unsafe.Pointer(name)))
	if handle == 0 {
		return false, func() {}
	}
	if callErr == errAlreadyExists {
		_, _, _ = procCloseHandle.Call(handle)
		return true, func() {}
	}
	return false, func() {
		_, _, _ = procReleaseMutex.Call(handle)
		_, _, _ = procCloseHandle.Call(handle)
	}
}

func claimGatewayInstanceRetry(timeout time.Duration) (already bool, release func()) {
	if timeout < 0 {
		timeout = 0
	}
	deadline := time.Now().Add(timeout)
	for {
		already, release = claimGatewayInstance()
		if !already {
			return false, release
		}
		if time.Now().After(deadline) {
			return true, func() {}
		}
		time.Sleep(150 * time.Millisecond)
	}
}
