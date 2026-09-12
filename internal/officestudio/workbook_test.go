package officestudio

import (
	"errors"
	"strings"
	"testing"
)

func TestWorkbookKindsKeepEqualsAsTextAndSplitInputFromDerived(t *testing.T) {
	facts := []Fact{{FactID: "orders", Value: "1280", Unit: "单", Period: "2026-08", Locator: "订单数"}}
	for _, kind := range WorkbookKinds() {
		spec, err := PlanWorkbook(kind, "经营簿", facts)
		if err != nil {
			t.Fatal(kind, err)
		}
		roles := map[string]bool{}
		for _, sh := range spec.Sheets {
			roles[sh.Name] = true
		}
		if !roles["说明"] || !roles["原始数据"] || !roles["计算"] || !roles["看板"] {
			t.Fatalf("%s missing input/derived sheets: %#v", kind, roles)
		}
		spec.Sheets[1].Rows = append(spec.Sheets[1].Rows, []Cell{{Type: "text", Value: "=SUM(1,2)"}})
		data, err := Generate(spec)
		if err != nil {
			t.Fatal(kind, err)
		}
		i, err := Inspect(XLSX, data)
		if err != nil {
			t.Fatal(kind, err)
		}
		foundText := false
		for _, n := range i.Nodes {
			if n.Text == "=SUM(1,2)" {
				if n.Kind != "cell:text" {
					t.Fatalf("%s executed imported equals: %#v", kind, n)
				}
				foundText = true
			}
			if strings.Contains(n.Text, "节约了") {
				t.Fatalf("%s invented savings", kind)
			}
		}
		if !foundText {
			t.Fatalf("%s lost text equals import", kind)
		}
		findText(t, i, "1280")
	}
}

