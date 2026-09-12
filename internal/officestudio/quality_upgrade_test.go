package officestudio

import (
	"bytes"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
)

func TestDefaultBrandHasLadderSafeAreaAndEmbedPolicy(t *testing.T) {
	brand := DefaultBrand()
	if brand.Fonts.TitlePt < 28 || brand.Fonts.BodyPt < 18 || brand.Fonts.NotesPt < 10 {
		t.Fatalf("font ladder missing: %#v", brand.Fonts)
	}
	if brand.Logo.SafeInsetEMU <= 0 {
		t.Fatal("logo safe inset missing")
	}
	policy := FontEmbedPolicy(brand.Fonts)
	if policy == "" || strings.Contains(strings.ToLower(policy), "embed yahei") || strings.Contains(policy, "已嵌入微软雅黑") {
		t.Fatalf("embed policy: %q", policy)
	}
	theme := ResolveTheme(brand)
	if theme.TitleSz != brand.Fonts.TitlePt*100 || theme.BodySz != brand.Fonts.BodyPt*100 {
		t.Fatalf("theme ladder: %#v", theme)
	}
}

func TestDesignOffIgnoresImportedFontLadder(t *testing.T) {
	t.Setenv("LUNITIDE_OFFICE_DESIGN", "on")
	custom := DefaultBrand()
	custom.BrandID = "ladder-wide"
	custom.Fonts.TitlePt = 48
	if err := RegisterBrand(custom); err != nil {
		t.Fatal(err)
	}
	on := ResolveTheme(brandForSpec(Spec{BrandID: "ladder-wide"}))
	if on.TitleSz != 4800 {
		t.Fatalf("design on ladder=%d", on.TitleSz)
	}
	t.Setenv("LUNITIDE_OFFICE_DESIGN", "off")
	off := ResolveTheme(brandForSpec(Spec{BrandID: "ladder-wide"}))
	if off.TitleSz != DefaultBrand().Fonts.TitlePt*100 {
		t.Fatalf("design off leaked ladder: %#v", off)
	}
}

func TestLogoSafeAreaBindsNodeWithoutRewritingFacts(t *testing.T) {
	n := Node{ID: "node_logo", Part: "ppt/slides/slide1.xml", Text: "1280", Image: &ImageInfo{X: 0, Y: 0, Width: 400000, Height: 400000}}
	issues := LogoSafeAreaIssues([]Node{n}, DefaultBrand())
	if len(issues) == 0 || issues[0].NodeID != "node_logo" || issues[0].Code != "OFFICE_LOGO_SAFE_AREA" {
		t.Fatalf("safe area: %#v", issues)
	}
	if strings.Contains(issues[0].Message, "1281") {
		t.Fatal("safe area rewrote fact")
	}
	ok := Node{ID: "node_ok", Image: &ImageInfo{X: DefaultBrand().Logo.SafeInsetEMU, Y: DefaultBrand().Logo.SafeInsetEMU, Width: 400000, Height: 400000}}
	if len(LogoSafeAreaIssues([]Node{ok}, DefaultBrand())) != 0 {
		t.Fatal("inset image flagged")
	}
}

func TestEstimateMeasureCountsCJKHeavierAndLabelsNotGlyph(t *testing.T) {
	if (EstimateMeasure{}).Runes("ab") != 2 || (EstimateMeasure{}).Runes("订单") != 4 {
		t.Fatalf("estimate: ascii=%d cjk=%d", (EstimateMeasure{}).Runes("ab"), (EstimateMeasure{}).Runes("订单"))
	}
	plans, err := PlanLayout(NarrativeNode{Title: "订单对照", Layout: "content", Bullets: []string{"口径说明"}}, DefaultBrand(), EstimateMeasure{})
	if err != nil || len(plans) == 0 {
		t.Fatal(err)
	}
	if !strings.Contains(plans[0].FitEvidence, "cjk-ascii-estimate") || !strings.Contains(plans[0].FitEvidence, "not glyph") {
		t.Fatalf("fitEvidence=%q", plans[0].FitEvidence)
	}
	if strings.Contains(plans[0].FitEvidence, "glyph-verified") {
		t.Fatal("claimed glyph metrics")
	}
}

