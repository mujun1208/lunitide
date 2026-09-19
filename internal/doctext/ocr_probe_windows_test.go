package doctext

import (
	"context"
	"os"
	"strings"
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

func TestWindowsOCRProbeTextOKAcceptsAnyRecognizedText(t *testing.T) {
	if !windowsOCRProbeTextOK("识别成功") {
		t.Fatal("non-empty CJK must pass")
	}
	if !windowsOCRProbeTextOK(windowsOCRProbeSample) {
		t.Fatal("sample word must pass")
	}
	if windowsOCRProbeTextOK("  \n") {
		t.Fatal("blank text must fail")
	}
}

func TestLiveWindowsOCRProbeReadyAfterOpenReadFix(t *testing.T) {
	if os.Getenv("LUNITIDE_OCR_LIVE_PROBE") == "" {
		t.Skip("set LUNITIDE_OCR_LIVE_PROBE=1")
	}
	t.Cleanup(func() {
		SetWindowsOCRProbeRunnerForTest(nil)
		ResetWindowsOCRProbeCacheForTest()
	})
	SetWindowsOCRProbeRunnerForTest(nil)
	ResetWindowsOCRProbeCacheForTest()
	got := RefreshWindowsOCRProbe(context.Background())
	if got.State != WindowsOCRReady || !got.Available {
		t.Fatalf("expected ready after OpenRead type fix: %+v", got)
	}
}

func TestWindowsOCRProbeScriptAwaitsOpenReadContentType(t *testing.T) {
	script := string(windowsOCRProbeScript)
	if !strings.Contains(script, "IRandomAccessStreamWithContentType") {
		t.Fatal("OpenReadAsync must await IRandomAccessStreamWithContentType")
	}
	if strings.Contains(script, "OpenReadAsync()) ([Windows.Storage.Streams.IRandomAccessStream])") {
		t.Fatal("OpenReadAsync must not await the raw IRandomAccessStream interface")
	}
	if !strings.Contains(script, "OK 测试") {
		t.Fatal("probe must render a readable System.Drawing sample")
	}
}
