package officestudio

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/xuri/excelize/v2"
)

// Embedded Office payloads remain blocked by default. Only a workbook reached
// by a chart's internal externalData/package relationship is considered here;
// the complete nested XLSX must independently pass the ordinary preflight.
// Its own embedded payloads are never exempted (Inspect is called as XLSX).
func safeChartWorkbooks(parts map[string][]byte) (map[string]bool, []Issue) {
	safe := map[string]bool{}
	issues := []Issue{}
	checked := map[string]bool{}
	for name, body := range parts {
		if !strings.HasPrefix(name, "ppt/charts/") || !strings.HasSuffix(name, ".xml") {
			continue
		}
		d := xml.NewDecoder(bytes.NewReader(body))
		for {
			tok, err := d.Token()
			if err != nil {
				break
			}
			s, ok := tok.(xml.StartElement)
			if !ok || s.Name.Space != chartNS || s.Name.Local != "externalData" {
				continue
			}
			rid := ""
			for _, a := range s.Attr {
				if a.Name.Space == officeRelNS && a.Name.Local == "id" {
					rid = a.Value
				}
			}
			rels, err := pictureRelationships(parts[slideRels(name)])
			r, ok := rels[rid]
			if err != nil || !ok || r.typ != officeRelNS+"/package" || r.mode != "" && r.mode != "Internal" {
				issues = append(issues, Issue{Code: "OFFICE_CHART_WORKBOOK", Severity: "blocked", Part: name, Message: "图表数据源关系缺失或不是内部工作簿，禁止自动渲染。"})
				continue
			}
			target, err := relationshipTarget(slideRels(name), r.target)
			if err != nil {
				issues = append(issues, Issue{Code: "OFFICE_CHART_WORKBOOK", Severity: "blocked", Part: name, Message: "图表工作簿路径无效。"})
				continue
			}
			if checked[target] {
				continue
			}
			checked[target] = true
			if len(checked) > MaxPresentationCharts {
				issues = append(issues, Issue{Code: "OFFICE_CHART_LIMIT", Severity: "blocked", Part: name, Message: "图表工作簿数量超过安全上限。"})
				continue
			}
			i, err := Inspect(XLSX, parts[target])
			if err != nil || !i.RenderAllowed {
				issues = append(issues, Issue{Code: "OFFICE_CHART_WORKBOOK", Severity: "blocked", Part: target, Message: "嵌入图表工作簿损坏或含宏、外联、其他嵌入内容；禁止自动渲染。"})
				continue
			}
			safe[target] = true
		}
	}
	return safe, issues
}

func managedChartSpec(body []byte) (SlideChart, error) {
	d := xml.NewDecoder(bytes.NewReader(body))
	count := 0
	var c SlideChart
	for {
		tok, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return c, ErrFormat
		}
		s, ok := tok.(xml.StartElement)
		if !ok || s.Name.Space != studioChartNS || s.Name.Local != "spec" {
			continue
		}
		count++
		if count != 1 {
			return c, ErrReadOnly
		}
		var value string
		if err := d.DecodeElement(&value, &s); err != nil {
			return c, ErrFormat
		}
		if len(value) > 96<<10 {
			return c, ErrLimit
		}
		jd := json.NewDecoder(strings.NewReader(value))
		jd.DisallowUnknownFields()
		if err := jd.Decode(&c); err != nil {
			return c, ErrFormat
		}
		if jd.Decode(new(any)) != io.EOF {
			return c, ErrFormat
		}
	}
	if count != 1 {
		return c, ErrReadOnly
	}
	if err := validateChart(c); err != nil {
		return c, err
	}
	if !managedChartBytesEqual(body, chartXML(c)) {
		return c, ErrReadOnly
	}
	return c, nil
}

