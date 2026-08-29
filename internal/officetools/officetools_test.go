package officetools

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestGenAndParseXLSXRoundTrip(t *testing.T) {
	data, err := GenXLSX([]SheetSpec{{
		Name:    "报告",
		Headers: []string{"月份", "销量"},
		Rows: [][]interface{}{
			{"1月", 120}, {"2月", 180}, {"3月", 90},
		},
		Chart: &ChartSpec{Type: "col", Title: "月度销量"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 || !bytes.HasPrefix(data, []byte("PK")) {
		t.Fatalf("xlsx not a zip container: %d bytes", len(data))
	}
	// Round-trip through the parser.
	summary, err := ParseXLSX(data)
	if err != nil {
		t.Fatal(err)
	}
	var parsed XLSXSummary
	if err := json.Unmarshal([]byte(summary), &parsed); err != nil {
		t.Fatal(err)
	}
	if len(parsed.Sheets) != 1 || parsed.Sheets[0].Name != "报告" {
		t.Fatalf("sheets = %+v", parsed.Sheets)
	}
	s := parsed.Sheets[0]
	if s.Rows != 4 || len(s.Header) != 2 || s.Header[0] != "月份" {
		t.Fatalf("summary = %+v", s)
	}
	if len(s.Preview) < 4 || s.Preview[1][0] != "1月" || s.Preview[1][1] != "120" {
		t.Fatalf("preview = %+v", s.Preview)
	}
	if s.Truncated {
		t.Fatal("small sheet must not be truncated")
	}
	// The chart part must exist inside the workbook.
	if !zipHasPart(data, "xl/charts/chart1.xml") {
		t.Fatal("chart part missing from workbook")
	}
}

func TestGenXLSXGuards(t *testing.T) {
	if _, err := GenXLSX(nil); err == nil {
		t.Fatal("empty sheets accepted")
	}
	tooMany := make([]SheetSpec, MaxSheets+1)
	if _, err := GenXLSX(tooMany); err == nil {
		t.Fatal("sheet count over limit accepted")
	}
	big := make([][]interface{}, MaxRowsPerSheet+1)
	for i := range big {
		big[i] = []interface{}{i}
	}
	if _, err := GenXLSX([]SheetSpec{{Name: "s", Rows: big}}); err == nil {
		t.Fatal("row count over limit accepted")
	}
	if _, err := GenXLSX([]SheetSpec{{Name: "s", Chart: &ChartSpec{Type: "scatter"}}}); err == nil {
		t.Fatal("unsupported chart type accepted")
	}
}

func TestGenDocxStructure(t *testing.T) {
	blocks := SampleStyledDocxBlocks()
	blocks[0].Text = "第一章 & 概述"
	blocks = append(blocks, DocxBlock{Type: "bullet", Text: "要点 '一'"})
	data, err := GenDocx("测试报告 <Q1>", blocks)
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range []string{
		"word/document.xml", "word/styles.xml", "word/theme/theme1.xml",
		"word/fontTable.xml", "word/numbering.xml", "[Content_Types].xml",
	} {
		if !zipHasPart(data, part) {
			t.Fatalf("docx missing part %s", part)
		}
	}
	doc := zipPartBody(t, data, "word/document.xml")
	if !strings.Contains(doc, "&lt;Q1&gt;") || !strings.Contains(doc, "&amp; 概述") || !strings.Contains(doc, "&apos;一&apos;") {
		t.Fatalf("docx escaping broken: %.200s", doc)
	}
	if !strings.Contains(doc, `w:val="Heading1"`) || !strings.Contains(doc, `w:val="Heading2"`) || !strings.Contains(doc, `w:val="Title"`) {
		t.Fatal("docx heading styles missing")
	}
	if !strings.Contains(doc, "SimSun") || !strings.Contains(doc, "SimHei") {
		t.Fatal("docx must set Chinese-friendly fonts on runs")
	}
	styles := zipPartBody(t, data, "word/styles.xml")
	if !strings.Contains(styles, `w:styleId="Heading1"`) || !strings.Contains(styles, `w:line="360"`) || !strings.Contains(styles, "SimSun") {
		t.Fatal("styles.xml must define headings, 1.5 line spacing, and 宋体")
	}
	if err := ValidateDocx(data); err != nil {
		t.Fatalf("valid doc failed validation: %v", err)
	}
	if _, err := GenDocx("t", nil); err == nil {
		t.Fatal("empty blocks accepted")
	}
	if _, err := GenDocx("t", []DocxBlock{{Type: "table", Text: "x"}}); err == nil {
		t.Fatal("unknown block type accepted")
	}
	if _, err := GenDocx("t", []DocxBlock{{Type: "paragraph", Text: "hi"}}); err == nil {
		t.Fatal("unstyled trivial body accepted")
	}
}

func TestValidateDocxRejectsEmptyAndUnstyled(t *testing.T) {
	if err := ValidateDocx(UnstyledDocxFixture()); err == nil {
		t.Fatal("unstyled single-style body must fail validation")
	}
	if err := ValidateDocx(EmptyDocxFixture()); err == nil {
		t.Fatal("empty docx must fail validation")
	}
	data, err := GenDocxDoc(DocxDoc{Title: "季度报告", Kind: "report", Subtitle: "内部稿", Author: "月汐", Blocks: SampleReportDocxBlocks()})
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateDocx(data); err != nil {
		t.Fatalf("report fixture must pass: %v", err)
	}
	doc := zipPartBody(t, data, "word/document.xml")
	if !strings.Contains(doc, `w:type="page"`) || !strings.Contains(doc, `w:val="Title"`) {
		t.Fatal("report must have a cover/title page break")
	}
	novel, err := GenDocxDoc(SampleNovelDocxDoc())
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateDocx(novel); err != nil {
		t.Fatalf("novel fixture must pass: %v", err)
	}
	nxml := zipPartBody(t, novel, "word/document.xml")
	if !strings.Contains(nxml, "作者") || strings.Count(nxml, `w:val="Heading1"`) < 2 {
		t.Fatal("novel must include author and chapter Heading 1")
	}
	if _, err := GenDocxDoc(DocxDoc{Title: "残稿", Kind: "novel", Author: "阿潮", Blocks: []DocxBlock{
		{Type: "heading", Text: "第一章"},
		{Type: "bullet", Text: "起"},
		{Type: "bullet", Text: "承"},
		{Type: "heading", Text: "第二章"},
		{Type: "bullet", Text: "转"},
	}}); err == nil {
		t.Fatal("outline dump must not pass as a novel")
	}
}

func TestGenPptxStructure(t *testing.T) {
	data, err := GenPptx("季度汇报", []SlideSpec{
		{Title: "封面 <标题>", Bullets: []string{"要点A", "要点B"}},
		{Title: "第二章", Bullets: nil},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range []string{
		"ppt/presentation.xml", "ppt/slides/slide1.xml", "ppt/slides/slide2.xml",
		"ppt/slideMasters/slideMaster1.xml", "ppt/slideLayouts/slideLayout1.xml",
		"ppt/theme/theme1.xml", "docProps/core.xml",
	} {
		if !zipHasPart(data, part) {
			t.Fatalf("pptx missing part %s", part)
		}
	}
	slide1 := zipPartBody(t, data, "ppt/slides/slide1.xml")
	if !strings.Contains(slide1, "&lt;标题&gt;") || !strings.Contains(slide1, "要点A") {
		t.Fatalf("slide1 content broken: %.200s", slide1)
	}
	if !strings.Contains(slide1, "Microsoft YaHei") || !strings.Contains(slide1, `val="0B1F3A"`) {
		t.Fatalf("cover slide must use business theme: %.300s", slide1)
	}
	slide2 := zipPartBody(t, data, "ppt/slides/slide2.xml")
	if !strings.Contains(slide2, "第二章") || !strings.Contains(slide2, `val="0B1F3A"`) {
		t.Fatalf("section slide missing branding: %.200s", slide2)
	}
	pres := zipPartBody(t, data, "ppt/presentation.xml")
	if strings.Count(pres, "<p:sldId ") != 2 {
		t.Fatalf("presentation must list 2 slides: %.200s", pres)
	}
	if _, err := GenPptx("t", nil); err == nil {
		t.Fatal("empty slides accepted")
	}
	if _, err := GenPptx("t", []SlideSpec{{Title: "   "}}); err == nil {
		t.Fatal("blank title accepted")
	}
	if !strings.Contains(slide1, "</a:pPr>") || strings.Contains(slide1, `<a:pPr algn="l"><a:r>`) {
		t.Fatal("title run must sit after a closed a:pPr, not inside it")
	}
	if strings.Contains(zipPartBody(t, data, "ppt/slides/_rels/slide1.xml.rels"), "p:Relationships") {
		t.Fatal("slide rels must use the package Relationships element")
	}
	if err := ValidatePptx(data); err != nil {
		t.Fatalf("valid deck failed validation: %v", err)
	}
	preview, err := ExtractPptxText(data)
	if err != nil || !strings.Contains(preview, "封面") || !strings.Contains(preview, "第二章") {
		t.Fatalf("valid deck must expose titles in XML text: %q %v", preview, err)
	}
}

func TestValidatePptxRejectsEmptyDarkOnlySlides(t *testing.T) {
	if err := ValidatePptx(DarkOnlySlideFixture()); err == nil {
		t.Fatal("empty navy fill-only slides must fail validation")
	}
	if _, err := GenPptx("", []SlideSpec{{Title: "A", Bullets: []string{"x"}}}); err == nil {
		t.Fatal("empty deck title accepted")
	}
}

func TestGenPDF(t *testing.T) {
	data, err := GenPDF("Report <Q1>", "Line one.\n\nLine two & three.")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(data, []byte("%PDF-")) {
		t.Fatal("pdf header missing")
	}
	if !bytes.Contains(data, []byte("%%EOF")) {
		t.Fatal("pdf EOF missing")
	}
	if _, err := GenPDF("t", strings.Repeat("x", MaxPDFBodyRunes+1)); err == nil {
		t.Fatal("oversized body accepted")
	}
}

// zipHasPart reports whether the zip archive carries the named entry.
func zipHasPart(data []byte, name string) bool {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return false
	}
	for _, f := range zr.File {
		if f.Name == name {
			return true
		}
	}
	return false
}

// zipPartBody extracts one entry body (test helper).
func zipPartBody(t *testing.T, data []byte, name string) string {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range zr.File {
		if f.Name != name {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		defer rc.Close()
		var buf bytes.Buffer
		if _, err := buf.ReadFrom(rc); err != nil {
			t.Fatal(err)
		}
		return buf.String()
	}
	t.Fatalf("part %s not found", name)
	return ""
}
