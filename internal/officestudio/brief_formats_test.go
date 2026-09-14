package officestudio

import (
	"bytes"
	"strings"
	"testing"
)

func TestOfficeBriefBrandFactsAcrossFormats(t *testing.T) {
	facts := []Fact{
		{FactID: "revenue_q1", Value: "1200000", Unit: "CNY", Period: "2026Q1", SourceID: "sales-v1", Locator: "Q1收入", Locked: true},
		{FactID: "orders", Value: "1280", Unit: "单", Period: "2026-06", SourceID: "ops-v1", Locator: "订单数", Locked: true},
	}
	brief := Brief{
		Audience: "管理层", Purpose: "月度经营对照", Language: "zh-CN",
		Deliverables: []Kind{DOCX, PPTX, XLSX, PDF},
		Facts:        facts,
	}

	var generated [][]byte
	var specs []Spec
	for _, brand := range []string{"ops-clear", "brand-pitch", "editorial-report"} {
		byKind, err := SpecsFromBrief(brief, brand)
		if err != nil {
			t.Fatal(err)
		}
		if len(byKind) != 4 {
			t.Fatalf("%s: want 4 formats, got %d", brand, len(byKind))
		}
		for _, kind := range []Kind{DOCX, PPTX, XLSX, PDF} {
			spec, ok := byKind[kind]
			if !ok {
				t.Fatalf("%s missing %s", brand, kind)
			}
			if spec.BrandID != brand && spec.TemplateID != brand && !strings.Contains(spec.TemplateID, brandStyle(brand)) {
				t.Fatalf("%s %s did not reuse brand tokens: brand=%s template=%s", brand, kind, spec.BrandID, spec.TemplateID)
			}
			if len(spec.Facts) != 2 {
				t.Fatalf("%s %s lost facts: %+v", brand, kind, spec.Facts)
			}
			for _, f := range spec.Facts {
				if !f.Locked || f.Value != factValue(facts, f.FactID) {
					t.Fatalf("%s %s unlocked or mutated fact: %+v", brand, kind, f)
				}
			}
			data, genErr := Generate(spec)
			if genErr != nil {
				t.Fatalf("%s %s generate: %v", brand, kind, genErr)
			}
			insp, inspErr := Inspect(kind, data)
			if inspErr != nil {
				t.Fatalf("%s %s inspect: %v", brand, kind, inspErr)
			}
			if err = AssertFactSetCoverage(spec.Facts, []Inspection{insp}); err != nil {
				t.Fatalf("%s %s lost locked facts: %v", brand, kind, err)
			}
			generated = append(generated, data)
			specs = append(specs, spec)
		}
	}

	opsDoc, brandDoc, editorialDoc := specs[0], specs[4], specs[8]
	if headingKey(opsDoc) == headingKey(brandDoc) || headingKey(brandDoc) == headingKey(editorialDoc) {
		t.Fatalf("docx brand difference must be structure, not only chrome: %s / %s / %s", headingKey(opsDoc), headingKey(brandDoc), headingKey(editorialDoc))
	}
	opsX, brandX, editorialX := specs[2], specs[6], specs[10]
	if sheetKey(opsX) == sheetKey(brandX) || sheetKey(brandX) == sheetKey(editorialX) {
		t.Fatalf("xlsx brand difference must be sheets: %s / %s / %s", sheetKey(opsX), sheetKey(brandX), sheetKey(editorialX))
	}
	if bytes.Equal(generated[1], generated[5]) {
		t.Fatal("pptx brands produced identical bytes")
	}
}

func brandStyle(brand string) string {
	switch brand {
	case "ops-clear":
		return "ops"
	case "brand-pitch":
		return "brand"
	case "editorial-report":
		return "editorial"
	default:
		return brand
	}
}

func factValue(facts []Fact, id string) string {
	for _, f := range facts {
		if f.FactID == id {
			return f.Value
		}
	}
	return ""
}

func headingKey(spec Spec) string {
	var parts []string
	for _, b := range spec.Blocks {
		if b.Type == "heading" || b.Type == "heading2" {
			parts = append(parts, b.Text)
		}
	}
	return strings.Join(parts, "|")
}

func sheetKey(spec Spec) string {
	var parts []string
	for _, s := range spec.Sheets {
		parts = append(parts, s.Name)
	}
	return strings.Join(parts, "|")
}
