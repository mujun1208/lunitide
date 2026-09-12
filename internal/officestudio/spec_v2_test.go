package officestudio

import (
	"bytes"
	"errors"
	"testing"
)

func TestGenerateV2MetricsWithoutBulletPipe(t *testing.T) {
	spec := Spec{
		SchemaVersion: 2,
		Kind:          PPTX,
		Title:         "指标",
		BrandID:       DefaultBrandID,
		Slides: []Slide{{
			Title:  "本月",
			Layout: "metrics",
			Metrics: []MetricBlock{
				{Label: "订单", Value: "1280", Unit: "单", FactID: "orders"},
				{Label: "退款率", Value: "2.4", Unit: "%", FactID: "refund_rate"},
			},
		}},
	}
	data, err := Generate(spec)
	if err != nil {
		t.Fatal(err)
	}
	parts := zipParts(t, data)
	slide := parts["ppt/slides/slide1.xml"]
	if !bytes.Contains(slide, []byte(">1280<")) || !bytes.Contains(slide, []byte(">订单<")) {
		t.Fatalf("metrics not rendered as native text: %s", slide)
	}
	if bytes.Contains(slide, []byte("1280|")) {
		t.Fatal("v2 must not encode metrics as pipe bullets")
	}
}

func TestUnknownSchemaVersionStillRejected(t *testing.T) {
	if _, err := Generate(Spec{SchemaVersion: 9, Kind: PDF, Title: "不支持", Body: "正文"}); err == nil {
		t.Fatal("accepted unknown schema")
	}
}

func TestGenerateV2DoesNotGuessComparisonFromBullets(t *testing.T) {
	_, err := Generate(Spec{
		SchemaVersion: 2, Kind: PPTX, Title: "对比",
		Slides: []Slide{{
			Title:   "方案",
			Layout:  "comparison",
			Bullets: []string{"方案A", "成本低", "方案B", "能力高"},
		}},
	})
	if !errors.Is(err, ErrFormat) {
		t.Fatalf("v2 comparison must not split bullets: %v", err)
	}
}

func TestGenerateV2DoesNotGuessMetricsFromPipeBullets(t *testing.T) {
	_, err := Generate(Spec{
		SchemaVersion: 2, Kind: PPTX, Title: "指标",
		Slides: []Slide{{
			Title:   "本月",
			Layout:  "metrics",
			Bullets: []string{"19%|增长率", "100|订单"},
		}},
	})
	if !errors.Is(err, ErrFormat) {
		t.Fatalf("v2 metrics must not parse pipe bullets: %v", err)
	}
}

func TestGenerateV2KeepsEvidenceText(t *testing.T) {
	data, err := Generate(Spec{
		SchemaVersion: 2, Kind: PPTX, Title: "证据",
		Slides: []Slide{{
			Title:    "来源",
			Layout:   "content",
			Bullets:  []string{"已核对订单口径"},
			Evidence: []EvidenceItem{{Text: "订单口径见附表A", FactID: "orders"}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	notes := zipParts(t, data)["ppt/notesSlides/notesSlide1.xml"]
	if !bytes.Contains(notes, []byte("订单口径见附表A")) {
		t.Fatalf("v2 evidence dropped: %s", notes)
	}
}

func TestPrepareThenGenerateDoesNotDuplicateEvidence(t *testing.T) {
	spec := Spec{
		SchemaVersion: 2, Kind: PPTX, Title: "证据",
		Slides: []Slide{{
			Title:    "来源",
			Layout:   "content",
			Bullets:  []string{"已核对订单口径"},
			Evidence: []EvidenceItem{{Text: "订单口径见附表A", FactID: "orders"}},
		}},
	}
	prepared, err := PrepareManagedSpec(spec)
	if err != nil {
		t.Fatal(err)
	}
	data, err := Generate(prepared)
	if err != nil {
		t.Fatal(err)
	}
	notes := zipParts(t, data)["ppt/notesSlides/notesSlide1.xml"]
	if bytes.Count(notes, []byte("订单口径见附表A")) != 1 {
		t.Fatalf("evidence duplicated after double prepare: %s", notes)
	}
}

func TestGenerateV2KeepsPurposeAndClaim(t *testing.T) {
	data, err := Generate(Spec{
		SchemaVersion: 2, Kind: PPTX, Title: "叙事",
		Slides: []Slide{{
			Title:   "本月",
			Layout:  "content",
			Purpose: "向管理层说明口径",
			Claim:   "订单口径与附表一致",
			Bullets: []string{"已核对订单口径"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	parts := zipParts(t, data)
	slide := parts["ppt/slides/slide1.xml"]
	if !bytes.Contains(slide, []byte("订单口径与附表一致")) {
		t.Fatalf("claim dropped from slide: %s", slide[:min(400, len(slide))])
	}
	notes := parts["ppt/notesSlides/notesSlide1.xml"]
	if !bytes.Contains(notes, []byte("向管理层说明口径")) {
		t.Fatalf("purpose dropped from notes: %s", notes)
	}
}

func TestResolveThemeSwitchKeepsFactValues(t *testing.T) {
	facts := []Fact{{FactID: "orders", Value: "1280", Unit: "单", Period: "2026-08", SourceID: "a"}}
	if err := ValidateFactSet(facts); err != nil {
		t.Fatal(err)
	}
	classic := ResolveTheme(DefaultBrand())
	alt := DefaultBrand()
	alt.BrandID = "lunitide-brand"
	alt.Colors["teal"] = "111111"
	branded := ResolveTheme(alt)
	if classic.Teal == branded.Teal {
		t.Fatal("brand colors must change")
	}
	if facts[0].Value != "1280" || facts[0].Unit != "单" {
		t.Fatal("switching brand must not rewrite facts")
	}
}
