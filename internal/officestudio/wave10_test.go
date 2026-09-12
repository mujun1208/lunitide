package officestudio

import (
	"strings"
	"testing"
)

func TestWorkbookCalcCountsSourceDataNotOwnHeader(t *testing.T) {
	spec, err := PlanWorkbook("ops-ledger", "经营簿", []Fact{
		{FactID: "orders", Value: "1280", Unit: "单", Period: "2026-08-01", Locator: "订单数"},
	})
	if err != nil {
		t.Fatal(err)
	}
	var formula string
	var periodType string
	for _, sh := range spec.Sheets {
		switch sh.Name {
		case "计算":
			for _, row := range sh.Rows {
				for _, cell := range row {
					if cell.Type == "formula" {
						formula = cell.Value
					}
				}
			}
		case "原始数据":
			if len(sh.Rows) > 1 && len(sh.Rows[1]) > 3 {
				periodType = sh.Rows[1][3].Type
			}
		}
	}
	if !strings.Contains(formula, "原始数据") || strings.Contains(formula, "A:A") && !strings.Contains(formula, "原始数据") {
		t.Fatalf("calc must count 原始数据, got %q", formula)
	}
	if periodType != "date" {
		t.Fatalf("YYYY-MM-DD period should be date, got %q", periodType)
	}
	data, err := Generate(spec)
	if err != nil {
		t.Fatal(err)
	}
	i, err := Inspect(XLSX, data)
	if err != nil {
		t.Fatal(err)
	}
	findText(t, i, "1280")
	findText(t, i, "2026-08-01")
}

func TestGenerateWorkbookTemplateKeepsBrandAndFillsGuidePurpose(t *testing.T) {
	t.Setenv("LUNITIDE_OFFICE_DESIGN", "on")
	brand, err := ImportBrandL1(BrandImport{
		BrandID: "xlsx-keep-brand",
		Colors:  map[string]string{"navy": "112233", "risk": "CC3300"},
		Fonts:   BrandFonts{Latin: "Georgia", East: "SimSun"},
	}, AssetRecord{
		SourceURL: "https://example.invalid/keep", License: "client-granted",
		Digest: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa3", Commercial: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	data, err := Generate(Spec{
		SchemaVersion: 2, Kind: XLSX, Title: "经营簿",
		TemplateID: "ops-ledger", BrandID: brand.BrandID,
		Facts: []Fact{{FactID: "orders", Value: "1280", Unit: "单", Period: "2026-08", Locator: "订单数"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	styles := string(zipParts(t, data)["xl/styles.xml"])
	if !strings.Contains(styles, `name val="Georgia"`) {
		t.Fatalf("empty-sheet PlanWorkbook dropped brand: %s", styles[:min(500, len(styles))])
	}
	i, err := Inspect(XLSX, data)
	if err != nil {
		t.Fatal(err)
	}
	foundGuide := false
	for _, n := range i.Nodes {
		if strings.Contains(n.Text, "工作簿说明") || strings.Contains(n.Text, "用途") {
			foundGuide = true
		}
		if n.Text == "1281" || strings.Contains(n.Text, "节约") {
			t.Fatalf("invented fact: %#v", n)
		}
	}
	if !foundGuide {
		t.Fatal("说明页 missing narrative purpose")
	}
	findText(t, i, "1280")
}

func TestGenerateOverflowWritesEvidenceNotesWithoutDroppingValues(t *testing.T) {
	metrics := make([]MetricBlock, 8)
	for i := range metrics {
		metrics[i] = MetricBlock{Label: "项", Value: "1280", Unit: "单", FactID: "orders"}
	}
	data, err := Generate(Spec{
		SchemaVersion: 2, Kind: PPTX, Title: "指标",
		Facts:  []Fact{{FactID: "orders", Value: "1280", Unit: "单", Locked: true}},
		Slides: []Slide{{Title: "指标", Layout: "metrics", Metrics: metrics, EvidenceRefs: []string{"orders"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	parts := zipParts(t, data)
	var notes []byte
	for name, body := range parts {
		if strings.Contains(name, "notesSlide") {
			notes = append(notes, body...)
		}
	}
	if !strings.Contains(string(notes), "orders") {
		t.Fatalf("overflow notes lost citation: %s", string(notes)[:min(400, len(notes))])
	}
	i, err := Inspect(PPTX, data)
	if err != nil {
		t.Fatal(err)
	}
	findText(t, i, "1280")
}

func TestResolveThemeExposesPurposeColorsAndDesignOffDropsImport(t *testing.T) {
	t.Setenv("LUNITIDE_OFFICE_DESIGN", "on")
	theme := ResolveTheme(DefaultBrand())
	if theme.Risk == "" || theme.Emphasis == "" || theme.Group == "" {
		t.Fatalf("purpose colors missing: %#v", theme)
	}
	custom := DefaultBrand()
	custom.BrandID = "risk-import"
	custom.Colors["risk"] = "CC3300"
	if err := RegisterBrand(custom); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LUNITIDE_OFFICE_DESIGN", "off")
	off := ResolveTheme(brandForSpec(Spec{BrandID: "risk-import"}))
	if off.Risk == "CC3300" {
		t.Fatal("design off leaked imported risk")
	}
}
