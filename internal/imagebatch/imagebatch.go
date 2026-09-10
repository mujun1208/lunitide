package imagebatch

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"strings"
)

type Op struct {
	Kind   string
	Width  int
	Height int
}

func Process(raw []byte, op Op) ([]byte, error) {
	kind := strings.ToLower(strings.TrimSpace(op.Kind))
	switch kind {
	case "gif":
		return nil, errors.New("unsupported image kind")
	case "png", "jpeg":
		return recode(raw, kind)
	case "scale":
		return Scale(raw, op.Width, op.Height)
	case "crop":
		return Crop(raw, op.Width, op.Height)
	case "compress":
		return Compress(raw)
	case "watermark":
		return Watermark(raw)
	default:
		return nil, errors.New("unsupported image kind")
	}
}

func Scale(raw []byte, w, h int) ([]byte, error) {
	src, _, err := decodeImage(raw)
	if err != nil {
		return nil, err
	}
	if w <= 0 || h <= 0 {
		return nil, errors.New("scale size rejected")
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	sb := src.Bounds()
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			sx := sb.Min.X + x*sb.Dx()/w
			sy := sb.Min.Y + y*sb.Dy()/h
			dst.Set(x, y, src.At(sx, sy))
		}
	}
	return encodePNG(dst)
}

func Crop(raw []byte, w, h int) ([]byte, error) {
	src, _, err := decodeImage(raw)
	if err != nil {
		return nil, err
	}
	sb := src.Bounds()
	if w <= 0 || h <= 0 || w > sb.Dx() || h > sb.Dy() {
		return nil, errors.New("crop size rejected")
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			dst.Set(x, y, src.At(sb.Min.X+x, sb.Min.Y+y))
		}
	}
	return encodePNG(dst)
}

func Compress(raw []byte) ([]byte, error) {
	src, _, err := decodeImage(raw)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, src, &jpeg.Options{Quality: 70}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func Watermark(raw []byte) ([]byte, error) {
	src, _, err := decodeImage(raw)
	if err != nil {
		return nil, err
	}
	sb := src.Bounds()
	dst := image.NewRGBA(sb)
	draw.Draw(dst, sb, src, sb.Min, draw.Src)
	mark := color.RGBA{R: 255, G: 255, B: 255, A: 255}
	for y := sb.Min.Y; y < sb.Min.Y+4 && y < sb.Max.Y; y++ {
		for x := sb.Min.X; x < sb.Min.X+4 && x < sb.Max.X; x++ {
			dst.Set(x, y, mark)
		}
	}
	return encodePNG(dst)
}

func decodeImage(raw []byte) (image.Image, string, error) {
	src, format, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, "", err
	}
	if strings.EqualFold(format, "gif") {
		return nil, "", errors.New("unsupported image kind")
	}
	return src, format, nil
}

func recode(raw []byte, kind string) ([]byte, error) {
	src, _, err := decodeImage(raw)
	if err != nil {
		return nil, err
	}
	if kind == "jpeg" {
		var buf bytes.Buffer
		if err := jpeg.Encode(&buf, src, &jpeg.Options{Quality: 90}); err != nil {
			return nil, err
		}
		return buf.Bytes(), nil
	}
	return encodePNG(src)
}

func encodePNG(src image.Image) ([]byte, error) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, src); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
