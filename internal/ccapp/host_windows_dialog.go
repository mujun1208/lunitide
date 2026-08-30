//go:build windows

package ccapp

import (
	"fmt"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	objidClient  = 0xFFFFFFFC
	rolePushBtn  = 0x2B
	roleSplitBtn = 0x3E

	bmClick   = 0x00F5
	wmCommand = 0x0111
	idOK      = 1
	idYes     = 6

	vtI4       = 3
	vtBSTR     = 8
	vtDispatch = 9
)

var (
	oleacc   = windows.NewLazySystemDLL("oleacc.dll")
	oleaut32 = windows.NewLazySystemDLL("oleaut32.dll")
	ole32    = windows.NewLazySystemDLL("ole32.dll")

	procGetClassNameW    = user32.NewProc("GetClassNameW")
	procIsWindow         = user32.NewProc("IsWindow")
	procIsWindowVisible  = user32.NewProc("IsWindowVisible")
	procEnumWindowsCC    = user32.NewProc("EnumWindows")
	procEnumChildWindows = user32.NewProc("EnumChildWindows")
	procSendMessageW     = user32.NewProc("SendMessageW")
	procGetWindowTextLen = user32.NewProc("GetWindowTextLengthW")
	procSetForeground    = user32.NewProc("SetForegroundWindow")

	procAccessibleObjectFromWindow = oleacc.NewProc("AccessibleObjectFromWindow")
	procSysFreeString              = oleaut32.NewProc("SysFreeString")
	procSysAllocString             = oleaut32.NewProc("SysAllocString")
	procCoInitializeEx             = ole32.NewProc("CoInitializeEx")
	procCoUninitialize             = ole32.NewProc("CoUninitialize")
)

var iidIAccessible = windows.GUID{
	Data1: 0x618736E0,
	Data2: 0x3C3D,
	Data3: 0x11CF,
	Data4: [8]byte{0x81, 0x0C, 0x00, 0xAA, 0x00, 0x38, 0x9B, 0x71},
}

type variant struct {
	VT  uint16
	_   [3]uint16
	Val [16]byte
}

func variantI4(v int32) variant {
	var out variant
	out.VT = vtI4
	*(*int32)(unsafe.Pointer(&out.Val[0])) = v
	return out
}

type liveDialog struct {
	snap       DialogSnapshot
	hwnd       uintptr
	buttonHwnd map[string]uintptr
}

type dialogEnumState struct {
	dialogs []liveDialog
}

var (
	dialogEnumMu  sync.Mutex
	dialogEnumSeq uintptr
	dialogEnums   = map[uintptr]*dialogEnumState{}

	childEnumMu  sync.Mutex
	childEnumSeq uintptr
	childEnums   = map[uintptr]*childEnumState{}
)

type childEnumState struct {
	parent *liveDialog
}

var enumTopLevelCallback = sync.OnceValue(func() uintptr {
	return syscall.NewCallback(enumTopLevelProc)
})

var enumChildCallback = sync.OnceValue(func() uintptr {
	return syscall.NewCallback(enumChildProc)
})

func enumTopLevelProc(hwnd uintptr, lParam uintptr) uintptr {
	dialogEnumMu.Lock()
	st := dialogEnums[lParam]
	dialogEnumMu.Unlock()
	if st == nil {
		return 1
	}
	if len(st.dialogs) >= CcMaxObserveDialogs {
		return 0
	}
	vis, _, _ := procIsWindowVisible.Call(hwnd)
	if vis == 0 {
		return 1
	}
	title := windowText(hwnd)
	class := windowClass(hwnd)
	_, process, _ := windowProcess(hwnd)
	x, y, w, h := windowRect(hwnd)
	d := liveDialog{
		snap: DialogSnapshot{
			Title:   title,
			Process: process,
			Class:   class,
			X:       x, Y: y, W: w, H: h,
		},
		hwnd:       hwnd,
		buttonHwnd: map[string]uintptr{},
	}
	collectWin32Buttons(&d)
	collectAccessibleButtons(hwnd, &d)
	if len(d.snap.Buttons) == 0 && class != "#32770" {
		return 1
	}
	ok, reason := ClassifyDialog(title, process, class, d.snap.Buttons)
	d.snap.Confirmable = ok
	d.snap.Refused = reason
	looksDialog := class == "#32770" || ok || reason == "uac dialog" || reason == "elevation dialog" || reason == "file open/save dialog"
	if !looksDialog {
		return 1
	}
	if d.snap.Refused == "" || d.snap.Refused == "file open/save dialog" {
		attachDialogNodes(&d)
	}
	st.dialogs = append(st.dialogs, d)
	return 1
}