func chartWorkbookMatches(body []byte, c SlideChart) bool {
	i, err := Inspect(XLSX, body)
	if err != nil || !i.RenderAllowed {
		return false
	}
	want := map[string]Cell{"A1": {Type: "text", Value: "Category"}}
	for j, s := range c.Series {
		address, _ := excelize.CoordinatesToCellName(j+2, 1)
		want[address] = Cell{Type: "text", Value: s.Name}
		for ri, v := range s.Values {
			address, _ := excelize.CoordinatesToCellName(j+2, ri+2)
			want[address] = Cell{Type: "number", Value: v}
		}
	}
	for ri, v := range c.Categories {
		address, _ := excelize.CoordinatesToCellName(1, ri+2)
		want[address] = Cell{Type: "text", Value: v}
	}
	if len(i.Nodes) != len(want) {
		return false
	}
	for _, n := range i.Nodes {
		v, ok := want[strings.TrimPrefix(n.Locator, "cell:")]
		if !ok || n.Part != "xl/worksheets/sheet1.xml" || n.Kind != "cell:"+v.Type || n.Text != v.Value {
			return false
		}
	}
	// Source references explicitly address Data, and an extra/renamed sheet must
	// not make a cache appear valid merely because cell addresses coincide.
	p, err := readPackage(body)
	if err != nil {
		return false
	}
	d := xml.NewDecoder(bytes.NewReader(p.parts["xl/workbook.xml"]))
	sheetCount := 0
	for {
		tok, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return false
		}
		if s, ok := tok.(xml.StartElement); ok && s.Name.Space == sheetNS && s.Name.Local == "sheet" {
			sheetCount++
			if xmlAttr(s, "name") != "Data" {
				return false
			}
		}
	}
	return sheetCount == 1
}

type chartFramePayload struct {
	NV struct {
		Properties struct {
			ID   int64  `xml:"id,attr"`
			Name string `xml:"name,attr"`
		} `xml:"cNvPr"`
	} `xml:"nvGraphicFramePr"`
	Transform struct {
		Off struct {
			X int64 `xml:"x,attr"`
			Y int64 `xml:"y,attr"`
		} `xml:"off"`
		Ext struct {
			Width  int64 `xml:"cx,attr"`
			Height int64 `xml:"cy,attr"`
		} `xml:"ext"`
	} `xml:"xfrm"`
	Graphic struct {
		Data struct {
			URI   string `xml:"uri,attr"`
			Chart struct {
				ID string `xml:"id,attr"`
			} `xml:"chart"`
		} `xml:"graphicData"`
	} `xml:"graphic"`
}
type chartLocation struct {
	node       Node
	start, end int
	shapeID    int64
	name       string
}

// ReadSlideChart returns the actual managed chart data only after the exact
// frame, chart XML, relationship and workbook content digest has been checked.
func ReadSlideChart(data []byte, nodeID, expectedDigest string) (SlideChart, error) {
	inspection, err := Inspect(PPTX, data)
	if err != nil {
		return SlideChart{}, err
	}
	if !inspection.RenderAllowed {
		return SlideChart{}, ErrReadOnly
	}
	for _, n := range inspection.Nodes {
		if n.ID != nodeID {
			continue
		}
		if expectedDigest == "" || expectedDigest != n.Digest {
			return SlideChart{}, ErrConflict
		}
		if n.Chart == nil || !n.Editable {
			return SlideChart{}, ErrReadOnly
		}
		p, err := readPackage(data)
		if err != nil {
			return SlideChart{}, err
		}
		return managedChartSpec(p.parts[n.Chart.ChartPart])
	}
	return SlideChart{}, ErrConflict
}

