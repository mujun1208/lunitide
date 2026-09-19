package doctext

import (
	"context"
	"testing"
)

func TestRepairWindowsOCRUsesHookThenRefresh(t *testing.T) {
	repaired := 0
	probed := 0
	t.Cleanup(func() {
		SetWindowsOCRRepairForTest(nil)
		SetWindowsOCRProbeRunnerForTest(nil)
		ResetWindowsOCRProbeCacheForTest()
	})
	SetWindowsOCRRepairForTest(func(context.Context) { repaired++ })
	SetWindowsOCRProbeRunnerForTest(func(context.Context) WindowsOCRProbe {
		probed++
		return WindowsOCRProbe{State: WindowsOCRReady, Languages: []string{"zh-Hans-CN"}}
	})
	got := RepairWindowsOCR(context.Background())
	if repaired != 1 || probed < 1 || got.State != WindowsOCRReady || !got.Available {
		t.Fatalf("repaired=%d probed=%d %+v", repaired, probed, got)
	}
}
