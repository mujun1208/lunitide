package ccapp

import "testing"

func TestBgraHasVisiblePixels(t *testing.T) {
	if bgraHasVisiblePixels(nil) || bgraHasVisiblePixels(make([]byte, 256)) {
		t.Fatal("empty/black should be blank")
	}
	buf := make([]byte, 256)
	buf[80] = 12
	if !bgraHasVisiblePixels(buf) {
		t.Fatal("expected visible pixel")
	}
}