func scanCharts(part string, parts map[string][]byte) ([]chartLocation, []Issue, error) {
	frames, err := spans(parts[part], presentationNS, "graphicFrame")
	if err != nil {
		return nil, nil, err
	}
	rels, err := pictureRelationships(parts[slideRels(part)])
	if err != nil {
		return nil, nil, err
	}
	out := []chartLocation{}
	issues := []Issue{}
	seen := map[string]bool{}
	for _, f := range frames {
		raw := parts[part][f.start:f.end]
		var frame chartFramePayload
		if xml.Unmarshal(raw, &frame) != nil {
			return nil, nil, ErrFormat
		}
		if frame.Graphic.Data.URI != chartNS {
			continue
		}
		locator := "chart:" + strconv.FormatInt(frame.NV.Properties.ID, 10)
		id := "node_" + digest([]byte(part + "\x00" + locator))[:24]
		if seen[id] {
			return nil, nil, ErrFormat
		}
		seen[id] = true
		info := &ChartInfo{X: frame.Transform.Off.X, Y: frame.Transform.Off.Y, Width: frame.Transform.Ext.Width, Height: frame.Transform.Ext.Height}
		n := Node{ID: id, Part: part, Kind: "chart", Locator: locator, Chart: info}
		r, ok := rels[frame.Graphic.Data.Chart.ID]
		if !ok || r.typ != officeRelNS+"/chart" || r.mode != "" && r.mode != "Internal" {
			issues = append(issues, Issue{Code: "OFFICE_CHART_RELATIONSHIP", Severity: "blocked", Part: part, NodeID: id, Message: "图表引用无效，禁止自动渲染。"})
		} else {
			target, e := relationshipTarget(slideRels(part), r.target)
			if e != nil {
				return nil, nil, e
			}
			info.ChartPart = target
			info.ChartSHA256 = digest(parts[target])
			chartRels, e := pictureRelationships(parts[slideRels(target)])
			if e != nil {
				return nil, nil, e
			}
			wr, wok := chartRels["rIdWorkbook"]
			if wok && wr.typ == officeRelNS+"/package" && (wr.mode == "" || wr.mode == "Internal") {
				wp, e := relationshipTarget(slideRels(target), wr.target)
				if e != nil {
					return nil, nil, e
				}
				info.WorkbookPart = wp
				info.WorkbookSHA256 = digest(parts[wp])
			}
			c, e := managedChartSpec(parts[target])
			if e == nil {
				info.Type = c.Type
				info.SeriesCount = len(c.Series)
				info.CategoryCount = len(c.Categories)
				n.Text = c.Title
				n.Editable = frame.NV.Properties.ID > 0 && frame.NV.Properties.ID <= 4_294_967_295 && len(chartRels) == 1 && info.WorkbookPart != "" && chartWorkbookMatches(parts[info.WorkbookPart], c) && bytes.Equal(raw, []byte(chartFrameXML(frame.NV.Properties.ID, frame.NV.Properties.Name, frame.Graphic.Data.Chart.ID, c)))
				if !n.Editable {
					issues = append(issues, Issue{Code: "OFFICE_CHART_SOURCE_MISMATCH", Severity: "info", Part: target, NodeID: id, Message: "图表的源数据、缓存或框架与受管结构不一致，保留原件，只读检查。"})
				}
			} else {
				issues = append(issues, Issue{Code: "OFFICE_CHART_READONLY", Severity: "info", Part: target, NodeID: id, Message: "原生图表保留；复杂或未知图表当前只读，不能重建覆盖。"})
			}
			n.Digest = digest(bytes.Join([][]byte{raw, r.raw, parts[target], parts[slideRels(target)], []byte(info.WorkbookSHA256)}, []byte{0}))
		}
		if n.Digest == "" {
			n.Digest = digest(raw)
		}
		out = append(out, chartLocation{node: n, start: f.start, end: f.end, shapeID: frame.NV.Properties.ID, name: frame.NV.Properties.Name})
		if len(out) > MaxPresentationCharts {
			return nil, nil, ErrLimit
		}
	}
	return out, issues, nil
}

