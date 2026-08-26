//go:build windows

package ccapp

import (
	"errors"
	"fmt"
	"sync"
	"time"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

// PlatformHost answers the Windows control host backed by User32 SendInput
// (mouse/keyboard), GDI (screen capture) and the foreground-window query.
func PlatformHost() Host {
	enableDPIAwareness()
	return &windowsHost{}
}

// ── User32 / GDI bindings ───────────────────────────────────────────────────

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	gdi32    = windows.NewLazySystemDLL("gdi32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")

	procGetSystemMetrics      = user32.NewProc("GetSystemMetrics")
	procSendInput             = user32.NewProc("SendInput")
	procGetForegroundWindow   = user32.NewProc("GetForegroundWindow")
	procGetWindowTextW        = user32.NewProc("GetWindowTextW")
	procGetWindowThreadProcID = user32.NewProc("GetWindowThreadProcessId")
	procGetDC                 = user32.NewProc("GetDC")
	procReleaseDC             = user32.NewProc("ReleaseDC")
	procSetCursorPos          = user32.NewProc("SetCursorPos")
	procGetCursorPos          = user32.NewProc("GetCursorPos")
	procSetProcessDpiCtx      = user32.NewProc("SetProcessDpiAwarenessContext")
	procGetWindowRect         = user32.NewProc("GetWindowRect")
	procShowWindow            = user32.NewProc("ShowWindow")
	procPrintWindow           = user32.NewProc("PrintWindow")
	procAttachThreadInput     = user32.NewProc("AttachThreadInput")
	procBringWindowToTop      = user32.NewProc("BringWindowToTop")
	procSetFocus              = user32.NewProc("SetFocus")
	procIsIconic              = user32.NewProc("IsIconic")
	procAllowSetForeground    = user32.NewProc("AllowSetForegroundWindow")
	procGetCurrentThreadId    = kernel32.NewProc("GetCurrentThreadId")
	procPostMessageW          = user32.NewProc("PostMessageW")
	procMoveWindow            = user32.NewProc("MoveWindow")

	procCreateCompatibleDC  = gdi32.NewProc("CreateCompatibleDC")
	procCreateCompatibleBmp = gdi32.NewProc("CreateCompatibleBitmap")
	procCreateDIBSection    = gdi32.NewProc("CreateDIBSection")
	procSelectObject        = gdi32.NewProc("SelectObject")
	procDeleteObject        = gdi32.NewProc("DeleteObject")
	procDeleteDC            = gdi32.NewProc("DeleteDC")
	procBitBlt              = gdi32.NewProc("BitBlt")
	procGetDIBits           = gdi32.NewProc("GetDIBits")

	procOpenProcess         = kernel32.NewProc("OpenProcess")
	procQueryFullProcessImg = kernel32.NewProc("QueryFullProcessImageNameW")
	procCloseHandle         = kernel32.NewProc("CloseHandle")
)

const (
	smCXScreen        = 0
	smCYScreen        = 1
	smXVIRTUALSCREEN  = 76
	smYVIRTUALSCREEN  = 77
	smCXVIRTUALSCREEN = 78
	smCYVIRTUALSCREEN = 79

	inputMouse    = 0
	inputKeyboard = 1

	mouseEventfMove        = 0x0001
	mouseEventfLeftDown    = 0x0002
	mouseEventfLeftUp      = 0x0004
	mouseEventfRightDown   = 0x0008
	mouseEventfRightUp     = 0x0010
	mouseEventfMiddleDown  = 0x0020
	mouseEventfMiddleUp    = 0x0040
	mouseEventfWheel       = 0x0800
	mouseEventfHWheel      = 0x1000
	mouseEventfVirtualDesk = 0x4000
	mouseEventfAbsolute    = 0x8000

	keyEventfKeyUp   = 0x0002
	keyEventfUnicode = 0x0004

	captureBlt = 0x40000000

	srccopy = 0x00CC0020

	dibRgbColors = 0
	biRgb        = 0

	processQueryLimitedInformation = 0x1000
	swShow                         = 5
	swRestore                      = 9
	pwRenderFullContent            = 2
	vkMenu                         = 0x12
	asfwAny                        = ^uintptr(0)
)

// ptrFromUintptr converts a Win32 pointer stored as uintptr back to
// unsafe.Pointer via address-of indirection (vet-safe; see webviewhost).
func ptrFromUintptr(p uintptr) unsafe.Pointer {
	return *(*unsafe.Pointer)(unsafe.Pointer(&p))
}

var dpiOnce sync.Once

func enableDPIAwareness() {
	dpiOnce.Do(func() {
		// DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2 == (HANDLE)-4 == ^uintptr(3)
		if r, _, _ := procSetProcessDpiCtx.Call(^uintptr(3)); r != 0 {
			return
		}
		_, _, _ = user32.NewProc("SetProcessDPIAware").Call()
	})
}

