package officestudio

import (
	"bytes"
	"strings"
	"testing"
)

func TestWordCacheMergeUpdatesSimpleFieldDisplayAndKeepsBody(t *testing.T) {
	original, err := Generate(Spec{SchemaVersion: 1, Kind: DOCX, Title: "复核报告", Document: &DocumentOptions{Header: "内部材料", Footer: "保留来源", PageNumbers: true, PageNumberStart: 1}, Blocks: []Block{{Type: "toc"}, {Type: "heading", Text: "第一章"}, {Type: "paragraph", Text: "可继续修改正文"}}})
	if err != nil {
		t.Fatal(err)
	}
	parts := zipParts(t, original)
	candidate := editZIP(t, original, map[string][]byte{
		"word/document.xml": bytes.Replace(parts["word/document.xml"], []byte("目录待实际排版后更新；当前未计算页码。"), []byte("第一章"), 1),
		"word/footer1.xml":  bytes.Replace(parts["word/footer1.xml"], []byte("页码待更新"), []byte("3"), 1),
	})
	merged, err := MergeNativeCaches(DOCX, original, candidate)
	if err != nil || merged.UpdatedFields == 0 || bytes.Equal(merged.Data, original) {
		t.Fatalf("word merge: %+v %v", merged, err)
	}
	out := zipParts(t, merged.Data)
	if !bytes.Contains(out["word/document.xml"], []byte("可继续修改正文")) || !bytes.Contains(out["word/document.xml"], []byte("第一章")) {
		t.Fatal("body or TOC display lost")
	}
	if bytes.Contains(out["word/document.xml"], []byte("目录待实际排版后更新；当前未计算页码。")) {
		t.Fatal("TOC display not refreshed")
	}
}

func TestWorkbookCacheMergeCopiesFormulaValuesWithoutChangingInputs(t *testing.T) {
	original, err := Generate(Spec{SchemaVersion: 1, Kind: XLSX, Title: "缓存", Sheets: []Sheet{{Name: "数据", Rows: [][]Cell{{{Type: "number", Value: "19.25"}, {Type: "formula", Value: "=SUM(A1,1)"}}}}}})
	if err != nil {
		t.Fatal(err)
	}
	parts := zipParts(t, original)
	sheet := parts["xl/worksheets/sheet1.xml"]
	if !bytes.Contains(sheet, []byte("<f>SUM(A1,1)</f>")) {
		t.Fatalf("formula missing: %s", sheet)
	}
	withCache := bytes.Replace(sheet, []byte("<c r=\"B1\">"), []byte("<c r=\"B1\"><v>20.25</v>"), 1)
	if bytes.Equal(withCache, sheet) {
		withCache = bytes.Replace(sheet, []byte("<f>SUM(A1,1)</f>"), []byte("<f>SUM(A1,1)</f><v>20.25</v>"), 1)
	}
	candidate := editZIP(t, original, map[string][]byte{"xl/worksheets/sheet1.xml": withCache})
	merged, err := MergeNativeCaches(XLSX, original, candidate)
	if err != nil || merged.UpdatedCells == 0 {
		t.Fatalf("xlsx merge: %+v %v", merged, err)
	}
	out := zipParts(t, merged.Data)
	if !bytes.Contains(out["xl/worksheets/sheet1.xml"], []byte("<f>SUM(A1,1)</f>")) || !bytes.Contains(out["xl/worksheets/sheet1.xml"], []byte("20.25")) {
		t.Fatal("formula or cache lost")
	}
	if !bytes.Contains(out["xl/worksheets/sheet1.xml"], []byte("19.25")) {
		t.Fatal("input cell changed")
	}
	changedFormula := bytes.Replace(withCache, []byte("<f>SUM(A1,1)</f>"), []byte("<f>SUM(A1,2)</f>"), 1)
	if _, err = MergeNativeCaches(XLSX, original, editZIP(t, original, map[string][]byte{"xl/worksheets/sheet1.xml": changedFormula})); err == nil || !strings.Contains(err.Error(), "公式") {
		t.Fatalf("formula rewrite accepted: %v", err)
	}
}

