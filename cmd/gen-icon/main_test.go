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

func TestRenderMoonMarkIsOneDisc(t *testing.T) {
	img := renderMoonMark(256)
	corner := img.RGBAAt(0, 0)
	if corner.A != 0 {
		t.Fatalf("canvas must be transparent, alpha=%d", corner.A)
	}
	moon := img.RGBAAt(128, 128)
	if moon.A < 200 {
		t.Fatalf("moon missing alpha=%d", moon.A)
	}
	if img.RGBAAt(128, 8).A > 40 || img.RGBAAt(128, 248).A > 40 {
		t.Fatal("moon disc should not fill the canvas edge")
	}
	assertNoBlueCloudOnMoon(t, img)
}

func TestMoonHasCloudStrokeBelowDisc(t *testing.T) {
	img := renderMoonMark(256)
	if img.RGBAAt(128, 205).A < 40 {
		t.Fatal("cloud stroke missing below the moon")
	}
	if img.RGBAAt(128, 250).A > 40 {
		t.Fatal("cloud stroke should stay under the disc")
	}
}

func TestMoonVisibleAtDesktopSize(t *testing.T) {
	img := renderMoonMark(32)
	if img.RGBAAt(16, 16).A < 200 {
		t.Fatal("32px frame must keep a visible moon disc")
	}
}

func assertNoBlueCloudOnMoon(t *testing.T, img *image.RGBA) {
	t.Helper()
	b := img.Bounds()
	h := b.Dy()
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
	yMoon := img.Bounds().Min.Y + img.Bounds().Dy()/2
	moon := img.RGBAAt(x, yMoon)
	if moon.A < 200 {
		t.Fatalf("moon body vanished at (%d,%d) alpha=%d", x, yMoon, moon.A)
	}
	yCloud := img.Bounds().Min.Y + img.Bounds().Dy()*80/100
	cloud := img.RGBAAt(x, yCloud)
	if cloud.A < 40 {
		t.Fatalf("cloud stroke missing below the moon, alpha=%d", cloud.A)
	}
	assertNoBlueCloudOnMoon(t, img)
}
