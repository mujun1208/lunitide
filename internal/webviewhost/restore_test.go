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
	if got := surfaceActionForMessage(wmPowerBroadcast, pbtAPMResumeAutomatic, false); got != surfaceWake {
		t.Fatalf("PBT_APMRESUMEAUTOMATIC action=%v want wake", got)
	}
	if got := surfaceActionForMessage(wmPowerBroadcast, pbtAPMResumeSuspend, false); got != surfaceWake {
		t.Fatalf("PBT_APMRESUMESUSPEND action=%v want wake", got)
	}
	if got := surfaceActionForMessage(wmExitSizeMove, 0, false); got != surfaceWake {
		t.Fatalf("WM_EXITSIZEMOVE action=%v want wake", got)
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
		t.Fatalf("WM_WINDOWPOSCHANGED action=%v want notify", got)
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

func TestWebViewFillIsDocumentDarkNotWhite(t *testing.T) {
	if webViewFillA != 255 || webViewFillR == 255 && webViewFillG == 255 && webViewFillB == 255 {
		t.Fatal("controller fill must not be opaque white")
	}
	if webViewFillR != 3 || webViewFillG != 6 || webViewFillB != 12 {
		t.Fatalf("fill=#%02x%02x%02x want #03060c", webViewFillR, webViewFillG, webViewFillB)
	}
}
