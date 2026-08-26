package ccapp

import (
	"bytes"
	"image"
	"image/jpeg"
	"image/png"
)

const (
	visionMaxEdge  = 1280
	visionMaxBytes = 380 << 10
)

// PrepareVisionImage downscales a desktop PNG so it can ride the next model
// turn as a vision attachment without blowing the 1 MiB gateway budget.
func PrepareVisionImage(pngBytes []byte) (data []byte, mime string, err error) {
	if len(pngBytes) == 0 {
		return nil, "", nil
	}
	if len(pngBytes) <= visionMaxBytes {
		return pngBytes, "image/png", nil
	}
	img, err := png.Decode(bytes.NewReader(pngBytes))
	if err != nil {
		return nil, "", err
	}
	scaled := scaleToMaxEdge(img, visionMaxEdge)
	for _, quality := range []int{72, 58, 42} {
		var buf bytes.Buffer
		if err := jpeg.Encode(&buf, scaled, &jpeg.Options{Quality: quality}); err != nil {
			return nil, "", err
		}
		if buf.Len() <= visionMaxBytes || quality == 42 {
			return buf.Bytes(), "image/jpeg", nil
		}
	}
	return pngBytes, "image/png", nil
}

func scaleToMaxEdge(src image.Image, maxEdge int) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		return src
	}
	if w <= maxEdge && h <= maxEdge {
		return src
	}
	scale := float64(maxEdge) / float64(w)
	if h > w {
		scale = float64(maxEdge) / float64(h)
	}
	nw := int(float64(w)*scale + 0.5)
	nh := int(float64(h)*scale + 0.5)
	if nw < 1 {
		nw = 1
	}
	if nh < 1 {
		nh = 1
	}
	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	for y := 0; y < nh; y++ {
		sy := b.Min.Y + y*h/nh
		for x := 0; x < nw; x++ {
			sx := b.Min.X + x*w/nw
			dst.Set(x, y, src.At(sx, sy))
		}
	}
	return dst
}
