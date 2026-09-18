//go:build !windows

package doctext

import (
	"context"
	"os"
	"testing"
)

func TestWindowsOCRProbeOtherStub(t *testing.T) {
	t.Setenv("LUNITIDE_OCR_LIVE_PROBE", "1")
	SetWindowsOCRProbeRunnerForTest(nil)
	ResetWindowsOCRProbeCacheForTest()
	got := probeWindowsOCRPlatform(context.Background())
	if got.State != WindowsOCRUnsupportedOS || got.Available {
		t.Fatalf("non-windows platform stub: %+v", got)
	}
	_ = os.Unsetenv("LUNITIDE_OCR_LIVE_PROBE")
}
