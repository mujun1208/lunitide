package officestudio

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"io"
	"strconv"
	"strings"
)

type sheetAnchorPayload struct {
	From struct {
		Column int `xml:"col"`
		Row    int `xml:"row"`
	} `xml:"from"`
	Extent struct {
		Width  int64 `xml:"cx,attr"`
		Height int64 `xml:"cy,attr"`
	} `xml:"ext"`
	Frame struct {
		NV struct {
			Properties struct {
				ID   int    `xml:"id,attr"`
				Name string `xml:"name,attr"`
			} `xml:"cNvPr"`
		} `xml:"nvGraphicFramePr"`
		Graphic struct {
			Data struct {
				URI   string `xml:"uri,attr"`
				Chart struct {
					ID string `xml:"id,attr"`
				} `xml:"chart"`
			} `xml:"graphicData"`
		} `xml:"graphic"`
	} `xml:"graphicFrame"`
}

func sheetSourceDigest(parts map[string][]byte, part string, s SheetChart) string {
	shared, err := sharedStrings(parts["xl/sharedStrings.xml"])
	if err != nil {
		return ""
	}
	nodes, err := scanNodes(XLSX, part, parts[part], shared)
	if err != nil {
		return ""
	}
	byAddress := map[string]Node{}
	for _, loc := range nodes {
		byAddress[strings.TrimPrefix(loc.node.Locator, "cell:")] = loc.node
	}
	ranges := []string{s.Categories}
	for _, series := range s.Series {
		ranges = append(ranges, series.Range)
	}
	var b bytes.Buffer
	for _, value := range ranges {
		r, err := chartSourceRange(value)
		if err != nil {
			return ""
		}
		for row := r.y1; row <= r.y2; row++ {
			address := columnName(r.x1) + strconv.Itoa(row)
			n := byAddress[address]
			b.WriteString(address)
			b.WriteByte(0)
			b.WriteString(n.Kind)
			b.WriteByte(0)
			b.WriteString(n.Text)
			b.WriteByte(0)
			// Formula text alone does not identify the cached value. A cache clear
			// or native refresh must invalidate or refresh dependent charts too.
			b.WriteString(n.Digest)
			b.WriteByte(0)
		}
	}
	return digest(b.Bytes())
}
func columnName(index int) string {
	out := ""
	for index > 0 {
		index--
		out = string(rune('A'+index%26)) + out
		index /= 26
	}
	return out
}

func scanSheetCharts(part string, parts map[string][]byte) ([]Node, []Issue, error) {
	d := xml.NewDecoder(bytes.NewReader(parts[part]))
	ids := []string{}
	for {
		tok, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, nil, ErrFormat
		}
		s, ok := tok.(xml.StartElement)
		if !ok || s.Name.Space != sheetNS || s.Name.Local != "drawing" {
			continue
		}
		for _, a := range s.Attr {
			if a.Name.Space == officeRelNS && a.Name.Local == "id" {
				ids = append(ids, a.Value)
			}
		}
	}
	if len(ids) == 0 {
		return nil, nil, nil
	}
	sheetRels, err := pictureRelationships(parts[slideRels(part)])
	if err != nil {
		return nil, nil, err
	}
	out := []Node{}
	issues := []Issue{}
	seen := map[string]bool{}
	names, err := worksheetNames(parts)
	if err != nil {
		return nil, nil, err
	}
	for _, id := range ids {
		rel, ok := sheetRels[id]
		if !ok || rel.typ != officeRelNS+"/drawing" || rel.mode != "" && rel.mode != "Internal" {
			return nil, nil, ErrFormat
		}
		drawing, err := relationshipTarget(slideRels(part), rel.target)
		if err != nil {
			return nil, nil, err
		}
		drawingRels, err := pictureRelationships(parts[slideRels(drawing)])
		if err != nil {
			return nil, nil, err
		}
		for _, kind := range []string{"oneCellAnchor", "twoCellAnchor", "absoluteAnchor"} {
			anchors, err := spans(parts[drawing], sheetDrawingNS, kind)
			if err != nil {
				return nil, nil, err
			}
			for _, a := range anchors {
				raw := parts[drawing][a.start:a.end]
				var payload sheetAnchorPayload
				if xml.Unmarshal(raw, &payload) != nil {
					return nil, nil, ErrFormat
				}
				if payload.Frame.Graphic.Data.URI != chartNS {
					continue
				}
				rid := payload.Frame.Graphic.Data.Chart.ID
				cr, ok := drawingRels[rid]
				if !ok || cr.typ != officeRelNS+"/chart" || cr.mode != "" && cr.mode != "Internal" {
					return nil, nil, ErrFormat
				}
				chartPart, err := relationshipTarget(slideRels(drawing), cr.target)
				if err != nil {
					return nil, nil, err
				}
				locator := "chart:" + drawing + ":" + strconv.Itoa(payload.Frame.NV.Properties.ID)
				nodeID := "node_" + digest([]byte(part + "\x00" + locator))[:24]
				if seen[nodeID] {
					return nil, nil, ErrFormat
				}
				seen[nodeID] = true
				info := &ChartInfo{ChartPart: chartPart, ChartSHA256: digest(parts[chartPart]), Width: payload.Extent.Width, Height: payload.Extent.Height, CacheState: "unverified"}
				n := Node{ID: nodeID, Part: part, Kind: "chart", Locator: locator, Chart: info}
				sourceHash := ""
				r, err := managedSheetChart(parts[chartPart])
				if err == nil && names[r.Sheet] == part {
					n.Text = r.Data.Title
					info.Type = r.Data.Type
					info.SeriesCount = len(r.Data.Series)
					info.CategoryCount = len(r.Data.Categories)
					info.SourceRanges = []string{r.Source.Categories}
					for _, s := range r.Source.Series {
						info.SourceRanges = append(info.SourceRanges, s.Range)
					}
					sourceHash = sheetSourceDigest(parts, part, r.Source)
					actual, err := sheetChartDataWithCaches(parts, part, r.Source, r.NativeCacheSheetSHA256)
					actualJSON, _ := json.Marshal(actual)
					recordJSON, _ := json.Marshal(r.Data)
					if r.Invalidated {
						info.CacheState = "requires-recalculation"
					} else if err == nil && bytes.Equal(actualJSON, recordJSON) && kind == "oneCellAnchor" && bytes.Equal(raw, []byte(sheetChartAnchorXML(payload.Frame.NV.Properties.ID, rid, r.Source))) {
						info.CacheState = "current"
					}
					if info.CacheState != "current" {
						issues = append(issues, Issue{Code: "OFFICE_CHART_CACHE_UNVERIFIED", Severity: "info", Part: chartPart, NodeID: nodeID, Message: "图表缓存尚未与当前源数据核实，需要原生重算或数据修正。"})
					}
				} else {
					issues = append(issues, Issue{Code: "OFFICE_CHART_READONLY", Severity: "info", Part: chartPart, NodeID: nodeID, Message: "保留未知原生图表；当前不自动刷新其缓存。"})
				}
				n.Digest = digest(bytes.Join([][]byte{raw, cr.raw, parts[chartPart], []byte(sourceHash)}, []byte{0}))
				out = append(out, n)
				if len(out) > MaxPresentationCharts {
					return nil, nil, ErrLimit
				}
			}
		}
	}
	return out, issues, nil
}
