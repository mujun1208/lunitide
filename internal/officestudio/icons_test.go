package officestudio

import (
	"bytes"
	"image"
	"image/png"
	"strings"
	"testing"

	"github.com/oklog/ulid/v2"
)

var iconCatalogWant = []string{
	"arrow-up-right", "bar-chart-line", "bookmark", "box", "briefcase", "building",
	"bullseye", "calendar", "cash-coin", "chat", "check-circle", "clipboard",
	"clock", "cloud", "compass", "cpu", "database", "envelope", "exclamation-triangle",
	"flag", "funnel", "gear", "globe", "graph-up-arrow", "hand-thumbs-up", "key",
	"layers", "lightbulb", "link-45deg", "list-check", "lock", "map", "mic",
	"people", "person-badge", "phone", "pin-map", "plug", "rocket", "search",
	"send", "shield-check", "sliders", "stack", "star", "tag", "trophy", "wrench",
}

func TestIconCatalogComplete(t *testing.T) {
	if len(iconCatalog) != 48 {
		t.Fatalf("catalog must hold exactly 48 icons, got %d", len(iconCatalog))
	}
	if len(iconCatalog) != len(iconCatalogNames) {
		t.Fatalf("name list out of sync: %d names for %d icons", len(iconCatalogNames), len(iconCatalog))
	}
	if !reflectStringsEqual(iconCatalogNames, iconCatalogWant) {
		t.Fatalf("catalog names drifted:\n got %v\nwant %v", iconCatalogNames, iconCatalogWant)
	}
}

func reflectStringsEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// A3-1: every catalog icon rasterizes to a decodable, non-empty, tinted PNG
// within the size budget.
func TestIconCatalogRendersAllIcons(t *testing.T) {
	const teal = "0D9488"
	for _, name := range iconCatalogNames {
		name := name
		t.Run(name, func(t *testing.T) {
			pngBytes, err := renderIconPNG(iconCatalog[name], teal, 480000)
			if err != nil {
				t.Fatalf("render %s: %v", name, err)
			}
			if len(pngBytes) >= iconMaxPNGBytes {
				t.Fatalf("render %s: %d bytes exceeds budget", name, len(pngBytes))
			}
			img, err := png.Decode(bytes.NewReader(pngBytes))
			if err != nil {
				t.Fatalf("decode %s: %v", name, err)
			}
			if got := img.Bounds().Dx(); got != 50 {
				t.Fatalf("decode %s: want 50px raster, got %d", name, got)
			}
			if img.Bounds().Dy() != 50 {
				t.Fatalf("decode %s: want square raster, got %d tall", name, img.Bounds().Dy())
			}
			nrgba, ok := img.(*image.NRGBA)
			if !ok {
				t.Fatalf("icon %s raster is not NRGBA", name)
			}
			ink := 0
			b := nrgba.Bounds()
			for y := b.Min.Y; y < b.Max.Y; y++ {
				for x := b.Min.X; x < b.Max.X; x++ {
					i := nrgba.PixOffset(x, y)
					if nrgba.Pix[i+3] == 0 {
						continue
					}
					ink++
					// Non-premultiplied tint: every inked pixel carries the theme RGB.
					if nrgba.Pix[i] != 0x0D || nrgba.Pix[i+1] != 0x94 || nrgba.Pix[i+2] != 0x88 {
						t.Fatalf("icon %s pixel (%d,%d) has color %02X %02X %02X, want teal", name, x, y, nrgba.Pix[i], nrgba.Pix[i+1], nrgba.Pix[i+2])
					}
				}
			}
			if ink < 16 {
				t.Fatalf("icon %s looks blank: only %d inked pixels", name, ink)
			}
			again, err := renderIconPNG(iconCatalog[name], teal, 480000)
			if err != nil || !bytes.Equal(pngBytes, again) {
				t.Fatalf("icon %s raster is not deterministic", name)
			}
		})
	}
}

func TestIconRenderClampsPixelRange(t *testing.T) {
	tiny, err := renderIconPNG(iconCatalog["rocket"], "0D9488", 20000)
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(tiny))
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() != iconMinPixels {
		t.Fatalf("tiny icon should clamp to %d px, got %d", iconMinPixels, img.Bounds().Dx())
	}
	huge, err := renderIconPNG(iconCatalog["rocket"], "0D9488", 9144000)
	if err != nil {
		t.Fatal(err)
	}
	img, err = png.Decode(bytes.NewReader(huge))
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() != iconMaxPixels {
		t.Fatalf("huge icon should clamp to %d px, got %d", iconMaxPixels, img.Bounds().Dx())
	}
}

