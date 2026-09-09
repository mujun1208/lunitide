package officestudio

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
)

func TestWordSectionsHeadersFootersRealTOCAndHeadingThree(t *testing.T) {
	spec := Spec{SchemaVersion: 1, Kind: DOCX, Title: "复核报告", Document: &DocumentOptions{Header: "内部材料", Footer: "保留来源", PageNumbers: true, PageNumberStart: 1}, Blocks: []Block{{Type: "toc"}, {Type: "heading", Text: "第一章"}, {Type: "heading2", Text: "方法"}, {Type: "heading3", Text: "口径说明"}, {Type: "paragraph", Text: "可继续修改正文"}, {Type: "section", Section: &DocumentOptions{Orientation: "landscape", PageSize: "Letter", Header: "附录", PageNumbers: true, PageNumberStart: 10}}, {Type: "table", Rows: [][]string{{"项目", "数值"}, {"收入", "100"}}}}}
	data, err := Generate(spec)
	if err != nil {
		t.Fatal(err)
	}
	i, err := Inspect(DOCX, data)
	if err != nil {
		t.Fatal(err)
	}
	if !i.RenderAllowed || i.Structure == nil || i.Structure.Sections != 2 || i.Structure.Tables != 1 || i.Structure.HeadingCounts["3"] != 1 || i.Structure.TOCFields != 1 || i.Structure.PageFields != 2 || !i.Structure.NeedsFieldUpdate {
		t.Fatalf("structural evidence incomplete: %#v", i.Structure)
	}
	parts := zipParts(t, data)
	for part, texts := range map[string][]string{"word/document.xml": {`w:orient="landscape"`, `w:w="15840"`, `w:start="10"`, `w:instr="TOC`, `目录待实际排版后更新`, `w:tblW w:w="12240"`}, "word/styles.xml": {`w:styleId="Heading3"`, `w:outlineLvl w:val="2"`}, "word/header1.xml": {"内部材料"}, "word/header2.xml": {"附录"}, "word/footer1.xml": {"保留来源", `w:instr="PAGE"`}, "word/footer2.xml": {`w:instr="PAGE"`}} {
		for _, text := range texts {
			if !bytes.Contains(parts[part], []byte(text)) {
				t.Errorf("%s missing %s", part, text)
			}
		}
	}
	v, err := Validate(DOCX, data)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range v.Checks {
		if c.ID == "fields_update" && c.Status == "missing" {
			found = true
		}
	}
	if !found || v.Status != "partial" {
		t.Fatal("TOC/page values falsely marked updated", v)
	}
	n := findText(t, i, "可继续修改正文")
	if !n.Editable {
		t.Fatal("safe TOC field disabled unrelated document editing")
	}
	patched, err := Patch(data, PatchRequest{Kind: DOCX, BaseSHA256: i.SHA256, Operations: []TextPatch{{NodeID: n.ID, ExpectedDigest: n.Digest, Text: "修改后正文"}}})
	if err != nil {
		t.Fatal(err)
	}
	if patched.Inspection.Structure.TOCFields != 1 || patched.Inspection.Structure.PageFields != 2 {
		t.Fatal("patch destroyed document fields")
	}
	for _, bad := range []*DocumentOptions{{Orientation: "diagonal"}, {PageSize: "Unknown"}, {PageNumberStart: -1}} {
		spec.Document = bad
		if _, err := Generate(spec); err == nil {
			t.Fatal("invalid page setup accepted")
		}
	}
}

