package webviewhost

import (
	"strings"
	"time"
)

// Win32 message and size constants used by the restore policy. Duplicated so
// the decision logic stays COM-free and regression-testable off the STA
// thread. Values must match win32.WM_* / SIZE_* / PBT_* / WA_*.
const (
	wmActivate            = 0x0006
	wmSize                = 0x0005
	wmShowWindow          = 0x0018
	wmWindowPosChanged    = 0x0047
	wmPowerBroadcast      = 0x0218
	wmExitSizeMove        = 0x0232
	sizeRestored          = 0
	sizeMinimized         = 1
	sizeMaximized         = 2
	pbtAPMResumeCritical  = 0x6
	pbtAPMResumeSuspend   = 0x7
	pbtAPMResumeStandby   = 0x8
	pbtAPMResumeAutomatic = 0x12
	waInactive            = 0
	waActive              = 1
	waClickActive         = 2
	// SWP_NOSIZE / SWP_NOMOVE from WINDOWPOS.flags. WM_WINDOWPOSCHANGED
	// with SWP_NOMOVE is a size/z-order/activate tick — WM_SIZE already
	// refits the controller. Notifying on those ticks is the SetBounds ↔
	// NotifyParentWindowPositionChanged loop that makes the native frame
	// tremble while the workbench streams.
	swpNoSize = 0x0001
	swpNoMove = 0x0002
)

const (
	boundsApplyMinInterval = 16 * time.Millisecond
	boundsJitterPx         = int32(2)
)

// WebView fill matches html,#root { background:#03060c }. WebView2's default
// controller background and COLOR_WINDOW are white; after a GPU/DWM hang the
// unpainted client area was that white rectangle with a live title bar.
const (
	webViewFillA = 255
	webViewFillR = 3
	webViewFillG = 6
	webViewFillB = 12
)

const restoreReloadCooldown = 30 * time.Second

// restoreScript asks the page to reflow, or to report that #root is empty so
// the host can Reload. It never calls location.reload itself: a second reload
// from the host would double-navigate. Secrets are not passed here.
const restoreScript = `(function(){try{if(typeof window.__lunitideRestoreSurface==='function'){return window.__lunitideRestoreSurface();}var root=document.getElementById('root');if(!document.body||!root||root.childElementCount===0){return 'reload';}void document.body.offsetHeight;return 'repaint';}catch(e){return 'reload';}})()`

type surfaceAction int

const (
	surfaceNone surfaceAction = iota
	surfaceHide
	surfaceFit
	surfaceWake
	surfaceNotify
)

func shouldSkipWebViewBounds(width, height int32) bool {
	return width <= 0 || height <= 0
}

type clientBounds struct {
	Left, Top, Right, Bottom int32
}

func (b clientBounds) width() int32  { return b.Right - b.Left }
func (b clientBounds) height() int32 { return b.Bottom - b.Top }

func abs32(v int32) int32 {
	if v < 0 {
		return -v
	}
	return v
}

func boundsManhattan(a, b clientBounds) int32 {
	return abs32(a.Left-b.Left) + abs32(a.Top-b.Top) + abs32(a.Right-b.Right) + abs32(a.Bottom-b.Bottom)
}

// shouldApplyWebViewBounds drops no-op and re-entrant SetBounds calls.
// Unchanged rects and 1–2px compositor jitter must not be written back:
// WebView2 put_Bounds can emit WM_SIZE / WM_WINDOWPOSCHANGED, which used
// to call SetBounds again and oscillate the Win32 frame. Live user resize
// still applies every distinct rect. Rapid non-live applies are coalesced
// to boundsApplyMinInterval so chat tokens cannot pump the controller.
func shouldApplyWebViewBounds(next, prev clientBounds, hasPrev, fitting, liveResize bool, last, now time.Time, minInterval time.Duration) bool {
	if fitting {
		return false
	}
	if shouldSkipWebViewBounds(next.width(), next.height()) {
		return false
	}
	if hasPrev && next == prev {
		return false
	}
	if hasPrev && !liveResize && boundsManhattan(prev, next) <= boundsJitterPx {
		return false
	}
	if hasPrev && !liveResize && minInterval > 0 && !last.IsZero() && now.Sub(last) < minInterval {
		return false
	}
	return true
}

