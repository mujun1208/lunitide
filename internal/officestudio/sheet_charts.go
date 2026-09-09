package officestudio

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/xuri/excelize/v2"
)

const sheetDrawingNS = "http://schemas.openxmlformats.org/drawingml/2006/spreadsheetDrawing"

var localChartRange = regexp.MustCompile(`^[A-Z]{1,3}[1-9][0-9]{0,6}(?::[A-Z]{1,3}[1-9][0-9]{0,6})?$`)

type sheetChartRecord struct {
	Sheet       string     `json:"sheet"`
	Source      SheetChart `json:"source"`
	Data        SlideChart `json:"data"`
	Invalidated bool       `json:"invalidated,omitempty"`
	// Binds formula-derived display caches to the exact, selectively merged
	// sheet. It is set only by native cache refresh, never ordinary range edits.
	NativeCacheSheetSHA256 string `json:"nativeCacheSheetSha256,omitempty"`
}

func chartSourceRange(value string) (rectangle, error) {
	if !localChartRange.MatchString(value) {
		return rectangle{}, fmt.Errorf("%w: chart source must be a local uppercase A1 range", ErrFormat)
	}
	r, err := parseRange(value)
	if err != nil || r.x1 != r.x2 || r.y2-r.y1+1 > MaxChartCategories {
		return rectangle{}, ErrFormat
	}
	return r, nil
}
func validateSheetChart(c SheetChart) error {
	area, err := chartSourceRange(c.Categories)
	if err != nil {
		return err
	}
	if len(c.Series) == 0 || len(c.Series) > MaxChartSeries || len(c.Title) > 1024 || !validText(c.Title) || c.Width < 160 || c.Width > 1920 || c.Height < 120 || c.Height > 1440 {
		return ErrLimit
	}
	if strings.Contains(c.Anchor, ":") {
		return ErrFormat
	}
	if _, err := chartSourceRange(c.Anchor); err != nil {
		return err
	}
	if c.Type != "column" && c.Type != "bar" && c.Type != "line" && c.Type != "pie" {
		return ErrFormat
	}
	if c.Type == "pie" && len(c.Series) != 1 {
		return ErrFormat
	}
	for _, s := range c.Series {
		r, err := chartSourceRange(s.Range)
		if err != nil || r.y2-r.y1 != area.y2-area.y1 || len(s.Name) > 1024 || strings.TrimSpace(s.Name) == "" || !validText(s.Name) {
			return ErrFormat
		}
	}
	return nil
}

func worksheetNames(parts map[string][]byte) (map[string]string, error) {
	rels, err := pictureRelationships(parts["xl/_rels/workbook.xml.rels"])
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	d := xml.NewDecoder(bytes.NewReader(parts["xl/workbook.xml"]))
	for {
		tok, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, ErrFormat
		}
		s, ok := tok.(xml.StartElement)
		if !ok || s.Name.Space != sheetNS || s.Name.Local != "sheet" {
			continue
		}
		id := ""
		for _, a := range s.Attr {
			if a.Name.Space == officeRelNS && a.Name.Local == "id" {
				id = a.Value
			}
		}
		r, ok := rels[id]
		if !ok || r.typ != officeRelNS+"/worksheet" || r.mode != "" && r.mode != "Internal" {
			return nil, ErrFormat
		}
		part, err := relationshipTarget("xl/_rels/workbook.xml.rels", r.target)
		if err != nil {
			return nil, err
		}
		out[xmlAttr(s, "name")] = part
	}
	return out, nil
}

func sheetChartData(parts map[string][]byte, part string, c SheetChart) (SlideChart, error) {
	return sheetChartDataWithCaches(parts, part, c, "")
}

