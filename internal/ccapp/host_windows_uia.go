//go:build windows

package ccapp

import (
	"fmt"
	"strings"
	"syscall"
	"unsafe"

	"github.com/zzl/go-win32api/v2/win32"
)

// CLSID_CUIAutomation is not exported by go-win32api v2.0.1.
var clsidCUIAutomation = syscall.GUID{
	Data1: 0xff48dba4,
	Data2: 0x60ef,
	Data3: 0x4201,
	Data4: [8]byte{0xaa, 0x87, 0x54, 0x10, 0x3e, 0xef, 0x59, 0x4e},
}

func observeUIAutomation(hwnd uintptr, maxNodes int) ([]UINode, error) {
	if hwnd == 0 || maxNodes <= 0 {
		return nil, nil
	}
	var auto *win32.IUIAutomation
	hr := win32.CoCreateInstance(&clsidCUIAutomation, nil, win32.CLSCTX_INPROC_SERVER,
		&win32.IID_IUIAutomation, unsafe.Pointer(&auto))
	if win32.FAILED(hr) || auto == nil {
		return nil, fmt.Errorf("UI Automation unavailable hr=0x%X", uint32(hr))
	}
	defer auto.Release()

	var cond *win32.IUIAutomationCondition
	if hr = auto.CreateTrueCondition(&cond); win32.FAILED(hr) || cond == nil {
		return nil, fmt.Errorf("CreateTrueCondition hr=0x%X", uint32(hr))
	}
	defer cond.Release()

	var root *win32.IUIAutomationElement
	if hr = auto.ElementFromHandle(win32.HWND(hwnd), &root); win32.FAILED(hr) || root == nil {
		if hr = auto.GetRootElement(&root); win32.FAILED(hr) || root == nil {
			return nil, fmt.Errorf("ElementFromHandle hr=0x%X", uint32(hr))
		}
	}
	defer root.Release()

	var arr *win32.IUIAutomationElementArray
	if hr = root.FindAll(win32.TreeScope_Descendants, cond, &arr); win32.FAILED(hr) || arr == nil {
		return nil, fmt.Errorf("FindAll hr=0x%X", uint32(hr))
	}
	defer arr.Release()

	var n int32
	if hr = arr.Get_Length(&n); win32.FAILED(hr) || n <= 0 {
		return nil, nil
	}
	if n > 400 {
		n = 400
	}
	out := make([]UINode, 0, maxNodes)
	for i := int32(0); i < n && len(out) < maxNodes; i++ {
		var el *win32.IUIAutomationElement
		if hr = arr.GetElement(i, &el); win32.FAILED(hr) || el == nil {
			continue
		}
		if node, ok := uiaReadNode(el); ok {
			out = append(out, node)
		}
		el.Release()
	}
	return out, nil
}

func uiaReadNode(el *win32.IUIAutomationElement) (UINode, bool) {
	var control win32.UIA_CONTROLTYPE_ID
	if hr := el.Get_CurrentControlType(&control); win32.FAILED(hr) || !uiaActionable(control) {
		return UINode{}, false
	}
	var isControl win32.BOOL = 1
	_ = el.Get_CurrentIsControlElement(&isControl)
	if isControl == 0 {
		return UINode{}, false
	}
	var off win32.BOOL
	_ = el.Get_CurrentIsOffscreen(&off)
	if off != 0 {
		return UINode{}, false
	}
	var rc win32.RECT
	if hr := el.Get_CurrentBoundingRectangle(&rc); win32.FAILED(hr) {
		return UINode{}, false
	}
	w := int(rc.Right - rc.Left)
	h := int(rc.Bottom - rc.Top)
	if w <= 1 || h <= 1 {
		return UINode{}, false
	}
	name := strings.TrimSpace(uiaName(el))
	if name == "" {
		name = strings.TrimSpace(uiaAutomationID(el))
	}
	if name == "" {
		name = uiaRoleName(control) + " (unnamed)"
	}
	if len([]rune(name)) > 80 {
		name = string([]rune(name)[:80])
	}
	return UINode{
		Role:  uiaRoleName(control),
		Name:  name,
		Value: uiaValue(el),
		X:     int(rc.Left),
		Y:     int(rc.Top),
		W:     w,
		H:     h,
	}, true
}

