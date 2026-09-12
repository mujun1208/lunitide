package officestudio

import (
	"strconv"
	"strings"
	"testing"
)

func TestWorkbookPrintAreaAndNumberDisplayKeepStoredValue(t *testing.T) {
	spec, err := PlanWorkbook("ops-ledger", "经营簿", []Fact{
		{FactID: "orders", Value: "1280", Unit: "单", Period: "2026-08", Locator: "订单数"},
		{FactID: "delta", Value: "-12.50", Unit: "单", Period: "2026-08", Locator: "差额"},
	})
	if err != nil {
		t.Fatal(err)
	}
	var typed bool
	for _, sh := range spec.Sheets {
		if sh.Name != "原始数据" {
			continue
		}
		for _, row := range sh.Rows {
			for _, cell := range row {
				if cell.Value == "-12.50" && cell.Type == "number" {
					typed = true
				}
			}
		}
	}
	if !typed {
		t.Fatal("decimal fact should be a number cell so display format can apply")
	}
	data, err := Generate(spec)
	if err != nil {
		t.Fatal(err)
	}
	parts := zipParts(t, data)
	wb := string(parts["xl/workbook.xml"])
	if !strings.Contains(wb, "Print_Area") {
		t.Fatalf("print area missing: %s", wb[:min(600, len(wb))])
	}
	styles := string(parts["xl/styles.xml"])
	if !strings.Contains(styles, "-") || !(strings.Contains(styles, "#,##0") || strings.Contains(styles, "0.00")) {
		t.Fatalf("negative/number display format missing: %s", styles[:min(800, len(styles))])
	}
	i, err := Inspect(XLSX, data)
	if err != nil {
		t.Fatal(err)
	}
	findText(t, i, "1280")
	findText(t, i, "-12.50")
	for _, n := range i.Nodes {
		if strings.Contains(n.Text, "节约") {
			t.Fatal("invented savings")
		}
	}
}

func TestWordBodyUsesBrandWordLadderNotPPTBodyPt(t *testing.T) {
	t.Setenv("LUNITIDE_OFFICE_DESIGN", "on")
	if DefaultBrand().Fonts.WordBodyPt < 10 || DefaultBrand().Fonts.WordBodyPt > 12 {
		t.Fatalf("default Word body out of 10.5–12pt band: %#v", DefaultBrand().Fonts)
	}
	theme := ResolveTheme(DefaultBrand())
	if theme.WordBodySz != DefaultBrand().Fonts.WordBodyPt*2 || theme.WordBodySz == theme.BodySz {
		t.Fatalf("word half-points vs PPT ladder: %#v", theme)
	}
	data, err := Generate(Spec{
		SchemaVersion: 2, Kind: DOCX, Title: "研究报告",
		Facts:  []Fact{{FactID: "orders", Value: "1280", Unit: "单", Locked: true}},
		Blocks: []Block{{Type: "paragraph", Text: "订单 1280单"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	doc := string(zipParts(t, data)["word/document.xml"])
	want := `w:sz w:val="` + strconv.Itoa(theme.WordBodySz) + `"`
	if !strings.Contains(doc, want) {
		t.Fatalf("word body sz missing %s in %s", want, doc[:min(500, len(doc))])
	}
	if strings.Contains(doc, `w:sz w:val="36"`) {
		t.Fatal("PPT 18pt leaked into Word body")
	}
	wide := DefaultBrand()
	wide.BrandID = "word-wide"
	wide.Fonts.WordBodyPt = 16
	if err := RegisterBrand(wide); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LUNITIDE_OFFICE_DESIGN", "off")
	off := ResolveTheme(brandForSpec(Spec{BrandID: "word-wide"}))
	if off.WordBodySz != DefaultBrand().Fonts.WordBodyPt*2 {
		t.Fatalf("design off leaked word ladder: %#v", off)
	}
}

func TestIndependentTypstHeadingsDoNotClaimPDFA(t *testing.T) {
	body := FormatIndependentReport("研究报告", "客户", "复盘", "订单 1280单", []string{"台账A1"})
	markup := typstMarkup("研究报告", body)
	for _, h := range []string{"= 封面", "= 目录", "= 正文", "= 引用"} {
		if !strings.Contains(markup, h) {
			t.Fatalf("missing heading %s in %s", h, markup)
		}
	}
	if strings.Contains(markup, "1281") {
		t.Fatal("typst markup rewrote fact")
	}
	check := IndependentPDFACheck()
	if check.Status == "passed" || strings.Contains(check.Message, "已符合 PDF/A") {
		t.Fatalf("pdfa claimed: %#v", check)
	}
	if !strings.Contains(IndependentPDFNotice(), "PDF/A") || strings.Contains(IndependentPDFNotice(), "已符合 PDF/A") {
		t.Fatalf("notice=%q", IndependentPDFNotice())
	}
}

func TestDefaultBrandDeclaresFontFallbackWithoutEmbedding(t *testing.T) {
	fb := DefaultBrand().Fonts.Fallback
	if !strings.Contains(fb, "Calibri") || !strings.Contains(fb, "Microsoft YaHei") {
		t.Fatalf("fallback chain: %q", fb)
	}
	policy := FontEmbedPolicy(DefaultBrand().Fonts)
	if !strings.Contains(policy, "fallback") || strings.Contains(policy, "已嵌入微软雅黑") {
		t.Fatalf("embed policy: %q", policy)
	}
}

func TestPlanLayoutSetsReadingOrderAndKeepsEvidenceInNotes(t *testing.T) {
	metrics := make([]MetricBlock, 8)
	for i := range metrics {
		metrics[i] = MetricBlock{Label: "项", Value: "1280", Unit: "单", FactID: "orders"}
	}
	plans, err := PlanLayout(NarrativeNode{
		Title:        "指标",
		Layout:       "metrics",
		Metrics:      metrics,
		EvidenceRefs: []string{"orders"},
	}, DefaultBrand(), EstimateMeasure{})
	if err != nil || len(plans) < 2 {
		t.Fatalf("split plans: %v %#v", err, plans)
	}
	for i, p := range plans {
		if p.ReadingOrder != i+1 {
			t.Fatalf("readingOrder[%d]=%d", i, p.ReadingOrder)
		}
		if !strings.Contains(p.FitEvidence, "readingOrder=") {
			t.Fatalf("fitEvidence=%q", p.FitEvidence)
		}
		if !strings.Contains(p.Notes, "orders") {
			t.Fatalf("overflow notes lost citation: %#v", p)
		}
	}
}
