//go:build windows

package ccapp

import (
	"fmt"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	roleLink      = 0x1E
	roleMenuItem  = 0x0C
	roleListItem  = 0x22
	roleOutlineIt = 0x25
	roleText      = 0x2A
	roleCheck     = 0x2C
	roleRadio     = 0x2D
	roleCombo     = 0x2E
	rolePageTab   = 0x3C
	roleSlider    = 0x33
)

func (h *windowsHost) ObserveUI(maxNodes int) ([]UINode, error) {
	if maxNodes <= 0 {
		maxNodes = 40
	}
	hwnd, _, _ := procGetForegroundWindow.Call()
	if hwnd == 0 {
		return nil, nil
	}
	_, _, _ = procCoInitializeEx.Call(0, uintptr(windows.COINIT_MULTITHREADED))
	defer procCoUninitialize.Call()
	acc := accessibleFromWindow(hwnd)
	if acc == 0 {
		return nil, nil
	}
	defer comRelease(acc)
	var nodes []UINode
	walkActionable(acc, &nodes, maxNodes, 0)
	return nodes, nil
}

func walkActionable(acc uintptr, nodes *[]UINode, max, depth int) {
	if acc == 0 || depth > 8 || len(*nodes) >= max {
		return
	}
	self := variantI4(0)
	collectIfActionable(acc, self, nodes, max)
	var count int32
	hr, _, _ := syscall.SyscallN(comVtbl(acc, 8), acc, uintptr(unsafe.Pointer(&count)))
	if hr != 0 || count <= 0 || count > 120 {
		return
	}
	for i := int32(1); i <= count && len(*nodes) < max; i++ {
		child := variantI4(i)
		if collectIfActionable(acc, child, nodes, max) {
			continue
		}
		nested := accChild(acc, child)
		if nested != 0 {
			walkActionable(nested, nodes, max, depth+1)
			comRelease(nested)
		}
	}
}

func collectIfActionable(acc uintptr, child variant, nodes *[]UINode, max int) bool {
	if len(*nodes) >= max {
		return true
	}
	role := accRole(acc, child)
	if !actionableRole(role) {
		return false
	}
	name := strings.TrimSpace(accName(acc, child))
	if name == "" {
		return false
	}
	if len([]rune(name)) > 80 {
		name = string([]rune(name)[:80])
	}
	x, y, w, h, ok := accLocation(acc, child)
	if !ok || w <= 1 || h <= 1 {
		return true
	}
	*nodes = append(*nodes, UINode{Role: roleName(role), Name: name, X: x, Y: y, W: w, H: h})
	return true
}

func actionableRole(role uint32) bool {
	switch role {
	case rolePushBtn, roleSplitBtn, roleLink, roleMenuItem, roleListItem,
		roleOutlineIt, roleText, roleCheck, roleRadio, roleCombo, rolePageTab, roleSlider:
		return true
	}
	return false
}

func roleName(role uint32) string {
	switch role {
	case rolePushBtn, roleSplitBtn:
		return "button"
	case roleLink:
		return "link"
	case roleMenuItem:
		return "menuitem"
	case roleListItem:
		return "listitem"
	case roleOutlineIt:
		return "treeitem"
	case roleText:
		return "edit"
	case roleCheck:
		return "checkbox"
	case roleRadio:
		return "radio"
	case roleCombo:
		return "combobox"
	case rolePageTab:
		return "tab"
	case roleSlider:
		return "slider"
	default:
		return "other"
	}
}

func accLocation(acc uintptr, child variant) (x, y, w, h int, ok bool) {
	var left, top, width, height int32
	hr, _, _ := syscall.SyscallN(comVtbl(acc, 22), acc,
		uintptr(unsafe.Pointer(&left)), uintptr(unsafe.Pointer(&top)),
		uintptr(unsafe.Pointer(&width)), uintptr(unsafe.Pointer(&height)),
		uintptr(unsafe.Pointer(&child)))
	if hr != 0 || width <= 0 || height <= 0 {
		return 0, 0, 0, 0, false
	}
	return int(left), int(top), int(width), int(height), true
}

func sysAllocString(s string) uintptr {
	u, err := windows.UTF16PtrFromString(s)
	if err != nil {
		return 0
	}
	r, _, _ := procSysAllocString.Call(uintptr(unsafe.Pointer(u)))
	return r
}

func accNameScore(got, want string) int {
	g := strings.ToLower(strings.TrimSpace(got))
	w := strings.ToLower(strings.TrimSpace(want))
	if g == "" || w == "" {
		return 0
	}
	if g == w {
		return 100
	}
	if strings.Contains(g, w) || strings.Contains(w, g) {
		return 50
	}
	return 0
}

func accDoDefault(acc uintptr, child variant) bool {
	hr, _, _ := syscall.SyscallN(comVtbl(acc, 25), acc, uintptr(unsafe.Pointer(&child)))
	return hr == 0
}

func accPutValue(acc uintptr, child variant, value string) error {
	bstr := sysAllocString(value)
	if bstr == 0 && value != "" {
		return fmt.Errorf("alloc string failed")
	}
	if bstr != 0 {
		defer procSysFreeString.Call(bstr)
	}
	hr, _, _ := syscall.SyscallN(comVtbl(acc, 27), acc, uintptr(unsafe.Pointer(&child)), bstr)
	if hr != 0 {
		return fmt.Errorf("put_accValue hr=0x%X", uint32(hr))
	}
	return nil
}

