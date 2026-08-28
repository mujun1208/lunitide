package webviewhost

import (
	"strings"
	"testing"
	"time"
)

func TestShouldSkipWebViewBounds(t *testing.T) {
	if !shouldSkipWebViewBounds(0, 0) {
		t.Fatal("0x0 client after minimize must not be applied as WebView bounds")
	}
	if !shouldSkipWebViewBounds(1280, 0) {
		t.Fatal("zero-height client must not be applied as WebView bounds")
	}
	if shouldSkipWebViewBounds(1280, 800) {
		t.Fatal("normal client was skipped")
	}
}

func TestSurfaceActionHidesOnMinimizeAndShowWindowHide(t *testing.T) {
	if got := surfaceActionForMessage(wmSize, sizeMinimized, false); got != surfaceHide {
		t.Fatalf("SIZE_MINIMIZED action=%v want hide", got)
	}
	if got := surfaceActionForMessage(wmShowWindow, 0, false); got != surfaceHide {
		t.Fatalf("WM_SHOWWINDOW hide action=%v want hide", got)
	}
}

func TestSurfaceActionWakesAfterMinimizeAndSleep(t *testing.T) {
	if got := surfaceActionForMessage(wmSize, sizeRestored, true); got != surfaceWake {
		t.Fatalf("SIZE_RESTORED after minimize action=%v want wake", got)
	}
	if got := surfaceActionForMessage(wmSize, sizeMaximized, true); got != surfaceWake {
		t.Fatalf("SIZE_MAXIMIZED after minimize action=%v want wake", got)
	}
	if got := surfaceActionForMessage(wmShowWindow, 1, true); got != surfaceWake {
		t.Fatalf("WM_SHOWWINDOW show action=%v want wake", got)
	}
	if got := surfaceActionForMessage(wmShowWindow, 1, false); got != surfaceFit {
		t.Fatalf("WM_SHOWWINDOW while already visible action=%v want fit (not Resume)", got)
	}
	if got := surfaceActionForMessage(wmPowerBroadcast, pbtAPMResumeAutomatic, false); got != surfaceWake {
		t.Fatalf("PBT_APMRESUMEAUTOMATIC action=%v want wake", got)
	}
	if got := surfaceActionForMessage(wmPowerBroadcast, pbtAPMResumeSuspend, false); got != surfaceWake {
		t.Fatalf("PBT_APMRESUMESUSPEND action=%v want wake", got)
	}
	if got := surfaceActionForMessage(wmExitSizeMove, 0, false); got != surfaceFit {
		t.Fatalf("WM_EXITSIZEMOVE action=%v want fit (Resume is occlusion-only)", got)
	}
	if got := surfaceActionForMessage(wmActivate, waClickActive, true); got != surfaceWake {
		t.Fatalf("WM_ACTIVATE after hide action=%v want wake", got)
	}
}

func TestSurfaceActionFitsOrdinaryResizeWithoutReload(t *testing.T) {
	if got := surfaceActionForMessage(wmSize, sizeRestored, false); got != surfaceFit {
		t.Fatalf("live SIZE_RESTORED action=%v want fit (not wake/reload)", got)
	}
	if got := surfaceActionForMessage(wmWindowPosChanged, 0, false); got != surfaceNotify {
		t.Fatalf("WM_WINDOWPOSCHANGED move action=%v want notify", got)
	}
	if got := surfaceActionForMessage(wmWindowPosChanged, swpNoMove, false); got != surfaceNone {
		t.Fatalf("size-only WINDOWPOSCHANGED action=%v want none (WM_SIZE fits)", got)
	}
	if got := surfaceActionForMessage(wmActivate, waActive, false); got != surfaceNotify {
		t.Fatalf("WM_ACTIVATE while shown action=%v want notify", got)
	}
	if got := surfaceActionForMessage(wmActivate, waInactive, true); got != surfaceNone {
		t.Fatalf("WM_ACTIVATE inactive action=%v want none", got)
	}
	if got := surfaceActionForMessage(wmPowerBroadcast, 0, false); got != surfaceNone {
		t.Fatalf("non-resume power event action=%v want none", got)
	}
}

func TestParseScriptRestoreAction(t *testing.T) {
	if got := parseScriptRestoreAction(`"reload"`); got != "reload" {
		t.Fatalf("quoted reload=%q", got)
	}
	if got := parseScriptRestoreAction("repaint"); got != "repaint" {
		t.Fatalf("repaint=%q", got)
	}
	if got := parseScriptRestoreAction("null"); got != "ok" {
		t.Fatalf("null=%q want ok", got)
	}
}

func TestShouldReloadFromScriptResultRespectsBootAndCooldown(t *testing.T) {
	now := time.Date(2026, 8, 28, 10, 0, 0, 0, time.UTC)
	if shouldReloadFromScriptResult(`"reload"`, true, time.Time{}, now, restoreReloadCooldown) {
		t.Fatal("reload during initial navigation")
	}
	if !shouldReloadFromScriptResult(`"reload"`, false, time.Time{}, now, restoreReloadCooldown) {
		t.Fatal("blank-document reload was suppressed")
	}
	if shouldReloadFromScriptResult(`"repaint"`, false, time.Time{}, now, restoreReloadCooldown) {
		t.Fatal("repaint must not Reload (companion audio / people P2P)")
	}
	last := now.Add(-5 * time.Second)
	if shouldReloadFromScriptResult(`"reload"`, false, last, now, restoreReloadCooldown) {
		t.Fatal("reload cooldown was ignored")
	}
}

