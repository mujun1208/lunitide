//go:build windows

package webviewhost

import (
	"github.com/zzl/go-com/com"
	"github.com/zzl/go-webview2/wv2"
	"syscall"
	"unsafe"
)

// These two bindings in go-webview2 pass the output's **T to AddToScope.
// go-com does not recognize **T and prints "?". BrowserHost already owns and
// explicitly releases these results, so call the same ABI without auto-scope.
func getBrowserCoreOwned(controller *wv2.ICoreWebView2Controller, out **wv2.ICoreWebView2) com.Error {
	if controller == nil || out == nil {
		return com.Error(-2147467261)
	}
	*out = nil
	result, _, _ := syscall.SyscallN((*controller.LpVtbl)[25], uintptr(unsafe.Pointer(controller)), uintptr(unsafe.Pointer(out)))
	return com.Error(result)
}
func getBrowserSettingsOwned(core *wv2.ICoreWebView2, out **wv2.ICoreWebView2Settings) com.Error {
	if core == nil || out == nil {
		return com.Error(-2147467261)
	}
	*out = nil
	result, _, _ := syscall.SyscallN((*core.LpVtbl)[3], uintptr(unsafe.Pointer(core)), uintptr(unsafe.Pointer(out)))
	return com.Error(result)
}
