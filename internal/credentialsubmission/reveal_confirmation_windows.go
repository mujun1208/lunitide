//go:build windows

package credentialsubmission

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"syscall"
	"unsafe"
)

const (
	messageBoxYesNo         = 0x00000004
	messageBoxIconWarning   = 0x00000030
	messageBoxDefaultNo     = 0x00000100
	messageBoxTaskModal     = 0x00002000
	messageBoxSetForeground = 0x00010000
	messageBoxResultYes     = 6
)

var messageBoxW = syscall.NewLazyDLL("user32.dll").NewProc("MessageBoxW")
var revealConfirmationMu sync.Mutex

func confirmCredentialRevealNative(ctx context.Context, target RevealTarget) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	revealConfirmationMu.Lock()
	defer revealConfirmationMu.Unlock()
	if err := ctx.Err(); err != nil {
		return false, err
	}
	idSuffix := target.ProviderID
	if len(idSuffix) > 8 {
		idSuffix = idSuffix[len(idSuffix)-8:]
	}
	message := fmt.Sprintf("Lunitide 请求显示已保存的 API Key。\n\n协议：%s\n目标：%s\nProvider ID：…%s\n\n仅当这是您刚刚发起的操作时选择“是”。", target.Protocol, target.Origin, idSuffix)
	text, err := syscall.UTF16PtrFromString(message)
	if err != nil {
		return false, errors.New("native credential reveal confirmation unavailable")
	}
	title, err := syscall.UTF16PtrFromString("确认显示 API Key")
	if err != nil {
		return false, errors.New("native credential reveal confirmation unavailable")
	}
	result, _, callErr := messageBoxW.Call(
		0,
		uintptr(unsafe.Pointer(text)),
		uintptr(unsafe.Pointer(title)),
		messageBoxYesNo|messageBoxIconWarning|messageBoxDefaultNo|messageBoxTaskModal|messageBoxSetForeground,
	)
	if result == 0 {
		if callErr != nil && callErr != syscall.Errno(0) {
			return false, errors.New("native credential reveal confirmation failed")
		}
		return false, errors.New("native credential reveal confirmation failed")
	}
	return result == messageBoxResultYes, nil
}
