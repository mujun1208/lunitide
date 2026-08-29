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
}