// A3-2: parser flattening across command families.
func TestIconPathParser(t *testing.T) {
	t.Run("rect", func(t *testing.T) {
		subs, err := parseIconPath("M0 0H16V16H0Z")
		if err != nil {
			t.Fatal(err)
		}
		if len(subs) != 1 || len(subs[0]) != 4 {
			t.Fatalf("want one closed 4-point subpath, got %+v", subs)
		}
		want := []iconPoint{{0, 0}, {16, 0}, {16, 16}, {0, 16}}
		for i, p := range subs[0] {
			if p != want[i] {
				t.Fatalf("point %d = %+v, want %+v", i, p, want[i])
			}
		}
	})
	t.Run("relative", func(t *testing.T) {
		subs, err := parseIconPath("m1 1l2 0v2h-2z")
		if err != nil {
			t.Fatal(err)
		}
		if len(subs) != 1 || len(subs[0]) != 4 {
			t.Fatalf("relative square: %+v", subs)
		}
		if subs[0][1] != (iconPoint{x: 3, y: 1}) || subs[0][2] != (iconPoint{x: 3, y: 3}) || subs[0][3] != (iconPoint{x: 1, y: 3}) {
			t.Fatalf("relative square points wrong: %+v", subs[0])
		}
	})
	t.Run("implicit lineto after moveto", func(t *testing.T) {
		subs, err := parseIconPath("M0 0 5 5 10 10")
		if err != nil {
			t.Fatal(err)
		}
		if len(subs) != 1 || len(subs[0]) != 3 {
			t.Fatalf("repeated M groups must behave as lineto: %+v", subs)
		}
		if subs[0][1] != (iconPoint{x: 5, y: 5}) {
			t.Fatalf("implicit line point wrong: %+v", subs[0])
		}
	})
	t.Run("cubic and smooth cubic", func(t *testing.T) {
		subs, err := parseIconPath("M0 0C4 0 8 4 8 8")
		if err != nil {
			t.Fatal(err)
		}
		if len(subs[0]) != iconCurveSteps+1 {
			t.Fatalf("cubic should add %d points, got %d", iconCurveSteps, len(subs[0])-1)
		}
		last := subs[0][len(subs[0])-1]
		if last != (iconPoint{x: 8, y: 8}) {
			t.Fatalf("cubic endpoint wrong: %+v", last)
		}
		subs, err = parseIconPath("M0 0C4 0 8 4 8 8S12 12 16 8")
		if err != nil {
			t.Fatal(err)
		}
		end := subs[0][len(subs[0])-1]
		if end != (iconPoint{x: 16, y: 8}) {
			t.Fatalf("smooth cubic endpoint wrong: %+v", end)
		}
	})
	t.Run("quadratic and smooth quadratic", func(t *testing.T) {
		subs, err := parseIconPath("M0 0Q8 0 8 8")
		if err != nil {
			t.Fatal(err)
		}
		if len(subs[0]) != iconCurveSteps+1 {
			t.Fatalf("quadratic should add %d points, got %d", iconCurveSteps, len(subs[0])-1)
		}
		subs, err = parseIconPath("M0 0Q8 0 8 8T16 16")
		if err != nil {
			t.Fatal(err)
		}
		end := subs[0][len(subs[0])-1]
		if end != (iconPoint{x: 16, y: 16}) {
			t.Fatalf("smooth quadratic endpoint wrong: %+v", end)
		}
	})
	t.Run("multiple subpaths", func(t *testing.T) {
		subs, err := parseIconPath("M0 0H4V4H0Z M8 8h4v4h-4z")
		if err != nil {
			t.Fatal(err)
		}
		if len(subs) != 2 {
			t.Fatalf("want 2 subpaths, got %d", len(subs))
		}
	})
}

