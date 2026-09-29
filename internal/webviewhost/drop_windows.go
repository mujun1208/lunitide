//go:build windows

package webviewhost

import (
	"sync"
	"sync/atomic"
	"syscall"
	"unsafe"
)

const (
	wmDropFiles = 0x0233
	gwChild     = 5
	gwlpWndProc = ^uintptr(3) // GWLP_WNDPROC == -4
)

var (
	dragAcceptFiles  = shell32.NewProc("DragAcceptFiles")
	dragQueryFileW   = shell32.NewProc("DragQueryFileW")
	dragFinish       = shell32.NewProc("DragFinish")
	setWindowLongPtr = user32Menu.NewProc("SetWindowLongPtrW")
	callWindowProcW  = user32Menu.NewProc("CallWindowProcW")
	getWindow        = user32Menu.NewProc("GetWindow")
	enumChildWindows = user32Menu.NewProc("EnumChildWindows")

	currentDropHost atomic.Pointer[Host]
	dropOriginal    sync.Map
	dropChildProc   = syscall.NewCallback(dropChildWindowProc)
	enumDropChild   = syscall.NewCallback(func(hwnd, _ uintptr) uintptr {
		subclassDropWindow(hwnd)
		return 1
	})
)

func (h *Host) enableFileDrop() {
	if h == nil || h.hwnd == 0 {
		return
	}
	currentDropHost.Store(h)
	acceptFileDrop(uintptr(h.hwnd))
	child, _, _ := getWindow.Call(uintptr(h.hwnd), gwChild)
	if child != 0 {
		subclassDropWindow(child)
	}
	_, _, _ = enumChildWindows.Call(uintptr(h.hwnd), enumDropChild, 0)
}

func acceptFileDrop(hwnd uintptr) {
	if hwnd != 0 {
		_, _, _ = dragAcceptFiles.Call(hwnd, 1)
	}
}

func subclassDropWindow(hwnd uintptr) {
	if hwnd == 0 {
		return
	}
	if _, loaded := dropOriginal.Load(hwnd); loaded {
		acceptFileDrop(hwnd)
		return
	}
	prev, _, _ := setWindowLongPtr.Call(hwnd, gwlpWndProc, dropChildProc)
	if prev == 0 || prev == dropChildProc {
		return
	}
	dropOriginal.Store(hwnd, prev)
	acceptFileDrop(hwnd)
}

func dropChildWindowProc(hwnd, msg, wParam, lParam uintptr) uintptr {
	if msg == wmDropFiles {
		deliverDroppedFiles(wParam)
		return 0
	}
	prev, ok := dropOriginal.Load(hwnd)
	if !ok {
		return 0
	}
	proc, _ := prev.(uintptr)
	if proc == 0 {
		return 0
	}
	ret, _, _ := callWindowProcW.Call(proc, hwnd, msg, wParam, lParam)
	return ret
}

func deliverDroppedFiles(hdrop uintptr) {
	paths := queryDropPaths(hdrop)
	host := currentDropHost.Load()
	if host == nil || host.OnFilesDropped == nil || len(paths) == 0 {
		return
	}
	copied := append([]string(nil), paths...)
	go host.OnFilesDropped(copied)
}

func queryDropPaths(hdrop uintptr) []string {
	if hdrop == 0 {
		return nil
	}
	defer dragFinish.Call(hdrop)
	count, _, _ := dragQueryFileW.Call(hdrop, 0xFFFFFFFF, 0, 0)
	if count > 20 {
		count = 20
	}
	buf := make([]uint16, 32768)
	paths := make([]string, 0, count)
	for i := uintptr(0); i < count; i++ {
		n, _, _ := dragQueryFileW.Call(hdrop, i, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
		if n == 0 {
			continue
		}
		paths = append(paths, syscall.UTF16ToString(buf))
	}
	return paths
}
