package main

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestRenderMoonMarkHasGapAndCloudLine(t *testing.T) {
	img := renderMoonMark(256)
	corner := img.RGBAAt(0, 0)
	if corner.A != 0 {
		t.Fatalf("canvas must be transparent, alpha=%d", corner.A)
	}
	moon := img.RGBAAt(128, 256*40/100)
	if moon.A < 200 {
		t.Fatalf("moon missing alpha=%d", moon.A)
	}
	gap := img.RGBAAt(128, 256*62/100)
	if gap.A > 40 {
		t.Fatalf("gap should be empty, alpha=%d", gap.A)
	}
	cloudY := 256 * 78 / 100
	foundCloud := false
	for y := 256 * 74 / 100; y <= 256*84/100; y++ {
		for dx := -50; dx <= 50; dx++ {
			c := img.RGBAAt(128+dx, y)
			if c.A > 40 && c.R >= 200 {
				foundCloud = true
				break
			}
		}
	}
	if !foundCloud {
		t.Fatalf("cloud line missing around y=%d", cloudY)
	}
	assertNoBlueCloudOnMoon(t, img)
}

func TestCloudLineVisibleAtDesktopSize(t *testing.T) {
	img := renderMoonMark(32)
	found := false
	for y := 32 * 72 / 100; y <= 32*86/100; y++ {
		for x := 32 * 20 / 100; x <= 32*80/100; x++ {
			c := img.RGBAAt(x, y)
			if c.A > 60 && c.R >= 180 {
				found = true
				break
			}
		}
	}
	if !found {
		t.Fatal("32px desktop frame must keep a visible cloud line")
	}
}

func assertNoBlueCloudOnMoon(t *testing.T, img *image.RGBA) {
	t.Helper()
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	moonBottom := b.Min.Y + h*58/100
	for y := b.Min.Y; y < moonBottom; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			c := img.RGBAAt(x, y)
			if c.A < 80 {
				continue
			}
			if int(c.B) > int(c.R)+40 && int(c.B) > int(c.G)+20 {
				t.Fatalf("saturated blue cloud covers moon at (%d,%d) rgba=%d,%d,%d,%d", x, y, c.R, c.G, c.B, c.A)
			}
		}
	}
	x := b.Min.X + w/2
	y0 := b.Min.Y + h*74/100
	y1 := b.Min.Y + h*84/100
	foundPale := false
	for y := y0; y <= y1; y++ {
		for dx := -w / 5; dx <= w/5; dx++ {
			c := img.RGBAAt(x+dx, y)
			if c.A > 40 && c.R >= 200 && c.G >= 210 {
				foundPale = true
			}
		}
	}
	if !foundPale {
		t.Fatalf("pale cloud line missing around y=%d-%d", y0, y1)
	}
}

func TestKnockOutBlackLeavesMoonAndClouds(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 4, 4))
	src.Set(0, 0, color.RGBA{0, 0, 0, 255})
	src.Set(1, 0, color.RGBA{240, 248, 255, 255})
	src.Set(2, 0, color.RGBA{59, 130, 246, 255})
	out := knockOutBlack(src)
	if out.RGBAAt(0, 0).A != 0 {
		t.Fatal("black fill should be transparent")
	}
	if out.RGBAAt(1, 0).A != 255 || out.RGBAAt(2, 0).A != 255 {
		t.Fatal("moon and cloud pixels must stay opaque")
	}
}

func TestKnockOutMoonHaloRemovesBlueRing(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 10, 10))
	src.Set(5, 1, color.RGBA{80, 120, 190, 255})
	src.Set(4, 1, color.RGBA{1, 9, 60, 255})
	src.Set(5, 3, color.RGBA{240, 248, 255, 255})
	src.Set(5, 8, color.RGBA{59, 130, 246, 255})
	out := knockOutMoonHalo(src)
	if out.RGBAAt(5, 1).A != 0 || out.RGBAAt(4, 1).A != 0 {
		t.Fatal("upper halo should be transparent")
	}
	if out.RGBAAt(5, 3).A != 255 {
		t.Fatal("moon body must stay opaque")
	}
	if out.RGBAAt(5, 8).A != 255 {
		t.Fatal("lower clouds must stay opaque")
	}
}

