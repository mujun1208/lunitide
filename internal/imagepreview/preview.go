// Package imagepreview creates bounded previews without changing original files.
package imagepreview

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"image"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
)

var ErrUnsupported = errors.New("unsupported image")
var ErrTooLarge = errors.New("image preview exceeds limit")

func Encode(ctx context.Context, raw []byte) (string, error) {
	config, format, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		return "", ErrUnsupported
	}
	if config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > 40_000_000 {
		return "", ErrTooLarge
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	// Screenshots retain their original pixels. Larger images use a bounded
	// preview; the Open original action always uses the unchanged attachment.
	if len(raw) <= 2<<20 {
		mime := "image/" + format
		return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(raw), nil
	}
	src, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return "", ErrUnsupported
	}
	w, h := config.Width, config.Height
	if max(w, h) > 1600 {
		w = max(1, w*1600/max(config.Width, config.Height))
		h = max(1, h*1600/max(config.Width, config.Height))
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	bounds := src.Bounds()
	for y := 0; y < h; y++ {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		for x := 0; x < w; x++ {
			dst.Set(x, y, src.At(bounds.Min.X+x*config.Width/w, bounds.Min.Y+y*config.Height/h))
		}
	}
	var out bytes.Buffer
	if err := jpeg.Encode(&out, dst, &jpeg.Options{Quality: 85}); err != nil {
		return "", err
	}
	if out.Len() > 2<<20 {
		return "", ErrTooLarge
	}
	return "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(out.Bytes()), nil
}
