package officestudio

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"runtime"
	"strings"
	"testing"
)

func syntheticPage(t *testing.T, size int, background color.RGBA, paint func(img *image.RGBA)) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			img.SetRGBA(x, y, background)
		}
	}
	if paint != nil {
		paint(img)
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

func findRenderCheck(checks []Check, id string) Check {
	for _, c := range checks {
		if c.ID == id {
			return c
		}
	}
	return Check{}
}

func TestRenderedPageQAUnsupportedWithoutPages(t *testing.T) {
	checks := RenderedPageQA(nil, 0)
	if len(checks) != 1 {
		t.Fatalf("expected only pixel check without pages, got %#v", checks)
	}
	if checks[0].ID != "render-pixel-qa" || checks[0].Status != "unsupported" {
		t.Fatalf("pixel=%#v", checks[0])
	}
	if findRenderCheck(checks, "render-border-ink").ID != "" {
		t.Fatal("border check must be absent without pages")
	}
}

func TestRenderedPageQAPassesCleanPages(t *testing.T) {
	pages := [][]byte{
		// Dark cover with a light title in the middle: dominant color is navy,
		// so the border band must read as clean, not as ink.
		syntheticPage(t, 200, color.RGBA{R: 22, G: 50, B: 79, A: 255}, func(img *image.RGBA) {
			for y := 90; y < 110; y++ {
				for x := 60; x < 140; x++ {
					img.SetRGBA(x, y, color.RGBA{R: 255, G: 255, B: 255, A: 255})
				}
			}
		}),
		// Light content page with a dark paragraph in the middle.
		syntheticPage(t, 200, color.RGBA{R: 255, G: 255, B: 255, A: 255}, func(img *image.RGBA) {
			for y := 100; y < 130; y++ {
				for x := 40; x < 160; x++ {
					img.SetRGBA(x, y, color.RGBA{R: 29, G: 39, B: 51, A: 255})
				}
			}
		}),
	}
	checks := RenderedPageQA(pages, 2)
	pixel := findRenderCheck(checks, "render-pixel-qa")
	border := findRenderCheck(checks, "render-border-ink")
	if pixel.ID == "" || pixel.Status != "passed" {
		t.Fatalf("pixel=%#v", pixel)
	}
	if border.ID == "" || border.Status != "passed" {
		t.Fatalf("border=%#v", border)
	}
}

func TestRenderedPageQAFailsPageCountMismatch(t *testing.T) {
	page := syntheticPage(t, 200, color.RGBA{R: 255, G: 255, B: 255, A: 255}, func(img *image.RGBA) {
		for y := 100; y < 120; y++ {
			for x := 50; x < 150; x++ {
				img.SetRGBA(x, y, color.RGBA{R: 20, G: 20, B: 30, A: 255})
			}
		}
	})
	checks := RenderedPageQA([][]byte{page, page}, 5)
	pixel := findRenderCheck(checks, "render-pixel-qa")
	if pixel.Status != "failed" {
		t.Fatalf("pixel=%#v", pixel)
	}
}

func TestRenderedPageQAFailsBlankPage(t *testing.T) {
	blank := syntheticPage(t, 200, color.RGBA{R: 255, G: 255, B: 255, A: 255}, nil)
	inked := syntheticPage(t, 200, color.RGBA{R: 255, G: 255, B: 255, A: 255}, func(img *image.RGBA) {
		for y := 90; y < 130; y++ {
			for x := 40; x < 160; x++ {
				img.SetRGBA(x, y, color.RGBA{R: 20, G: 20, B: 30, A: 255})
			}
		}
	})
	checks := RenderedPageQA([][]byte{inked, blank}, 2)
	pixel := findRenderCheck(checks, "render-pixel-qa")
	if pixel.Status != "failed" {
		t.Fatalf("pixel=%#v", pixel)
	}
	border := findRenderCheck(checks, "render-border-ink")
	if border.ID == "" || border.Status != "unsupported" {
		t.Fatalf("border must degrade when base fails: %#v", border)
	}
}

func TestRenderedPageQAFailsDecodeErrors(t *testing.T) {
	checks := RenderedPageQA([][]byte{[]byte("not a png")}, 1)
	pixel := findRenderCheck(checks, "render-pixel-qa")
	if pixel.Status != "failed" {
		t.Fatalf("pixel=%#v", pixel)
	}
}

func TestRenderedPageQAWarnsOnBorderInk(t *testing.T) {
	// Ink only in the outer band: pixel base passes, border check warns.
	page := syntheticPage(t, 200, color.RGBA{R: 255, G: 255, B: 255, A: 255}, func(img *image.RGBA) {
		for x := 0; x < 200; x++ {
			for _, y := range []int{0, 1, 198, 199} {
				img.SetRGBA(x, y, color.RGBA{R: 10, G: 10, B: 10, A: 255})
			}
		}
		for y := 100; y < 120; y++ {
			for x := 60; x < 140; x++ {
				img.SetRGBA(x, y, color.RGBA{R: 10, G: 10, B: 10, A: 255})
			}
		}
	})
	checks := RenderedPageQA([][]byte{page}, 1)
	pixel := findRenderCheck(checks, "render-pixel-qa")
	border := findRenderCheck(checks, "render-border-ink")
	if pixel.Status != "passed" {
		t.Fatalf("pixel=%#v", pixel)
	}
	if border.Status != "failed" {
		t.Fatalf("border=%#v", border)
	}
}

func TestExtractVisualPagesRendersGeneratedPDFOnWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("platform PDF page renderer only exists on Windows")
	}
	pdf, err := Generate(Spec{SchemaVersion: 1, Kind: PDF, Title: "视觉QA接线", Body: "第一页正文内容，用于验证逐页渲染管线。" + "\n" + "第二段文字。"})
	if err != nil {
		t.Fatal(err)
	}
	pages, err := ExtractVisualPages(context.Background(), pdf)
	if err != nil {
		t.Skipf("platform renderer unavailable in this environment: %v", err)
	}
	if len(pages) == 0 {
		t.Fatal("production extractor returned no pages")
	}
	img, err := png.Decode(bytes.NewReader(pages[0]))
	if err != nil {
		t.Fatalf("rendered page is not a decodable PNG: %v", err)
	}
	if img.Bounds().Dx() < renderQAMinDimension || img.Bounds().Dy() < renderQAMinDimension {
		t.Fatalf("rendered page too small: %v", img.Bounds())
	}
}