func sheetChartDataWithCaches(parts map[string][]byte, part string, c SheetChart, nativeSheetSHA string) (SlideChart, error) {
	if err := validateSheetChart(c); err != nil {
		return SlideChart{}, err
	}
	shared, err := sharedStrings(parts["xl/sharedStrings.xml"])
	if err != nil {
		return SlideChart{}, err
	}
	locations, err := scanNodes(XLSX, part, parts[part], shared)
	if err != nil {
		return SlideChart{}, err
	}
	cells := map[string]Node{}
	for _, loc := range locations {
		cells[strings.TrimPrefix(loc.node.Locator, "cell:")] = loc.node
	}
	if nativeSheetSHA != "" && nativeSheetSHA == digest(parts[part]) {
		// The caller verified all original inputs/formulas before copying these
		// caches from a real calculation. Ignore old caches in every other path.
		for _, loc := range locations {
			if loc.node.Kind != "cell:formula" {
				continue
			}
			var cell struct {
				Type  string  `xml:"t,attr"`
				Value *string `xml:"v"`
			}
			if xml.Unmarshal(parts[part][loc.start:loc.end], &cell) != nil || cell.Value == nil {
				continue
			}
			node := loc.node
			switch cell.Type {
			case "", "n":
				node.Kind = "cell:number"
			case "str":
				node.Kind = "cell:text"
			default:
				continue
			}
			node.Text = *cell.Value
			cells[strings.TrimPrefix(node.Locator, "cell:")] = node
		}
	}
	read := func(value string, numeric bool) ([]string, error) {
		area, err := chartSourceRange(value)
		if err != nil {
			return nil, err
		}
		values := []string{}
		for row := area.y1; row <= area.y2; row++ {
			address, _ := excelize.CoordinatesToCellName(area.x1, row)
			n, ok := cells[address]
			if !ok || numeric && n.Kind != "cell:number" || !numeric && n.Kind != "cell:text" && n.Kind != "cell:number" {
				return nil, fmt.Errorf("%w: chart source %s requires explicit %s cells, not formula caches or blanks", ErrReadOnly, address, map[bool]string{true: "number", false: "text/number"}[numeric])
			}
			values = append(values, n.Text)
		}
		return values, nil
	}
	d := SlideChart{Type: c.Type, Title: c.Title, Legend: c.Legend}
	d.Categories, err = read(c.Categories, false)
	if err != nil {
		return d, err
	}
	for _, s := range c.Series {
		values, err := read(s.Range, true)
		if err != nil {
			return d, err
		}
		d.Series = append(d.Series, ChartSeries{Name: s.Name, Values: values})
	}
	return d, validateChart(d)
}

func absoluteChartRange(sheet, area string) string {
	r, _ := chartSourceRange(area)
	col, _ := excelize.ColumnNumberToName(r.x1)
	prefix := "'" + strings.ReplaceAll(sheet, "'", "''") + "'!"
	if r.y1 == r.y2 {
		return fmt.Sprintf("%s$%s$%d", prefix, col, r.y1)
	}
	return fmt.Sprintf("%s$%s$%d:$%s$%d", prefix, col, r.y1, col, r.y2)
}
func sheetChartXML(r sheetChartRecord) []byte {
	c := r.Data
	body := string(chartXML(c))
	for i, s := range c.Series {
		col, _ := excelize.ColumnNumberToName(i + 2)
		old := fmt.Sprintf(`<c:tx><c:strRef><c:f>Data!$%s$1</c:f>%s</c:strRef></c:tx>`, col, chartCache("strCache", []string{s.Name}, false))
		body = strings.Replace(body, old, `<c:tx><c:v>`+escapeText(s.Name)+`</c:v></c:tx>`, 1)
		old = fmt.Sprintf(`<c:f>Data!$%s$2:$%s$%d</c:f>`, col, col, len(c.Categories)+1)
		body = strings.Replace(body, old, `<c:f>`+escapeText(absoluteChartRange(r.Sheet, r.Source.Series[i].Range))+`</c:f>`, 1)
	}
	old := fmt.Sprintf(`<c:f>Data!$A$2:$A$%d</c:f>`, len(c.Categories)+1)
	body = strings.ReplaceAll(body, old, `<c:f>`+escapeText(absoluteChartRange(r.Sheet, r.Source.Categories))+`</c:f>`)
	body = strings.Replace(body, `<c:externalData r:id="rIdWorkbook"><c:autoUpdate val="0"/></c:externalData>`, "", 1)
	meta, _ := json.Marshal(c)
	record, _ := json.Marshal(r)
	body = strings.Replace(body, `<os:spec xmlns:os="`+studioChartNS+`">`+escapeText(string(meta))+`</os:spec>`, `<os:sheetSpec xmlns:os="`+studioChartNS+`">`+escapeText(string(record))+`</os:sheetSpec>`, 1)
	if r.Invalidated {
		body = strings.ReplaceAll(body, chartCache("strCache", c.Categories, false), chartCache("strCache", nil, false))
		for _, s := range c.Series {
			body = strings.ReplaceAll(body, chartCache("numCache", s.Values, true), chartCache("numCache", nil, true))
		}
	}
	return []byte(body)
}
func managedSheetChart(body []byte) (sheetChartRecord, error) {
	d := xml.NewDecoder(bytes.NewReader(body))
	count := 0
	var r sheetChartRecord
	for {
		tok, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return r, ErrFormat
		}
		s, ok := tok.(xml.StartElement)
		if !ok || s.Name.Space != studioChartNS || s.Name.Local != "sheetSpec" {
			continue
		}
		count++
		var value string
		if count > 1 || d.DecodeElement(&value, &s) != nil || len(value) > 128<<10 {
			return r, ErrReadOnly
		}
		jd := json.NewDecoder(strings.NewReader(value))
		jd.DisallowUnknownFields()
		if jd.Decode(&r) != nil || jd.Decode(new(any)) != io.EOF {
			return r, ErrFormat
		}
	}
	if count != 1 || validateSheetChart(r.Source) != nil || validateChart(r.Data) != nil || r.Data.Type != r.Source.Type || r.Data.Title != r.Source.Title || r.Data.Legend != r.Source.Legend || len(r.Data.Series) != len(r.Source.Series) {
		return r, ErrReadOnly
	}
	for i, s := range r.Source.Series {
		if r.Data.Series[i].Name != s.Name {
			return r, ErrReadOnly
		}
	}
	if !managedChartBytesEqual(body, sheetChartXML(r)) {
		return r, ErrReadOnly
	}
	return r, nil
}

