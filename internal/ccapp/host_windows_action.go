//go:build windows

package ccapp

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	wmClose         = 0x0010
	swHide          = 0
	swShowMaximized = 3
	swMinimize      = 6
)

func hwndFromInfo(info WindowInfo) (uintptr, error) {
	raw := strings.TrimPrefix(strings.TrimPrefix(info.ID, "0x"), "0X")
	id, err := strconv.ParseUint(raw, 16, 64)
	if err != nil || id == 0 {
		return 0, fmt.Errorf("window id %s", info.ID)
	}
	hwnd := uintptr(id)
	if ok, _, _ := procIsWindow.Call(hwnd); ok == 0 {
		return 0, fmt.Errorf("window %s is gone", info.ID)
	}
	return hwnd, nil
}

func (h *windowsHost) resolveWindow(query string) (uintptr, WindowInfo, error) {
	query = strings.TrimSpace(query)
	if query == "" || strings.EqualFold(query, "foreground") {
		hwnd, _, _ := procGetForegroundWindow.Call()
		if hwnd == 0 {
			return 0, WindowInfo{}, fmt.Errorf("no foreground window")
		}
		title, process, _ := windowProcess(hwnd)
		x, y, w, ht := windowRect(hwnd)
		return hwnd, WindowInfo{
			ID: fmt.Sprintf("0x%X", hwnd), Title: title, Process: process,
			Class: windowClass(hwnd), X: x, Y: y, W: w, H: ht, Foreground: true,
		}, nil
	}
	return findWindow(query)
}

func (h *windowsHost) WindowAction(query, op string, x, y, w, height int) (WindowInfo, error) {
	hwnd, info, err := h.resolveWindow(query)
	if err != nil {
		return WindowInfo{}, err
	}
	op = strings.ToLower(strings.TrimSpace(op))
	if destructiveWindowOp(op) && ProtectedDesktopProcess(info.Process) {
		return info, fmt.Errorf("%w: protected process %s", ErrCcRiskBlocked, info.Process)
	}
	switch op {
	case "close":
		r, _, err := procPostMessageW.Call(hwnd, wmClose, 0, 0)
		if r == 0 {
			return info, fmt.Errorf("close: %w", err)
		}
	case "hide":
		_, _, _ = procShowWindow.Call(hwnd, uintptr(swHide))
	case "minimize":
		_, _, _ = procShowWindow.Call(hwnd, uintptr(swMinimize))
	case "maximize":
		forceForeground(hwnd)
		_, _, _ = procShowWindow.Call(hwnd, uintptr(swShowMaximized))
	case "restore":
		forceForeground(hwnd)
		_, _, _ = procShowWindow.Call(hwnd, uintptr(swRestore))
	case "move":
		if w <= 0 || height <= 0 {
			w, height = info.W, info.H
		}
		if ok, _, err := procMoveWindow.Call(hwnd, uintptr(uint32(int32(x))), uintptr(uint32(int32(y))),
			uintptr(w), uintptr(height), 1); ok == 0 {
			return info, fmt.Errorf("move: %w", err)
		}
	case "resize":
		if w < 1 || height < 1 {
			return info, fmt.Errorf("resize needs w,h")
		}
		if ok, _, err := procMoveWindow.Call(hwnd, uintptr(uint32(int32(info.X))), uintptr(uint32(int32(info.Y))),
			uintptr(w), uintptr(height), 1); ok == 0 {
			return info, fmt.Errorf("resize: %w", err)
		}
	default:
		return info, fmt.Errorf("unknown window op %q", op)
	}
	time.Sleep(40 * time.Millisecond)
	if nx, ny, nw, nh := windowRect(hwnd); nw > 0 && nh > 0 {
		info.X, info.Y, info.W, info.H = nx, ny, nw, nh
	}
	return info, nil
}

func (h *windowsHost) QuitApp(query string) (int, WindowInfo, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return 0, WindowInfo{}, fmt.Errorf("empty app query")
	}
	wins, err := h.ListWindows()
	if err != nil {
		return 0, WindowInfo{}, err
	}
	hits := MatchWindows(wins, query)
	if len(hits) == 0 {
		return 0, WindowInfo{}, fmt.Errorf("no window matching %q", query)
	}
	closed := 0
	var sample WindowInfo
	var blocked string
	for _, w := range hits {
		if ProtectedDesktopProcess(w.Process) {
			blocked = w.Process
			continue
		}
		hwnd, err := hwndFromInfo(w)
		if err != nil {
			continue
		}
		if r, _, _ := procPostMessageW.Call(hwnd, wmClose, 0, 0); r != 0 {
			closed++
			sample = w
		}
	}
	if closed == 0 {
		if blocked != "" {
			return 0, WindowInfo{}, fmt.Errorf("%w: protected process %s", ErrCcRiskBlocked, blocked)
		}
		return 0, WindowInfo{}, fmt.Errorf("failed to close windows matching %q", query)
	}
	return closed, sample, nil
}
