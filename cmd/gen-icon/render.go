package main

import (
	"image"
	"image/color"
	"math"
)

// renderMoonMark draws one pale full moon, centered. The canvas is
// transparent outside the disc.
func renderMoonMark(size int) *image.RGBA {
	if size < 16 {
		size = 16
	}
	dst := image.NewRGBA(image.Rect(0, 0, size, size))
	drawMoon(dst, float64(size)*0.50, float64(size)*0.50, float64(size)*0.36)
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