func TestWordTOCCachePreservesRealTabStopsAndSourceParagraph(t *testing.T) {
	original, err := Generate(Spec{SchemaVersion: 1, Kind: DOCX, Title: "目录对齐", Blocks: []Block{{Type: "toc"}, {Type: "heading", Text: "第一章"}, {Type: "paragraph", Text: "编号 2100040404301 保留"}}})
	if err != nil {
		t.Fatal(err)
	}
	parts := zipParts(t, original)
	source := parts["word/document.xml"]
	fields, err := spans(source, wordNS, "fldSimple")
	if err != nil || len(fields) != 1 {
		t.Fatal("missing source TOC", err)
	}
	f := fields[0]
	result := []byte(`<w:r><w:rPr><w:b/></w:rPr><w:t>第一章</w:t><w:tab/><w:t>3</w:t><w:br/><w:t>第二章</w:t><w:tab/><w:t>12</w:t></w:r>`)
	candidateBody, err := splice(source, []byteEdit{{start: f.innerStart, end: f.innerEnd, body: result}})
	if err != nil {
		t.Fatal(err)
	}
	merged, err := MergeNativeCaches(DOCX, original, editZIP(t, original, map[string][]byte{"word/document.xml": candidateBody}))
	if err != nil {
		t.Fatal(err)
	}
	out := zipParts(t, merged.Data)["word/document.xml"]
	if bytes.Count(out, []byte(`<w:tab/>`)) != 2 || !bytes.Contains(out, []byte(`w:leader="dot"`)) || bytes.Contains(out, []byte("\t")) {
		t.Fatalf("TOC requires OOXML tabs and dotted right alignment: %s", out)
	}
	updated, err := spans(out, wordNS, "fldSimple")
	if err != nil || len(updated) != 1 || !bytes.Equal(source[:f.innerStart], out[:updated[0].innerStart]) || !bytes.Equal(source[f.innerEnd:], out[updated[0].innerEnd:]) {
		t.Fatal("cache merge changed source paragraph or non-field body", err)
	}
	values, _, err := wordFieldValues(out)
	if err != nil || len(values) != 1 || values[0].value != "第一章\t3\n第二章\t12" {
		t.Fatal("cached field round trip lost its entries", values, err)
	}
}

func TestNativeWordFieldReadsDisplayedTabsNotParagraphTabDefinitions(t *testing.T) {
	body := []byte(`<w:document xmlns:w="` + wordNS + `"><w:body><w:p><w:r><w:fldChar w:fldCharType="begin"/></w:r><w:r><w:instrText>TOC</w:instrText></w:r><w:r><w:fldChar w:fldCharType="separate"/></w:r><w:r><w:t>第一章</w:t><w:tab/><w:t>3</w:t></w:r></w:p><w:p><w:pPr><w:tabs><w:tab w:val="right" w:leader="dot" w:pos="8000"/><w:tab w:val="clear" w:pos="4000"/></w:tabs></w:pPr><w:r><w:t>第二章</w:t><w:tab/><w:t>4</w:t></w:r><w:r><w:fldChar w:fldCharType="end"/></w:r></w:p></w:body></w:document>`)
	values, _, err := wordFieldValues(body)
	if err != nil || len(values) != 1 || values[0].value != "第一章\t3\n第二章\t4" {
		t.Fatal("paragraph tab definitions leaked into displayed TOC", values, err)
	}
}

