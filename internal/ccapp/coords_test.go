package ccapp

import "testing"

func TestMapCapturePointIdentity(t *testing.T) {
	x, y := MapCapturePoint(100, 50, 1920, 1080, 1920, 1080)
	if x != 100 || y != 50 {
		t.Fatalf("identity map = %d,%d", x, y)
	}
}

func TestMapCapturePointScalesVisionToDesktop(t *testing.T) {
	// Vision 1280x720 of a 2560x1440 desktop; centre of vision → centre of desktop.
	x, y := MapCapturePoint(640, 360, 1280, 720, 2560, 1440)
	if x != 1280 || y != 720 {
		t.Fatalf("scaled map = %d,%d want 1280,720", x, y)
	}
}

func TestScaledSizeMatchesMaxEdge(t *testing.T) {
	w, h := scaledSize(3840, 2160, 1280)
	if w != 1280 || h != 720 {
		t.Fatalf("scaled = %dx%d", w, h)
	}
}