func enumChildProc(hwnd uintptr, lParam uintptr) uintptr {
	childEnumMu.Lock()
	st := childEnums[lParam]
	childEnumMu.Unlock()
	if st == nil || st.parent == nil {
		return 1
	}
	if windowClass(hwnd) != "Button" {
		return 1
	}
	label := windowText(hwnd)
	if label == "" {
		return 1
	}
	addButton(st.parent, label, hwnd)
	return 1
}

func collectWin32Buttons(d *liveDialog) {
	childEnumMu.Lock()
	childEnumSeq++
	token := childEnumSeq
	childEnums[token] = &childEnumState{parent: d}
	childEnumMu.Unlock()
	defer func() {
		childEnumMu.Lock()
		delete(childEnums, token)
		childEnumMu.Unlock()
	}()
	_, _, _ = procEnumChildWindows.Call(d.hwnd, enumChildCallback(), token)
}

func addButton(d *liveDialog, label string, hwnd uintptr) {
	for _, existing := range d.snap.Buttons {
		if existing == label {
			if hwnd != 0 {
				d.buttonHwnd[normalizeButton(label)] = hwnd
			}
			return
		}
	}
	d.snap.Buttons = append(d.snap.Buttons, label)
	if hwnd != 0 {
		d.buttonHwnd[normalizeButton(label)] = hwnd
	}
}

func attachDialogNodes(d *liveDialog) {
	if d == nil {
		return
	}
	for _, label := range d.snap.Buttons {
		if len(d.snap.Nodes) >= CcMaxDialogNodes {
			return
		}
		x, y, w, h := d.snap.X, d.snap.Y, d.snap.W, d.snap.H
		if hwnd := d.buttonHwnd[normalizeButton(label)]; hwnd != 0 {
			x, y, w, h = windowRect(hwnd)
		}
		d.snap.Nodes = append(d.snap.Nodes, UINode{
			Role: "button", Name: label, X: x, Y: y, W: w, H: h,
		})
	}
}

func windowText(hwnd uintptr) string {
	n, _, _ := procGetWindowTextLen.Call(hwnd)
	if int(n) <= 0 {
		buf := make([]uint16, 512)
		got, _, _ := procGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
		return windows.UTF16ToString(buf[:got])
	}
	buf := make([]uint16, int(n)+2)
	got, _, _ := procGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return windows.UTF16ToString(buf[:got])
}

func windowClass(hwnd uintptr) string {
	buf := make([]uint16, 256)
	n, _, _ := procGetClassNameW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return windows.UTF16ToString(buf[:n])
}

func windowProcess(hwnd uintptr) (title, process string, err error) {
	title = windowText(hwnd)
	var pid uint32
	_, _, _ = procGetWindowThreadProcID.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	if pid == 0 {
		return title, "", nil
	}
	handle, _, _ := procOpenProcess.Call(processQueryLimitedInformation, 0, uintptr(pid))
	if handle == 0 {
		return title, "", nil
	}
	defer procCloseHandle.Call(handle)
	pbuf := make([]uint16, 1024)
	pn := uint32(len(pbuf))
	if ok, _, _ := procQueryFullProcessImg.Call(handle, 0, uintptr(unsafe.Pointer(&pbuf[0])), uintptr(unsafe.Pointer(&pn))); ok != 0 {
		full := windows.UTF16ToString(pbuf[:pn])
		for i := len(full) - 1; i >= 0; i-- {
			if full[i] == '\\' || full[i] == '/' {
				full = full[i+1:]
				break
			}
		}
		process = full
	}
	return title, process, nil
}