func TestNativeCacheMergeRejectsWholePackageReplacement(t *testing.T) {
	original, err := Generate(Spec{SchemaVersion: 1, Kind: DOCX, Title: "原稿", Blocks: []Block{{Type: "toc"}, {Type: "paragraph", Text: "保留这段"}}})
	if err != nil {
		t.Fatal(err)
	}
	other, err := Generate(Spec{SchemaVersion: 1, Kind: DOCX, Title: "另一份", Blocks: []Block{{Type: "paragraph", Text: "完全不同的正文"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = MergeNativeCaches(DOCX, original, other); err == nil {
		t.Fatal("LibreOffice-style whole file replace was accepted")
	}
}

func TestNativeWorkbookCachesRefreshManagedChartAndInvalidateAgainAfterAnInputChange(t *testing.T) {
	data := generatedSheetChart(t)
	patched := sheetRangeEdit(t, data, "B3", [][]Cell{{{Type: "formula", Value: "=SUM(B2:B2)"}}})
	original := patched.Data
	parts := zipParts(t, original)
	body := bytes.Replace(parts["xl/worksheets/sheet1.xml"], []byte(`</f>`), []byte(`</f><v>123456789012345</v>`), 1)
	candidate := editZIP(t, original, map[string][]byte{"xl/worksheets/sheet1.xml": body})
	merged, err := MergeNativeCaches(XLSX, original, candidate)
	if err != nil || merged.UpdatedCells != 1 || merged.Charts == nil || merged.Charts.ChartCachesUpdated != 1 {
		t.Fatalf("merge=%+v error=%v", merged, err)
	}
	_, chart := inspectSheetChart(t, merged.Data)
	if chart.Chart.CacheState != "current" {
		t.Fatalf("chart cache not linked: %+v", chart.Chart)
	}
	// Changing another cell clears workbook formulas; matching formula text is
	// insufficient to keep the previously calculated chart current.
	next := sheetRangeEdit(t, merged.Data, "D2", [][]Cell{{{Type: "number", Value: "9"}}})
	_, chart = inspectSheetChart(t, next.Data)
	if chart.Chart.CacheState != "requires-recalculation" || next.Calculation.ChartCachesInvalidated != 1 {
		t.Fatalf("stale formula chart: %+v %+v", chart.Chart, next.Calculation)
	}
}

func TestNativeDecimalRejectsNonOOXMLNumbersAndExtremeExponents(t *testing.T) {
	for _, value := range []string{"1/2", "NaN", "+Inf", "1e999999999", "1e9999", "0xFF"} {
		if _, ok := nativeNumber(value); ok {
			t.Errorf("accepted %q", value)
		}
	}
	if !sameCellInput("0.12500", "number", "1.25E-1", "number") {
		t.Fatal("equal exact decimal values rejected")
	}
	if sameCellInput("123456789012345", "number", "123456789012346", "number") {
		t.Fatal("unequal inputs accepted")
	}
}

func TestWordNativeFooterRenumberingFollowsSectionRelationship(t *testing.T) {
	original, err := Generate(Spec{SchemaVersion: 1, Kind: DOCX, Title: "页码", Document: &DocumentOptions{PageNumbers: true}, Blocks: []Block{{Type: "paragraph", Text: "保留正文"}}})
	if err != nil {
		t.Fatal(err)
	}
	parts := zipParts(t, original)
	footer := bytes.Replace(parts["word/footer1.xml"], []byte("页码待更新"), []byte("7"), 1)
	candidate := editZIP(t, original, map[string][]byte{
		"word/_rels/document.xml.rels": bytes.ReplaceAll(parts["word/_rels/document.xml.rels"], []byte(`Target="footer1.xml"`), []byte(`Target="footer2.xml"`)),
		"[Content_Types].xml":          bytes.ReplaceAll(parts["[Content_Types].xml"], []byte(`/word/footer1.xml`), []byte(`/word/footer2.xml`)),
		"word/footer2.xml":             footer,
		"word/footer1.xml":             []byte(`<w:ftr xmlns:w="` + wordNS + `"><w:p/></w:ftr>`),
	})
	merged, err := MergeNativeCaches(DOCX, original, candidate)
	if err != nil || merged.UpdatedFields != 1 {
		t.Fatalf("%+v %v", merged, err)
	}
	out := zipParts(t, merged.Data)
	if !bytes.Equal(out["word/_rels/document.xml.rels"], parts["word/_rels/document.xml.rels"]) || out["word/footer2.xml"] != nil {
		t.Fatal("source relationships replaced")
	}
	if !bytes.Contains(out["word/footer1.xml"], []byte(`>7<`)) {
		t.Fatal("matched native footer display missing")
	}
}
