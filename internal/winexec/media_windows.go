//go:build windows

package winexec

import (
	"fmt"
	"strings"
	"syscall"
	"unsafe"
)

const (
	inputKeyboard     = 1
	keyEventfKeyUp    = 0x0002
	vkMediaPlayPause  = 0xB3
	vkMediaNextTrack  = 0xB0
	vkMediaPrevTrack  = 0xB1
	vkMediaStop       = 0xB2
)

var procSendInputMedia = syscall.NewLazyDLL("user32.dll").NewProc("SendInput")

type mediaKeyEvent struct {
	Type      uint32
	Flag      uint32
	Time      uint32
	ExtraInfo uintptr
	WVk       uint16
	WScan     uint16
	DwFlags   uint32
	TimeStamp uint32
	_         uint64
}

func sendMediaVK(vk uint16) error {
	down := mediaKeyEvent{Type: inputKeyboard, WVk: vk}
	up := mediaKeyEvent{Type: inputKeyboard, WVk: vk, DwFlags: keyEventfKeyUp}
	events := []mediaKeyEvent{down, up}
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

// SendMediaKey sends one Windows media key (play, pause, next, prev, stop).
func SendMediaKey(action string) error {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "play", "pause", "play_pause", "toggle":
		return sendMediaVK(vkMediaPlayPause)
	case "next", "skip":
		return sendMediaVK(vkMediaNextTrack)
	case "prev", "previous":
		return sendMediaVK(vkMediaPrevTrack)
	case "stop":
		return sendMediaVK(vkMediaStop)
	default:
		return fmt.Errorf("unknown media action %q", action)
	}
}
