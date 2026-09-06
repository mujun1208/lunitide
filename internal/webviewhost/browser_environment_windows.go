//go:build windows

package webviewhost

import (
	"syscall"
	"unsafe"

	"github.com/zzl/go-com/com"
	"github.com/zzl/go-webview2/wv2"
	"github.com/zzl/go-win32api/v2/win32"
)

// Per-environment options never mutate the process-wide WebView environment.
// Returned strings follow the WebView2 CoTaskMemFree ownership contract.
type browserEnvironmentImpl struct {
	com.IUnknownImpl
	arguments, version string
}

type browserEnvironmentObject struct{ com.IUnknownComObj }
type browserEnvironmentVtbl struct {
	win32.IUnknownVtbl
	arguments, setArguments, language, setLanguage, version, setVersion, sso, setSSO uintptr
}

var browserEnvironmentTable *browserEnvironmentVtbl

func (o *browserEnvironmentObject) IID() *syscall.GUID {
	return &wv2.IID_ICoreWebView2EnvironmentOptions
}
func (o *browserEnvironmentObject) GetVtbl() *win32.IUnknownVtbl {
	com.MuVtbl.Lock()
	defer com.MuVtbl.Unlock()
	if browserEnvironmentTable == nil {
		browserEnvironmentTable = &browserEnvironmentVtbl{
			IUnknownVtbl: *o.BuildVtbl(false),
			arguments:    syscall.NewCallback((*browserEnvironmentObject).arguments),
			setArguments: syscall.NewCallback((*browserEnvironmentObject).readOnly),
			language:     syscall.NewCallback((*browserEnvironmentObject).language),
			setLanguage:  syscall.NewCallback((*browserEnvironmentObject).readOnly),
			version:      syscall.NewCallback((*browserEnvironmentObject).version),
			setVersion:   syscall.NewCallback((*browserEnvironmentObject).readOnly),
			sso:          syscall.NewCallback((*browserEnvironmentObject).sso),
			setSSO:       syscall.NewCallback((*browserEnvironmentObject).readOnly),
		}
	}
	return &browserEnvironmentTable.IUnknownVtbl
}

func environmentString(out *win32.PWSTR, value string) uintptr {
	if out == nil {
		return uintptr(uint32(0x80004003))
	}
	*out = nil
	encoded, err := syscall.UTF16FromString(value)
	if err != nil {
		return uintptr(uint32(0x80070057))
	}
	allocated := win32.CoTaskMemAlloc(uintptr(len(encoded) * 2))
	if allocated == nil {
		return uintptr(uint32(0x8007000e))
	}
	copy(unsafe.Slice((*uint16)(allocated), len(encoded)), encoded)
	*out = (*uint16)(allocated)
	return 0
}
func (o *browserEnvironmentObject) arguments(out *win32.PWSTR) uintptr {
	return environmentString(out, o.Impl().(*browserEnvironmentImpl).arguments)
}
func (o *browserEnvironmentObject) language(out *win32.PWSTR) uintptr {
	return environmentString(out, "")
}
func (o *browserEnvironmentObject) version(out *win32.PWSTR) uintptr {
	return environmentString(out, o.Impl().(*browserEnvironmentImpl).version)
}
func (o *browserEnvironmentObject) sso(out *int32) uintptr {
	if out == nil {
		return uintptr(uint32(0x80004003))
	}
	*out = 0
	return 0
}
func (o *browserEnvironmentObject) readOnly(_ uintptr) uintptr { return uintptr(uint32(0x80070005)) }
func newBrowserEnvironmentOptions(arguments, version string) *wv2.ICoreWebView2EnvironmentOptions {
	o := com.NewComObj[browserEnvironmentObject](&browserEnvironmentImpl{arguments: arguments, version: version})
	return (*wv2.ICoreWebView2EnvironmentOptions)(unsafe.Pointer(o))
}
