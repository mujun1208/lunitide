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

func TestMapScreenToVisionRoundTrip(t *testing.T) {
	visX, visY := 100, 50
	deskX, deskY := MapCapturePoint(visX, visY, 1280, 720, 2560, 1440)
	backX, backY := MapScreenToVision(deskX, deskY, 0, 0, 1280, 720, 2560, 1440)
	if backX != visX || backY != visY {
		t.Fatalf("round-trip %d,%d → %d,%d → %d,%d", visX, visY, deskX, deskY, backX, backY)
	}
	ox, oy := -1920, 0
	sx, sy := ox+deskX, oy+deskY
	imgX, imgY := MapScreenToVision(sx, sy, ox, oy, 1280, 720, 2560, 1440)
	if imgX != visX || imgY != visY {
		t.Fatalf("origin-aware map = %d,%d", imgX, imgY)
	}
}

func TestProjectRectIdentityAndOrigin(t *testing.T) {
	x, y, w, h := ProjectRect(-1880, 80, 200, 100, -1920, 0, 400, 300, 400, 300)
	if x != 40 || y != 80 || w != 200 || h != 100 {
		t.Fatalf("origin map = %d,%d %dx%d", x, y, w, h)
	}
}

func TestProjectRectScalesIntoVision(t *testing.T) {
	x, y, w, h := ProjectRect(1280, 720, 256, 144, 0, 0, 1280, 720, 2560, 1440)
	if x != 640 || y != 360 || w != 128 || h != 72 {
		t.Fatalf("scaled rect = %d,%d %dx%d want 640,360 128x72", x, y, w, h)
	}
}

func TestScaledSizeMatchesMaxEdge(t *testing.T) {
	w, h := scaledSize(3840, 2160, 1280)
	if w != 1280 || h != 720 {
		t.Fatalf("scaled = %dx%d", w, h)
	}
}
