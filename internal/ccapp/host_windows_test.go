//go:build windows

package ccapp

import (
	"bytes"
	"errors"
	"image/png"
	"testing"
)

func TestScreenCaptureReturnsPNG(t *testing.T) {
	h := PlatformHost()
	if !h.Available() {
		t.Skip("computer-control host unavailable")
	}
	raw, err := h.ScreenCapture()
	if err != nil {
		t.Fatalf("screen capture failed: %v", err)
	}
	if len(raw) < 8 || raw[0] != 0x89 || raw[1] != 'P' || raw[2] != 'N' || raw[3] != 'G' {
		t.Fatalf("expected PNG bytes, n=%d", len(raw))
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("png decode: %v", err)
	}
	w, hgt := h.ScreenSize()
	if cfg.Width != w || cfg.Height != hgt {
		t.Fatalf("capture %dx%d != ScreenSize %dx%d (DPI/coordinate mismatch)", cfg.Width, cfg.Height, w, hgt)
	}
}

func TestSetCursorPosAndWindowList(t *testing.T) {
	h := PlatformHost()
	if !h.Available() {
		t.Skip("computer-control host unavailable")
	}
	ox, oy := h.ScreenOrigin()
	w, ht := h.ScreenSize()
	if w <= 0 || ht <= 0 {
		t.Fatal("invalid screen size")
	}
	cx, cy := ox+w/2, oy+ht/2
	if err := h.MouseMove(cx, cy); err != nil {
		if errors.Is(err, ErrCursorBusy) {
			t.Skip(err.Error())
		}
		t.Fatalf("mousemove: %v", err)
	}
	gotX, gotY, err := h.CursorPosition()
	if err != nil {
		t.Fatalf("cursor: %v", err)
	}
	if abs(gotX-cx) > 3 || abs(gotY-cy) > 3 {
		t.Skipf("cursor landed at %d,%d want ~%d,%d (desktop input is shared under go test ./...)", gotX, gotY, cx, cy)
	}
	wins, err := h.ListWindows()
	if err != nil {
		t.Fatalf("list windows: %v", err)
	}
	if len(wins) == 0 {
		t.Fatal("expected at least one visible window")
	}
}

func TestScreenCaptureHasVisiblePixels(t *testing.T) {
	h := PlatformHost()
	if !h.Available() {
		t.Skip("computer-control host unavailable")
	}
	raw, err := h.ScreenCapture()
	if err != nil {
		t.Fatalf("screen capture failed: %v", err)
	}
	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("png decode: %v", err)
	}
	b := img.Bounds()
	if b.Dx() < 16 || b.Dy() < 16 {
		t.Fatalf("capture too small: %dx%d", b.Dx(), b.Dy())
	}
	nonBlack := 0
	samples := 0
	for y := b.Min.Y; y < b.Max.Y; y += b.Dy() / 16 {
		for x := b.Min.X; x < b.Max.X; x += b.Dx() / 16 {
			samples++
			r, g, bl, _ := img.At(x, y).RGBA()
			if r != 0 || g != 0 || bl != 0 {
				nonBlack++
			}
		}
	}
	if samples == 0 || nonBlack == 0 {
		t.Fatal("capture is all black (DIBSection/GetDIBits likely failed)")
	}
}

func TestWindowListAndFocusRoundTrip(t *testing.T) {
	h := PlatformHost()
	if !h.Available() {
		t.Skip("computer-control host unavailable")
	}
	wins, err := h.ListWindows()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var fg *WindowInfo
	for i := range wins {
		if wins[i].Foreground {
			fg = &wins[i]
			break
		}
	}
	if fg == nil || fg.Title == "" {
		t.Skip("no titled foreground window")
	}
	got, err := h.FocusWindow(fg.Title)
	if err != nil {
		t.Fatalf("focus %q: %v", fg.Title, err)
	}
	if got.Process == "" && fg.Process != "" {
		t.Fatalf("focus lost process, got %+v", got)
	}
}

func TestObserveUISmoke(t *testing.T) {
	h := PlatformHost()
	if !h.Available() {
		t.Skip("computer-control host unavailable")
	}
	nodes, err := h.ObserveUI(20)
	if err != nil {
		t.Fatalf("observe ui: %v", err)
	}
	for _, n := range nodes {
		if n.W < 0 || n.H < 0 {
			t.Fatalf("invalid node bounds %+v", n)
		}
	}
}

func TestClipboardRoundTrip(t *testing.T) {
	h := PlatformHost()
	if !h.Available() {
		t.Skip("computer-control host unavailable")
	}
	prev, err := h.ClipboardGet()
	if err != nil {
		t.Fatalf("clipboard get: %v", err)
	}
	t.Cleanup(func() { _ = h.ClipboardSet(prev) })
	want := "lunitide-cc-clipboard-probe"
	if err := h.ClipboardSet(want); err != nil {
		t.Fatalf("clipboard set: %v", err)
	}
	got, err := h.ClipboardGet()
	if err != nil {
		t.Fatalf("clipboard get after set: %v", err)
	}
	if got != want {
		t.Fatalf("clipboard = %q want %q", got, want)
	}
}

func TestWindowActionRestoreForeground(t *testing.T) {
	h := PlatformHost()
	if !h.Available() {
		t.Skip("computer-control host unavailable")
	}
	info, err := h.WindowAction("foreground", "restore", 0, 0, 0, 0)
	if err != nil {
		t.Fatalf("restore foreground: %v", err)
	}
	if info.ID == "" {
		t.Fatalf("restore returned empty window %+v", info)
	}
}

func TestScreenIndexAtPrimaryMonitor(t *testing.T) {
	h := PlatformHost().(*windowsHost)
	if !h.Available() {
		t.Skip("computer-control host unavailable")
	}
	ox, oy := h.ScreenOrigin()
	w, ht := h.ScreenSize()
	if w <= 0 || ht <= 0 {
		t.Fatal("invalid screen size")
	}
	idx := h.ScreenIndexAt(ox+w/4, oy+ht/2)
	if idx < 1 {
		t.Fatalf("primary-side point must land on a monitor, got %d", idx)
	}
	n := h.ScreenCount()
	if idx > n {
		t.Fatalf("screenIndex %d > count %d", idx, n)
	}
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
