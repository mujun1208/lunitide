package ccapp

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func TestPrepareVisionImagePassthroughSmallPNG(t *testing.T) {
	raw := tinyPNG(t, 8, 8)
	data, mime, err := PrepareVisionImage(raw)
	if err != nil {
		t.Fatal(err)
	}
	if mime != "image/png" || len(data) != len(raw) {
		t.Fatalf("small png should pass through, mime=%s n=%d", mime, len(data))
	}
}

func TestPrepareVisionImageDownscalesLargePNG(t *testing.T) {
	raw := tinyPNG(t, 2400, 1600)
	data, mime, err := PrepareVisionImage(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 {
		t.Fatal("expected vision bytes")
	}
	if len(data) > visionMaxBytes {
		t.Fatalf("vision payload too large: %d", len(data))
	}
	if mime != "image/jpeg" && mime != "image/png" {
		t.Fatalf("unexpected mime %s", mime)
	}
}

func tinyPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 80, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
