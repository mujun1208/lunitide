//go:build windows

package ccapp

import "testing"

func TestScreenCaptureReturnsPNG(t *testing.T) {
	h := PlatformHost()
	if !h.Available() {
		t.Skip("computer-control host unavailable")
	}
	raw, err := h.ScreenCapture()
	if err != nil {
		t.Skipf("screen capture unavailable: %v", err)
	}
	if len(raw) < 8 || raw[0] != 0x89 || raw[1] != 'P' || raw[2] != 'N' || raw[3] != 'G' {
		t.Fatalf("expected PNG bytes, n=%d", len(raw))
	}
}