func TestGenerateExcelAppliesRegisteredBrandAndDesignOffFallsBack(t *testing.T) {
	t.Setenv("LUNITIDE_OFFICE_DESIGN", "on")
	brand, err := ImportBrandL1(BrandImport{
		BrandID: "xlsx-serif",
		Colors:  map[string]string{"navy": "112233", "soft": "EDE4F5"},
		Fonts:   BrandFonts{Latin: "Georgia", East: "SimSun"},
	}, AssetRecord{
		SourceURL: "https://example.invalid/xlsx", License: "client-granted",
		Digest: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa1", Commercial: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	spec, err := PlanWorkbook("ops-ledger", "经营簿", []Fact{{FactID: "orders", Value: "1280", Unit: "单", Period: "2026-08", Locator: "订单数"}})
	if err != nil {
		t.Fatal(err)
	}
	spec.BrandID = brand.BrandID
	data, err := Generate(spec)
	if err != nil {
		t.Fatal(err)
	}
	styles := string(zipParts(t, data)["xl/styles.xml"])
	if !strings.Contains(styles, `name val="Georgia"`) || !strings.Contains(styles, `name val="SimSun"`) {
		t.Fatalf("excel brand fonts missing latin/east: %s", styles[:min(800, len(styles))])
	}
	if !strings.Contains(styles, "112233") && !strings.Contains(styles, "FF112233") {
		t.Fatalf("excel brand navy missing: %s", styles[:min(500, len(styles))])
	}
	i, err := Inspect(XLSX, data)
	if err != nil {
		t.Fatal(err)
	}
	findText(t, i, "1280")
	t.Setenv("LUNITIDE_OFFICE_DESIGN", "off")
	off, err := Generate(spec)
	if err != nil {
		t.Fatal(err)
	}
	classic := string(zipParts(t, off)["xl/styles.xml"])
	if strings.Contains(classic, `name val="Georgia"`) || strings.Contains(classic, `name val="SimSun"`) || strings.Contains(classic, "112233") {
		t.Fatal("design off leaked excel brand")
	}
}

func TestGenerateExcelThemeAppliesBrandLatinAndEast(t *testing.T) {
	t.Setenv("LUNITIDE_OFFICE_DESIGN", "on")
	brand, err := ImportBrandL1(BrandImport{
		BrandID: "xlsx-theme",
		Colors:  map[string]string{"navy": "112233", "soft": "EDE4F5"},
		Fonts:   BrandFonts{Latin: "Georgia", East: "SimSun"},
	}, AssetRecord{
		SourceURL: "https://example.invalid/xlsx-theme", License: "client-granted",
		Digest: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa2", Commercial: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	spec, err := PlanWorkbook("ops-ledger", "经营簿", []Fact{{FactID: "orders", Value: "1280", Unit: "单", Period: "2026-08", Locator: "订单数"}})
	if err != nil {
		t.Fatal(err)
	}
	spec.BrandID = brand.BrandID
	data, err := Generate(spec)
	if err != nil {
		t.Fatal(err)
	}
	theme := string(zipParts(t, data)["xl/theme/theme1.xml"])
	if !strings.Contains(theme, `typeface="Georgia"`) || !strings.Contains(theme, `typeface="SimSun"`) {
		t.Fatalf("excel theme fonts missing latin/east: %s", theme[:min(400, len(theme))])
	}
	if !strings.Contains(theme, "112233") {
		t.Fatalf("excel theme navy missing: %s", theme[:min(400, len(theme))])
	}
	types := string(zipParts(t, data)["[Content_Types].xml"])
	if !strings.Contains(types, "/xl/theme/theme1.xml") {
		t.Fatal("excel theme content type missing")
	}
	rels := string(zipParts(t, data)["xl/_rels/workbook.xml.rels"])
	if !strings.Contains(rels, "theme/theme1.xml") {
		t.Fatal("excel theme relationship missing")
	}
	i, err := Inspect(XLSX, data)
	if err != nil {
		t.Fatal(err)
	}
	findText(t, i, "1280")
	t.Setenv("LUNITIDE_OFFICE_DESIGN", "off")
	off, err := Generate(spec)
	if err != nil {
		t.Fatal(err)
	}
	classic := string(zipParts(t, off)["xl/theme/theme1.xml"])
	if strings.Contains(classic, `typeface="Georgia"`) || strings.Contains(classic, `typeface="SimSun"`) || strings.Contains(classic, "112233") {
		t.Fatal("design off leaked excel theme brand")
	}
}

func TestGenerateExcelRejectsDroppedLockedFact(t *testing.T) {
	_, err := Generate(Spec{
		SchemaVersion: 2, Kind: XLSX, Title: "缺事实",
		Facts:  []Fact{{FactID: "orders", Value: "1280", Unit: "单", Locked: true}},
		Sheets: []Sheet{{Name: "数据", Rows: [][]Cell{{{Type: "text", Value: "待填"}}}}},
	})
	if !errors.Is(err, ErrFactConflict) {
		t.Fatalf("dropped locked excel fact: %v", err)
	}
}

func TestPrepareRejectsConflictingSpecFactsAndSlideMetrics(t *testing.T) {
	_, err := PrepareManagedSpec(Spec{
		SchemaVersion: 2, Kind: PPTX, Title: "冲突",
		Facts: []Fact{{FactID: "orders", Value: "1280", Unit: "单"}},
		Slides: []Slide{{
			Title: "指标", Layout: "metrics",
			Metrics: []MetricBlock{{Label: "订单", Value: "999", Unit: "单", FactID: "orders"}},
		}},
	})
	if !errors.Is(err, ErrFactConflict) {
		t.Fatalf("spec facts vs slide metrics: %v", err)
	}
}

func TestFactSetCoverageReportsConflictWhenLockedValueMissing(t *testing.T) {
	facts := []Fact{{FactID: "orders", Value: "1280", Unit: "单", Locked: true}}
	ok := Inspection{Nodes: []Node{{Text: "1280", Kind: "text"}}}
	if err := AssertFactSetCoverage(facts, []Inspection{ok, ok}); err != nil {
		t.Fatal(err)
	}
	missing := Inspection{Nodes: []Node{{Text: "订单待填", Kind: "text"}}}
	if err := AssertFactSetCoverage(facts, []Inspection{ok, missing}); !errors.Is(err, ErrFactConflict) {
		t.Fatalf("missing locked fact: %v", err)
	}
	formulaOnly := Inspection{Nodes: []Node{{Text: "=1280", Kind: "cell:formula"}}}
	if err := AssertFactSetCoverage(facts, []Inspection{formulaOnly}); !errors.Is(err, ErrFactConflict) {
		t.Fatalf("formula must not satisfy locked fact: %v", err)
	}
	if _, ok := LocateFactNode(formulaOnly, facts[0]); ok {
		t.Fatal("formula cell used as fact source")
	}
}