func TestPPTComparisonMetricsAndTableAreEditableNativeObjects(t *testing.T) {
	spec := Spec{SchemaVersion: 1, Kind: PPTX, Title: "指标报告", Slides: []Slide{{Title: "方案对比", Layout: "comparison", Bullets: []string{"方案A", "成本低", "方案B", "能力高"}}, {Title: "指标", Layout: "metrics", Bullets: []string{"19%|增长率", "100|订单"}}, {Title: "原始数据", Layout: "table", Rows: [][]string{{"指标", "数值"}, {"收入", "100"}, {"成本", "60"}}}}}
	data, err := Generate(spec)
	if err != nil {
		t.Fatal(err)
	}
	parts := zipParts(t, data)
	i, err := Inspect(PPTX, data)
	if err != nil {
		t.Fatal(err)
	}
	if i.Structure == nil || i.Structure.Tables != 1 {
		t.Fatal("native table index missing")
	}
	for part, tokens := range map[string][]string{"ppt/slides/slide1.xml": {`name="Column1"`, `name="Column2"`, `<p:sp>`}, "ppt/slides/slide2.xml": {`name="Metric1"`, `name="MetricLabel1"`, ">19%<", ">增长率<"}, "ppt/slides/slide3.xml": {`<p:graphicFrame>`, `<a:tbl>`, `<a:tblGrid>`, `<a:tc>`, ">收入<", ">100<"}} {
		for _, token := range tokens {
			if !bytes.Contains(parts[part], []byte(token)) {
				t.Errorf("%s lacks actual object %s", part, token)
			}
		}
	}
	n := findText(t, i, "收入")
	patched, err := Patch(data, PatchRequest{Kind: PPTX, BaseSHA256: i.SHA256, Operations: []TextPatch{{NodeID: n.ID, ExpectedDigest: n.Digest, Text: "营业收入"}}})
	if err != nil {
		t.Fatal(err)
	}
	if patched.Inspection.Structure.Tables != 1 {
		t.Fatal("table flattened after edit")
	}
}

func TestManagedWorkbookLayoutKeepsIdentifiersDatesAndTypesReadable(t *testing.T) {
	spec := Spec{SchemaVersion: 1, Kind: XLSX, Title: "排版回归", Sheets: []Sheet{{Name: "Data", FreezeHeader: true, Rows: [][]Cell{
		{{Type: "text", Value: "身份证"}, {Type: "text", Value: "日期"}, {Type: "text", Value: "说明"}, {Type: "text", Value: "金额"}},
		{{Type: "text", Value: "001234567890123456"}, {Type: "date", Value: "2026-09-07"}, {Type: "text", Value: strings.Repeat("完整内容", 20)}, {Type: "number", Value: "12.5", Format: "0.00"}},
	}}}}
	data, err := Generate(spec)
	if err != nil {
		t.Fatal(err)
	}
	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if text, err := f.GetCellValue("Data", "A2"); err != nil || text != "001234567890123456" {
		t.Fatalf("identifier changed: %q, %v", text, err)
	}
	if text, err := f.GetCellValue("Data", "B2"); err != nil || text != "2026-09-07" {
		t.Fatalf("date changed: %q, %v", text, err)
	}
	if text, err := f.GetCellValue("Data", "D2"); err != nil || text != "12.50" {
		t.Fatalf("number format lost: %q, %v", text, err)
	}
	for col, minimum := range map[string]float64{"A": 20, "B": 12} {
		width, err := f.GetColWidth("Data", col)
		if err != nil || width < minimum {
			t.Fatalf("%s too narrow: %v, %v", col, width, err)
		}
	}
	styleID, err := f.GetCellStyle("Data", "C2")
	if err != nil {
		t.Fatal(err)
	}
	style, err := f.GetStyle(styleID)
	if err != nil || style.Alignment == nil || !style.Alignment.WrapText {
		t.Fatal("long text cannot wrap", err)
	}
	height, err := f.GetRowHeight("Data", 2)
	if err != nil || height <= 24 {
		t.Fatal("wrapped row is clipped", height, err)
	}
	if formula, err := f.GetCellFormula("Data", "A2"); err != nil || formula != "" {
		t.Fatal("layout changed identifier type", formula, err)
	}
}

func TestSpecRejectsFieldsThatWouldSilentlyDisappear(t *testing.T) {
	for _, spec := range []Spec{
		{SchemaVersion: 1, Kind: DOCX, Title: "报告", Blocks: []Block{{Type: "paragraph", Text: "正文"}}, Body: "不能丢失"},
		{SchemaVersion: 1, Kind: DOCX, Title: "报告", Blocks: []Block{{Type: "paragraph", Text: "正文", Rows: [][]string{{"不能丢失"}}}}},
		{SchemaVersion: 1, Kind: DOCX, Title: "报告", Blocks: []Block{{Type: "table", Text: "不能丢失", Rows: [][]string{{"数据"}}}}},
		{SchemaVersion: 1, Kind: PDF, Title: "报告", Body: "正文", Document: &DocumentOptions{Header: "不能丢失"}},
	} {
		if _, err := Generate(spec); !errors.Is(err, ErrFormat) {
			t.Fatalf("unused spec content was silently discarded: %+v, %v", spec, err)
		}
	}
}