func (h *windowsHost) ObserveDialogs() ([]DialogSnapshot, error) {
	live, err := collectLiveDialogs()
	if err != nil {
		return nil, err
	}
	out := make([]DialogSnapshot, 0, len(live))
	for _, d := range live {
		out = append(out, d.snap)
	}
	return out, nil
}

func (h *windowsHost) ConfirmDialog(button string) (DialogSnapshot, error) {
	live, err := collectLiveDialogs()
	if err != nil {
		return DialogSnapshot{}, err
	}
	var chosen *liveDialog
	fg, _, _ := procGetForegroundWindow.Call()
	for i := range live {
		d := &live[i]
		if !d.snap.Confirmable {
			continue
		}
		if ConfirmButtonName(d.snap.Buttons, button) == "" {
			continue
		}
		if chosen == nil || d.hwnd == fg {
			chosen = d
			if d.hwnd == fg {
				break
			}
		}
	}
	if chosen == nil {
		for _, d := range live {
			if d.snap.Refused != "" {
				if d.snap.Refused == "file open/save dialog" {
					return d.snap, fmt.Errorf("%w: %s — %s", ErrCcRiskBlocked, d.snap.Refused, FilePickerUserPrompt)
				}
				return d.snap, fmt.Errorf("%w: %s", ErrCcRiskBlocked, d.snap.Refused)
			}
		}
		return DialogSnapshot{}, fmt.Errorf("no confirmable dialog")
	}
	caption := ConfirmButtonName(chosen.snap.Buttons, button)
	if caption == "" {
		return chosen.snap, fmt.Errorf("confirm button %q not found", button)
	}
	if err := clickDialogButton(chosen, caption); err != nil {
		return chosen.snap, err
	}
	chosen.snap.Refused = ""
	return chosen.snap, nil
}

func collectLiveDialogs() ([]liveDialog, error) {
	_, _, _ = procCoInitializeEx.Call(0, uintptr(windows.COINIT_MULTITHREADED))
	defer procCoUninitialize.Call()

	dialogEnumMu.Lock()
	dialogEnumSeq++
	token := dialogEnumSeq
	st := &dialogEnumState{}
	dialogEnums[token] = st
	dialogEnumMu.Unlock()
	defer func() {
		dialogEnumMu.Lock()
		delete(dialogEnums, token)
		dialogEnumMu.Unlock()
	}()
	_, _, _ = procEnumWindowsCC.Call(enumTopLevelCallback(), token)
	return st.dialogs, nil
}

func clickDialogButton(d *liveDialog, caption string) error {
	_, _, _ = procSetForeground.Call(d.hwnd)
	key := normalizeButton(caption)
	if hwnd := d.buttonHwnd[key]; hwnd != 0 {
		r, _, err := procSendMessageW.Call(hwnd, bmClick, 0, 0)
		if r != 0 || err == windows.ERROR_SUCCESS || err == syscall.Errno(0) {
			return nil
		}
	}
	id := uintptr(idOK)
	if key == "yes" || key == "是" || key == "是的" {
		id = idYes
	}
	_, _, _ = procSendMessageW.Call(d.hwnd, wmCommand, id, 0)
	if err := accessibleInvoke(d.hwnd, caption); err != nil && len(d.buttonHwnd) == 0 {
		return err
	}
	return nil
}

func comVtbl(obj uintptr, idx uintptr) uintptr {
	vtbl := *(*uintptr)(ptrFromUintptr(obj))
	slot := vtbl + idx*unsafe.Sizeof(uintptr(0))
	return *(*uintptr)(ptrFromUintptr(slot))
}

func comRelease(obj uintptr) {
	if obj == 0 {
		return
	}
	_, _, _ = syscall.SyscallN(comVtbl(obj, 2), obj)
}

func collectAccessibleButtons(hwnd uintptr, d *liveDialog) {
	acc := accessibleFromWindow(hwnd)
	if acc == 0 {
		return
	}
	defer comRelease(acc)
	walkAccessible(acc, d, 0)
}

func accessibleFromWindow(hwnd uintptr) uintptr {
	var acc uintptr
	hr, _, _ := procAccessibleObjectFromWindow.Call(
		hwnd, uintptr(objidClient),
		uintptr(unsafe.Pointer(&iidIAccessible)),
		uintptr(unsafe.Pointer(&acc)),
	)
	if hr != 0 || acc == 0 {
		return 0
	}
	return acc
}

