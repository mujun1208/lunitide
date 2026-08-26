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

	wmAppCommand              = 0x0319
	appCommandMediaNext       = 11
	appCommandMediaPrev       = 12
	appCommandMediaStop       = 13
	appCommandMediaPlayPause  = 14
	appCommandMediaPlay       = 46
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

// SendMediaKey sends one Windows media key (play, pause, next, prev, stop)
// via SendInput and a WM_APPCOMMAND to the foreground window so desktop
// players that listen to either path actually start.
func SendMediaKey(action string) error {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "play":
		if err := sendMediaVK(vkMediaPlayPause); err != nil {
			return err
		}
		sendAppCommand(appCommandMediaPlay)
		return nil
	case "pause", "play_pause", "toggle":
		if err := sendMediaVK(vkMediaPlayPause); err != nil {
			return err
		}
		sendAppCommand(appCommandMediaPlayPause)
		return nil
	case "next", "skip":
		if err := sendMediaVK(vkMediaNextTrack); err != nil {
			return err
		}
		sendAppCommand(appCommandMediaNext)
		return nil
	case "prev", "previous":
		if err := sendMediaVK(vkMediaPrevTrack); err != nil {
			return err
		}
		sendAppCommand(appCommandMediaPrev)
		return nil
	case "stop":
		if err := sendMediaVK(vkMediaStop); err != nil {
			return err
		}
		sendAppCommand(appCommandMediaStop)
		return nil
	default:
		return fmt.Errorf("unknown media action %q", action)
	}
}