// mouseEvent mirrors the Win32 INPUT union with the MOUSEINPUT arm active.
// The union begins after the 8-byte-aligned type word on x64.
type mouseEvent struct {
	Type uint32
	_    uint32
	Dx   int32
	Dy   int32
	Data uint32
	Flag uint32
	Time uint32
	_    uint32
	Info uintptr
}

// keyEvent mirrors the Win32 INPUT union with the KEYBDINPUT arm active.
// Both structs are exactly sizeof(INPUT)=40 on x64, so SendInput accepts
// either array shape.
type keyEvent struct {
	Type  uint32
	_     uint32
	WVk   uint16
	WScan uint16
	Flag  uint32
	Time  uint32
	_     uint32
	Info  uintptr
	_     uint64 // padding to match sizeof(INPUT) = 40 on x64
}

// virtualKeyCodes maps the portable key vocabulary onto virtual-key codes.
var virtualKeyCodes = map[string]uint16{
	"ctrl": 0x11, "shift": 0x10, "alt": 0x12, "win": 0x5B,
	"enter": 0x0D, "esc": 0x1B, "space": 0x20, "tab": 0x09,
	"backspace": 0x08, "delete": 0x2E, "home": 0x24, "end": 0x23,
	"pageup": 0x21, "pagedown": 0x22, "up": 0x26, "down": 0x28,
	"left": 0x25, "right": 0x27, "printscreen": 0x2C, "capslock": 0x14,
	"0": 0x30, "1": 0x31, "2": 0x32, "3": 0x33, "4": 0x34,
	"5": 0x35, "6": 0x36, "7": 0x37, "8": 0x38, "9": 0x39,
	"a": 0x41, "b": 0x42, "c": 0x43, "d": 0x44, "e": 0x45,
	"f": 0x46, "g": 0x47, "h": 0x48, "i": 0x49, "j": 0x4A,
	"k": 0x4B, "l": 0x4C, "m": 0x4D, "n": 0x4E, "o": 0x4F,
	"p": 0x50, "q": 0x51, "r": 0x52, "s": 0x53, "t": 0x54,
	"u": 0x55, "v": 0x56, "w": 0x57, "x": 0x58, "y": 0x59,
	"z":  0x5A,
	"f1": 0x70, "f2": 0x71, "f3": 0x72, "f4": 0x73, "f5": 0x74,
	"f6": 0x75, "f7": 0x76, "f8": 0x77, "f9": 0x78, "f10": 0x79,
	"f11": 0x7A, "f12": 0x7B, "f13": 0x7C, "f14": 0x7D, "f15": 0x7E,
	"f16": 0x7F, "f17": 0x80, "f18": 0x81, "f19": 0x82, "f20": 0x83,
	"f21": 0x84, "f22": 0x85, "f23": 0x86, "f24": 0x87,
	"media_play": 0xB3, "media_pause": 0xB3, "media_next": 0xB0, "media_prev": 0xB1, "media_stop": 0xB2,
}

type windowsHost struct{}

var _ Host = (*windowsHost)(nil)

func (h *windowsHost) Available() bool { return true }

func (h *windowsHost) ScreenSize() (int, int) {
	w, hgt := virtualScreenSize()
	if w <= 0 || hgt <= 0 {
		cw, _, _ := procGetSystemMetrics.Call(uintptr(smCXScreen))
		ch, _, _ := procGetSystemMetrics.Call(uintptr(smCYScreen))
		return int(int32(cw)), int(int32(ch))
	}
	return w, hgt
}

func virtualScreenRect() (x, y, w, h int) {
	vx, _, _ := procGetSystemMetrics.Call(uintptr(smXVIRTUALSCREEN))
	vy, _, _ := procGetSystemMetrics.Call(uintptr(smYVIRTUALSCREEN))
	vw, vh := virtualScreenSize()
	return int(int32(vx)), int(int32(vy)), vw, vh
}

func virtualScreenSize() (int, int) {
	w, _, _ := procGetSystemMetrics.Call(uintptr(smCXVIRTUALSCREEN))
	h, _, _ := procGetSystemMetrics.Call(uintptr(smCYVIRTUALSCREEN))
	return int(int32(w)), int(int32(h))
}

func sendMouse(events []mouseEvent) error {
	if len(events) == 0 {
		return nil
	}
	n, _, err := procSendInput.Call(uintptr(len(events)),
		uintptr(unsafe.Pointer(&events[0])), unsafe.Sizeof(events[0]))
	if int(n) != len(events) {
		return fmt.Errorf("sendinput delivered %d/%d: %w", int(n), len(events), err)
	}
	return nil
}