func TestWriteICOUsesBMP32(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 32, 32))
	src.Set(16, 16, color.RGBA{255, 255, 255, 255})
	dir := t.TempDir()
	path := filepath.Join(dir, "mark.ico")
	if err := writeICO(path, src, []int{16, 32}, false); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte{0x89, 'P', 'N', 'G'}) {
		t.Fatal("ICO must use 32bpp BMP frames so NSIS Setup/Uninstall keep alpha")
	}
	if len(raw) < 22 {
		t.Fatal("ico too short")
	}
	off := binary.LittleEndian.Uint32(raw[18:22])
	if int(off)+16 > len(raw) {
		t.Fatal("frame offset")
	}
	if got := binary.LittleEndian.Uint32(raw[off : off+4]); got != 40 {
		t.Fatalf("BITMAPINFOHEADER size %d", got)
	}
	if got := binary.LittleEndian.Uint16(raw[off+14 : off+16]); got != 32 {
		t.Fatalf("bit count %d", got)
	}
}

func TestPNGRoundtripKeepsPaleCloud(t *testing.T) {
	src := knockOutMoonHalo(knockOutBlack(renderMoonMark(256)))
	path := filepath.Join(t.TempDir(), "mark.png")
	if err := writePNG(path, src); err != nil {
		t.Fatal(err)
	}
	got, err := loadPNG(path)
	if err != nil {
		t.Fatal(err)
	}
	var srcPeak, gotPeak color.RGBA
	for y := 256 * 74 / 100; y <= 256*84/100; y++ {
		for x := 256 * 30 / 100; x <= 256*70/100; x++ {
			c := src.RGBAAt(x, y)
			if c.A >= srcPeak.A && c.R >= 200 {
				srcPeak = c
			}
			d := got.RGBAAt(x, y)
			if d.A >= gotPeak.A {
				gotPeak = d
			}
		}
	}
	t.Logf("src peak=%v got peak=%v", srcPeak, gotPeak)
	if gotPeak.A < 40 || gotPeak.R < 180 {
		t.Fatalf("png roundtrip lost pale cloud: src=%v got=%v", srcPeak, gotPeak)
	}
}

func TestRepoIconHasTransparentFill(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller path")
	}
	dir := filepath.Join(filepath.Dir(thisFile), "..", "..", "resources")
	img, err := loadPNG(filepath.Join(dir, "lunitide-icon.png"))
	if err != nil {
		t.Fatal(err)
	}
	corner := img.RGBAAt(img.Bounds().Min.X, img.Bounds().Min.Y)
	if corner.A != 0 {
		t.Fatalf("corner fill should be transparent, got alpha=%d rgb=%d,%d,%d", corner.A, corner.R, corner.G, corner.B)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "lunitide-icon.ico"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte{0x89, 'P', 'N', 'G'}) {
		t.Fatal("shipped ICO must use 32bpp BMP frames so NSIS Setup/Uninstall keep alpha")
	}
	if len(raw) < 22 {
		t.Fatal("ico too short")
	}
	off := binary.LittleEndian.Uint32(raw[18:22])
	if int(off)+16 > len(raw) {
		t.Fatal("frame offset")
	}
	if got := binary.LittleEndian.Uint32(raw[off : off+4]); got != 40 {
		t.Fatalf("shipped ICO BITMAPINFOHEADER size %d", got)
	}
	x := img.Bounds().Min.X + img.Bounds().Dx()/2
	yHalo := img.Bounds().Min.Y + img.Bounds().Dy()*8/100
	halo := img.RGBAAt(x, yHalo)
	if halo.A > 40 {
		t.Fatalf("desktop halo at (%d,%d) still visible alpha=%d rgb=%d,%d,%d", x, yHalo, halo.A, halo.R, halo.G, halo.B)
	}
	yMoon := img.Bounds().Min.Y + img.Bounds().Dy()*35/100
	moon := img.RGBAAt(x, yMoon)
	if moon.A < 200 {
		t.Fatalf("moon body vanished at (%d,%d) alpha=%d", x, yMoon, moon.A)
	}
	yGap := img.Bounds().Min.Y + img.Bounds().Dy()*62/100
	gap := img.RGBAAt(x, yGap)
	if gap.A > 40 {
		t.Fatalf("moon/cloud gap at (%d,%d) should be open, alpha=%d", x, yGap, gap.A)
	}
	yCloud := img.Bounds().Min.Y + img.Bounds().Dy()*78/100
	foundCloud := false
	y0 := img.Bounds().Min.Y + img.Bounds().Dy()*74/100
	y1 := img.Bounds().Min.Y + img.Bounds().Dy()*84/100
	for y := y0; y <= y1; y++ {
		for dx := -img.Bounds().Dx() / 5; dx <= img.Bounds().Dx()/5; dx++ {
			c := img.RGBAAt(x+dx, y)
			if c.A > 40 && c.R >= 200 {
				foundCloud = true
				break
			}
		}
	}
	if !foundCloud {
		t.Fatalf("faint cloud line missing around y=%d", yCloud)
	}
	assertNoBlueCloudOnMoon(t, img)
}