func invokeNamedMin(acc uintptr, want string, min, depth int) bool {
	if acc == 0 || depth > 8 {
		return false
	}
	self := variantI4(0)
	if accNameScore(accName(acc, self), want) >= min && accDoDefault(acc, self) {
		return true
	}
	var count int32
	hr, _, _ := syscall.SyscallN(comVtbl(acc, 8), acc, uintptr(unsafe.Pointer(&count)))
	if hr != 0 || count <= 0 || count > 120 {
		return false
	}
	for i := int32(1); i <= count; i++ {
		child := variantI4(i)
		if accNameScore(accName(acc, child), want) >= min && accDoDefault(acc, child) {
			return true
		}
		nested := accChild(acc, child)
		if nested == 0 {
			continue
		}
		ok := invokeNamedMin(nested, want, min, depth+1)
		comRelease(nested)
		if ok {
			return true
		}
	}
	return false
}

func invokeNamedOn(acc uintptr, want string) bool {
	return invokeNamedMin(acc, want, 100, 0) || invokeNamedMin(acc, want, 50, 0)
}

func putValueMin(acc uintptr, want, value string, min, depth int) error {
	if acc == 0 || depth > 8 {
		return fmt.Errorf("no UI node matching %q", want)
	}
	self := variantI4(0)
	if accNameScore(accName(acc, self), want) >= min {
		return accPutValue(acc, self, value)
	}
	var count int32
	hr, _, _ := syscall.SyscallN(comVtbl(acc, 8), acc, uintptr(unsafe.Pointer(&count)))
	if hr != 0 || count <= 0 || count > 120 {
		return fmt.Errorf("no UI node matching %q", want)
	}
	for i := int32(1); i <= count; i++ {
		child := variantI4(i)
		if accNameScore(accName(acc, child), want) >= min {
			return accPutValue(acc, child, value)
		}
		nested := accChild(acc, child)
		if nested == 0 {
			continue
		}
		err := putValueMin(nested, want, value, min, depth+1)
		comRelease(nested)
		if err == nil {
			return nil
		}
	}
	return fmt.Errorf("no UI node matching %q", want)
}

func invokeNamedOnHWND(hwnd uintptr, want string) bool {
	acc := accessibleFromWindow(hwnd)
	if acc == 0 {
		return false
	}
	defer comRelease(acc)
	return invokeNamedOn(acc, want)
}

func invokeOnMenuPopups(want string) bool {
	wins, err := (&windowsHost{}).ListWindows()
	if err != nil {
		return false
	}
	for _, w := range wins {
		if w.Class != "#32768" && !strings.EqualFold(w.Class, "NetUIHWND") {
			continue
		}
		hwnd, err := hwndFromInfo(w)
		if err != nil {
			continue
		}
		if invokeNamedOnHWND(hwnd, want) {
			return true
		}
	}
	return false
}

func (h *windowsHost) MenuClick(path string) error {
	segs := SplitMenuPath(path)
	if len(segs) == 0 {
		return fmt.Errorf("empty menu path")
	}
	hwnd, _, _ := procGetForegroundWindow.Call()
	if hwnd == 0 {
		return fmt.Errorf("no foreground window")
	}
	_, _, _ = procCoInitializeEx.Call(0, uintptr(windows.COINIT_MULTITHREADED))
	defer procCoUninitialize.Call()
	for i, seg := range segs {
		ok := invokeNamedOnHWND(hwnd, seg)
		if !ok && i > 0 {
			ok = invokeOnMenuPopups(seg)
		}
		if !ok {
			fg, _, _ := procGetForegroundWindow.Call()
			if fg != 0 && fg != hwnd {
				ok = invokeNamedOnHWND(fg, seg)
			}
		}
		if !ok {
			return fmt.Errorf("menu item %q not found in %q", seg, path)
		}
		if i+1 < len(segs) {
			time.Sleep(120 * time.Millisecond)
		}
	}
	return nil
}

func (h *windowsHost) InvokeUI(target string) error {
	target = strings.TrimSpace(target)
	if target == "" {
		return fmt.Errorf("empty UI target")
	}
	hwnd, _, _ := procGetForegroundWindow.Call()
	if hwnd == 0 {
		return fmt.Errorf("no foreground window")
	}
	_, _, _ = procCoInitializeEx.Call(0, uintptr(windows.COINIT_MULTITHREADED))
	defer procCoUninitialize.Call()
	acc := accessibleFromWindow(hwnd)
	if acc == 0 {
		return fmt.Errorf("no accessible object")
	}
	defer comRelease(acc)
	if invokeNamedOn(acc, target) {
		return nil
	}
	return fmt.Errorf("no UI node matching %q", target)
}

func (h *windowsHost) SetValue(target, value string) error {
	hwnd, _, _ := procGetForegroundWindow.Call()
	if hwnd == 0 {
		return fmt.Errorf("no foreground window")
	}
	_, _, _ = procCoInitializeEx.Call(0, uintptr(windows.COINIT_MULTITHREADED))
	defer procCoUninitialize.Call()
	acc := accessibleFromWindow(hwnd)
	if acc == 0 {
		return fmt.Errorf("no accessible object")
	}
	defer comRelease(acc)
	if err := putValueMin(acc, target, value, 100, 0); err == nil {
		return nil
	}
	return putValueMin(acc, target, value, 50, 0)
}
