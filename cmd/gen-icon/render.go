package main

import (
	"image"
	"image/color"
	"math"
)

// renderMoonMark draws a pale moon with a gap below it and a faint cloud
// stroke. The canvas is fully transparent outside those two marks.
func renderMoonMark(size int) *image.RGBA {
	if size < 16 {
		size = 16
	}
	dst := image.NewRGBA(image.Rect(0, 0, size, size))
	cx := float64(size) * 0.50
	cy := float64(size) * 0.36
	r := float64(size) * 0.22
	drawMoon(dst, cx, cy, r)
	// Keep the cloud below the moon disc (bottom ≈ 0.58). Amplitude is in
	// canvas units so a thicker stroke at 32px cannot climb into the moon.
	cloudY := float64(size) * 0.82
	thick := float64(size) * 0.042
	if thick < 2.3 {
		thick = 2.3
	}
	drawCloudLine(dst, float64(size)*0.17, float64(size)*0.83, cloudY, thick, float64(size)*0.018, float64(size)*0.008)
	return dst
}

func drawMoon(dst *image.RGBA, cx, cy, r float64) {
	minX := int(math.Floor(cx - r - 2))
	maxX := int(math.Ceil(cx + r + 2))
	minY := int(math.Floor(cy - r - 2))
	maxY := int(math.Ceil(cy + r + 2))
	b := dst.Bounds()
	for y := max(b.Min.Y, minY); y < min(b.Max.Y, maxY); y++ {
		for x := max(b.Min.X, minX); x < min(b.Max.X, maxX); x++ {
			dx := float64(x) + 0.5 - cx
			dy := float64(y) + 0.5 - cy
			d := math.Hypot(dx, dy)
			if d > r {
				continue
			}
			t := d / r
			hx := dx/r + 0.38
			hy := dy/r + 0.48
			hi := math.Max(0, 1-math.Hypot(hx, hy))
			cr := 255 - t*42 + hi*18
			cg := 248 - t*28 + hi*12
			cb := 255 - t*18
			a := 255.0
			edge := 1.4
			if d > r-edge {
				a = 255 * (r - d) / edge
			}
			blend(dst, x, y, cr, cg, cb, a)
		}
	}
}

func drawCloudLine(dst *image.RGBA, x0, x1, yMid, thickness, amp1, amp2 float64) {
	if x1 <= x0 || thickness < 0.5 {
		return
	}
	b := dst.Bounds()
	if amp1 < 0 {
		amp1 = 0
	}
	if amp2 < 0 {
		amp2 = 0
	}
	reach := thickness * 1.65
	pad := thickness*3 + amp1 + amp2 + 2
	minX := int(math.Floor(x0 - pad))
	maxX := int(math.Ceil(x1 + pad))
	minY := int(math.Floor(yMid - pad))
	maxY := int(math.Ceil(yMid + pad))
	span := x1 - x0
	for y := max(b.Min.Y, minY); y < min(b.Max.Y, maxY); y++ {
		for x := max(b.Min.X, minX); x < min(b.Max.X, maxX); x++ {
			px := float64(x) + 0.5
			py := float64(y) + 0.5
			if px < x0-thickness || px > x1+thickness {
				continue
			}
			t := (px - x0) / span
			wave := yMid + math.Sin(t*math.Pi*2.05)*amp1 + math.Sin(t*math.Pi*5.1)*amp2
			d := math.Abs(py - wave)
			endFade := 1.0
			if px < x0+thickness*5 {
				endFade = (px - (x0 - thickness)) / (thickness * 6)
			} else if px > x1-thickness*5 {
				endFade = (x1 + thickness - px) / (thickness * 6)
			}
			if endFade < 0 {
				continue
			}
			if endFade > 1 {
				endFade = 1
			}
			if d > reach {
				continue
			}
			cover := 1 - d/reach
			a := 175 * cover * cover * endFade
			blend(dst, x, y, 236, 243, 252, a)
		}
	}
}

func blend(img *image.RGBA, x, y int, r, g, b, a float64) {
	if a <= 0 {
		return
	}
	if a > 255 {
		a = 255
	}
	src := color.RGBA{R: clamp8(r), G: clamp8(g), B: clamp8(b), A: clamp8(a)}
	dst := img.RGBAAt(x, y)
	if dst.A == 0 {
		img.SetRGBA(x, y, src)
		return
	}
	sa := float64(src.A) / 255
	da := float64(dst.A) / 255 * (1 - sa)
	outA := sa + da
	if outA <= 0 {
		return
	}
	img.SetRGBA(x, y, color.RGBA{
		R: clamp8((float64(src.R)*sa + float64(dst.R)*da) / outA),
		G: clamp8((float64(src.G)*sa + float64(dst.G)*da) / outA),
		B: clamp8((float64(src.B)*sa + float64(dst.B)*da) / outA),
		A: clamp8(outA * 255),
	})
}

func clamp8(v float64) uint8 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(v + 0.5)
}