func sendKeys(events []keyEvent) error {
	if len(events) == 0 {
		return nil
	}
	n, _, err := procSendInput.Call(uintptr(len(events)),
		uintptr(unsafe.Pointer(&events[0])), unsafe.Sizeof(events[0]))
	if int(n) != len(events) {
		return fmt.Errorf("sendinput delivered %d/%d: %w", int(n), len(events), err)
	}
	return nil
}

func (h *windowsHost) ScreenOrigin() (int, int) {
	x, y, _, _ := virtualScreenRect()
	return x, y
}

func (h *windowsHost) CursorPosition() (int, int, error) {
	var pt struct{ X, Y int32 }
	ok, _, err := procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	if ok == 0 {
		return 0, 0, fmt.Errorf("getcursorpos: %w", err)
	}
	return int(pt.X), int(pt.Y), nil
}

func (h *windowsHost) MouseMove(x, y int) error {
	ok, _, err := procSetCursorPos.Call(uintptr(int32(x)), uintptr(int32(y)))
	if ok == 0 {
		return fmt.Errorf("setcursorpos: %w", err)
	}
	return nil
}

func (h *windowsHost) MouseClick(button string, clicks int) error {
	var down, up uint32
	switch button {
	case "left":
		down, up = mouseEventfLeftDown, mouseEventfLeftUp
	case "right":
		down, up = mouseEventfRightDown, mouseEventfRightUp
	case "middle":
		down, up = mouseEventfMiddleDown, mouseEventfMiddleUp
	default:
		return fmt.Errorf("unknown button %q", button)
	}
	events := make([]mouseEvent, 0, clicks*2)
	for i := 0; i < clicks; i++ {
		events = append(events,
			mouseEvent{Type: inputMouse, Flag: down},
			mouseEvent{Type: inputMouse, Flag: up})
	}
	return sendMouse(events)
}

func (h *windowsHost) MouseScroll(notches int) error {
	return sendWheel(notches, mouseEventfWheel)
}

func (h *windowsHost) MouseScrollH(notches int) error {
	return sendWheel(notches, mouseEventfHWheel)
}

func sendWheel(notches int, flag uint32) error {
	if notches == 0 {
		return nil
	}
	if notches > 12 {
		notches = 12
	}
	if notches < -12 {
		notches = -12
	}
	delta := int32(notches) * 120
	return sendMouse([]mouseEvent{{
		Type: inputMouse, Data: uint32(delta), Flag: flag,
	}})
}

func (h *windowsHost) MouseDrag(x1, y1, x2, y2 int) error {
	if err := h.MouseMove(x1, y1); err != nil {
		return err
	}
	time.Sleep(20 * time.Millisecond)
	if err := sendMouse([]mouseEvent{{Type: inputMouse, Flag: mouseEventfLeftDown}}); err != nil {
		return err
	}
	dx := x2 - x1
	dy := y2 - y1
	dist := dx*dx + dy*dy
	steps := 8
	if dist > 1600 {
		steps = 16
	}
	if dist > 40000 {
		steps = 24
	}
	for i := 1; i <= steps; i++ {
		x := x1 + dx*i/steps
		y := y1 + dy*i/steps
		if err := h.MouseMove(x, y); err != nil {
			_ = sendMouse([]mouseEvent{{Type: inputMouse, Flag: mouseEventfLeftUp}})
			return err
		}
		time.Sleep(8 * time.Millisecond)
	}
	return sendMouse([]mouseEvent{{Type: inputMouse, Flag: mouseEventfLeftUp}})
}

func (h *windowsHost) EnsureForeground() error {
	hwnd, _, _ := procGetForegroundWindow.Call()
	if hwnd == 0 {
		return nil
	}
	forceForeground(hwnd)
	return nil
}

// vkFromRune answers the virtual-key code for one printable rune.
func vkFromRune(r rune) uint16 {
	switch {
	case r >= '0' && r <= '9':
		return virtualKeyCodes[string(r)]
	case r >= 'a' && r <= 'z':
		return virtualKeyCodes[string(r)]
	case r >= 'A' && r <= 'Z':
		return virtualKeyCodes[string(r+32)]
	case r == '\t':
		return 0x09
	case r == '\n', r == '\r':
		return 0x0D
	case r == ' ':
		return 0x20
	}
	return 0
}

