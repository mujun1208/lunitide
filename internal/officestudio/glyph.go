package officestudio

import "strings"

type GlyphExtent func(text, fontFamily string) (pixelWidth int, ok bool)

type GlyphMeasure struct {
	FontFamily string
	Extent     GlyphExtent
}

func (g GlyphMeasure) Runes(s string) int {
	fallback := (EstimateMeasure{}).Runes(s)
	if g.Extent == nil {
		return fallback
	}
	unit, ok := g.Extent("国", g.FontFamily)
	if !ok || unit < 1 {
		return fallback
	}
	w, ok := g.Extent(s, g.FontFamily)
	if !ok || w < 1 {
		return fallback
	}
	scaled := (w * 2) / unit
	if scaled < 1 {
		return 1
	}
	return scaled
}

func DefaultTextMeasure(fontFamily string, extent ...GlyphExtent) TextMeasure {
	fontFamily = strings.TrimSpace(fontFamily)
	var ext GlyphExtent
	if len(extent) > 0 && extent[0] != nil {
		ext = extent[0]
	} else {
		ext = defaultGlyphExtent
	}
	if fontFamily == "" || ext == nil {
		return EstimateMeasure{}
	}
	if _, ok := ext("国", fontFamily); !ok {
		return EstimateMeasure{}
	}
	return GlyphMeasure{FontFamily: fontFamily, Extent: ext}
}
