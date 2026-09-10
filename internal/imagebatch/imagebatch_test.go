package imagebatch

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func TestImageBatchCopiesAndRejectsAnimation(t *testing.T) {
	src := solidPNG(t, 40, 20)
	scaled, err := Scale(src, 20, 10)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(scaled))
	if err != nil || cfg.Width != 20 || cfg.Height != 10 {
		t.Fatalf("scale pixels %+v %v", cfg, err)
	}
	if bytes.Equal(src, scaled) {
		t.Fatal("must write a copy")
	}
	if _, err := Process(src, Op{Kind: "gif"}); err == nil {
		t.Fatal("animated/unsupported kinds must be rejected")
	}
	cropped, err := Process(src, Op{Kind: "crop", Width: 10, Height: 8})
	if err != nil {
		t.Fatal(err)
	}
	cfg, err = png.DecodeConfig(bytes.NewReader(cropped))
	if err != nil || cfg.Width != 10 || cfg.Height != 8 {
		t.Fatalf("crop pixels %+v %v", cfg, err)
	}
	if bytes.Equal(src, cropped) {
		t.Fatal("crop must write a copy")
	}
}

func solidPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: 200, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