func (h *windowsHost) KeyboardType(text string) error {
	runes := []rune(text)
	events := make([]keyEvent, 0, len(runes)*4)
	for _, r := range runes {
		switch r {
		case '\t', '\n', '\r':
			vk := vkFromRune(r)
			events = append(events,
				keyEvent{Type: inputKeyboard, WVk: vk},
				keyEvent{Type: inputKeyboard, WVk: vk, Flag: keyEventfKeyUp},
			)
		default:
			for _, unit := range utf16.Encode([]rune{r}) {
				events = append(events,
					keyEvent{Type: inputKeyboard, WScan: unit, Flag: keyEventfUnicode},
					keyEvent{Type: inputKeyboard, WScan: unit, Flag: keyEventfUnicode | keyEventfKeyUp},
				)
			}
		}
	}
	return sendKeys(events)
}

func (h *windowsHost) KeyboardShortcut(keys []string) error {
	codes := make([]uint16, 0, len(keys))
	for _, key := range keys {
		vk, ok := virtualKeyCodes[key]
		if !ok {
			return fmt.Errorf("unknown key %q", key)
		}
		codes = append(codes, vk)
	}
	events := make([]keyEvent, 0, len(codes)*2)
	for _, vk := range codes {
		events = append(events, keyEvent{Type: inputKeyboard, WVk: vk})
	}
	for i := len(codes) - 1; i >= 0; i-- {
		events = append(events, keyEvent{Type: inputKeyboard, WVk: codes[i], Flag: keyEventfKeyUp})
	}
	return sendKeys(events)
}

func (h *windowsHost) ActiveWindow() (string, string, error) {
	hwnd, _, _ := procGetForegroundWindow.Call()
	if hwnd == 0 {
		return "", "", errors.New("no foreground window")
	}
	buf := make([]uint16, 512)
	n, _, _ := procGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	title := windows.UTF16ToString(buf[:n])
	process := ""
	var pid uint32
	_, _, _ = procGetWindowThreadProcID.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	if pid != 0 {
		if handle, _, _ := procOpenProcess.Call(processQueryLimitedInformation,
			0, uintptr(pid)); handle != 0 {
			defer procCloseHandle.Call(handle)
			pbuf := make([]uint16, 1024)
			pn := uint32(len(pbuf))
			if ok, _, _ := procQueryFullProcessImg.Call(handle, 0,
				uintptr(unsafe.Pointer(&pbuf[0])), uintptr(unsafe.Pointer(&pn))); ok != 0 {
				full := windows.UTF16ToString(pbuf[:pn])
				for i := len(full) - 1; i >= 0; i-- {
					if full[i] == '\\' || full[i] == '/' {
						full = full[i+1:]
						break
					}
				}
				process = full
			}
		}
	}
	return title, process, nil
}

func forceForeground(hwnd uintptr) {
	if hwnd == 0 {
		return
	}
	if iconic, _, _ := procIsIconic.Call(hwnd); iconic != 0 {
		_, _, _ = procShowWindow.Call(hwnd, uintptr(swRestore))
	} else {
		_, _, _ = procShowWindow.Call(hwnd, uintptr(swShow))
	}
	_, _, _ = procAllowSetForeground.Call(asfwAny)
	// A brief Alt tap satisfies Windows' foreground-lock so typing and
	// clicks land on the window we just restored, not Lunitide itself.
	_ = sendKeys([]keyEvent{
		{Type: inputKeyboard, WVk: vkMenu},
		{Type: inputKeyboard, WVk: vkMenu, Flag: keyEventfKeyUp},
	})
	fg, _, _ := procGetForegroundWindow.Call()
	curThread, _, _ := procGetCurrentThreadId.Call()
	var fgPid, targetPid uint32
	fgThread, _, _ := procGetWindowThreadProcID.Call(fg, uintptr(unsafe.Pointer(&fgPid)))
	targetThread, _, _ := procGetWindowThreadProcID.Call(hwnd, uintptr(unsafe.Pointer(&targetPid)))
	if fgThread != 0 && fgThread != curThread {
		_, _, _ = procAttachThreadInput.Call(curThread, fgThread, 1)
		defer procAttachThreadInput.Call(curThread, fgThread, 0)
	}
	if targetThread != 0 && targetThread != curThread && targetThread != fgThread {
		_, _, _ = procAttachThreadInput.Call(curThread, targetThread, 1)
		defer procAttachThreadInput.Call(curThread, targetThread, 0)
	}
	_, _, _ = procBringWindowToTop.Call(hwnd)
	_, _, _ = procSetForeground.Call(hwnd)
	_, _, _ = procSetFocus.Call(hwnd)
}

func windowRect(hwnd uintptr) (x, y, w, h int) {
	var r struct{ Left, Top, Right, Bottom int32 }
	ok, _, _ := procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&r)))
	if ok == 0 {
		return 0, 0, 0, 0
	}
	return int(r.Left), int(r.Top), int(r.Right - r.Left), int(r.Bottom - r.Top)
}