func invokeUIAutomation(hwnd uintptr, want string) error {
	want = strings.TrimSpace(want)
	if hwnd == 0 || want == "" {
		return fmt.Errorf("empty UI target")
	}
	var auto *win32.IUIAutomation
	hr := win32.CoCreateInstance(&clsidCUIAutomation, nil, win32.CLSCTX_INPROC_SERVER,
		&win32.IID_IUIAutomation, unsafe.Pointer(&auto))
	if win32.FAILED(hr) || auto == nil {
		return fmt.Errorf("UI Automation unavailable hr=0x%X", uint32(hr))
	}
	defer auto.Release()
	var cond *win32.IUIAutomationCondition
	if hr = auto.CreateTrueCondition(&cond); win32.FAILED(hr) || cond == nil {
		return fmt.Errorf("CreateTrueCondition hr=0x%X", uint32(hr))
	}
	defer cond.Release()
	var root *win32.IUIAutomationElement
	if hr = auto.ElementFromHandle(win32.HWND(hwnd), &root); win32.FAILED(hr) || root == nil {
		return fmt.Errorf("ElementFromHandle hr=0x%X", uint32(hr))
	}
	defer root.Release()
	var arr *win32.IUIAutomationElementArray
	if hr = root.FindAll(win32.TreeScope_Descendants, cond, &arr); win32.FAILED(hr) || arr == nil {
		return fmt.Errorf("FindAll hr=0x%X", uint32(hr))
	}
	defer arr.Release()
	var n int32
	if hr = arr.Get_Length(&n); win32.FAILED(hr) || n <= 0 {
		return fmt.Errorf("no UI node matching %q", want)
	}
	if n > 400 {
		n = 400
	}
	for i := int32(0); i < n; i++ {
		var el *win32.IUIAutomationElement
		if hr = arr.GetElement(i, &el); win32.FAILED(hr) || el == nil {
			continue
		}
		name := strings.TrimSpace(uiaName(el))
		if name == "" {
			name = strings.TrimSpace(uiaAutomationID(el))
		}
		if !namesEquivalent(want, name) {
			el.Release()
			continue
		}
		var pat *win32.IUIAutomationInvokePattern
		hr = el.GetCurrentPatternAs(win32.UIA_InvokePatternId, &win32.IID_IUIAutomationInvokePattern, unsafe.Pointer(&pat))
		el.Release()
		if win32.FAILED(hr) || pat == nil {
			continue
		}
		invHR := pat.Invoke()
		pat.Release()
		if win32.FAILED(invHR) {
			return fmt.Errorf("invoke %q hr=0x%X", want, uint32(invHR))
		}
		return nil
	}
	return fmt.Errorf("no UI node matching %q", want)
}

func uiaValue(el *win32.IUIAutomationElement) string {
	if el == nil {
		return ""
	}
	var isPwd win32.BOOL
	if hr := el.Get_CurrentIsPassword(&isPwd); !win32.FAILED(hr) && isPwd != 0 {
		return ""
	}
	var pat *win32.IUIAutomationValuePattern
	if hr := el.GetCurrentPatternAs(win32.UIA_ValuePatternId, &win32.IID_IUIAutomationValuePattern, unsafe.Pointer(&pat)); win32.FAILED(hr) || pat == nil {
		return ""
	}
	defer pat.Release()
	var bstr win32.BSTR
	if hr := pat.Get_CurrentValue(&bstr); win32.FAILED(hr) || bstr == nil {
		return ""
	}
	return strings.TrimSpace(win32.BstrToStrAndFree(bstr))
}

func uiaName(el *win32.IUIAutomationElement) string {
	var bstr win32.BSTR
	if hr := el.Get_CurrentName(&bstr); win32.FAILED(hr) || bstr == nil {
		return ""
	}
	return win32.BstrToStrAndFree(bstr)
}

func uiaAutomationID(el *win32.IUIAutomationElement) string {
	var bstr win32.BSTR
	if hr := el.Get_CurrentAutomationId(&bstr); win32.FAILED(hr) || bstr == nil {
		return ""
	}
	return win32.BstrToStrAndFree(bstr)
}

func uiaActionable(control win32.UIA_CONTROLTYPE_ID) bool {
	switch control {
	case win32.UIA_ButtonControlTypeId, win32.UIA_CheckBoxControlTypeId,
		win32.UIA_ComboBoxControlTypeId, win32.UIA_EditControlTypeId,
		win32.UIA_HyperlinkControlTypeId, win32.UIA_ListItemControlTypeId,
		win32.UIA_MenuItemControlTypeId, win32.UIA_RadioButtonControlTypeId,
		win32.UIA_SliderControlTypeId, win32.UIA_TabItemControlTypeId,
		win32.UIA_TreeItemControlTypeId, win32.UIA_SplitButtonControlTypeId:
		return true
	}
	return false
}

func uiaRoleName(control win32.UIA_CONTROLTYPE_ID) string {
	switch control {
	case win32.UIA_ButtonControlTypeId, win32.UIA_SplitButtonControlTypeId:
		return "button"
	case win32.UIA_HyperlinkControlTypeId:
		return "link"
	case win32.UIA_EditControlTypeId:
		return "edit"
	case win32.UIA_CheckBoxControlTypeId:
		return "checkbox"
	case win32.UIA_RadioButtonControlTypeId:
		return "radio"
	case win32.UIA_ComboBoxControlTypeId:
		return "combobox"
	case win32.UIA_TabItemControlTypeId:
		return "tab"
	case win32.UIA_MenuItemControlTypeId:
		return "menuitem"
	case win32.UIA_ListItemControlTypeId:
		return "listitem"
	case win32.UIA_TreeItemControlTypeId:
		return "treeitem"
	case win32.UIA_SliderControlTypeId:
		return "slider"
	default:
		return "other"
	}
}