func patchCharts(data []byte, req PatchRequest) (PatchResult, error) {
	if req.Kind != PPTX || len(req.Operations) > 0 || len(req.Ranges) > 0 || len(req.Images) > 0 || len(req.Charts) > MaxPresentationCharts {
		return PatchResult{}, fmt.Errorf("%w: chart changes require a separate PPTX-only batch", ErrFormat)
	}
	before, err := Inspect(PPTX, data)
	if err != nil {
		return PatchResult{}, err
	}
	if !before.RenderAllowed || before.Editability == "readonly" {
		return PatchResult{}, ErrReadOnly
	}
	p, err := readPackage(data)
	if err != nil {
		return PatchResult{}, err
	}
	sw, sh, err := deckSize(p.parts)
	if err != nil {
		return PatchResult{}, err
	}
	byID := map[string]chartLocation{}
	for part := range p.parts {
		if !isNodePart(PPTX, part) {
			continue
		}
		locs, _, err := scanCharts(part, p.parts)
		if err != nil {
			return PatchResult{}, err
		}
		for _, loc := range locs {
			byID[loc.node.ID] = loc
		}
	}
	replaced := map[string][]byte{}
	edits := map[string][]byteEdit{}
	seen := map[string]bool{}
	for _, op := range req.Charts {
		loc, ok := byID[op.NodeID]
		if !ok || seen[op.NodeID] || op.ExpectedDigest == "" || loc.node.Digest != op.ExpectedDigest {
			return PatchResult{}, ErrConflict
		}
		seen[op.NodeID] = true
		if !loc.node.Editable {
			return PatchResult{}, ErrReadOnly
		}
		c := op.Chart
		b := loc.node.Chart
		if (c.X != 0 || c.Y != 0 || c.Width != 0 || c.Height != 0) && (c.X != b.X || c.Y != b.Y || c.Width != b.Width || c.Height != b.Height) {
			return PatchResult{}, fmt.Errorf("%w: chart data patch preserves source geometry", ErrFormat)
		}
		c.X, c.Y, c.Width, c.Height = b.X, b.Y, b.Width, b.Height
		if !validImageBox(c.X, c.Y, c.Width, c.Height, sw, sh) {
			return PatchResult{}, ErrReadOnly
		}
		target, err := addNativeChartParts(p, replaced, c)
		if err != nil {
			return PatchResult{}, err
		}
		rel, err := addChartRelationship(p, replaced, loc.node.Part, target)
		if err != nil {
			return PatchResult{}, err
		}
		edits[loc.node.Part] = append(edits[loc.node.Part], byteEdit{start: loc.start, end: loc.end, body: []byte(chartFrameXML(loc.shapeID, loc.name, rel, c))})
	}
	for part, list := range edits {
		body, err := splice(p.parts[part], list)
		if err != nil {
			return PatchResult{}, err
		}
		replaced[part] = body
	}
	return finishObjectPatch(PPTX, p, before, replaced, seen)
}

func finishObjectPatch(kind Kind, p packageData, before Inspection, replaced map[string][]byte, targets map[string]bool) (PatchResult, error) {
	data, err := rewritePackage(p, replaced)
	if err != nil {
		return PatchResult{}, err
	}
	after, err := Inspect(kind, data)
	if err != nil {
		return PatchResult{}, err
	}
	if !after.RenderAllowed {
		return PatchResult{}, ErrReadOnly
	}
	old := map[string]Node{}
	for _, n := range before.Nodes {
		old[n.ID] = n
	}
	if len(after.Nodes) != len(before.Nodes) {
		return PatchResult{}, ErrConflict
	}
	for _, n := range after.Nodes {
		v, ok := old[n.ID]
		if !ok || !targets[n.ID] && v.Digest != n.Digest {
			return PatchResult{}, fmt.Errorf("%w: a non-target node changed", ErrConflict)
		}
	}
	changed := []string{}
	for _, part := range after.Parts {
		if _, ok := replaced[part.Name]; ok {
			if digest(p.parts[part.Name]) != part.SHA256 {
				changed = append(changed, part.Name)
			}
		} else if digest(p.parts[part.Name]) != part.SHA256 {
			return PatchResult{}, fmt.Errorf("%w: a non-target part changed", ErrConflict)
		}
	}
	return PatchResult{Data: data, Inspection: after, ChangedParts: changed}, nil
}
