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
		return surfaceWake
	case wmPowerBroadcast:
		if isPowerResume(wParam) {
			return surfaceWake
		}
	case wmExitSizeMove:
		return surfaceWake
	case wmWindowPosChanged:
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
