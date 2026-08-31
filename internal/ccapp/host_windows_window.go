//go:build windows

package ccapp

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

type windowListState struct {
	fg      uintptr
	windows []WindowInfo
}

var (
	windowListMu  sync.Mutex
	windowListSeq uintptr
	windowLists   = map[uintptr]*windowListState{}
)

var enumWindowListCallback = sync.OnceValue(func() uintptr {
	return syscall.NewCallback(enumWindowListProc)
})

func enumWindowListProc(hwnd uintptr, lParam uintptr) uintptr {
	windowListMu.Lock()
	st := windowLists[lParam]
	windowListMu.Unlock()
	if st == nil {
		return 1
	}
	if len(st.windows) >= CcMaxListedWindows {
		return 0
	}
	vis, _, _ := procIsWindowVisible.Call(hwnd)
	if vis == 0 {
		return 1
	}
	title := windowText(hwnd)
	class := windowClass(hwnd)
	if title == "" && class != "#32770" && class != "#32768" {
		return 1
	}
	_, process, _ := windowProcess(hwnd)
	x, y, w, h := windowRect(hwnd)
	if w <= 0 || h <= 0 {
		return 1
	}
	st.windows = append(st.windows, WindowInfo{
		ID:         fmt.Sprintf("0x%X", hwnd),
		Title:      title,
		Process:    process,
		Class:      class,
		X:          x,
		Y:          y,
		W:          w,
		H:          h,
		Foreground: hwnd == st.fg,
	})
	return 1
}

func (h *windowsHost) ListWindows() ([]WindowInfo, error) {
	fg, _, _ := procGetForegroundWindow.Call()
	windowListMu.Lock()
	windowListSeq++
	token := windowListSeq
	st := &windowListState{fg: fg}
	windowLists[token] = st
	windowListMu.Unlock()
	defer func() {
		windowListMu.Lock()
		delete(windowLists, token)
		windowListMu.Unlock()
	}()
	_, _, _ = procEnumWindowsCC.Call(enumWindowListCallback(), token)
	return st.windows, nil
}

func (h *windowsHost) FocusWindow(query string) (WindowInfo, error) {
	hwnd, info, err := findWindow(query)
	if err != nil {
		return WindowInfo{}, err
	}
	forceForeground(hwnd)
	time.Sleep(40 * time.Millisecond)
	h.rememberHWND(hwnd)
	info.Foreground = true
	return info, nil
}

func (h *windowsHost) WindowCapture(query string) ([]byte, int, int, error) {
	query = strings.TrimSpace(query)
	var hwnd uintptr
	if query == "" || strings.EqualFold(query, "foreground") {
		hwnd, _, _ = procGetForegroundWindow.Call()
		if hwnd == 0 {
			return nil, 0, 0, fmt.Errorf("no foreground window")
		}
	} else {
		var err error
		hwnd, _, err = findWindow(query)
		if err != nil {
			return nil, 0, 0, err
		}
	}
	return captureWindowHWND(hwnd)
}

func findWindow(query string) (uintptr, WindowInfo, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return 0, WindowInfo{}, fmt.Errorf("empty window query")
	}
	if strings.HasPrefix(query, "0x") || strings.HasPrefix(query, "0X") {
		if id, err := strconv.ParseUint(query[2:], 16, 64); err == nil && id != 0 {
			hwnd := uintptr(id)
			if ok, _, _ := procIsWindow.Call(hwnd); ok == 0 {
				return 0, WindowInfo{}, fmt.Errorf("window %s is gone", query)
			}
			title, process, _ := windowProcess(hwnd)
			x, y, w, h := windowRect(hwnd)
			return hwnd, WindowInfo{
				ID: fmt.Sprintf("0x%X", hwnd), Title: title, Process: process,
				Class: windowClass(hwnd), X: x, Y: y, W: w, H: h,
			}, nil
		}
	}
	wins, err := (&windowsHost{}).ListWindows()
	if err != nil {
		return 0, WindowInfo{}, err
	}
	best, ok := MatchWindow(wins, query)
	if !ok {
		return 0, WindowInfo{}, fmt.Errorf("no window matching %q", query)
	}
	id, err := strconv.ParseUint(strings.TrimPrefix(best.ID, "0x"), 16, 64)
	if err != nil {
		return 0, best, fmt.Errorf("window id %s", best.ID)
	}
	return uintptr(id), best, nil
}
