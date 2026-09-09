package officestudio

import (
	"bytes"
	"strings"
	"testing"
)

func chartSheet() Sheet {
	return Sheet{Name: "每周 数据", Rows: [][]Cell{{{Type: "text", Value: "项目"}, {Type: "text", Value: "金额"}}, {{Type: "text", Value: "001"}, {Type: "number", Value: "123456789012345"}}, {{Type: "text", Value: "=标签"}, {Type: "number", Value: "0.1200"}}}, Charts: []SheetChart{{Type: "column", Title: "原生范围图表", Categories: "A2:A3", Series: []SheetChartSeries{{Name: "收入", Range: "B2:B3"}}, Anchor: "E2", Width: 640, Height: 360, Legend: true}}}
}
func generatedSheetChart(t *testing.T) []byte {
	t.Helper()
	data, err := Generate(Spec{SchemaVersion: 1, Kind: XLSX, Title: "测试", Sheets: []Sheet{chartSheet()}})
	if err != nil {
		t.Fatal(err)
	}
	return data
}
func inspectSheetChart(t *testing.T, data []byte) (Inspection, Node) {
	t.Helper()
	i, err := Inspect(XLSX, data)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range i.Nodes {
		if n.Chart != nil {
			return i, n
		}
	}
	t.Fatal("no chart node indexed")
	return i, Node{}
}
func sheetRangeEdit(t *testing.T, data []byte, area string, rows [][]Cell) PatchResult {
	t.Helper()
	parts := zipParts(t, data)
	out, err := Patch(data, PatchRequest{Kind: XLSX, BaseSHA256: digest(data), Ranges: []RangePatch{{Part: "xl/worksheets/sheet1.xml", Range: area, ExpectedDigest: digest(parts["xl/worksheets/sheet1.xml"]), Rows: rows}}})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestXLSXNativeChartsReadExactLocalSourceAndRefreshOnRangePatch(t *testing.T) {
	data := generatedSheetChart(t)
	before, chart := inspectSheetChart(t, data)
	if !before.RenderAllowed || chart.Chart.CacheState != "current" {
		t.Fatalf("source/cache not verified: %+v %+v", chart, before.Issues)
	}
	parts := zipParts(t, data)
	body := string(parts[chart.Chart.ChartPart])
	if !strings.Contains(body, `<c:v>0.1200</c:v>`) || !strings.Contains(body, `&#39;每周 数据&#39;!$B$2:$B$3`) || strings.Contains(body, "externalData") {
		t.Fatal("not a native local range chart with exact decimal values")
	}
	data = editZIP(t, data, map[string][]byte{"custom/unknown.xml": []byte(`<keep>unchanged</keep>`)})
	out := sheetRangeEdit(t, data, "B3", [][]Cell{{{Type: "number", Value: "4.5600"}}})
	_, after := inspectSheetChart(t, out.Data)
	if after.ID != chart.ID || after.Chart.CacheState != "current" || out.Calculation.ChartCachesUpdated != 1 {
		t.Fatalf("chart update lost identity/coverage: %+v %+v", after, out.Calculation)
	}
	updated := zipParts(t, out.Data)
	if !bytes.Contains(updated[after.Chart.ChartPart], []byte(`<c:v>4.5600</c:v>`)) || bytes.Contains(updated[after.Chart.ChartPart], []byte(`<c:v>0.1200</c:v>`)) {
		t.Fatal("chart cache did not change with real cell")
	}
	if !bytes.Equal(updated["custom/unknown.xml"], []byte(`<keep>unchanged</keep>`)) {
		t.Fatal("unknown part lost")
	}
}

func TestXLSXChartFormulaSourceInvalidatesInsteadOfInventingAResult(t *testing.T) {
	data := generatedSheetChart(t)
	out := sheetRangeEdit(t, data, "B3", [][]Cell{{{Type: "formula", Value: "=SUM(B2:B2)"}}})
	_, chart := inspectSheetChart(t, out.Data)
	if out.Calculation.ChartCachesInvalidated != 1 || chart.Chart.CacheState != "requires-recalculation" {
		t.Fatalf("missing invalidation evidence: %+v %+v", out.Calculation, chart.Chart)
	}
	body := zipParts(t, out.Data)[chart.Chart.ChartPart]
	if !bytes.Contains(body, []byte(`<c:numCache><c:formatCode>General</c:formatCode><c:ptCount val="0"/></c:numCache>`)) {
		t.Fatal("old calculated display retained")
	}
	second := sheetRangeEdit(t, out.Data, "B3", [][]Cell{{{Type: "formula", Value: "=SUM(B2:B2)*2"}}})
	if second.Calculation.ChartCachesUpdated != 0 {
		t.Fatal("formula source counted as calculated")
	}
	final := sheetRangeEdit(t, second.Data, "B3", [][]Cell{{{Type: "number", Value: "12.34"}}})
	_, chart = inspectSheetChart(t, final.Data)
	if chart.Chart.CacheState != "current" || final.Calculation.ChartCachesUpdated != 1 {
		t.Fatal("explicit replacement did not restore current chart")
	}
}

func TestXLSXUnknownChartsPreservedAndUnrelatedRangesDoNotRewriteChart(t *testing.T) {
	data := generatedSheetChart(t)
	_, chart := inspectSheetChart(t, data)
	parts := zipParts(t, data)
	foreign := bytes.Replace(parts[chart.Chart.ChartPart], []byte(`<c:lang val="zh-CN"/>`), []byte(`<c:lang val="en-US"/>`), 1)
	data = editZIP(t, data, map[string][]byte{chart.Chart.ChartPart: foreign})
	out := sheetRangeEdit(t, data, "B3", [][]Cell{{{Type: "number", Value: "42"}}})
	if out.Calculation.UnknownCharts != 1 || out.Calculation.ChartDependencyCoverage != "partial-managed-only" || !bytes.Equal(zipParts(t, out.Data)[chart.Chart.ChartPart], foreign) {
		t.Fatal("unknown chart was rewritten or claimed covered")
	}
	data = generatedSheetChart(t)
	_, chart = inspectSheetChart(t, data)
	out = sheetRangeEdit(t, data, "D2", [][]Cell{{{Type: "text", Value: "其他单元格"}}})
	if out.Calculation.ChartCachesUpdated != 0 || !bytes.Equal(zipParts(t, data)[chart.Chart.ChartPart], zipParts(t, out.Data)[chart.Chart.ChartPart]) {
		t.Fatal("unrelated range changed chart")
	}
	out = sheetRangeEdit(t, data, "B3", [][]Cell{{{Type: "number", Value: "0.1200"}}})
	if out.Calculation.ChartCachesUpdated != 0 {
		t.Fatal("same exact value falsely counted as chart change")
	}
}

func TestXLSXChartsRejectNonlocalSourcesAndFormulaCaches(t *testing.T) {
	for name, change := range map[string]func(*Sheet){"external": func(s *Sheet) { s.Charts[0].Series[0].Range = "[other.xlsx]Data!B2:B3" }, "two-columns": func(s *Sheet) { s.Charts[0].Series[0].Range = "B2:C3" }, "mismatched": func(s *Sheet) { s.Charts[0].Categories = "A2:A4" }, "formula": func(s *Sheet) { s.Rows[2][1] = Cell{Type: "formula", Value: "=SUM(B2:B2)"} }, "too-large": func(s *Sheet) { s.Charts[0].Width = 10000 }} {
		t.Run(name, func(t *testing.T) {
			sheet := chartSheet()
			change(&sheet)
			if _, err := Generate(Spec{SchemaVersion: 1, Kind: XLSX, Title: "错误", Sheets: []Sheet{sheet}}); err == nil {
				t.Fatal("invalid chart source accepted")
			}
		})
	}
}
