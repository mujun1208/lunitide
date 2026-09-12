package officestudio

import (
	"errors"
	"strings"
	"testing"
)

func TestWordReportTemplatesKeepFactsAndDistinctHeaders(t *testing.T) {
	facts := []Fact{{FactID: "sample_n", Value: "42", Unit: "份", Period: "研究窗口", Locator: "样本数"}}
	seen := map[string]string{}
	for _, id := range WordTemplateIDs() {
		spec, err := WordReportSpec(id, "正式文档", facts)
		if err != nil {
			t.Fatal(id, err)
		}
		data, err := Generate(spec)
		if err != nil {
			t.Fatal(id, err)
		}
		i, err := Inspect(DOCX, data)
		if err != nil {
			t.Fatal(id, err)
		}
		findText(t, i, "样本数 42份")
		joined := i.Preview
		for _, n := range i.Nodes {
			joined += n.Text
		}
		if strings.Contains(joined, "周报完成率") || strings.Contains(joined, "节约了") {
			t.Fatalf("%s invented weekly metrics", id)
		}
		if spec.Document == nil || spec.Document.Header == "" {
			t.Fatalf("%s missing header chrome", id)
		}
		seen[id] = spec.Document.Header
	}
	if len(seen) != 3 {
		t.Fatalf("want 3 word templates, got %d", len(seen))
	}
	headers := []string{}
	for _, id := range WordTemplateIDs() {
		headers = append(headers, seen[id])
	}
	if headers[0] == headers[1] || headers[1] == headers[2] || headers[0] == headers[2] {
		t.Fatalf("templates share header: %v", headers)
	}
}

