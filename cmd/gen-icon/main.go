// Package main renders the Lunitide moon mark into a PNG + multi-size ICO
// with a true alpha channel. Explorer treats a black-backed PNG as an opaque
// square; this keys that fill out so the desktop and ARP icons sit on glass.
//
//	go run ./cmd/gen-icon -png resources/lunitide-icon.png -ico resources/lunitide-icon.ico
package main

import (
	"bytes"
	"encoding/binary"
	"flag"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
)

func main() {
	srcPath := flag.String("src", "resources/lunitide-icon.png", "source PNG (may have a black fill)")
	pngPath := flag.String("png", "resources/lunitide-icon.png", "output PNG with alpha")
	icoPath := flag.String("ico", "resources/lunitide-icon.ico", "output ICO")
	flag.Parse()

	src, err := loadPNG(*srcPath)
	if err != nil {
		fatal("read source: %v", err)
	}
	keyed := knockOutMoonHalo(knockOutBlack(src))
	if err := writePNG(*pngPath, keyed); err != nil {
		fatal("write png: %v", err)
	}
	if err := writeICO(*icoPath, keyed, []int{16, 24, 32, 48, 64, 128, 256}); err != nil {
		fatal("write ico: %v", err)
	}
	fmt.Printf("wrote %s and %s (%dx%d, alpha)\n", *pngPath, *icoPath, keyed.Bounds().Dx(), keyed.Bounds().Dy())
}

func loadPNG(path string) (*image.RGBA, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	rgba := image.NewRGBA(img.Bounds())
	draw.Draw(rgba, rgba.Bounds(), img, img.Bounds().Min, draw.Src)
	return rgba, nil
}

func knockOutBlack(src *image.RGBA) *image.RGBA {
	b := src.Bounds()
	out := image.NewRGBA(b)
	copy(out.Pix, src.Pix)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			i := out.PixOffset(x, y)
			r, g, bl := out.Pix[i], out.Pix[i+1], out.Pix[i+2]
			if r < 14 && g < 14 && bl < 18 {
				out.Pix[i+3] = 0
			}
		}
	}
	return out
}

// knockOutMoonHalo clears the navy ring / glow around the moon while leaving
// the pale disc and the lower cloud bank intact.
func knockOutMoonHalo(src *image.RGBA) *image.RGBA {
	b := src.Bounds()
	out := image.NewRGBA(b)
	copy(out.Pix, src.Pix)
	cutoff := b.Min.Y + b.Dy()*62/100
	for y := b.Min.Y; y < cutoff; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			i := out.PixOffset(x, y)
			r, g, bl, a := out.Pix[i], out.Pix[i+1], out.Pix[i+2], out.Pix[i+3]
			if a == 0 {
				continue
			}
			lum := int(r) + int(g) + int(bl)
			if lum >= 420 {
				continue
			}
			if bl >= r && bl >= g {
				out.Pix[i+3] = 0
			}
		}
	}
	return out
}

func writePNG(path string, img *image.RGBA) error {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0644)
}

func writeICO(path string, src *image.RGBA, sizes []int) error {
	frames := make([][]byte, 0, len(sizes))
	for _, size := range sizes {
		scaled := scaleRGBA(src, size)
		var buf bytes.Buffer
		if err := png.Encode(&buf, scaled); err != nil {
			return err
		}
		frames = append(frames, buf.Bytes())
	}
	count := len(frames)
	header := 6 + count*16
	ico := make([]byte, header)
	binary.LittleEndian.PutUint16(ico[2:4], 1)
	binary.LittleEndian.PutUint16(ico[4:6], uint16(count))
	off := uint32(header)
	for i, frame := range frames {
		w := sizes[i]
		if w >= 256 {
			w = 0
		}
		eo := 6 + i*16
		ico[eo+0] = byte(w)
		ico[eo+1] = byte(w)
		binary.LittleEndian.PutUint16(ico[eo+4:eo+6], 1)
		binary.LittleEndian.PutUint16(ico[eo+6:eo+8], 32)
		binary.LittleEndian.PutUint32(ico[eo+8:eo+12], uint32(len(frame)))
		binary.LittleEndian.PutUint32(ico[eo+12:eo+16], off)
		off += uint32(len(frame))
	}
	for _, frame := range frames {
		ico = append(ico, frame...)
	}
	return os.WriteFile(path, ico, 0644)
}

func scaleRGBA(src *image.RGBA, size int) *image.RGBA {
	sb := src.Bounds()
	sw, sh := sb.Dx(), sb.Dy()
	dst := image.NewRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		sy := sb.Min.Y + y*sh/size
		for x := 0; x < size; x++ {
			sx := sb.Min.X + x*sw/size
			si := src.PixOffset(sx, sy)
			di := dst.PixOffset(x, y)
			copy(dst.Pix[di:di+4], src.Pix[si:si+4])
		}
	}
	return dst
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
