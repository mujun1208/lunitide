package doctext

import (
	"context"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// WindowsOCRProbeState is the closed set of Windows.Media.Ocr probe outcomes.
type WindowsOCRProbeState string

const (
	WindowsOCRReady             WindowsOCRProbeState = "ready"
	WindowsOCRUnsupportedOS     WindowsOCRProbeState = "unsupported_os"
	WindowsOCRInitializationErr WindowsOCRProbeState = "initialization_failed"
	WindowsOCRLanguageMissing   WindowsOCRProbeState = "language_unavailable"
	WindowsOCRSampleFailed      WindowsOCRProbeState = "sample_failed"
	WindowsOCRTimedOut          WindowsOCRProbeState = "timed_out"
	windowsOCRProbeTTL                               = 5 * time.Minute
	windowsOCRProbeTimeout                           = 15 * time.Second
)

// WindowsOCRProbe is the honest local-OCR snapshot. Available is true only for ready.
type WindowsOCRProbe struct {
	State     WindowsOCRProbeState `json:"state"`
	Available bool                 `json:"available"`
	Languages []string             `json:"languages"`
	ErrorCode string               `json:"errorCode,omitempty"`
	CheckedAt time.Time            `json:"checkedAt"`
}

type windowsOCRProbeFn func(context.Context) WindowsOCRProbe

var (
	windowsOCRProbeTest atomic.Value
	windowsOCRProbeMu   sync.Mutex
	windowsOCRProbeLast WindowsOCRProbe
	windowsOCRProbeUntil time.Time
)

// SetWindowsOCRProbeRunnerForTest injects the WinRT launcher. Pass nil to restore.
func SetWindowsOCRProbeRunnerForTest(fn windowsOCRProbeFn) {
	if fn == nil {
		windowsOCRProbeTest.Store(windowsOCRProbeFn(nil))
		return
	}
	windowsOCRProbeTest.Store(fn)
}

func windowsOCRProbeTestRunner() windowsOCRProbeFn {
	v := windowsOCRProbeTest.Load()
	if v == nil {
		return nil
	}
	fn, _ := v.(windowsOCRProbeFn)
	return fn
}

func finalizeWindowsOCRProbe(p WindowsOCRProbe) WindowsOCRProbe {
	p.Available = p.State == WindowsOCRReady
	if p.CheckedAt.IsZero() {
		p.CheckedAt = time.Now().UTC()
	}
	if p.Languages == nil {
		p.Languages = []string{}
	}
	return p
}

// ProbeWindowsOCR returns a six-state snapshot. runtime.GOOS never implies ready.
func ProbeWindowsOCR(ctx context.Context) WindowsOCRProbe {
	return probeWindowsOCR(ctx, false)
}

// RefreshWindowsOCRProbe drops the 5-minute cache and probes again.
func RefreshWindowsOCRProbe(ctx context.Context) WindowsOCRProbe {
	return probeWindowsOCR(ctx, true)
}

func probeWindowsOCR(ctx context.Context, refresh bool) WindowsOCRProbe {
	if ctx == nil {
		ctx = context.Background()
	}
	if fn := windowsOCRProbeTestRunner(); fn != nil {
		return finalizeWindowsOCRProbe(fn(ctx))
	}
	if testing.Testing() && os.Getenv("LUNITIDE_OCR_LIVE_PROBE") == "" {
		return finalizeWindowsOCRProbe(WindowsOCRProbe{State: WindowsOCRUnsupportedOS})
	}
	windowsOCRProbeMu.Lock()
	defer windowsOCRProbeMu.Unlock()
	if refresh {
		windowsOCRProbeLast = WindowsOCRProbe{}
		windowsOCRProbeUntil = time.Time{}
	}
	if !refresh && !windowsOCRProbeLast.CheckedAt.IsZero() && time.Now().Before(windowsOCRProbeUntil) {
		return windowsOCRProbeLast
	}
	runCtx, cancel := context.WithTimeout(ctx, windowsOCRProbeTimeout)
	defer cancel()
	p := finalizeWindowsOCRProbe(probeWindowsOCRPlatform(runCtx))
	if runCtx.Err() != nil && p.State != WindowsOCRReady {
		p.State = WindowsOCRTimedOut
		p.ErrorCode = "OCR_PROBE_TIMEOUT"
		p.Available = false
	}
	windowsOCRProbeLast = p
	windowsOCRProbeUntil = time.Now().Add(windowsOCRProbeTTL)
	return p
}

// CachedWindowsOCRProbe is the 5-minute health cache used by OCR routing.
func CachedWindowsOCRProbe() WindowsOCRProbe {
	return ProbeWindowsOCR(context.Background())
}

// ResetWindowsOCRProbeCacheForTest drops the production cache.
func ResetWindowsOCRProbeCacheForTest() {
	windowsOCRProbeMu.Lock()
	windowsOCRProbeLast = WindowsOCRProbe{}
	windowsOCRProbeUntil = time.Time{}
	windowsOCRProbeMu.Unlock()
}
