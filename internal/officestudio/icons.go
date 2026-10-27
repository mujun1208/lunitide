package officestudio

// Pure-Go SVG path rasterizer for the built-in icon catalog (icondata.go).
// Each icon is flattened, scanline-filled at 4x supersampling, downsampled
// to 16-level alpha, tinted with the requested RGB, and encoded as a
// deterministic PNG that flows through the regular slide image pipeline.

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"image"
	"image/png"
	"math"
	"sort"
	"strconv"

	"github.com/oklog/ulid/v2"
)

const (
	iconEmuPerPixel = 9525
	iconMinPixels   = 16
	iconMaxPixels   = 256
	iconSSAA        = 4
	iconViewBox     = 16.0
	iconCurveSteps  = 16
	iconMaxPNGBytes = 128 << 10
	iconSlideWidth  = 12192000
	iconSlideHeight = 6858000
)

// iconCatalogNames lists every catalog entry in stable order for errors,
// schemas, and prompts.
var iconCatalogNames = func() []string {
	names := make([]string, 0, len(iconCatalog))
	for name := range iconCatalog {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}()

// IconNames returns the built-in icon names in stable order.
func IconNames() []string {
	return append([]string(nil), iconCatalogNames...)
}

type iconPoint struct{ x, y float64 }

type iconRGB struct{ r, g, b uint8 }

func iconHexRGB(s string) (iconRGB, error) {
	if len(s) != 6 {
		return iconRGB{}, fmt.Errorf("%w: icon color must be a 6-digit hex RGB value", ErrFormat)
	}
	v, err := strconv.ParseUint(s, 16, 24)
	if err != nil {
		return iconRGB{}, fmt.Errorf("%w: icon color must be a 6-digit hex RGB value", ErrFormat)
	}
	return iconRGB{r: uint8(v >> 16), g: uint8(v >> 8), b: uint8(v)}, nil
}

// --- SVG path parser: flattens path data into polyline subpaths ---

type iconPathParser struct {
	d   string
	pos int

	sub        [][]iconPoint // completed subpaths
	cur        []iconPoint   // current subpath vertices
	cx, cy     float64       // current pen
	sx, sy     float64       // current subpath start
	lastC      iconPoint     // previous cubic c2 (S reflection)
	lastQ      iconPoint     // previous quadratic control (T reflection)
	haveC      bool
	haveQ      bool
	started    bool // whether any coordinate was placed (pen is meaningful)
	subStarted bool // whether cur is open
}

func (p *iconPathParser) skipSeparators() {
	for p.pos < len(p.d) {
		switch p.d[p.pos] {
		case ' ', '\t', '\n', '\r', ',':
			p.pos++
		default:
			return
		}
	}
}

func (p *iconPathParser) readNumber() (float64, bool) {
	p.skipSeparators()
	start := p.pos
	if p.pos < len(p.d) && (p.d[p.pos] == '+' || p.d[p.pos] == '-') {
		p.pos++
	}
	digits := 0
	for p.pos < len(p.d) && p.d[p.pos] >= '0' && p.d[p.pos] <= '9' {
		p.pos++
		digits++
	}
	if p.pos < len(p.d) && p.d[p.pos] == '.' {
		p.pos++
		for p.pos < len(p.d) && p.d[p.pos] >= '0' && p.d[p.pos] <= '9' {
			p.pos++
			digits++
		}
	}
	if digits == 0 {
		p.pos = start
		return 0, false
	}
	v, err := strconv.ParseFloat(p.d[start:p.pos], 64)
	if err != nil {
		p.pos = start
		return 0, false
	}
	return v, true
}

func (p *iconPathParser) readFlag() (bool, bool) {
	p.skipSeparators()
	if p.pos >= len(p.d) {
		return false, false
	}
	switch p.d[p.pos] {
	case '0':
		p.pos++
		return false, true
	case '1':
		p.pos++
		return true, true
	}
	return false, false
}

func isPathCommand(c byte) bool {
	switch c {
	case 'M', 'm', 'L', 'l', 'H', 'h', 'V', 'v', 'C', 'c', 'S', 's',
		'Q', 'q', 'T', 't', 'A', 'a', 'Z', 'z':
		return true
	}
	return false
}

// closeCur commits the open subpath. Z already appended the start vertex;
// an implicit close (a new M) leaves the closing edge to the scanline.
func (p *iconPathParser) closeCur() {
	if len(p.cur) > 0 {
		p.sub = append(p.sub, p.cur)
		p.cur = nil
	}
	p.subStarted = false
}

// parseIconPath flattens an SVG path into polyline subpaths in viewBox
// coordinates. Curves contribute iconCurveSteps segments per command;
// elliptical arcs convert to cubic segments of at most 90 degrees each.
func parseIconPath(d string) ([][]iconPoint, error) {
	p := &iconPathParser{d: d}
	var cmd byte
	for {
		p.skipSeparators()
		if p.pos >= len(p.d) {
			break
		}
		c := p.d[p.pos]
		if isPathCommand(c) {
			cmd = c
			p.pos++
			if c == 'Z' || c == 'z' {
				if p.subStarted && len(p.cur) > 0 {
					// The scanline filler closes subpaths implicitly; Z only
					// resets the pen and commits the subpath.
					p.cx, p.cy = p.sx, p.sy
					p.closeCur()
				}
				p.haveC, p.haveQ = false, false
				cmd = 0
				continue
			}
		} else if cmd == 0 {
			return nil, fmt.Errorf("%w: icon path has coordinates before a command", ErrFormat)
		}
		first := true
		for {
			effective := cmd
			if !first && (cmd == 'M' || cmd == 'm') {
				if cmd == 'M' {
					effective = 'L'
				} else {
					effective = 'l'
				}
			}
			moved, err := p.execSegment(effective)
			if err != nil {
				return nil, err
			}
			if !moved {
				break
			}
			first = false
		}
	}
	p.closeCur()
	return p.sub, nil
}

// execSegment consumes one parameter group for cmd, appending flattened
// geometry. It reports whether a complete group was available.
func (p *iconPathParser) execSegment(cmd byte) (bool, error) {
	rel := cmd >= 'a' && cmd <= 'z'
	readPair := func() (float64, float64, bool) {
		x, ok := p.readNumber()
		if !ok {
			return 0, 0, false
		}
		y, ok := p.readNumber()
		if !ok {
			return 0, 0, false
		}
		return x, y, true
	}
	abs := func(x, y float64) (float64, float64) {
		if rel {
			return p.cx + x, p.cy + y
		}
		return x, y
	}
	markCubic := func(c2 iconPoint) {
		p.lastC = c2
		p.haveC = true
		p.haveQ = false
	}
	markQuad := func(q iconPoint) {
		p.lastQ = q
		p.haveQ = true
		p.haveC = false
	}
	markNone := func() {
		p.haveC, p.haveQ = false, false
	}
	lineTo := func(x, y float64) {
		p.cur = append(p.cur, iconPoint{x: x, y: y})
		p.cx, p.cy = x, y
	}

	switch cmd {
	case 'M', 'm':
		x, y, ok := readPair()
		if !ok {
			return false, nil
		}
		ax, ay := abs(x, y)
		p.closeCur() // implicit close of any open subpath
		p.cur = []iconPoint{{x: ax, y: ay}}
		p.cx, p.cy = ax, ay
		p.sx, p.sy = ax, ay
		p.subStarted, p.started = true, true
		markNone()
	case 'L', 'l':
		x, y, ok := readPair()
		if !ok {
			return false, nil
		}
		ax, ay := abs(x, y)
		lineTo(ax, ay)
		markNone()
	case 'H', 'h':
		x, ok := p.readNumber()
		if !ok {
			return false, nil
		}
		if rel {
			x += p.cx
		}
		lineTo(x, p.cy)
		markNone()
	case 'V', 'v':
		y, ok := p.readNumber()
		if !ok {
			return false, nil
		}
		if rel {
			y += p.cy
		}
		lineTo(p.cx, y)
		markNone()
	case 'C', 'c':
		x1, y1, ok := readPair()
		if !ok {
			return false, nil
		}
		x2, y2, ok := readPair()
		if !ok {
			return false, nil
		}
		x3, y3, ok := readPair()
		if !ok {
			return false, nil
		}
		ax1, ay1 := abs(x1, y1)
		ax2, ay2 := abs(x2, y2)
		ax3, ay3 := abs(x3, y3)
		p.flattenCubic(ax1, ay1, ax2, ay2, ax3, ay3)
		markCubic(iconPoint{x: ax2, y: ay2})
	case 'S', 's':
		x2, y2, ok := readPair()
		if !ok {
			return false, nil
		}
		x3, y3, ok := readPair()
		if !ok {
			return false, nil
		}
		ax2, ay2 := abs(x2, y2)
		ax3, ay3 := abs(x3, y3)
		ax1, ay1 := p.cx, p.cy
		if p.haveC {
			ax1, ay1 = 2*p.cx-p.lastC.x, 2*p.cy-p.lastC.y
		}
		p.flattenCubic(ax1, ay1, ax2, ay2, ax3, ay3)
		markCubic(iconPoint{x: ax2, y: ay2})
	case 'Q', 'q':
		x1, y1, ok := readPair()
		if !ok {
			return false, nil
		}
		x2, y2, ok := readPair()
		if !ok {
			return false, nil
		}
		ax1, ay1 := abs(x1, y1)
		ax2, ay2 := abs(x2, y2)
		p.flattenQuad(ax1, ay1, ax2, ay2)
		markQuad(iconPoint{x: ax1, y: ay1})
	case 'T', 't':
		x2, y2, ok := readPair()
		if !ok {
			return false, nil
		}
		ax2, ay2 := abs(x2, y2)
		ax1, ay1 := p.cx, p.cy
		if p.haveQ {
			ax1, ay1 = 2*p.cx-p.lastQ.x, 2*p.cy-p.lastQ.y
		}
		p.flattenQuad(ax1, ay1, ax2, ay2)
		markQuad(iconPoint{x: ax1, y: ay1})
	case 'A', 'a':
		rx, ok := p.readNumber()
		if !ok {
			return false, nil
		}
		ry, ok := p.readNumber()
		if !ok {
			return false, nil
		}
		rot, ok := p.readNumber()
		if !ok {
			return false, nil
		}
		large, ok := p.readFlag()
		if !ok {
			return false, nil
		}
		sweep, ok := p.readFlag()
		if !ok {
			return false, nil
		}
		x1, y1, ok := readPair()
		if !ok {
			return false, nil
		}
		ax1, ay1 := abs(x1, y1)
		for _, seg := range arcToCubics(p.cx, p.cy, rx, ry, rot, large, sweep, ax1, ay1) {
			p.flattenCubic(seg[0], seg[1], seg[2], seg[3], seg[4], seg[5])
		}
		markNone()
	default:
		return false, fmt.Errorf("%w: unsupported icon path command %q", ErrFormat, string(cmd))
	}
	return true, nil
}

func (p *iconPathParser) flattenCubic(x1, y1, x2, y2, x3, y3 float64) {
	x0, y0 := p.cx, p.cy
	const n = iconCurveSteps
	for i := 1; i <= n; i++ {
		t := float64(i) / n
		u := 1 - t
		a := u * u * u
		b := 3 * u * u * t
		c := 3 * u * t * t
		e := t * t * t
		p.cur = append(p.cur, iconPoint{
			x: a*x0 + b*x1 + c*x2 + e*x3,
			y: a*y0 + b*y1 + c*y2 + e*y3,
		})
	}
	p.cx, p.cy = x3, y3
}

func (p *iconPathParser) flattenQuad(x1, y1, x2, y2 float64) {
	x0, y0 := p.cx, p.cy
	const n = iconCurveSteps
	for i := 1; i <= n; i++ {
		t := float64(i) / n
		u := 1 - t
		a := u * u
		b := 2 * u * t
		c := t * t
		p.cur = append(p.cur, iconPoint{
			x: a*x0 + b*x1 + c*x2,
			y: a*y0 + b*y1 + c*y2,
		})
	}
	p.cx, p.cy = x2, y2
}

// arcToCubics converts an SVG elliptical-arc parameter group into cubic
// Bezier segments using the W3C endpoint parameterization (each segment
// spans at most 90 degrees).
func arcToCubics(x0, y0, rx, ry, phiDeg float64, largeArc, sweep bool, x1, y1 float64) [][6]float64 {
	straight := [][6]float64{{
		x0 + (x1-x0)/3, y0 + (y1-y0)/3,
		x0 + 2*(x1-x0)/3, y0 + 2*(y1-y0)/3,
		x1, y1,
	}}
	if (x0 == x1 && y0 == y1) || rx == 0 || ry == 0 {
		return straight
	}
	rx, ry = math.Abs(rx), math.Abs(ry)
	phi := phiDeg * math.Pi / 180
	cosP, sinP := math.Cos(phi), math.Sin(phi)
	hx, hy := (x0-x1)/2, (y0-y1)/2
	x1p := cosP*hx + sinP*hy
	y1p := -sinP*hx + cosP*hy
	lambda := x1p*x1p/(rx*rx) + y1p*y1p/(ry*ry)
	if lambda > 1 {
		s := math.Sqrt(lambda)
		rx *= s
		ry *= s
	}
	num := rx*rx*ry*ry - rx*rx*y1p*y1p - ry*ry*x1p*x1p
	den := rx*rx*y1p*y1p + ry*ry*x1p*x1p
	if den == 0 {
		return straight
	}
	coef := math.Sqrt(math.Max(0, num/den))
	if largeArc == sweep {
		coef = -coef
	}
	cxp := coef * rx * y1p / ry
	cyp := -coef * ry * x1p / rx
	cx := cosP*cxp - sinP*cyp + (x0+x1)/2
	cy := sinP*cxp + cosP*cyp + (y0+y1)/2
	ux, uy := (x1p-cxp)/rx, (y1p-cyp)/ry
	vx, vy := (-x1p-cxp)/rx, (-y1p-cyp)/ry
	theta := math.Atan2(uy, ux)
	dtheta := math.Atan2(ux*vy-uy*vx, ux*vx+uy*vy)
	if !sweep && dtheta > 0 {
		dtheta -= 2 * math.Pi
	}
	if sweep && dtheta < 0 {
		dtheta += 2 * math.Pi
	}
	if dtheta == 0 {
		return straight
	}
	segs := int(math.Ceil(math.Abs(dtheta) / (math.Pi / 2)))
	if segs < 1 {
		segs = 1
	}
	delta := dtheta / float64(segs)
	kappa := 4.0 / 3.0 * math.Tan(delta/2)
	point := func(a float64) (float64, float64) {
		c, s := math.Cos(a), math.Sin(a)
		px, py := rx*c, ry*s
		return cosP*px - sinP*py + cx, sinP*px + cosP*py + cy
	}
	tangent := func(a float64) (float64, float64) {
		s, c := math.Sin(a), math.Cos(a)
		tx, ty := -rx*s, ry*c
		return cosP*tx - sinP*ty, sinP*tx + cosP*ty
	}
	out := make([][6]float64, 0, segs)
	for i := 0; i < segs; i++ {
		a0 := theta + delta*float64(i)
		a1 := a0 + delta
		px0, py0 := point(a0)
		px1, py1 := point(a1)
		tx0, ty0 := tangent(a0)
		tx1, ty1 := tangent(a1)
		out = append(out, [6]float64{
			px0 + kappa*tx0, py0 + kappa*ty0,
			px1 - kappa*tx1, py1 - kappa*ty1,
			px1, py1,
		})
	}
	return out
}

// --- Scanline fill with 4x supersampling ---

type iconCross struct {
	x   float64
	dir int
}

type iconEdge struct {
	x0, y0, x1, y1 float64 // y0 < y1
	dir            int     // +1 downward, -1 upward
}

// fillIconPath rasterizes one path's subpaths into cover. evenOdd picks the
// SVG fill rule for this path; the rule applies across all of its subpaths.
func fillIconPath(subs [][]iconPoint, evenOdd bool, n int, scale float64, cover []byte) {
	var edges []iconEdge
	for _, pts := range subs {
		if len(pts) < 2 {
			continue
		}
		for i := 0; i < len(pts); i++ {
			j := i + 1
			if j == len(pts) {
				j = 0 // implicit closing edge; duplicates of explicit Z collapse
			}
			a, b := pts[i], pts[j]
			if a.y == b.y {
				continue
			}
			e := iconEdge{dir: 1}
			if a.y < b.y {
				e.x0, e.y0, e.x1, e.y1 = a.x, a.y, b.x, b.y
			} else {
				e.x0, e.y0, e.x1, e.y1 = b.x, b.y, a.x, a.y
				e.dir = -1
			}
			edges = append(edges, e)
		}
	}
	if len(edges) == 0 {
		return
	}
	crossings := make([]iconCross, 0, len(edges))
	for row := 0; row < n; row++ {
		y := (float64(row) + 0.5) / scale
		crossings = crossings[:0]
		for _, e := range edges {
			if y < e.y0 || y >= e.y1 {
				continue
			}
			t := (y - e.y0) / (e.y1 - e.y0)
			crossings = append(crossings, iconCross{x: e.x0 + t*(e.x1-e.x0), dir: e.dir})
		}
		if len(crossings) < 2 {
			continue
		}
		sort.Slice(crossings, func(i, j int) bool { return crossings[i].x < crossings[j].x })
		if evenOdd {
			for i := 0; i+1 < len(crossings); i += 2 {
				fillIconRange(cover, n, scale, crossings[i].x, crossings[i+1].x, row)
			}
		} else {
			wind := 0
			xa := 0.0
			for _, c := range crossings {
				prev := wind
				wind += c.dir
				if prev == 0 && wind != 0 {
					xa = c.x
				} else if prev != 0 && wind == 0 {
					fillIconRange(cover, n, scale, xa, c.x, row)
				}
			}
		}
	}
}

// fillIconRange marks cover pixels whose centers fall inside [xa, xb).
func fillIconRange(cover []byte, n int, scale float64, xa, xb float64, row int) {
	if xb <= xa {
		return
	}
	i0 := int(math.Ceil(xa*scale - 0.5))
	i1 := int(math.Ceil(xb*scale - 0.5))
	if i0 < 0 {
		i0 = 0
	}
	if i1 > n {
		i1 = n
	}
	base := row * n
	for i := i0; i < i1; i++ {
		cover[base+i] = 1
	}
}

// renderIconPNG rasterizes one catalog icon into a tinted PNG. sizeEMU is
// the on-slide edge; the raster is clamped to [iconMinPixels,
// iconMaxPixels]. Output is deterministic for identical inputs.
func renderIconPNG(paths []iconPathData, hexColor string, sizeEMU int64) ([]byte, error) {
	if sizeEMU <= 0 {
		return nil, fmt.Errorf("%w: icon size must be positive EMU", ErrFormat)
	}
	px := sizeEMU / iconEmuPerPixel
	if px < iconMinPixels {
		px = iconMinPixels
	}
	if px > iconMaxPixels {
		px = iconMaxPixels
	}
	rgb, err := iconHexRGB(hexColor)
	if err != nil {
		return nil, err
	}
	n := int(px) * iconSSAA
	scale := float64(n) / iconViewBox
	cover := make([]byte, n*n)
	pathCov := make([]byte, n*n)
	for _, pd := range paths {
		subs, err := parseIconPath(pd.d)
		if err != nil {
			return nil, err
		}
		for i := range pathCov {
			pathCov[i] = 0
		}
		fillIconPath(subs, pd.evenOdd, n, scale, pathCov)
		for i, v := range pathCov {
			if v > cover[i] {
				cover[i] = v
			}
		}
	}
	img := image.NewNRGBA(image.Rect(0, 0, int(px), int(px)))
	for py := 0; py < int(px); py++ {
		for pix := 0; pix < int(px); pix++ {
			sum := 0
			for dy := 0; dy < iconSSAA; dy++ {
				base := (py*iconSSAA + dy) * n
				for dx := 0; dx < iconSSAA; dx++ {
					sum += int(cover[base+pix*iconSSAA+dx])
				}
			}
			if sum == 0 {
				continue
			}
			alpha := byte((sum*255 + 8) / (iconSSAA * iconSSAA))
			i := img.PixOffset(pix, py)
			img.Pix[i] = rgb.r
			img.Pix[i+1] = rgb.g
			img.Pix[i+2] = rgb.b
			img.Pix[i+3] = alpha
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	if buf.Len() > iconMaxPNGBytes {
		return nil, fmt.Errorf("%w: icon raster is %d bytes, over the %d byte budget", ErrLimit, buf.Len(), iconMaxPNGBytes)
	}
	return buf.Bytes(), nil
}

// iconSourceID derives a deterministic valid ULID from the icon name and
// its PNG digest. The source ID is written into the package (picture
// cNvPr), so a time-random ULID would break byte-for-byte determinism.
func iconSourceID(name, pngSHA string) string {
	sum := sha256.Sum256([]byte("lunitide-icon\x00" + name + "\x00" + pngSHA))
	var raw [16]byte
	copy(raw[:], sum[:16])
	raw[15] &= 0xFC // ULID's final character must end in zero bits
	return ulid.ULID(raw).String()
}

// materializeSlideIcons rasterizes icons into slide pictures. The brand
// teal is the default tint; each icon becomes a contain-fit picture whose
// bytes, digest, and source ID are fully deterministic.
func materializeSlideIcons(icons []SlideIcon, teal string) ([]SlideImage, error) {
	out := make([]SlideImage, 0, len(icons))
	for _, ic := range icons {
		paths, ok := iconCatalog[ic.Name]
		if !ok {
			return nil, fmt.Errorf("%w: unknown icon %q; pick one of the %d built-in icons", ErrFormat, ic.Name, len(iconCatalog))
		}
		hexColor := ic.Color
		if hexColor == "" {
			hexColor = teal
		}
		if _, err := iconHexRGB(hexColor); err != nil {
			return nil, err
		}
		if ic.Size <= 0 {
			return nil, fmt.Errorf("%w: icon %q needs a positive size in EMU", ErrFormat, ic.Name)
		}
		if ic.X < 0 || ic.Y < 0 || ic.X+ic.Size > iconSlideWidth || ic.Y+ic.Size > iconSlideHeight {
			return nil, fmt.Errorf("%w: icon %q box exceeds slide bounds", ErrFormat, ic.Name)
		}
		png, err := renderIconPNG(paths, hexColor, ic.Size)
		if err != nil {
			return nil, err
		}
		sha := digest(png)
		out = append(out, SlideImage{
			SourceID: iconSourceID(ic.Name, sha),
			SHA256:   sha,
			X:        ic.X,
			Y:        ic.Y,
			Width:    ic.Size,
			Height:   ic.Size,
			Fit:      "contain",
			Alt:      "图标 " + ic.Name,
			Data:     png,
		})
	}
	return out, nil
}