func TestRenderedPageQAPhotoPagesSkipBorderRule(t *testing.T) {
	// An image-heavy page legitimately reaches the border; band ink must be
	// treated as imagery, not layout clipping.
	page := syntheticPage(t, 200, color.RGBA{R: 255, G: 255, B: 255, A: 255}, func(img *image.RGBA) {
		for y := 0; y < 200; y++ {
			for x := 0; x < 200; x++ {
				if (x*7+y*3)%5 < 2 {
					img.SetRGBA(x, y, color.RGBA{R: 12, G: 34, B: 56, A: 255})
				}
			}
		}
	})
	checks := RenderedPageQA([][]byte{page}, 1)
	pixel := findRenderCheck(checks, "render-pixel-qa")
	border := findRenderCheck(checks, "render-border-ink")
	if pixel.Status != "passed" {
		t.Fatalf("pixel=%#v", pixel)
	}
	if border.Status != "passed" {
		t.Fatalf("border=%#v", border)
	}
	if !strings.Contains(border.Message, "图像为主") {
		t.Fatalf("border note=%q", border.Message)
	}
}

func TestRenderedPageQASkipsCountWhenUnknown(t *testing.T) {
	page := syntheticPage(t, 128, color.RGBA{R: 240, G: 240, B: 240, A: 255}, func(img *image.RGBA) {
		for y := 50; y < 70; y++ {
			for x := 30; x < 90; x++ {
				img.SetRGBA(x, y, color.RGBA{R: 20, G: 20, B: 30, A: 255})
			}
		}
	})
	checks := RenderedPageQA([][]byte{page}, 0)
	pixel := findRenderCheck(checks, "render-pixel-qa")
	if pixel.Status != "passed" {
		t.Fatalf("pixel=%#v", pixel)
	}
}