// A3-2: arcs use endpoint parameterization and hit the geometric extremes.
func TestIconArcParsing(t *testing.T) {
	subs, err := parseIconPath("M8 1A7 7 0 0 1 8 15")
	if err != nil {
		t.Fatal(err)
	}
	if len(subs) != 1 {
		t.Fatalf("arc should stay one subpath, got %d", len(subs))
	}
	var rightPole, leftSpan bool
	for _, p := range subs[0] {
		if p.x > 14.5 && (p.y-8)*(p.y-8) < 1 {
			rightPole = true
		}
		if p.x > 7.9 && p.x < 8.1 && p.y > 14 {
			leftSpan = true
		}
	}
	if !rightPole {
		t.Fatalf("sweep=1 semicircle should pass the right pole, points=%+v", subs[0])
	}
	if !leftSpan {
		t.Fatalf("arc should end at (8,15), points=%+v", subs[0])
	}
	// A full circle via two arcs: nonzero winding covers the center.
	subs, err = parseIconPath("M8 1A7 7 0 1 1 8 15A7 7 0 1 1 8 1")
	if err != nil {
		t.Fatal(err)
	}
	if len(subs[0]) < iconCurveSteps {
		t.Fatalf("full circle should flatten many points, got %d", len(subs[0]))
	}
}

func TestIconScanlineFillRules(t *testing.T) {
	// A 4x4 ring (evenodd hole in the middle) drawn as one path.
	ring := "M0 0H8V8H0Z M2 2V6H6V2Z"
	subs, err := parseIconPath(ring)
	if err != nil {
		t.Fatal(err)
	}
	n := 32
	scale := float64(n) / 8.0
	even := make([]byte, n*n)
	fillIconPath(subs, true, n, scale, even)
	// Center of the hole (2..6) must be empty under evenodd.
	if even[(n/2)*n+n/2] != 0 {
		t.Fatal("evenodd fill must hollow the ring center")
	}
	// Same geometry under nonzero also hollows (opposite winding would not).
	nonzero := make([]byte, n*n)
	fillIconPath(subs, false, n, scale, nonzero)
	if nonzero[(n/2)*n+n/2] != 0 {
		t.Fatal("same-winding nested paths hollow under nonzero too")
	}
	if even[(n/8)*n] == 0 || nonzero[(n/8)*n] == 0 {
		t.Fatal("ring body must be filled on both rules")
	}
}

func TestIconSourceIDIsValidULID(t *testing.T) {
	for _, name := range []string{"rocket", "trophy", "star"} {
		sid := iconSourceID(name, strings.Repeat("ab", 32))
		u, err := ulid.ParseStrict(sid)
		if err != nil {
			t.Fatalf("source id %q is not a strict ULID: %v", sid, err)
		}
		if u.String() != sid {
			t.Fatalf("source id %q does not roundtrip: %q", sid, u.String())
		}
		if iconSourceID(name, strings.Repeat("ab", 32)) != sid {
			t.Fatal("source id is not deterministic")
		}
		if iconSourceID(name, strings.Repeat("cd", 32)) == sid {
			t.Fatal("different content should derive a different source id")
		}
	}
}

// A3-4: rejection paths for unknown names, bad colors, sizes, and bounds.
func TestIconMaterializeRejects(t *testing.T) {
	base := SlideIcon{Name: "rocket", X: 100, Y: 100, Size: 480000}
	if _, err := materializeSlideIcons([]SlideIcon{{Name: "sparkles", X: 1, Y: 1, Size: 100}}, "0D9488"); err == nil || !strings.Contains(err.Error(), "unknown icon") {
		t.Fatalf("unknown icon must be rejected, got %v", err)
	}
	if _, err := materializeSlideIcons([]SlideIcon{{Name: "rocket", X: 1, Y: 1, Size: 100, Color: "red"}}, "0D9488"); err == nil {
		t.Fatal("non-hex color must be rejected")
	}
	if _, err := materializeSlideIcons([]SlideIcon{{Name: "rocket", X: 1, Y: 1, Size: 100, Color: "0D948"}}, "0D9488"); err == nil {
		t.Fatal("5-digit color must be rejected")
	}
	if _, err := materializeSlideIcons([]SlideIcon{{Name: "rocket", X: 1, Y: 1, Size: 0}}, "0D9488"); err == nil {
		t.Fatal("zero size must be rejected")
	}
	if _, err := materializeSlideIcons([]SlideIcon{{Name: "rocket", X: -1, Y: 1, Size: 100}}, "0D9488"); err == nil {
		t.Fatal("negative x must be rejected")
	}
	if _, err := materializeSlideIcons([]SlideIcon{{Name: "rocket", X: 12192000 - 50, Y: 1, Size: 100}}, "0D9488"); err == nil {
		t.Fatal("right overflow must be rejected")
	}
	if _, err := materializeSlideIcons([]SlideIcon{{Name: "rocket", X: 1, Y: 6858000 - 50, Size: 100}}, "0D9488"); err == nil {
		t.Fatal("bottom overflow must be rejected")
	}
	if mats, err := materializeSlideIcons([]SlideIcon{base, {Name: "trophy", X: 200, Y: 200, Size: 480000, Color: "C9A227"}}, "0D9488"); err != nil || len(mats) != 2 {
		t.Fatalf("valid pair must materialize: %v", err)
	} else {
		for _, m := range mats {
			u, e := ulid.ParseStrict(m.SourceID)
			if e != nil || u.String() != m.SourceID {
				t.Fatalf("materialized source id invalid: %q", m.SourceID)
			}
			if m.Fit != "contain" || m.Alt == "" || !strings.HasPrefix(m.Alt, "图标 ") {
				t.Fatalf("materialized image metadata wrong: %+v", m)
			}
			if m.Width != m.Height || m.Width <= 0 {
				t.Fatalf("materialized box must stay square: %+v", m)
			}
		}
	}
}