// shouldNotifyParentWindow is true only when the host HWND actually moved.
// Size-only WINDOWPOSCHANGED (SWP_NOMOVE) is handled by WM_SIZE. 1–2px
// position jitter outside a user drag must not call
// NotifyParentWindowPositionChanged — that call itself can shift the frame.
func shouldNotifyParentWindow(posFlags uint32, live, hasLast bool, lastX, lastY, x, y int32) bool {
	if posFlags&swpNoMove != 0 {
		return false
	}
	if hasLast && lastX == x && lastY == y {
		return false
	}
	if hasLast && !live && abs32(lastX-x) <= boundsJitterPx && abs32(lastY-y) <= boundsJitterPx {
		return false
	}
	return true
}

// shouldResumeRenderer gates ICoreWebView2_3.Resume. Resume after ordinary
// WM_SIZE / WM_EXITSIZEMOVE / already-visible WM_SHOWWINDOW is what made
// streaming workbench tokens look like a restore loop.
func shouldResumeRenderer(fromOcclusion bool) bool {
	return fromOcclusion
}

func shouldAdoptDpiSuggestedRect(current, suggested clientBounds) bool {
	if current == suggested {
		return false
	}
	return !shouldSkipWebViewBounds(suggested.Right-suggested.Left, suggested.Bottom-suggested.Top)
}

func isPowerResume(wParam uint32) bool {
	switch wParam {
	case pbtAPMResumeCritical, pbtAPMResumeSuspend, pbtAPMResumeStandby, pbtAPMResumeAutomatic:
		return true
	default:
		return false
	}
}

func isActivate(wParam uint32) bool {
	switch wParam & 0xFFFF {
	case waActive, waClickActive:
		return true
	default:
		return false
	}
}

// surfaceActionForMessage maps a Win32 message to the WebView2 surface work
// that must follow. Minimize/hide must not SetBounds(0,0): that is the classic
// WebView2 compositor stall that survives restore as a blank client area.
func surfaceActionForMessage(message, wParam uint32, hidden bool) surfaceAction {
	switch message {
	case wmSize:
		switch wParam {
		case sizeMinimized:
			return surfaceHide
		case sizeRestored, sizeMaximized:
			if hidden {
				return surfaceWake
			}
			return surfaceFit
		}
	case wmShowWindow:
		if wParam == 0 {
			return surfaceHide
		}
		if hidden {
			return surfaceWake
		}
		return surfaceFit
	case wmPowerBroadcast:
		if isPowerResume(wParam) {
			return surfaceWake
		}
	case wmExitSizeMove:
		// Drag-resize already ran WM_SIZE → fit. Resume here was a
		// compositor kick on every mouse-up and during layout storms.
		return surfaceFit
	case wmWindowPosChanged:
		// wParam is WINDOWPOS.flags (the real message carries them in lParam).
		if wParam&swpNoMove != 0 {
			return surfaceNone
		}
		return surfaceNotify
	case wmActivate:
		if isActivate(wParam) {
			if hidden {
				return surfaceWake
			}
			return surfaceNotify
		}
	}
	return surfaceNone
}

func parseScriptRestoreAction(resultJSON string) string {
	s := strings.TrimSpace(resultJSON)
	s = strings.Trim(s, `"`)
	switch s {
	case "reload", "repaint", "ok":
		return s
	default:
		return "ok"
	}
}

func shouldReloadFromScriptResult(resultJSON string, initialPending bool, lastReload, now time.Time, cooldown time.Duration) bool {
	if initialPending || parseScriptRestoreAction(resultJSON) != "reload" {
		return false
	}
	if cooldown > 0 && !lastReload.IsZero() && now.Sub(lastReload) < cooldown {
		return false
	}
	return true
}

func shouldReloadAfterScriptError(scriptFailed, fromOcclusion, initialPending bool, lastReload, now time.Time, cooldown time.Duration) bool {
	if !scriptFailed || !fromOcclusion || initialPending {
		return false
	}
	if cooldown > 0 && !lastReload.IsZero() && now.Sub(lastReload) < cooldown {
		return false
	}
	return true
}
