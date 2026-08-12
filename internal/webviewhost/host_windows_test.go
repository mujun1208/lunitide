//go:build windows

package webviewhost

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/zzl/go-win32api/v2/win32"
)

func TestHostWindowStyleUsesStandardWindowsFrame(t *testing.T) {
	required := []win32.WINDOW_STYLE{
		win32.WS_CAPTION,
		win32.WS_SYSMENU,
		win32.WS_THICKFRAME,
		win32.WS_MINIMIZEBOX,
		win32.WS_MAXIMIZEBOX,
	}
	for _, style := range required {
		if hostWindowStyle&style != style {
			t.Fatalf("hostWindowStyle=%#x missing required style %#x", hostWindowStyle, style)
		}
	}
	if hostWindowStyle&win32.WS_POPUP != 0 {
		t.Fatalf("hostWindowStyle=%#x unexpectedly uses WS_POPUP", hostWindowStyle)
	}
}

func TestDispatchAndWaitReturnsOnQueueRejectionAndClosePostFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	h := &Host{runCtx: ctx, runCancel: cancel, uiQueue: NewBoundedQueue[func()](1)}
	if ok, _ := h.uiQueue.Push(func() {}); !ok {
		t.Fatal("failed to prefill queue")
	}
	var messages []uint32
	h.postMessage = func(_ win32.HWND, message uint32, _ win32.WPARAM, _ win32.LPARAM) (win32.BOOL, win32.WIN32_ERROR) {
		messages = append(messages, message)
		return 0, win32.ERROR_INVALID_WINDOW_HANDLE
	}
	done := make(chan bool, 1)
	go func() { done <- h.dispatchAndWait(func() bool { return true }) }()
	select {
	case delivered := <-done:
		if delivered {
			t.Fatal("rejected UI work reported delivered")
		}
	case <-time.After(time.Second):
		t.Fatal("queue rejection left waiter blocked")
	}
	if len(messages) != 1 || messages[0] != win32.WM_CLOSE {
		t.Fatalf("posted messages=%v want [WM_CLOSE]", messages)
	}
	if h.runErr == nil || !strings.Contains(h.runErr.Error(), "bounded UI queue exhausted") {
		t.Fatalf("runErr=%v", h.runErr)
	}
}

func TestDispatchAndWaitReturnsWhenNotificationAndClosePostsFail(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	h := &Host{runCtx: ctx, runCancel: cancel, uiQueue: NewBoundedQueue[func()](1)}
	var messages []uint32
	h.postMessage = func(_ win32.HWND, message uint32, _ win32.WPARAM, _ win32.LPARAM) (win32.BOOL, win32.WIN32_ERROR) {
		messages = append(messages, message)
		return 0, win32.ERROR_INVALID_WINDOW_HANDLE
	}
	done := make(chan bool, 1)
	go func() { done <- h.dispatchAndWait(func() bool { return true }) }()
	select {
	case delivered := <-done:
		if delivered {
			t.Fatal("unnotified UI work reported delivered")
		}
	case <-time.After(time.Second):
		t.Fatal("notification failure left waiter blocked")
	}
	if len(messages) != 2 || messages[0] != uiMessage || messages[1] != win32.WM_CLOSE {
		t.Fatalf("posted messages=%v want [uiMessage WM_CLOSE]", messages)
	}
	if h.runErr == nil || !strings.Contains(h.runErr.Error(), "PostMessage(uiMessage) failed") {
		t.Fatalf("runErr=%v", h.runErr)
	}
}
