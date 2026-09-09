package officestudio

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func testChartSpec() SlideChart {
	return SlideChart{Type: "column", Title: "季度收入", Categories: []string{"001", "=用户标签"}, Series: []ChartSeries{{Name: "本年", Values: []string{"123456789012345", "0.1200"}}}, X: 900000, Y: 1800000, Width: 8500000, Height: 4000000, Legend: true}
}
func testChartDeck(t *testing.T, charts ...SlideChart) []byte {
	t.Helper()
	data, err := Generate(Spec{SchemaVersion: 1, Kind: PPTX, Title: "图表", Slides: []Slide{{Title: "保留此标题", Charts: charts}}})
	if err != nil {
		t.Fatal(err)
	}
	return data
}
func chartNodes(t *testing.T, data []byte) []Node {
	t.Helper()
	i, err := Inspect(PPTX, data)
	if err != nil {
		t.Fatal(err)
	}
	var ns []Node
	for _, n := range i.Nodes {
		if n.Kind == "chart" {
			ns = append(ns, n)
		}
	}
	return ns
}

func TestPPTNativeChartsContainRealWorkbookAndExactDecimalCaches(t *testing.T) {
	for _, kind := range []string{"column", "bar", "line", "pie"} {
		t.Run(kind, func(t *testing.T) {
			c := testChartSpec()
			c.Type = kind
			data := testChartDeck(t, c)
			nodes := chartNodes(t, data)
			if len(nodes) != 1 || !nodes[0].Editable {
				t.Fatalf("missing editable native chart: %+v", nodes)
			}
			n := nodes[0]
			parts := zipParts(t, data)
			if !bytes.Contains(parts[n.Part], []byte(`<c:chart `)) || len(parts[n.Chart.WorkbookPart]) == 0 {
				t.Fatal("chart was a picture or has no workbook")
			}
			if !bytes.Contains(parts[n.Chart.ChartPart], []byte(`<c:v>0.1200</c:v>`)) {
				t.Fatal("decimal cache lost source representation")
			}
			if !chartWorkbookMatches(parts[n.Chart.WorkbookPart], c) {
				t.Fatal("workbook cells differ from cache or labels became formulas")
			}
			i, err := Inspect(PPTX, data)
			if err != nil || !i.RenderAllowed {
				t.Fatalf("safe chart workbook blocked: %+v %v", i.Issues, err)
			}
		})
	}
}

func TestPPTChartWithoutNewCanvasKeepsOriginalEditingCapability(t *testing.T) {
	c := testChartSpec()
	data := testChartDeck(t, c)
	n := chartNodes(t, data)[0]
	parts := zipParts(t, data)
	legacy := bytes.Replace(parts[n.Chart.ChartPart], []byte(chartCanvasXML), nil, 1)
	if bytes.Equal(legacy, parts[n.Chart.ChartPart]) {
		t.Fatal("new chart canvas missing")
	}
	data = editZIP(t, data, map[string][]byte{n.Chart.ChartPart: legacy})
	n = chartNodes(t, data)[0]
	if !n.Editable {
		t.Fatal("previously generated chart became read-only")
	}
	c.Series[0].Values[0] = "125.25"
	result, err := Patch(data, PatchRequest{Kind: PPTX, BaseSHA256: digest(data), Charts: []ChartPatch{{NodeID: n.ID, ExpectedDigest: n.Digest, Chart: c}}})
	if err != nil {
		t.Fatal(err)
	}
	next := chartNodes(t, result.Data)[0]
	if !next.Editable || !chartWorkbookMatches(zipParts(t, result.Data)[next.Chart.WorkbookPart], c) {
		t.Fatal("legacy chart update did not reach embedded workbook")
	}
}

func TestPPTChartPatchCopiesSharedSourceAndPreservesOtherParts(t *testing.T) {
	c := testChartSpec()
	data := testChartDeck(t, c, c)
	data = editZIP(t, data, map[string][]byte{"custom/opaque.bin": {0, 8, 2, 255}})
	before := chartNodes(t, data)
	replacement := testChartSpec()
	replacement.Series[0].Values = []string{"25.30", "60.70"}
	replacement.Type = "line"
	replacement.X, replacement.Y, replacement.Width, replacement.Height = 0, 0, 0, 0
	op := ChartPatch{NodeID: before[0].ID, ExpectedDigest: before[0].Digest, Chart: replacement}
	result, err := Patch(data, PatchRequest{Kind: PPTX, BaseSHA256: digest(data), Charts: []ChartPatch{op}})
	if err != nil {
		t.Fatal(err)
	}
	after := chartNodes(t, result.Data)
	if after[0].ID != before[0].ID || after[1].Digest != before[1].Digest || after[0].Chart.ChartPart == before[0].Chart.ChartPart {
		t.Fatal("target identity/shared chart preservation failed")
	}
	oldParts, newParts := zipParts(t, data), zipParts(t, result.Data)
	changed := map[string]bool{}
	for _, p := range result.ChangedParts {
		changed[p] = true
	}
	for p, b := range oldParts {
		if !changed[p] && !bytes.Equal(b, newParts[p]) {
			t.Fatalf("unrelated part changed %s", p)
		}
	}
	if !bytes.Equal(oldParts[before[0].Chart.WorkbookPart], newParts[before[0].Chart.WorkbookPart]) {
		t.Fatal("old shared workbook overwritten")
	}
	if _, err = Patch(result.Data, PatchRequest{Kind: PPTX, BaseSHA256: digest(data), Charts: []ChartPatch{op}}); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale base accepted %v", err)
	}
	if _, err = Patch(result.Data, PatchRequest{Kind: PPTX, BaseSHA256: digest(result.Data), Charts: []ChartPatch{op}}); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale chart accepted %v", err)
	}
}