func TestExplicitFormulaNormalizesPrefixForGenerationAndRange(t *testing.T) {
	for _, value := range []string{"SUM(A1,2)", "=SUM(A1,2)", " = SUM(A1,2) "} {
		data, err := Generate(Spec{SchemaVersion: 1, Kind: XLSX, Title: "公式", Sheets: []Sheet{{Name: "Data", Rows: [][]Cell{{{Type: "number", Value: "3"}, {Type: "formula", Value: value}, {Type: "text", Value: "=SUM(A1,2)"}}}}}})
		if err != nil {
			t.Fatal(err)
		}
		i, err := Inspect(XLSX, data)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, n := range i.Nodes {
			if n.Kind == "cell:formula" {
				found = true
				if n.Text != "=SUM(A1,2)" {
					t.Fatal("formula prefix duplicated", n.Text)
				}
			}
		}
		if !found {
			t.Fatal("formula was not emitted")
		}
		partDigest := ""
		for _, p := range i.Parts {
			if p.Name == "xl/worksheets/sheet1.xml" {
				partDigest = p.SHA256
			}
		}
		patched, err := Patch(data, PatchRequest{Kind: XLSX, BaseSHA256: i.SHA256, Ranges: []RangePatch{{Part: "xl/worksheets/sheet1.xml", Range: "B1", ExpectedDigest: partDigest, Rows: [][]Cell{{{Type: "formula", Value: value}}}}}})
		if err != nil {
			t.Fatal(err)
		}
		for _, n := range patched.Inspection.Nodes {
			if n.Kind == "cell:formula" && n.Text != "=SUM(A1,2)" {
				t.Fatal("range formula invalid", n.Text)
			}
		}
	}
	for _, value := range []string{"=", "==SUM(A1,2)", "WEBSERVICE(A1)", "[external.xlsx]Sheet1!A1"} {
		if _, err := localFormula(value); err == nil {
			t.Fatal("invalid formula accepted", value)
		}
	}
}

