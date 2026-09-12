package officestudio

import (
	"strings"
	"testing"
)

func TestGlyphMeasureLabelsGlyphWhenExtentWorks(t *testing.T) {
	m := GlyphMeasure{
		FontFamily: "TestFace",
		Extent: func(text, font string) (int, bool) {
			if font != "TestFace" {
				return 0, false
			}
			if text == "国" {
				return 20, true
			}
			return 20 * len([]rune(text)), true
		},
	}
	if m.Runes("订单") != 4 {
		t.Fatalf("scaled width=%d", m.Runes("订单"))
	}
	plans, err := PlanLayout(NarrativeNode{Title: "订单对照", Layout: "content", Bullets: []string{"口径说明"}}, DefaultBrand(), m)
	if err != nil || len(plans) == 0 {
		t.Fatal(err)
	}
	if !strings.Contains(plans[0].FitEvidence, "measure=glyph") || strings.Contains(plans[0].FitEvidence, "not glyph") {
		t.Fatalf("fitEvidence=%q", plans[0].FitEvidence)
	}
}

func TestGlyphMeasureFallsBackToEstimateWhenExtentFails(t *testing.T) {
	m := DefaultTextMeasure("MissingFace", func(string, string) (int, bool) { return 0, false })
	if _, ok := m.(EstimateMeasure); !ok {
		t.Fatalf("want EstimateMeasure, got %T", m)
	}
	plans, err := PlanLayout(NarrativeNode{Title: "订单对照", Layout: "content", Bullets: []string{"口径说明"}}, DefaultBrand(), m)
	if err != nil || len(plans) == 0 {
		t.Fatal(err)
	}
	if !strings.Contains(plans[0].FitEvidence, "not glyph") || strings.Contains(plans[0].FitEvidence, "measure=glyph") {
		t.Fatalf("fitEvidence=%q", plans[0].FitEvidence)
	}
	if strings.Contains(plans[0].FitEvidence, "glyph-verified") {
		t.Fatal("claimed glyph-verified without extent")
	}
}