// A3-3 + A3-5: end-to-end generation with icons, media wiring, and
// byte-for-byte determinism.
func TestIconSlideGenerateEndToEnd(t *testing.T) {
	spec := Spec{
		SchemaVersion: 1,
		Kind:          PPTX,
		Title:         "图标端到端",
		Slides: []Slide{{
			Title:  "图标页",
			Layout: "content",
			Bullets: []string{"要点一", "要点二"},
			Icons: []SlideIcon{
				{Name: "rocket", X: 10900000, Y: 5600000, Size: 480000},
				{Name: "trophy", X: 10900000, Y: 4800000, Size: 480000, Color: "C9A227"},
			},
		}},
	}
	data, err := Generate(spec)
	if err != nil {
		t.Fatal(err)
	}
	again, err := Generate(spec)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, again) {
		t.Fatal("icon deck is not byte-deterministic")
	}
	parts := zipParts(t, data)
	media := 0
	for name := range parts {
		if strings.HasPrefix(name, "ppt/media/studio-") && strings.HasSuffix(name, ".png") {
			media++
			if _, err := png.Decode(bytes.NewReader(parts[name])); err != nil {
				t.Fatalf("icon media %s does not decode: %v", name, err)
			}
		}
	}
	if media != 2 {
		t.Fatalf("want 2 icon media parts, got %d", media)
	}
	slide := string(parts["ppt/slides/slide1.xml"])
	if !strings.Contains(slide, "图标 rocket") || !strings.Contains(slide, "图标 trophy") {
		t.Fatal("slide XML must carry the icon alt text")
	}
	rels := string(parts["ppt/slides/_rels/slide1.xml.rels"])
	if strings.Count(rels, "image") < 2 {
		t.Fatalf("slide rels must wire both icons: %s", rels)
	}
}

// A3-3: switching brand re-tints rendered icon pixels.
func TestIconThemeColorFollow(t *testing.T) {
	tealPNG, err := renderIconPNG(iconCatalog["rocket"], "0D9488", 480000)
	if err != nil {
		t.Fatal(err)
	}
	goldPNG, err := renderIconPNG(iconCatalog["rocket"], "C9A227", 480000)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(tealPNG, goldPNG) {
		t.Fatal("different tints must produce different rasters")
	}
	img, err := png.Decode(bytes.NewReader(goldPNG))
	if err != nil {
		t.Fatal(err)
	}
	nrgba, ok := img.(*image.NRGBA)
	if !ok {
		t.Fatal("icon raster is not NRGBA")
	}
	b := nrgba.Bounds()
	found := false
	for y := b.Min.Y; !found && y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			i := nrgba.PixOffset(x, y)
			if nrgba.Pix[i+3] == 0 {
				continue
			}
			if nrgba.Pix[i] != 0xC9 || nrgba.Pix[i+1] != 0xA2 || nrgba.Pix[i+2] != 0x27 {
				t.Fatalf("gold tint not applied at (%d,%d): %02X %02X %02X", x, y, nrgba.Pix[i], nrgba.Pix[i+1], nrgba.Pix[i+2])
			}
			found = true
			break
		}
	}
	if !found {
		t.Fatal("gold icon raster is blank")
	}
}
