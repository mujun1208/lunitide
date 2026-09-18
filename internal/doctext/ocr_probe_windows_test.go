package doctext

import (
	"context"
	"testing"
	"time"
)

func TestWindowsOCRProbeStates(t *testing.T) {
	t.Cleanup(func() {
		SetWindowsOCRProbeRunnerForTest(nil)
		ResetWindowsOCRProbeCacheForTest()
	})
	cases := []struct {
		name  string
		state WindowsOCRProbeState
	}{
		{"ready", WindowsOCRReady},
		{"unsupported_os", WindowsOCRUnsupportedOS},
		{"initialization_failed", WindowsOCRInitializationErr},
		{"language_unavailable", WindowsOCRLanguageMissing},
		{"sample_failed", WindowsOCRSampleFailed},
		{"timed_out", WindowsOCRTimedOut},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			SetWindowsOCRProbeRunnerForTest(func(context.Context) WindowsOCRProbe {
				return WindowsOCRProbe{State: tc.state, Languages: []string{"zh-Hans"}, CheckedAt: time.Unix(0, 0).UTC()}
			})
			got := ProbeWindowsOCR(context.Background())
			if got.State != tc.state {
				t.Fatalf("state=%s available=%v", got.State, got.Available)
			}
			if got.Available != (tc.state == WindowsOCRReady) {
				t.Fatalf("available must follow ready only: %+v", got)
			}
		})
	}
}

func TestOCRWindowsProbe(t *testing.T) {
	TestWindowsOCRProbeStates(t)
}