func sheetChartAnchorXML(id int, rel string, c SheetChart) string {
	col, row, _ := excelize.CellNameToCoordinates(c.Anchor)
	return fmt.Sprintf(`<xdr:oneCellAnchor><xdr:from><xdr:col>%d</xdr:col><xdr:colOff>0</xdr:colOff><xdr:row>%d</xdr:row><xdr:rowOff>0</xdr:rowOff></xdr:from><xdr:ext cx="%d" cy="%d"/><xdr:graphicFrame macro=""><xdr:nvGraphicFramePr><xdr:cNvPr id="%d" name="Chart %d"/><xdr:cNvGraphicFramePr/></xdr:nvGraphicFramePr><xdr:xfrm><a:off x="0" y="0"/><a:ext cx="0" cy="0"/></xdr:xfrm><a:graphic><a:graphicData uri="%s"><c:chart xmlns:c="%s" r:id="%s"/></a:graphicData></a:graphic></xdr:graphicFrame><xdr:clientData/></xdr:oneCellAnchor>`, col-1, row-1, int64(c.Width)*9525, int64(c.Height)*9525, id, id, chartNS, chartNS, rel)
}

func appendRelationship(p packageData, replaced map[string][]byte, source, prefix, typ, target string) (string, error) {
	part := slideRels(source)
	body := p.parts[part]
	if v, ok := replaced[part]; ok {
		body = v
	}
	if len(body) == 0 {
		body = []byte(`<Relationships xmlns="` + packageRelNS + `"></Relationships>`)
	}
	rels, err := pictureRelationships(body)
	if err != nil {
		return "", err
	}
	for i := 1; i <= MaxParts; i++ {
		id := prefix + strconv.Itoa(i)
		if _, ok := rels[id]; ok {
			continue
		}
		next, err := appendXMLChild(body, packageRelNS, "Relationships", fmt.Sprintf(`<Relationship xmlns="%s" Id="%s" Type="%s" Target="%s"/>`, packageRelNS, id, typ, escapeText(target)))
		if err != nil {
			return "", err
		}
		replaced[part] = next
		return id, nil
	}
	return "", ErrLimit
}

