package ccapp

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"strings"
)

var badgeFill = color.RGBA{R: 255, G: 32, B: 160, A: 255}
var badgeInk = color.RGBA{R: 255, G: 255, B: 255, A: 255}

// 5x7 glyphs used on Peekaboo-style see overlays (IDs like B1, E2).
var badgeGlyphs = map[rune][7]string{
	'B': {"11110", "10001", "10001", "11110", "10001", "10001", "11110"},
	'C': {"01111", "10000", "10000", "10000", "10000", "10000", "01111"},
	'E': {"11111", "10000", "10000", "11110", "10000", "10000", "11111"},
	'L': {"10000", "10000", "10000", "10000", "10000", "10000", "11111"},
	'M': {"10001", "11011", "10101", "10001", "10001", "10001", "10001"},
	'N': {"10001", "11001", "10101", "10011", "10001", "10001", "10001"},
	'T': {"11111", "00100", "00100", "00100", "00100", "00100", "00100"},
	'0': {"01110", "10001", "10001", "10001", "10001", "10001", "01110"},
	'1': {"00100", "01100", "00100", "00100", "00100", "00100", "01110"},
	'2': {"01110", "10001", "00001", "00110", "01000", "10000", "11111"},
	'3': {"11110", "00001", "00001", "01110", "00001", "00001", "11110"},
	'4': {"00010", "00110", "01010", "10010", "11111", "00010", "00010"},
	'5': {"11111", "10000", "11110", "00001", "00001", "10001", "01110"},
	'6': {"01110", "10000", "11110", "10001", "10001", "10001", "01110"},
	'7': {"11111", "00001", "00010", "00100", "01000", "01000", "01000"},
	'8': {"01110", "10001", "10001", "01110", "10001", "10001", "01110"},
	'9': {"01110", "10001", "10001", "01111", "00001", "00001", "01110"},
}

func rolePrefix(role string) string {
	switch role {
	case "button":
		return "B"
	case "link":
		return "L"
	case "edit":
		return "E"
	case "checkbox", "radio":
		return "C"
	case "tab":
		return "T"
	case "menuitem":
		return "M"
	default:
		return "N"
	}
}

func assignNodeIDs(nodes []UINode) []UINode {
	counts := map[string]int{}
	out := make([]UINode, len(nodes))
	copy(out, nodes)
	for i := range out {
		p := rolePrefix(out[i].Role)
		counts[p]++
		out[i].ID = fmt.Sprintf("%s%d", p, counts[p])
	}
	return out
}

// AnnotateCapture draws Peekaboo-style ID badges onto a screenshot in the
// same pixel space as mapped UINode coordinates (vision image pixels).
func AnnotateCapture(pngBytes []byte, nodes []UINode, visW, visH int) ([]byte, error) {
	if len(pngBytes) == 0 {
		return pngBytes, nil
	}
	src, err := png.Decode(bytes.NewReader(pngBytes))
	if err != nil {
		return nil, err
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if visW <= 0 || visH <= 0 {
		visW, visH = w, h
	}
	canvas := image.NewRGBA(image.Rect(0, 0, visW, visH))
	if visW == w && visH == h {
		draw.Draw(canvas, canvas.Bounds(), src, b.Min, draw.Src)
	} else {
		scaled := scaleToMaxEdge(src, max(visW, visH))
		sb := scaled.Bounds()
		if sb.Dx() != visW || sb.Dy() != visH {
			scaled = scaleExact(scaled, visW, visH)
		}
		draw.Draw(canvas, canvas.Bounds(), scaled, scaled.Bounds().Min, draw.Src)
	}
	for _, n := range nodes {
		if n.ID == "" {
			continue
		}
		drawBadge(canvas, n.X, n.Y, n.ID)
	}
	var out bytes.Buffer
	if err := png.Encode(&out, canvas); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func scaleExact(src image.Image, nw, nh int) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
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

func drawBadge(img *image.RGBA, x, y int, label string) {
	label = strings.ToUpper(strings.TrimSpace(label))
	if label == "" {
		return
	}
	scale := 2
	gw, gh := 5*scale, 7*scale
	pad := 2
	bw := pad*2 + len([]rune(label))*(gw+scale) - scale
	bh := pad*2 + gh
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	max := img.Bounds().Max
	if x+bw > max.X {
		x = max.X - bw
	}
	if y+bh > max.Y {
		y = max.Y - bh
	}
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	for yy := 0; yy < bh; yy++ {
		for xx := 0; xx < bw; xx++ {
			img.SetRGBA(x+xx, y+yy, badgeFill)
		}
	}
	cx := x + pad
	cy := y + pad
	for _, r := range label {
		glyph, ok := badgeGlyphs[r]
		if !ok {
			cx += gw + scale
			continue
		}
		for row := 0; row < 7; row++ {
			bits := glyph[row]
			for col := 0; col < 5 && col < len(bits); col++ {
				if bits[col] != '1' {
					continue
				}
				for dy := 0; dy < scale; dy++ {
					for dx := 0; dx < scale; dx++ {
						img.SetRGBA(cx+col*scale+dx, cy+row*scale+dy, badgeInk)
					}
				}
			}
		}
		cx += gw + scale
	}
}
