//go:build windows

package ccapp

import (
	"fmt"
	"time"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	cfUnicodeText = 13
	gmemMoveable  = 0x0002
)

var (
	procOpenClipboard    = user32.NewProc("OpenClipboard")
	procCloseClipboard   = user32.NewProc("CloseClipboard")
	procEmptyClipboard   = user32.NewProc("EmptyClipboard")
	procGetClipboardData = user32.NewProc("GetClipboardData")
	procSetClipboardData = user32.NewProc("SetClipboardData")
	procGlobalAlloc      = kernel32.NewProc("GlobalAlloc")
	procGlobalLock       = kernel32.NewProc("GlobalLock")
	procGlobalUnlock     = kernel32.NewProc("GlobalUnlock")
	procGlobalFree       = kernel32.NewProc("GlobalFree")
)

func (h *windowsHost) ClipboardGet() (string, error) {
	if err := openClipboard(); err != nil {
		return "", err
	}
	defer procCloseClipboard.Call()
	handle, _, _ := procGetClipboardData.Call(cfUnicodeText)
	if handle == 0 {
		// Text-only: CF_HDROP / bitmaps / other formats are ignored.
		return "", nil
	}
	ptr, _, _ := procGlobalLock.Call(handle)
	if ptr == 0 {
		return "", fmt.Errorf("globallock failed")
	}
	defer procGlobalUnlock.Call(handle)
	return clampClipboard(windows.UTF16PtrToString((*uint16)(ptrFromUintptr(ptr)))), nil
}

func (h *windowsHost) ClipboardSet(text string) error {
	units := utf16.Encode([]rune(text + "\x00"))
	if err := openClipboard(); err != nil {
		return err
	}
	defer procCloseClipboard.Call()
	if ok, _, err := procEmptyClipboard.Call(); ok == 0 {
		return fmt.Errorf("emptyclipboard: %w", err)
	}
	bytes := len(units) * 2
	mem, _, err := procGlobalAlloc.Call(gmemMoveable, uintptr(bytes))
	if mem == 0 {
		return fmt.Errorf("globalalloc: %w", err)
	}
	ptr, _, _ := procGlobalLock.Call(mem)
	if ptr == 0 {
		procGlobalFree.Call(mem)
		return fmt.Errorf("globallock failed")
	}
	copy(unsafe.Slice((*uint16)(ptrFromUintptr(ptr)), len(units)), units)
	procGlobalUnlock.Call(mem)
	if ok, _, err := procSetClipboardData.Call(cfUnicodeText, mem); ok == 0 {
		procGlobalFree.Call(mem)
		return fmt.Errorf("setclipboard: %w", err)
	}
	return nil
}

func openClipboard() error {
	var err error
	for i := 0; i < 8; i++ {
		ok, _, e := procOpenClipboard.Call(0)
		if ok != 0 {
			return nil
		}
		err = e
		time.Sleep(time.Duration(8*(i+1)) * time.Millisecond)
	}
	return fmt.Errorf("openclipboard: %w", err)
}
