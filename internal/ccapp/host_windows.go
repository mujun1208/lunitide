//go:build windows

package ccapp

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/png"
	"unsafe"

	"golang.org/x/sys/windows"
)

// PlatformHost answers the Windows control host backed by User32 SendInput
// (mouse/keyboard), GDI (screen capture) and the foreground-window query.
func PlatformHost() Host { return &windowsHost{} }

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

	procCreateCompatibleDC  = gdi32.NewProc("CreateCompatibleDC")
	procCreateCompatibleBmp = gdi32.NewProc("CreateCompatibleBitmap")
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
	smCXScreen = 0
	smCYScreen = 1

	inputMouse    = 0
	inputKeyboard = 1

	mouseEventfMove       = 0x0001
	mouseEventfLeftDown   = 0x0002
	mouseEventfLeftUp     = 0x0004
	mouseEventfRightDown  = 0x0008
	mouseEventfRightUp    = 0x0010
	mouseEventfMiddleDown = 0x0020
	mouseEventfMiddleUp   = 0x0040
	mouseEventfAbsolute   = 0x8000

	keyEventfKeyUp = 0x0002

	srccopy = 0x00CC0020

	dibRgbColors = 0
	biRgb        = 0

	processQueryLimitedInformation = 0x1000
)

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
	"z": 0x5A,
	"f1": 0x70, "f2": 0x71, "f3": 0x72, "f4": 0x73, "f5": 0x74,
	"f6": 0x75, "f7": 0x76, "f8": 0x77, "f9": 0x78, "f10": 0x79,
	"f11": 0x7A, "f12": 0x7B, "f13": 0x7C, "f14": 0x7D, "f15": 0x7E,
	"f16": 0x7F, "f17": 0x80, "f18": 0x81, "f19": 0x82, "f20": 0x83,
	"f21": 0x84, "f22": 0x85, "f23": 0x86, "f24": 0x87,
}

type windowsHost struct{}

func (h *windowsHost) Available() bool { return true }

func (h *windowsHost) ScreenSize() (int, int) {
	w, _, _ := procGetSystemMetrics.Call(uintptr(smCXScreen))
	v, _, _ := procGetSystemMetrics.Call(uintptr(smCYScreen))
	return int(int32(w)), int(int32(v))
}

// absoluteCoords maps pixel coordinates onto the SendInput 0..65535 range.
func (h *windowsHost) absoluteCoords(x, y int) (int32, int32) {
	w, v := h.ScreenSize()
	if w <= 0 || v <= 0 {
		w, v = 1, 1
	}
	clamp := func(n, max int) int32 {
		if n < 0 {
			n = 0
		}
		if n > max {
			n = max
		}
		return int32((n * 65535) / max)
	}
	return clamp(x, w), clamp(y, v)
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

func (h *windowsHost) MouseMove(x, y int) error {
	ax, ay := h.absoluteCoords(x, y)
	return sendMouse([]mouseEvent{{
		Type: inputMouse, Dx: ax, Dy: ay,
		Flag: mouseEventfMove | mouseEventfAbsolute,
	}})
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
	events := make([]keyEvent, 0, len(runes)*2)
	for _, r := range runes {
		vk := vkFromRune(r)
		if vk == 0 {
			continue // unsupported runes are skipped, not injected
		}
		events = append(events, keyEvent{Type: inputKeyboard, WVk: vk})
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

func (h *windowsHost) ScreenCapture() ([]byte, error) {
	w, v := h.ScreenSize()
	if w <= 0 || v <= 0 {
		return nil, errors.New("invalid screen size")
	}
	screenDC, _, _ := procGetDC.Call(0)
	if screenDC == 0 {
		return nil, errors.New("getdc failed")
	}
	defer procReleaseDC.Call(0, screenDC)
	memDC, _, _ := procCreateCompatibleDC.Call(screenDC)
	if memDC == 0 {
		return nil, errors.New("createdc failed")
	}
	defer procDeleteDC.Call(memDC)
	bmp, _, _ := procCreateCompatibleBmp.Call(screenDC, uintptr(w), uintptr(v))
	if bmp == 0 {
		return nil, errors.New("createbitmap failed")
	}
	defer procDeleteObject.Call(bmp)
	old, _, _ := procSelectObject.Call(memDC, bmp)
	defer procSelectObject.Call(memDC, old)
	if ok, _, _ := procBitBlt.Call(memDC, 0, 0, uintptr(w), uintptr(v),
		screenDC, 0, 0, srccopy); ok == 0 {
		return nil, errors.New("bitblt failed")
	}

	type bitmapInfoHeader struct {
		BiSize          uint32
		BiWidth         int32
		BiHeight        int32
		BiPlanes        uint16
		BiBitCount      uint16
		BiCompression   uint32
		BiSizeImage     uint32
		BiXPelsPerMeter int32
		BiYPelsPerMeter int32
		BiClrUsed       uint32
		BiClrImportant  uint32
	}
	var bmi bitmapInfoHeader
	bmi.BiSize = uint32(unsafe.Sizeof(bmi))
	bmi.BiWidth = int32(w)
	bmi.BiHeight = -int32(v) // top-down rows
	bmi.BiPlanes = 1
	bmi.BiBitCount = 32
	bmi.BiCompression = biRgb

	row := int(w) * 4 // 32bpp rows are already DWORD aligned
	pixels := make([]byte, row*int(v))
	if ok, _, _ := procGetDIBits.Call(memDC, bmp, 0, uintptr(v),
		uintptr(unsafe.Pointer(&pixels[0])), uintptr(unsafe.Pointer(&bmi)),
		dibRgbColors); ok == 0 {
		return nil, errors.New("getdibits failed")
	}

	img := image.NewRGBA(image.Rect(0, 0, int(w), int(v)))
	for y := 0; y < int(v); y++ {
		src := pixels[y*row:]
		dst := img.Pix[y*img.Stride : y*img.Stride+int(w)*4]
		for x := 0; x < int(w); x++ {
			dst[x*4+0] = src[x*4+2] // BGRA → RGBA
			dst[x*4+1] = src[x*4+1]
			dst[x*4+2] = src[x*4+0]
			dst[x*4+3] = 0xFF
		}
	}
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
