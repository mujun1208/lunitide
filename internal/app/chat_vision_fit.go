package app

import (
	"bytes"
	"image"
	"image/jpeg"
	_ "image/png"

	"github.com/lunitide/lunitide/internal/attachmentapp"
)

const chatVisionMaxEdge = 1280

// fitChatVision shrinks a screenshot until it fits the chat vision budget.
// A desktop capture is often larger than 180 KiB; without this shrink the
// turn only has a file path, and the model tries to open it with StorageFile.
func fitChatVision(raw []byte) ([]byte, string, bool) {
	if len(raw) == 0 || len(raw) > attachmentapp.MaxFileSize {
		return nil, "", false
	}
	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, "", false
	}
	for _, edge := range []int{chatVisionMaxEdge, 960, 720, 480} {
		fitted := img
		if b := img.Bounds(); b.Dx() > edge || b.Dy() > edge {
			fitted = scaleChatVision(img, edge)
		}
		for _, quality := range []int{70, 50, 32} {
			var buf bytes.Buffer
			if err := jpeg.Encode(&buf, fitted, &jpeg.Options{Quality: quality}); err != nil {
				return nil, "", false
			}
			if buf.Len() <= attachmentapp.MaxVisionImageBytes {
				return buf.Bytes(), "image/jpeg", true
			}
		}
	}
	return nil, "", false
}

func scaleChatVision(src image.Image, maxEdge int) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
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
