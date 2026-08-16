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
	data, err := GenDocx("测试报告 <Q1>", []DocxBlock{
		{Type: "heading", Text: "第一章 & 概述"},
		{Type: "paragraph", Text: "这是正文段落。"},
		{Type: "bullet", Text: "要点 '一'"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !zipHasPart(data, "word/document.xml") || !zipHasPart(data, "[Content_Types].xml") {
		t.Fatal("docx missing required parts")
	}
	doc := zipPartBody(t, data, "word/document.xml")
	if !strings.Contains(doc, "&lt;Q1&gt;") || !strings.Contains(doc, "&amp; 概述") || !strings.Contains(doc, "&apos;一&apos;") {
		t.Fatalf("docx escaping broken: %.200s", doc)
	}
	if !strings.Contains(doc, `w:val="Heading1"`) || !strings.Contains(doc, `w:val="Title"`) {
		t.Fatal("docx styles missing")
	}
	if _, err := GenDocx("t", nil); err == nil {
		t.Fatal("empty blocks accepted")
	}
	if _, err := GenDocx("t", []DocxBlock{{Type: "table", Text: "x"}}); err == nil {
		t.Fatal("unknown block type accepted")
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
	pres := zipPartBody(t, data, "ppt/presentation.xml")
	if strings.Count(pres, "<p:sldId ") != 2 {
		t.Fatalf("presentation must list 2 slides: %.200s", pres)
	}
	if _, err := GenPptx("t", nil); err == nil {
		t.Fatal("empty slides accepted")
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