func walkAccessible(acc uintptr, d *liveDialog, depth int) {
	if acc == 0 || depth > 6 {
		return
	}
	self := variantI4(0)
	name := accName(acc, self)
	role := accRole(acc, self)
	if role == rolePushBtn || role == roleSplitBtn {
		if name != "" {
			addButton(d, name, 0)
		}
	}
	var count int32
	hr, _, _ := syscall.SyscallN(comVtbl(acc, 8), acc, uintptr(unsafe.Pointer(&count)))
	if hr != 0 || count <= 0 || count > 80 {
		return
	}
	for i := int32(1); i <= count; i++ {
		child := variantI4(i)
		crole := accRole(acc, child)
		cname := accName(acc, child)
		if crole == rolePushBtn || crole == roleSplitBtn {
			if cname != "" {
				addButton(d, cname, 0)
			}
			continue
		}
		nested := accChild(acc, child)
		if nested != 0 {
			walkAccessible(nested, d, depth+1)
			comRelease(nested)
		}
	}
}

func accName(acc uintptr, child variant) string {
	var bstr uintptr
	hr, _, _ := syscall.SyscallN(comVtbl(acc, 10), acc, uintptr(unsafe.Pointer(&child)), uintptr(unsafe.Pointer(&bstr)))
	if hr != 0 || bstr == 0 {
		return ""
	}
	defer procSysFreeString.Call(bstr)
	return windows.UTF16PtrToString((*uint16)(ptrFromUintptr(bstr)))
}

func accRole(acc uintptr, child variant) uint32 {
	var role variant
	hr, _, _ := syscall.SyscallN(comVtbl(acc, 13), acc, uintptr(unsafe.Pointer(&child)), uintptr(unsafe.Pointer(&role)))
	if hr != 0 {
		return 0
	}
	if role.VT != vtI4 {
		return 0
	}
	return uint32(*(*int32)(unsafe.Pointer(&role.Val[0])))
}

func accChild(acc uintptr, child variant) uintptr {
	var disp uintptr
	hr, _, _ := syscall.SyscallN(comVtbl(acc, 9), acc, uintptr(unsafe.Pointer(&child)), uintptr(unsafe.Pointer(&disp)))
	if hr != 0 || disp == 0 {
		return 0
	}
	var acc2 uintptr
	hr, _, _ = syscall.SyscallN(comVtbl(disp, 0), disp, uintptr(unsafe.Pointer(&iidIAccessible)), uintptr(unsafe.Pointer(&acc2)))
	comRelease(disp)
	if hr != 0 {
		return 0
	}
	return acc2
}

func accessibleInvoke(hwnd uintptr, caption string) error {
	acc := accessibleFromWindow(hwnd)
	if acc == 0 {
		return fmt.Errorf("no accessible object")
	}
	defer comRelease(acc)
	want := normalizeButton(caption)
	if invokeMatching(acc, want, 0) {
		return nil
	}
	return fmt.Errorf("accessible confirm button %q not invoked", caption)
}

func invokeMatching(acc uintptr, want string, depth int) bool {
	if acc == 0 || depth > 6 {
		return false
	}
	self := variantI4(0)
	if normalizeButton(accName(acc, self)) == want {
		hr, _, _ := syscall.SyscallN(comVtbl(acc, 25), acc, uintptr(unsafe.Pointer(&self)))
		return hr == 0
	}
	var count int32
	hr, _, _ := syscall.SyscallN(comVtbl(acc, 8), acc, uintptr(unsafe.Pointer(&count)))
	if hr != 0 || count <= 0 || count > 80 {
		return false
	}
	for i := int32(1); i <= count; i++ {
		child := variantI4(i)
		if normalizeButton(accName(acc, child)) == want {
			hr, _, _ := syscall.SyscallN(comVtbl(acc, 25), acc, uintptr(unsafe.Pointer(&child)))
			if hr == 0 {
				return true
			}
		}
		nested := accChild(acc, child)
		if nested != 0 {
			hit := invokeMatching(nested, want, depth+1)
			comRelease(nested)
			if hit {
				return true
			}
		}
	}
	return false
}