func TestShouldReloadAfterScriptErrorOnlyFromOcclusion(t *testing.T) {
	now := time.Date(2026, 8, 28, 10, 0, 0, 0, time.UTC)
	if shouldReloadAfterScriptError(true, false, false, time.Time{}, now, restoreReloadCooldown) {
		t.Fatal("script error during resize must not Reload")
	}
	if !shouldReloadAfterScriptError(true, true, false, time.Time{}, now, restoreReloadCooldown) {
		t.Fatal("dead renderer after hide/sleep should Reload")
	}
	if shouldReloadAfterScriptError(true, true, true, time.Time{}, now, restoreReloadCooldown) {
		t.Fatal("script error during first load must not Reload")
	}
}

func TestRestoreScriptAsksPageInsteadOfBlindNavigate(t *testing.T) {
	if !strings.Contains(restoreScript, "__lunitideRestoreSurface") {
		t.Fatal("restore script must call the page helper when present")
	}
	if strings.Contains(restoreScript, "location.reload") {
		t.Fatal("host script must not location.reload; host Reload is gated")
	}
}

func TestShouldApplyWebViewBoundsSkipsUnchangedAndJitter(t *testing.T) {
	prev := clientBounds{Right: 1280, Bottom: 800}
	now := time.Date(2026, 8, 28, 18, 0, 0, 0, time.UTC)
	if shouldApplyWebViewBounds(prev, prev, true, false, false, now.Add(-time.Second), now, boundsApplyMinInterval) {
		t.Fatal("unchanged client rect must not SetBounds")
	}
	if shouldApplyWebViewBounds(clientBounds{Right: 1280, Bottom: 799}, prev, true, false, false, now.Add(-time.Second), now, boundsApplyMinInterval) {
		t.Fatal("1px compositor jitter must not SetBounds")
	}
	if shouldApplyWebViewBounds(clientBounds{Right: 1400, Bottom: 900}, prev, true, true, false, now.Add(-time.Second), now, boundsApplyMinInterval) {
		t.Fatal("re-entrant apply during fitting must be skipped even for a real resize")
	}
	if !shouldApplyWebViewBounds(clientBounds{Right: 1280, Bottom: 799}, prev, true, false, true, now.Add(-time.Second), now, boundsApplyMinInterval) {
		t.Fatal("live user resize must apply 1px steps")
	}
	if shouldApplyWebViewBounds(clientBounds{Right: 1400, Bottom: 900}, prev, true, false, false, now.Add(-time.Millisecond), now, boundsApplyMinInterval) {
		t.Fatal("non-live SetBounds inside the debounce window must be coalesced")
	}
	if !shouldApplyWebViewBounds(clientBounds{Right: 1400, Bottom: 900}, prev, true, false, false, now.Add(-time.Second), now, boundsApplyMinInterval) {
		t.Fatal("real restore/maximize after debounce must SetBounds")
	}
	if shouldApplyWebViewBounds(clientBounds{}, prev, true, false, false, time.Time{}, now, boundsApplyMinInterval) {
		t.Fatal("zero client after minimize must still be skipped")
	}
}

func TestShouldNotifyParentWindowSkipsSizeOnlyAndJitter(t *testing.T) {
	if shouldNotifyParentWindow(swpNoMove, false, false, 0, 0, 40, 40) {
		t.Fatal("SWP_NOMOVE must not NotifyParentWindowPositionChanged")
	}
	if shouldNotifyParentWindow(0, false, true, 100, 80, 100, 80) {
		t.Fatal("unchanged screen position must not notify")
	}
	if shouldNotifyParentWindow(0, false, true, 100, 80, 101, 81) {
		t.Fatal("1px DWM jitter must not notify")
	}
	if !shouldNotifyParentWindow(0, true, true, 100, 80, 101, 81) {
		t.Fatal("live title-bar drag must notify 1px steps")
	}
	if !shouldNotifyParentWindow(0, false, true, 100, 80, 180, 120) {
		t.Fatal("real window move must notify")
	}
}

func TestShouldResumeRendererOnlyAfterOcclusion(t *testing.T) {
	if shouldResumeRenderer(false) {
		t.Fatal("Resume must not run on ordinary resize, EXITSIZEMOVE, or chat tokens")
	}
	if !shouldResumeRenderer(true) {
		t.Fatal("Resume after minimize/sleep/hide must still run")
	}
}

func TestShouldAdoptDpiSuggestedRectSkipsUnchanged(t *testing.T) {
	cur := clientBounds{Left: 10, Top: 10, Right: 1300, Bottom: 820}
	if shouldAdoptDpiSuggestedRect(cur, cur) {
		t.Fatal("identical WM_DPICHANGED rect must not SetWindowPos")
	}
	next := clientBounds{Left: 10, Top: 10, Right: 1920, Bottom: 1080}
	if !shouldAdoptDpiSuggestedRect(cur, next) {
		t.Fatal("real DPI suggested rect must be adopted")
	}
}

func TestWebViewFillIsDocumentDarkNotWhite(t *testing.T) {
	if webViewFillA != 255 || webViewFillR == 255 && webViewFillG == 255 && webViewFillB == 255 {
		t.Fatal("controller fill must not be opaque white")
	}
	if webViewFillR != 3 || webViewFillG != 6 || webViewFillB != 12 {
		t.Fatalf("fill=#%02x%02x%02x want #03060c", webViewFillR, webViewFillG, webViewFillB)
	}
}