func addSheetCharts(data []byte, sheets []Sheet) ([]byte, error) {
	count := 0
	for _, s := range sheets {
		if len(s.Charts) > MaxChartsPerSlide {
			return nil, ErrLimit
		}
		count += len(s.Charts)
	}
	if count == 0 {
		return data, nil
	}
	if count > MaxPresentationCharts {
		return nil, ErrLimit
	}
	p, err := readPackage(data)
	if err != nil {
		return nil, err
	}
	names, err := worksheetNames(p.parts)
	if err != nil {
		return nil, err
	}
	replaced := map[string][]byte{}
	for i, s := range sheets {
		if len(s.Charts) == 0 {
			continue
		}
		name := strings.TrimSpace(s.Name)
		if name == "" {
			name = fmt.Sprintf("Sheet%d", i+1)
		}
		part, ok := names[name]
		if !ok {
			return nil, ErrFormat
		}
		drawingPart := fmt.Sprintf("xl/drawings/studio-sheet%d.xml", i+1)
		if len(p.parts[drawingPart]) > 0 {
			return nil, ErrConflict
		}
		var drawing strings.Builder
		fmt.Fprintf(&drawing, `<xdr:wsDr xmlns:xdr="%s" xmlns:a="%s" xmlns:r="%s">`, sheetDrawingNS, drawingNS, officeRelNS)
		for j, c := range s.Charts {
			d, err := sheetChartData(p.parts, part, c)
			if err != nil {
				return nil, err
			}
			record := sheetChartRecord{Sheet: name, Source: c, Data: d}
			body := sheetChartXML(record)
			cp := "xl/charts/studio-" + digest(body) + ".xml"
			if existing, ok := p.parts[cp]; ok && !bytes.Equal(existing, body) {
				return nil, ErrConflict
			}
			replaced[cp] = body
			if err := addPartContentType(p, replaced, cp, "application/vnd.openxmlformats-officedocument.drawingml.chart+xml"); err != nil {
				return nil, err
			}
			rel, err := appendRelationship(p, replaced, drawingPart, "rIdStudioChart", officeRelNS+"/chart", "../charts/"+path.Base(cp))
			if err != nil {
				return nil, err
			}
			drawing.WriteString(sheetChartAnchorXML(j+1, rel, c))
		}
		drawing.WriteString(`</xdr:wsDr>`)
		replaced[drawingPart] = []byte(drawing.String())
		if err := addPartContentType(p, replaced, drawingPart, "application/vnd.openxmlformats-officedocument.drawing+xml"); err != nil {
			return nil, err
		}
		rel, err := appendRelationship(p, replaced, part, "rIdStudioDrawing", officeRelNS+"/drawing", "../drawings/"+path.Base(drawingPart))
		if err != nil {
			return nil, err
		}
		body, err := appendXMLChild(p.parts[part], sheetNS, "worksheet", `<drawing xmlns="`+sheetNS+`" xmlns:r="`+officeRelNS+`" r:id="`+rel+`"/>`)
		if err != nil {
			return nil, err
		}
		replaced[part] = body
	}
	return rewritePackage(p, replaced)
}

// Refresh only managed charts whose actual local source data changed. Unknown
// charts stay byte-for-byte intact and are reported outside calculation coverage.
func refreshSheetChartCaches(p packageData, replaced map[string][]byte, report *CalculationReport) (map[string]bool, error) {
	return refreshSheetCharts(p, replaced, report, false)
}

func refreshSheetCharts(p packageData, replaced map[string][]byte, report *CalculationReport, nativeVerified bool) (map[string]bool, error) {
	parts := map[string][]byte{}
	for name, b := range p.parts {
		parts[name] = b
	}
	for name, b := range replaced {
		parts[name] = b
	}
	names, err := worksheetNames(parts)
	if err != nil {
		return nil, err
	}
	changed := map[string]bool{}
	report.ChartDependencyCoverage = "managed-local-ranges"
	for part, body := range p.parts {
		if !strings.HasPrefix(part, "xl/charts/") || !strings.HasSuffix(part, ".xml") {
			continue
		}
		record, err := managedSheetChart(body)
		if err != nil {
			report.UnknownCharts++
			continue
		}
		sourcePart, ok := names[record.Sheet]
		if !ok {
			return nil, ErrReadOnly
		}
		// A local patch must not quietly repair an unrelated stale chart. Only
		// source cells actually changed by this operation affect its coverage.
		if !nativeVerified && sheetSourceDigest(p.parts, sourcePart, record.Source) == sheetSourceDigest(parts, sourcePart, record.Source) {
			continue
		}
		changed[part] = true
		record.NativeCacheSheetSHA256 = ""
		if nativeVerified {
			record.NativeCacheSheetSHA256 = digest(parts[sourcePart])
		}
		next, err := sheetChartDataWithCaches(parts, sourcePart, record.Source, record.NativeCacheSheetSHA256)
		if err != nil {
			if record.Invalidated {
				continue
			}
			record.Invalidated = true
			report.ChartCachesInvalidated++
		} else {
			record.Data = next
			record.Invalidated = false
		}
		updated := sheetChartXML(record)
		if bytes.Equal(updated, body) {
			continue
		}
		if !record.Invalidated {
			report.ChartCachesUpdated++
		}
		replaced[part] = updated
		changed[part] = true
	}
	if report.UnknownCharts > 0 {
		report.ChartDependencyCoverage = "partial-managed-only"
		report.Notice += " 未知图表依赖未重算，需原生应用复核。"
	}
	if report.ChartCachesInvalidated > 0 {
		report.Notice += " 部分图表源不再是可直接读取的数字/文本，已清空图表缓存，需原生重算。"
	}
	return changed, nil
}
