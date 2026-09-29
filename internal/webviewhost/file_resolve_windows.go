//go:build windows

package webviewhost

import (
	"syscall"
	"unsafe"

	"github.com/zzl/go-webview2/wv2"
	"github.com/zzl/go-win32api/v2/win32"
)

// The pinned go-webview2 predates these interfaces; IIDs and vtable slots are
// from WebView2.h in Microsoft.Web.WebView2 1.0.3537.50.
var (
	iidWebMessageReceivedEventArgs2 = syscall.GUID{Data1: 0x06fc7ab7, Data2: 0xc90c, Data3: 0x4297, Data4: [8]byte{0x93, 0x89, 0x33, 0xca, 0x01, 0xcf, 0x6d, 0x5e}}
	iidCoreWebView2File             = syscall.GUID{Data1: 0xf2c19559, Data2: 0x6bc1, Data3: 0x4583, Data4: [8]byte{0xa7, 0x57, 0x90, 0x02, 0x1b, 0xe9, 0xaf, 0xec}}
)

const (
	slotQueryInterface       = 0
	slotRelease              = 2
	slotGetAdditionalObjects = 6 // ICoreWebView2WebMessageReceivedEventArgs2
	slotCollectionCount      = 3 // ICoreWebView2ObjectCollectionView
	slotCollectionAt         = 4
	slotFilePath             = 3 // ICoreWebView2File
	maxResolvedFiles         = 20 // desktopfiles.GrantPaths cap
)

func comCall(obj unsafe.Pointer, slot uintptr, args ...uintptr) win32.HRESULT {
	vtbl := *(*unsafe.Pointer)(obj)
	method := *(*uintptr)(unsafe.Add(vtbl, slot*unsafe.Sizeof(uintptr(0))))
	r, _, _ := syscall.SyscallN(method, append([]uintptr{uintptr(obj)}, args...)...)
	return win32.HRESULT(r)
}

func comRelease(obj unsafe.Pointer) {
	if obj != nil {
		comCall(obj, slotRelease)
	}
}

// webMessageFilePaths returns the paths of File objects posted with
// chrome.webview.postMessageWithAdditionalObjects, in posting order.
func webMessageFilePaths(args *wv2.ICoreWebView2WebMessageReceivedEventArgs) []string {
	var args2 unsafe.Pointer
	if err := queryUnknown(&args.IUnknown, &iidWebMessageReceivedEventArgs2, unsafe.Pointer(&args2)); err != nil || args2 == nil {
		return nil
	}
	defer comRelease(args2)
	var collection unsafe.Pointer
	if failed(comCall(args2, slotGetAdditionalObjects, uintptr(unsafe.Pointer(&collection)))) || collection == nil {
		return nil
	}
	defer comRelease(collection)
	var count uint32
	if failed(comCall(collection, slotCollectionCount, uintptr(unsafe.Pointer(&count)))) {
		return nil
	}
	if count > maxResolvedFiles {
		count = maxResolvedFiles
	}
	paths := make([]string, 0, count)
	for i := uint32(0); i < count; i++ {
		var item unsafe.Pointer
		if failed(comCall(collection, slotCollectionAt, uintptr(i), uintptr(unsafe.Pointer(&item)))) || item == nil {
			continue
		}
		var file unsafe.Pointer
		hr := comCall(item, slotQueryInterface, uintptr(unsafe.Pointer(&iidCoreWebView2File)), uintptr(unsafe.Pointer(&file)))
		comRelease(item)
		if failed(hr) || file == nil {
			continue
		}
		var path win32.PWSTR
		hr = comCall(file, slotFilePath, uintptr(unsafe.Pointer(&path)))
		comRelease(file)
		if failed(hr) || path == nil {
			continue
		}
		paths = append(paths, win32.PwstrToStr(path))
		win32.CoTaskMemFree(unsafe.Pointer(path))
	}
	return paths
}