func TestPPTChartUnknownContentAndSourceTamperingAreNotEditable(t *testing.T) {
	c := testChartSpec()
	data := testChartDeck(t, c)
	n := chartNodes(t, data)[0]
	parts := zipParts(t, data)
	foreign := bytes.Replace(parts[n.Chart.ChartPart], []byte(`<c:lang val="zh-CN"/>`), []byte(`<c:lang val="en-US"/>`), 1)
	modified := editZIP(t, data, map[string][]byte{n.Chart.ChartPart: foreign})
	if chartNodes(t, modified)[0].Editable {
		t.Fatal("unknown chart formatting can be lost")
	}
	workbookParts := zipParts(t, parts[n.Chart.WorkbookPart])
	worksheet := bytes.Replace(workbookParts["xl/worksheets/sheet1.xml"], []byte(`0.1200`), []byte(`0.9900`), 1)
	workbook := editZIP(t, parts[n.Chart.WorkbookPart], map[string][]byte{"xl/worksheets/sheet1.xml": worksheet})
	tampered := editZIP(t, data, map[string][]byte{n.Chart.WorkbookPart: workbook})
	if chartNodes(t, tampered)[0].Editable {
		t.Fatal("different source value accepted as matching cached chart")
	}
	unsafe := editZIP(t, parts[n.Chart.WorkbookPart], map[string][]byte{"xl/vbaProject.bin": []byte("do not execute")})
	blocked := editZIP(t, data, map[string][]byte{n.Chart.WorkbookPart: unsafe})
	i, err := Inspect(PPTX, blocked)
	if err != nil || i.RenderAllowed {
		t.Fatalf("nested active content allowed %v", err)
	}
	unrelated := editZIP(t, data, map[string][]byte{"ppt/embeddings/unreferenced.xlsx": parts[n.Chart.WorkbookPart]})
	i, err = Inspect(PPTX, unrelated)
	if err != nil || i.RenderAllowed {
		t.Fatalf("filename-only embedded exemption %v", err)
	}
}

func TestPPTChartRejectsPrecisionTypesBoundsAndMixedPatch(t *testing.T) {
	for name, change := range map[string]func(*SlideChart){"precision": func(c *SlideChart) { c.Series[0].Values[0] = "1234567890123456" }, "formula": func(c *SlideChart) { c.Series[0].Values[0] = "=SUM(A1:A2)" }, "NaN": func(c *SlideChart) { c.Series[0].Values[0] = "NaN" }, "length": func(c *SlideChart) { c.Series[0].Values = nil }, "bounds": func(c *SlideChart) { c.X = -1 }, "pie-negative": func(c *SlideChart) { c.Type = "pie"; c.Series[0].Values[0] = "-1" }, "zero-pie": func(c *SlideChart) { c.Type = "pie"; c.Series[0].Values = []string{"0", "0.0"} }} {
		t.Run(name, func(t *testing.T) {
			c := testChartSpec()
			change(&c)
			_, err := Generate(Spec{SchemaVersion: 1, Kind: PPTX, Title: "拒绝", Slides: []Slide{{Title: "图", Charts: []SlideChart{c}}}})
			if err == nil {
				t.Fatal("invalid chart accepted")
			}
		})
	}
	data := testChartDeck(t, testChartSpec())
	n := chartNodes(t, data)[0]
	_, err := Patch(data, PatchRequest{Kind: PPTX, BaseSHA256: digest(data), Charts: []ChartPatch{{NodeID: n.ID, ExpectedDigest: n.Digest, Chart: testChartSpec()}}, Operations: []TextPatch{{NodeID: "other", Text: "x"}}})
	if err == nil || !strings.Contains(err.Error(), "separate") {
		t.Fatal("mixed patch partially applied")
	}
}

func TestLineChartKeepsSeriesLegendAndLegacyChartsEditable(t *testing.T) {
	c := testChartSpec()
	c.Type = "line"
	c.Legend = true
	data := testChartDeck(t, c)
	n := chartNodes(t, data)[0]
	parts := zipParts(t, data)
	current := parts[n.Chart.ChartPart]
	if !bytes.Contains(current, []byte(lineSeriesColorsXML)) {
		t.Fatal("line chart must explicitly group colours by series, not category")
	}
	for _, removeCanvas := range []bool{false, true} {
		legacy := bytes.Replace(current, []byte(lineSeriesColorsXML), []byte(lineGroupingXML), 1)
		if removeCanvas {
			legacy = bytes.Replace(legacy, []byte(chartCanvasXML), nil, 1)
		}
		older := editZIP(t, data, map[string][]byte{n.Chart.ChartPart: legacy})
		target := chartNodes(t, older)[0]
		if !target.Editable {
			t.Fatal("previous line chart became read-only")
		}
		c.Title = "Updated series"
		updated, err := Patch(older, PatchRequest{Kind: PPTX, BaseSHA256: digest(older), Charts: []ChartPatch{{NodeID: target.ID, ExpectedDigest: target.Digest, Chart: c}}})
		if err != nil {
			t.Fatal(err)
		}
		newNode := chartNodes(t, updated.Data)[0]
		if !bytes.Contains(zipParts(t, updated.Data)[newNode.Chart.ChartPart], []byte(lineSeriesColorsXML)) {
			t.Fatal("updated legacy chart lacks correct series colours")
		}
	}
}
