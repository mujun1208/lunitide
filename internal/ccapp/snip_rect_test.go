package ccapp

import (
	"bytes"
	"image"
	"testing"
)

func TestNormalizeSnipRectFlipsAndRequiresMinSize(t *testing.T) {
	x, y, w, h, ok := normalizeSnipRect(40, 30, 10, 12, 100, 80)
	if !ok || x != 10 || y != 12 || w != 30 || h != 18 {
		t.Fatalf("got %d,%d %dx%d ok=%v", x, y, w, h, ok)
	}
	if _, _, _, _, ok := normalizeSnipRect(0, 0, 4, 4, 100, 100); ok {
		t.Fatal("tiny rect should be rejected")
	}
}

func TestNormalizeSnipRectClampsToScreen(t *testing.T) {
	x, y, w, h, ok := normalizeSnipRect(-20, -8, 200, 90, 80, 60)
	if !ok || x != 0 || y != 0 || w != 80 || h != 60 {
		t.Fatalf("got %d,%d %dx%d ok=%v", x, y, w, h, ok)
	}
}

func TestCropBGRACopiesTheWindow(t *testing.T) {
	const fw, fh = 4, 3
	src := make([]byte, fw*fh*4)
	for i := range src {
		src[i] = byte(i + 1)
	}
	got, err := cropBGRA(src, fw, fh, 1, 1, 2, 1)
	if err != nil {
		t.Fatal(err)
	}
	want := src[1*fw*4+1*4 : 1*fw*4+1*4+8]
	if !bytes.Equal(got, want) {
		t.Fatalf("crop = %v want %v", got, want)
	}
}

func TestDarkenBGRAKeepsAlpha(t *testing.T) {
	src := []byte{200, 100, 50, 255, 10, 10, 10, 128}
	got := darkenBGRA(src)
	if got[3] != 255 || got[7] != 128 {
		t.Fatalf("alpha changed: %v", got)
	}
	if got[0] >= src[0] || got[1] >= src[1] || got[2] >= src[2] {
		t.Fatalf("rgb should dim: %v", got)
	}
}

func TestSnipToolbarStaysOnScreen(t *testing.T) {
	done, cancel := snipToolbarRects(0, 0, 40, 40, 400, 240)
	screen := image.Rect(0, 0, 400, 240)
	if !done.In(screen) || !cancel.In(screen) {
		t.Fatalf("toolbar %v %v outside %v", done, cancel, screen)
	}
	if done.Dx() != snipBtnW || cancel.Dx() != snipBtnW {
		t.Fatalf("button size done=%v cancel=%v", done, cancel)
	}
	above, _ := snipToolbarRects(10, 80, 80, 18, 200, 100)
	if above.Max.Y > 80 {
		t.Fatalf("expected toolbar above a bottom-edge selection, got %v", above)
	}
}

func TestPtInRect(t *testing.T) {
	if !ptInRect(5, 5, 0, 0, 10, 10) || ptInRect(10, 5, 0, 0, 10, 10) {
		t.Fatal("inclusive min, exclusive max")
	}
}

func TestCropBGRARejectsOOB(t *testing.T) {
	src := make([]byte, 4*4*4)
	if _, err := cropBGRA(src, 4, 4, 3, 0, 2, 1); err == nil {
		t.Fatal("expected out-of-bounds crop to fail")
	}
}
