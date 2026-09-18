package doctext

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
)

// windowsOCRProbeSample is the fixture word drawn into the probe bitmap.
const windowsOCRProbeSample = "OK"

func windowsOCRProbePNG() []byte {
	const scale = 8
	glyph := []string{
		"01110 01110",
		"10001 10001",
		"10001 10010",
		"10001 10100",
		"10001 11000",
		"10001 10100",
		"01110 01110",
	}
	width := (len(glyph[0]) + 2) * scale
	height := (len(glyph) + 2) * scale
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	white := color.RGBA{255, 255, 255, 255}
	black := color.RGBA{0, 0, 0, 255}
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, white)
		}
	}
	for row, line := range glyph {
		for col, ch := range line {
			if ch != '1' {
				continue
			}
			for dy := 0; dy < scale; dy++ {
				for dx := 0; dx < scale; dx++ {
					img.Set((col+1)*scale+dx, (row+1)*scale+dy, black)
				}
			}
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}
