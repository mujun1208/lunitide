//go:build windows

package winexec

import (
	"fmt"
	"strings"
	"syscall"
	"unsafe"
)

const (
	inputKeyboard    = 1
	keyEventfKeyUp   = 0x0002
	vkMediaPlayPause = 0xB3
	vkMediaNextTrack = 0xB0
	vkMediaPrevTrack = 0xB1
	vkMediaStop      = 0xB2

	wmAppCommand             = 0x0319
	appCommandMediaNext      = 11
	appCommandMediaPrev      = 12
	appCommandMediaStop      = 13
	appCommandMediaPlayPause = 14
	appCommandMediaPlay      = 46
)

var (
	user32Media            = syscall.NewLazyDLL("user32.dll")
	procSendInputMedia     = user32Media.NewProc("SendInput")
	procGetForegroundMedia = user32Media.NewProc("GetForegroundWindow")
	procSendMessageMedia   = user32Media.NewProc("SendMessageW")
)

// mediaInput matches sizeof(INPUT)=40 on x64 (KEYBDINPUT arm).
type mediaInput struct {
	Type  uint32
	_     uint32
	WVk   uint16
	WScan uint16
	Flag  uint32
	Time  uint32
	_     uint32
	Info  uintptr
	_     uint64
}

func sendMediaVK(vk uint16) error {
	events := []mediaInput{
		{Type: inputKeyboard, WVk: vk},
		{Type: inputKeyboard, WVk: vk, Flag: keyEventfKeyUp},
	}
	n, _, err := procSendInputMedia.Call(
		uintptr(len(events)),
		uintptr(unsafe.Pointer(&events[0])),
		unsafe.Sizeof(events[0]),
	)
	if int(n) != len(events) {
		return fmt.Errorf("SendInput media key delivered %d/%d: %w", int(n), len(events), err)
	}
	return nil
}

func sendAppCommand(cmd int) {
	hwnd, _, _ := procGetForegroundMedia.Call()
	if hwnd == 0 {
		return
	}
	_, _, _ = procSendMessageMedia.Call(hwnd, wmAppCommand, hwnd, uintptr(cmd)<<16)
}

// ClickMusicTransport clicks the bottom-center transport button of the
// visible player window. 汽水音乐 does not register a system session until that
// button is pressed, and a media key does not press it.
func ClickMusicTransport(fragment string) error {
	frag := strings.ToLower(strings.TrimSpace(fragment))
	if frag == "" {
		return fmt.Errorf("music window required")
	}
	match := windowMatch{fragment: frag}
	enumerateWindows(&match)
	if match.hwnd == 0 {
		return fmt.Errorf("no visible window matching %q", fragment)
	}
	_, _, _ = procShowWindowWin.Call(match.hwnd, swRestore)
	_, _, _ = procSetForegroundWin.Call(match.hwnd)
	var r struct{ L, T, R, B int32 }
	if ok, _, _ := user32Media.NewProc("GetWindowRect").Call(match.hwnd, uintptr(unsafe.Pointer(&r))); ok == 0 {
		return fmt.Errorf("music window rect unavailable")
	}
	w := int(r.R - r.L)
	h := int(r.B - r.T)
	if w < 40 || h < 40 {
		return fmt.Errorf("music window is too small")
	}
	x := int(r.L) + w/2
	y := int(r.T) + h*92/100
	user32Media.NewProc("SetCursorPos").Call(uintptr(x), uintptr(y))
	const mouseLeftDown, mouseLeftUp = 0x0002, 0x0004
	user32Media.NewProc("mouse_event").Call(mouseLeftDown, 0, 0, 0, 0)
	user32Media.NewProc("mouse_event").Call(mouseLeftUp, 0, 0, 0, 0)
	return nil
}

// SendMediaKey dispatches once. Sending both a key and WM_APPCOMMAND can
// toggle twice or skip two tracks in players that handle both paths.
func SendMediaKey(action string) error {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "play":
		sendAppCommand(appCommandMediaPlay)
		return nil
	case "pause", "play_pause", "toggle":
		if err := sendMediaVK(vkMediaPlayPause); err != nil {
			return err
		}
		return nil
	case "next", "skip":
		if err := sendMediaVK(vkMediaNextTrack); err != nil {
			return err
		}
		return nil
	case "prev", "previous":
		if err := sendMediaVK(vkMediaPrevTrack); err != nil {
			return err
		}
		return nil
	case "stop":
		if err := sendMediaVK(vkMediaStop); err != nil {
			return err
		}
		return nil
	default:
		return fmt.Errorf("unknown media action %q", action)
	}
}