func TestGenerateWordAppliesRegisteredBrandFonts(t *testing.T) {
	t.Setenv("LUNITIDE_OFFICE_DESIGN", "on")
	brand, err := ImportBrandL1(BrandImport{
		BrandID: "word-serif",
		Fonts:   BrandFonts{Latin: "Georgia", East: "SimSun"},
	}, AssetRecord{
		SourceURL: "https://example.invalid/word-fonts", License: "client-granted",
		Digest: "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd", Commercial: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	data, err := Generate(Spec{
		SchemaVersion: 2, Kind: DOCX, Title: "品牌字体报告", BrandID: brand.BrandID,
		TemplateID: "research-report",
		Blocks:     []Block{{Type: "paragraph", Text: "样本数 42份"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	doc := zipParts(t, data)["word/document.xml"]
	if !strings.Contains(string(doc), `w:ascii="Georgia"`) || !strings.Contains(string(doc), `w:eastAsia="SimSun"`) {
		t.Fatalf("word brand fonts missing: %s", doc[:min(500, len(doc))])
	}
	if !strings.Contains(string(doc), "样本数 42份") {
		t.Fatal("font apply rewrote fact")
	}
}

func TestGenerateWordDesignOffFallsBackFontsAndKeepsFact(t *testing.T) {
	t.Setenv("LUNITIDE_OFFICE_DESIGN", "on")
	brand, err := ImportBrandL1(BrandImport{
		BrandID: "word-off-serif",
		Colors:  map[string]string{"navy": "112233"},
		Fonts:   BrandFonts{Latin: "Georgia", East: "SimSun"},
	}, AssetRecord{
		SourceURL: "https://example.invalid/word-off", License: "client-granted",
		Digest: "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff", Commercial: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	spec := Spec{
		SchemaVersion: 2, Kind: DOCX, Title: "品牌字体报告", BrandID: brand.BrandID,
		TemplateID: "research-report",
		Blocks:     []Block{{Type: "heading", Text: "范围"}, {Type: "paragraph", Text: "样本数 42份"}},
	}
	on, err := Generate(spec)
	if err != nil {
		t.Fatal(err)
	}
	doc := string(zipParts(t, on)["word/document.xml"])
	theme := string(zipParts(t, on)["word/theme/theme1.xml"])
	if !strings.Contains(doc, `w:ascii="Georgia"`) || !strings.Contains(theme, `typeface="Georgia"`) {
		t.Fatal("word theme/document missing brand latin")
	}
	if !strings.Contains(doc, `w:val="112233"`) {
		t.Fatal("word heading must use brand navy")
	}
	t.Setenv("LUNITIDE_OFFICE_DESIGN", "off")
	off, err := Generate(spec)
	if err != nil {
		t.Fatal(err)
	}
	classicDoc := string(zipParts(t, off)["word/document.xml"])
	if strings.Contains(classicDoc, `w:ascii="Georgia"`) || strings.Contains(classicDoc, `w:val="112233"`) {
		t.Fatal("design off leaked word brand")
	}
	if !strings.Contains(classicDoc, "样本数 42份") {
		t.Fatal("design off rewrote fact")
	}
}

func TestGenerateWordRejectsDroppedLockedFact(t *testing.T) {
	_, err := Generate(Spec{
		SchemaVersion: 2, Kind: DOCX, Title: "缺事实",
		Facts:  []Fact{{FactID: "sample_n", Value: "42", Unit: "份", Locked: true, Locator: "样本数"}},
		Blocks: []Block{{Type: "paragraph", Text: "正文未写入已核对样本数"}},
	})
	if !errors.Is(err, ErrFactConflict) {
		t.Fatalf("dropped locked word fact: %v", err)
	}
}

func TestTOCPreviewDoesNotClearDocxCacheBlocker(t *testing.T) {
	report := EvaluateQuality([]Check{
		{ID: "preview_fields", Status: "passed", Message: "预览目录已刷新"},
		{ID: "fields_update", Status: "missing", Message: "原文件目录缓存未更新"},
	}, 90, nil)
	if report.FormalOK {
		t.Fatal("TOC preview must not stand in for DOCX field cache")
	}
}

func TestSameSourcePDFInvalidatesAfterSourceEdit(t *testing.T) {
	data, err := Generate(Spec{SchemaVersion: 2, Kind: DOCX, Title: "同源", TemplateID: "research-report", Blocks: []Block{{Type: "heading", Text: "范围"}, {Type: "paragraph", Text: "样本数 42份"}}})
	if err != nil {
		t.Fatal(err)
	}
	i, err := Inspect(DOCX, data)
	if err != nil {
		t.Fatal(err)
	}
	bind := BindSameSourcePDF(DOCX, i.SHA256, []byte("%PDF-1.4\nfixture\n%%EOF\n"))
	if bind.Stale || bind.SourceSHA256 != i.SHA256 || bind.PDFSHA256 == "" {
		t.Fatalf("bind: %#v", bind)
	}
	next := InvalidateSameSourcePDF(bind, "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff")
	if !next.Stale {
		t.Fatal("edited source must stale the bound PDF")
	}
}

func TestInspectWordReportsWidowCaptionAndTableHeaderWarnings(t *testing.T) {
	widow, err := Generate(Spec{
		SchemaVersion: 2, Kind: DOCX, Title: "标题检查",
		Facts:  []Fact{{FactID: "orders", Value: "1280", Unit: "单", Locked: true}},
		Blocks: []Block{{Type: "heading", Text: "孤行标题"}, {Type: "heading", Text: "另一标题"}, {Type: "paragraph", Text: "订单 1280单"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	i, err := Inspect(DOCX, widow)
	if err != nil {
		t.Fatal(err)
	}
	if !hasWordWarning(i.Issues, "OFFICE_WIDOW_HEADING") {
		t.Fatalf("missing widow heading: %#v", i.Issues)
	}
	if !strings.Contains(i.Preview, "1280") {
		t.Fatal("widow check rewrote locked fact")
	}
	v, err := Validate(DOCX, widow)
	if err != nil || v.Status == "blocked" {
		t.Fatalf("widow must stay a warning: %#v %v", v, err)
	}
	emptyCaption, err := Generate(Spec{
		SchemaVersion: 2, Kind: DOCX, Title: "题注检查",
		Blocks: []Block{{Type: "heading", Text: "图"}, {Type: "paragraph", Text: "说明"}, {Type: "caption", Text: ""}},
	})
	if err != nil {
		t.Fatal(err)
	}
	cap, err := Inspect(DOCX, emptyCaption)
	if err != nil {
		t.Fatal(err)
	}
	if !hasWordWarning(cap.Issues, "OFFICE_EMPTY_CAPTION") {
		t.Fatalf("missing empty caption: %#v", cap.Issues)
	}
	withTable, err := Generate(Spec{
		SchemaVersion: 2, Kind: DOCX, Title: "表头检查",
		Blocks: []Block{{Type: "paragraph", Text: "订单 1280单"}, {Type: "table", Rows: [][]string{{"项目", "值"}, {"订单", "1280"}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	okTable, err := Inspect(DOCX, withTable)
	if err != nil {
		t.Fatal(err)
	}
	if hasWordWarning(okTable.Issues, "OFFICE_TABLE_HEADER") {
		t.Fatalf("repeating header flagged: %#v", okTable.Issues)
	}
	stripped := editZIP(t, withTable, map[string][]byte{
		"word/document.xml": []byte(strings.Replace(string(zipParts(t, withTable)["word/document.xml"]), `<w:trPr><w:tblHeader/></w:trPr>`, "", 1)),
	})
	noHeader, err := Inspect(DOCX, stripped)
	if err != nil {
		t.Fatal(err)
	}
	if !hasWordWarning(noHeader.Issues, "OFFICE_TABLE_HEADER") {
		t.Fatalf("missing table header warning: %#v", noHeader.Issues)
	}
	if strings.Contains(string(zipParts(t, stripped)["word/document.xml"]), `w:sz w:val="10"`) &&
		!strings.Contains(string(zipParts(t, withTable)["word/document.xml"]), `w:sz w:val="10"`) {
		t.Fatal("layout warning must not shrink fonts")
	}
	healthy, err := Generate(Spec{
		SchemaVersion: 2, Kind: DOCX, Title: "正常报告", TemplateID: "research-report",
		Blocks: []Block{{Type: "heading", Text: "范围"}, {Type: "heading2", Text: "口径"}, {Type: "paragraph", Text: "订单 1280单"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	h, err := Inspect(DOCX, healthy)
	if err != nil {
		t.Fatal(err)
	}
	if hasWordWarning(h.Issues, "OFFICE_WIDOW_HEADING") {
		t.Fatalf("heading plus subsection plus body is not a widow: %#v", h.Issues)
	}
}

func hasWordWarning(issues []Issue, code string) bool {
	for _, issue := range issues {
		if issue.Code == code && issue.Severity == "warning" {
			return true
		}
	}
	return false
}