func TestGeometryOverlapBindsNodeID(t *testing.T) {
	data, err := Generate(Spec{
		SchemaVersion: 2, Kind: PPTX, Title: "重叠",
		Slides: []Slide{{
			Title:  "方案对比",
			Layout: "comparison",
			Comparison: &ComparisonBlock{
				Left:  []string{"方案A", "成本低"},
				Right: []string{"方案B", "能力高"},
			},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	part := zipParts(t, data)["ppt/slides/slide1.xml"]
	item1 := offAfterName(t, part, "Item1")
	item2 := offAfterName(t, part, "Item2")
	overlapped := append(append(append([]byte(nil), part[:item2.start]...), item1.body...), part[item2.end:]...)
	data = editZIP(t, data, map[string][]byte{"ppt/slides/slide1.xml": overlapped})
	i, err := Inspect(PPTX, data)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, issue := range i.Issues {
		if issue.Code == "OFFICE_GEOMETRY_OVERLAP" && issue.NodeID != "" {
			found = true
		}
	}
	if !found {
		t.Fatalf("overlap not bound: %#v", i.Issues)
	}
}

func TestInspectWordReportsFixedRowHeightClip(t *testing.T) {
	withTable, err := Generate(Spec{
		SchemaVersion: 2, Kind: DOCX, Title: "行高检查",
		Blocks: []Block{{Type: "paragraph", Text: "订单 1280单"}, {Type: "table", Rows: [][]string{{"项目", "值"}, {"订单", "1280"}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	body := string(zipParts(t, withTable)["word/document.xml"])
	clipped := strings.Replace(body, `<w:trPr><w:tblHeader/></w:trPr>`, `<w:trPr><w:tblHeader/><w:trHeight w:val="200"/></w:trPr>`, 1)
	data := editZIP(t, withTable, map[string][]byte{"word/document.xml": []byte(clipped)})
	i, err := Inspect(DOCX, data)
	if err != nil {
		t.Fatal(err)
	}
	if !hasWordWarning(i.Issues, "OFFICE_FIXED_ROW_CLIP") {
		t.Fatalf("missing fixed row clip: %#v", i.Issues)
	}
	if !strings.Contains(i.Preview, "1280") {
		t.Fatal("row-height check rewrote fact")
	}
}

func TestInspectExcelWarnsMergeOnDetailSheet(t *testing.T) {
	spec, err := PlanWorkbook("ops-ledger", "经营簿", []Fact{{FactID: "orders", Value: "1280", Unit: "单", Period: "2026-08", Locator: "订单数"}})
	if err != nil {
		t.Fatal(err)
	}
	data, err := Generate(spec)
	if err != nil {
		t.Fatal(err)
	}
	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if err = f.MergeCell("原始数据", "A1", "B1"); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err = f.Write(&buf); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	i, err := Inspect(XLSX, buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if !hasWordWarning(i.Issues, "OFFICE_DETAIL_MERGE") {
		t.Fatalf("missing detail merge warning: %#v", i.Issues)
	}
	if !strings.Contains(i.Preview, "1280") {
		t.Fatal("merge warning rewrote fact")
	}
}

func TestIndependentPDFIncludesCoverTOCAndCitations(t *testing.T) {
	body := FormatIndependentReport("经营汇报", "管理层", "对照", "订单 1280单", []string{"台账 A2"})
	if !strings.Contains(body, "封面") || !strings.Contains(body, "目录") || !strings.Contains(body, "引用") || !strings.Contains(body, "台账 A2") {
		t.Fatalf("structure: %q", body)
	}
	if !strings.Contains(body, "订单 1280单") {
		t.Fatal("body dropped")
	}
	t.Setenv("LUNITIDE_TYPST", "")
	data, err := Generate(Spec{
		SchemaVersion: 2, Kind: PDF, Title: "经营汇报",
		Body:  "订单 1280单",
		Facts: []Fact{{FactID: "orders", Value: "1280", Unit: "单", SourceID: "台账 A2"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(data, []byte("%PDF")) || len(data) < 64 {
		t.Fatal("independent pdf is empty or not a pdf")
	}
	if _, err = Inspect(PDF, data); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(IndependentPDFNotice(), "像素完全一致") {
		t.Fatal("claimed pixel match")
	}
}

func TestFindFactRefsReturnsNodeIDsAcrossKinds(t *testing.T) {
	fact := Fact{FactID: "orders", Value: "1280", Unit: "单", Locked: true}
	pptx, err := Generate(Spec{
		SchemaVersion: 2, Kind: PPTX, Title: "指标",
		Facts:  []Fact{fact},
		Slides: []Slide{{Title: "指标", Layout: "metrics", Metrics: []MetricBlock{{Label: "订单", Value: "1280", Unit: "单", FactID: "orders"}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	docx, err := Generate(Spec{
		SchemaVersion: 2, Kind: DOCX, Title: "报告",
		Facts:  []Fact{fact},
		Blocks: []Block{{Type: "paragraph", Text: "订单 1280单"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	pi, err := Inspect(PPTX, pptx)
	if err != nil {
		t.Fatal(err)
	}
	di, err := Inspect(DOCX, docx)
	if err != nil {
		t.Fatal(err)
	}
	refs := FindFactRefs([]Fact{fact}, []Inspection{pi, di})
	if len(refs) < 2 {
		t.Fatalf("refs=%#v", refs)
	}
	seen := map[Kind]bool{}
	for _, ref := range refs {
		if ref.FactID != "orders" || ref.NodeID == "" {
			t.Fatalf("ref=%#v", ref)
		}
		seen[ref.Kind] = true
	}
	if !seen[PPTX] || !seen[DOCX] {
		t.Fatalf("kinds=%#v", seen)
	}
}

type offSpan struct {
	start, end int
	body       []byte
}

func offAfterName(t *testing.T, part []byte, name string) offSpan {
	t.Helper()
	idx := bytes.Index(part, []byte(`name="`+name+`"`))
	if idx < 0 {
		t.Fatalf("missing %s", name)
	}
	off := bytes.Index(part[idx:], []byte(`<a:off `))
	if off < 0 {
		t.Fatalf("missing offset after %s", name)
	}
	start := idx + off
	endRel := bytes.Index(part[start:], []byte(`/>`))
	if endRel < 0 {
		t.Fatal("offset end")
	}
	end := start + endRel + 2
	return offSpan{start: start, end: end, body: append([]byte(nil), part[start:end]...)}
}