func workbookRangeFixture(t *testing.T) []byte {
	t.Helper()
	f := excelize.NewFile()
	defer f.Close()
	for cell, value := range map[string]any{"A1": 2, "B1": 3, "D1": "保留的原始文本", "A2": "old"} {
		if err := f.SetCellValue("Sheet1", cell, value); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.SetCellFormula("Sheet1", "C1", "SUM(A1:B1)"); err != nil {
		t.Fatal(err)
	}
	style, err := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true, Color: "FF0000"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.SetCellStyle("Sheet1", "A1", "A1", style); err != nil {
		t.Fatal(err)
	}
	if _, err := f.NewSheet("Summary"); err != nil {
		t.Fatal(err)
	}
	if err := f.SetCellFormula("Summary", "A1", "Sheet1!C1*2"); err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	if err := f.Write(&b); err != nil {
		t.Fatal(err)
	}
	base := b.Bytes()
	parts := zipParts(t, base)
	changes := map[string][]byte{"custom/opaque.dat": []byte("opaque contents that must survive")}
	for part, body := range parts {
		if !isNodePart(XLSX, part) {
			continue
		}
		clean, _, _, err := clearFormulaCaches(body)
		if err != nil {
			t.Fatal(err)
		}
		forms, err := spans(clean, sheetNS, "f")
		if err != nil {
			t.Fatal(err)
		}
		edits := []byteEdit{}
		for _, form := range forms {
			edits = append(edits, byteEdit{form.end, form.end, []byte(`<v>99</v>`)})
		}
		clean, err = splice(clean, edits)
		if err != nil {
			t.Fatal(err)
		}
		changes[part] = clean
	}
	return editZIP(t, base, changes)
}

func rangeRequest(t *testing.T, data []byte, part, address string, rows [][]Cell) PatchRequest {
	t.Helper()
	parts := zipParts(t, data)
	return PatchRequest{Kind: XLSX, BaseSHA256: digest(data), Ranges: []RangePatch{{Part: part, Range: address, ExpectedDigest: digest(parts[part]), Rows: rows}}}
}

func TestTypedRangePatchPreservesUnknownContentAndInvalidatesDependentCaches(t *testing.T) {
	data := workbookRangeFixture(t)
	part := "xl/worksheets/sheet1.xml"
	req := rangeRequest(t, data, part, "A1:B2", [][]Cell{{{Type: "number", Value: "10"}, {Type: "number", Value: "20"}}, {{Type: "text", Value: "=不执行"}, {Type: "boolean", Value: "true"}}})
	req.Ranges = append(req.Ranges, RangePatch{Part: part, Range: "B3:D3", ExpectedDigest: req.Ranges[0].ExpectedDigest, Rows: [][]Cell{{{Type: "date", Value: "2026-09-07"}, {Type: "text", Value: "001234567890123456789"}, {Type: "formula", Value: "=SUM(A1:B1)"}}}})
	result, err := Patch(data, req)
	if err != nil {
		t.Fatal(err)
	}
	if result.Calculation == nil || result.Calculation.FormulaCount != 3 || result.Calculation.InvalidatedCaches != 2 || result.Calculation.Recalculation != "required" || len(result.Calculation.AffectedFormulaNodes) != 3 {
		t.Fatalf("calculation evidence: %#v", result.Calculation)
	}
	f, err := excelize.OpenReader(bytes.NewReader(result.Data))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	for address, want := range map[string]string{"A1": "10", "B1": "20", "A2": "=不执行", "C3": "001234567890123456789", "D1": "保留的原始文本"} {
		got, err := f.GetCellValue("Sheet1", address)
		if err != nil || got != want {
			t.Errorf("%s=%q want %q %v", address, got, want, err)
		}
	}
	for _, sheet := range []string{"Sheet1", "Summary"} {
		address := "C1"
		if sheet == "Summary" {
			address = "A1"
		}
		cached, err := f.GetCellValue(sheet, address)
		if err != nil || cached != "" {
			t.Fatalf("stale formula cache exposed as result: %s!%s=%q %v", sheet, address, cached, err)
		}
	}
	formula, err := f.GetCellFormula("Sheet1", "D3")
	if err != nil || strings.TrimPrefix(formula, "=") != "SUM(A1:B1)" {
		t.Fatal("new explicit formula lost", formula, err)
	}
	before, after := zipParts(t, data), zipParts(t, result.Data)
	for name, old := range before {
		if name != part && name != "xl/worksheets/sheet2.xml" && name != "xl/workbook.xml" && !bytes.Equal(old, after[name]) {
			t.Errorf("unrelated part changed: %s", name)
		}
	}
	if !bytes.Contains(after[part], []byte(`t="d"`)) || !bytes.Contains(after[part], []byte(`ref="A1:D3"`)) {
		t.Fatal("date type or expanded worksheet dimension missing")
	}
	styleBefore, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	defer styleBefore.Close()
	a, _ := styleBefore.GetCellStyle("Sheet1", "A1")
	b, _ := f.GetCellStyle("Sheet1", "A1")
	if a != b {
		t.Fatal("range patch dropped existing cell style")
	}
	v, err := Validate(XLSX, result.Data)
	if err != nil || v.Status != "partial" {
		t.Fatal("partial calculation was marked complete", v, err)
	}
}

func TestRangePatchCASOverlapAndUnsupportedEditsFailBeforePublication(t *testing.T) {
	data := workbookRangeFixture(t)
	part := "xl/worksheets/sheet1.xml"
	original := rangeRequest(t, data, part, "A1", [][]Cell{{{Type: "number", Value: "8"}}})
	wrong := original
	wrong.BaseSHA256 = strings.Repeat("0", 64)
	if _, err := Patch(data, wrong); !errors.Is(err, ErrConflict) {
		t.Fatal("stale base accepted", err)
	}
	wrong = rangeRequest(t, data, part, "A1", [][]Cell{{{Type: "number", Value: "8"}}})
	wrong.Ranges[0].ExpectedDigest = strings.Repeat("0", 64)
	if _, err := Patch(data, wrong); !errors.Is(err, ErrConflict) {
		t.Fatal("stale part accepted", err)
	}
	wrong = original
	wrong.Ranges = append(append([]RangePatch(nil), original.Ranges...), original.Ranges[0])
	if _, err := Patch(data, wrong); !errors.Is(err, ErrConflict) {
		t.Fatal("overlapping writes accepted", err)
	}
	for _, cell := range []Cell{{Type: "number", Value: "12345678901234567"}, {Type: "formula", Value: "=WEBSERVICE(A1)"}, {Type: "number", Value: "8", Format: "0.00"}, {Type: "date", Value: "2026-02-30"}} {
		if _, err := Patch(data, rangeRequest(t, data, part, "A1", [][]Cell{{cell}})); err == nil {
			t.Fatalf("unsafe/unsupported edit accepted: %#v", cell)
		}
	}
}

func TestRangePatchCreatesRowsWithoutDroppingExistingCells(t *testing.T) {
	f := excelize.NewFile()
	defer f.Close()
	if err := f.SetCellStr("Sheet1", "D5", "keep"); err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	if err := f.Write(&b); err != nil {
		t.Fatal(err)
	}
	data := b.Bytes()
	part := "xl/worksheets/sheet1.xml"
	req := rangeRequest(t, data, part, "A2:B2", [][]Cell{{{Type: "text", Value: "before"}, {Type: "blank"}}})
	req.Ranges = append(req.Ranges, RangePatch{Part: part, Range: "A5:B5", ExpectedDigest: req.Ranges[0].ExpectedDigest, Rows: [][]Cell{{{Type: "number", Value: "1"}, {Type: "text", Value: "same row"}}}})
	r, err := Patch(data, req)
	if err != nil {
		t.Fatal(err)
	}
	out, err := excelize.OpenReader(bytes.NewReader(r.Data))
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	for address, want := range map[string]string{"A2": "before", "A5": "1", "B5": "same row", "D5": "keep"} {
		got, _ := out.GetCellValue("Sheet1", address)
		if got != want {
			t.Errorf("%s=%q want %q", address, got, want)
		}
	}
}

func TestRangePatchProtectsMergedFollowersRichTextAndArrayFormulas(t *testing.T) {
	for _, setup := range []func(*excelize.File) error{
		func(f *excelize.File) error {
			_ = f.SetCellStr("Sheet1", "A1", "merged")
			return f.MergeCell("Sheet1", "A1", "B1")
		},
		func(f *excelize.File) error {
			return f.SetCellRichText("Sheet1", "B1", []excelize.RichTextRun{{Text: "styled", Font: &excelize.Font{Bold: true}}})
		},
		func(f *excelize.File) error {
			array, ref := "array", "A1:B1"
			return f.SetCellFormula("Sheet1", "A1", "TRANSPOSE(A2:A3)", excelize.FormulaOpts{Type: &array, Ref: &ref})
		},
	} {
		f := excelize.NewFile()
		if err := setup(f); err != nil {
			t.Fatal(err)
		}
		var b bytes.Buffer
		if err := f.Write(&b); err != nil {
			t.Fatal(err)
		}
		_ = f.Close()
		data := b.Bytes()
		if _, err := Patch(data, rangeRequest(t, data, "xl/worksheets/sheet1.xml", "B1", [][]Cell{{{Type: "text", Value: "replacement"}}})); !errors.Is(err, ErrReadOnly) {
			t.Fatalf("protected content changed: %v", err)
		}
	}
}

func TestRangePatchCanPopulateAnEmptyWorkbook(t *testing.T) {
	f := excelize.NewFile()
	var b bytes.Buffer
	if err := f.Write(&b); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	data := b.Bytes()
	r, err := Patch(data, rangeRequest(t, data, "xl/worksheets/sheet1.xml", "B4", [][]Cell{{{Type: "text", Value: "new cell"}}}))
	if err != nil {
		t.Fatal(err)
	}
	out, err := excelize.OpenReader(bytes.NewReader(r.Data))
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	v, _ := out.GetCellValue("Sheet1", "B4")
	if v != "new cell" {
		t.Fatal("empty worksheet could not be populated")
	}
}

func TestLegacyTextPatchAlsoInvalidatesFormulaCaches(t *testing.T) {
	data := workbookRangeFixture(t)
	i, err := Inspect(XLSX, data)
	if err != nil {
		t.Fatal(err)
	}
	n := findText(t, i, "old")
	r, err := Patch(data, PatchRequest{Kind: XLSX, BaseSHA256: i.SHA256, Operations: []TextPatch{{NodeID: n.ID, ExpectedDigest: n.Digest, Text: "new"}}})
	if err != nil {
		t.Fatal(err)
	}
	if r.Calculation == nil || r.Calculation.InvalidatedCaches != 2 {
		t.Fatal("legacy text entry bypassed formula dependency handling")
	}
}
