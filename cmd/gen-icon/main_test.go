package main

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
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
	for dx := -50; dx <= 50; dx++ {
		c := img.RGBAAt(128+dx, cloudY)
		if c.A > 40 && c.B >= c.R {
			foundCloud = true
			break
		}
	}
	if !foundCloud {
		t.Fatalf("cloud line missing around y=%d", cloudY)
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

func TestWriteICOUsesPNGFrames(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 32, 32))
	src.Set(16, 16, color.RGBA{255, 255, 255, 255})
	dir := t.TempDir()
	path := filepath.Join(dir, "mark.ico")
	if err := writeICO(path, src, []int{16, 32}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte{0x89, 'P', 'N', 'G'}) {
		t.Fatal("ICO should embed PNG frames so Windows keeps alpha")
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, src); err != nil {
		t.Fatal(err)
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
	if !bytes.Contains(raw, []byte{0x89, 'P', 'N', 'G'}) {
		t.Fatal("shipped ICO should embed PNG frames so Explorer keeps alpha")
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
	for dx := -img.Bounds().Dx() / 5; dx <= img.Bounds().Dx()/5; dx++ {
		c := img.RGBAAt(x+dx, yCloud)
		if c.A > 40 && c.B >= c.R {
			foundCloud = true
			break
		}
	}
	if !foundCloud {
		t.Fatalf("faint cloud line missing around y=%d", yCloud)
	}
}
