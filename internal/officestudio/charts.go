package officestudio

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"path"
	"strconv"
	"strings"

	"github.com/xuri/excelize/v2"
)

const (
	chartNS               = "http://schemas.openxmlformats.org/drawingml/2006/chart"
	studioChartNS         = "urn:lunitide:office-studio:chart:1"
	MaxChartSeries        = 6
	MaxChartCategories    = 100
	MaxChartsPerSlide     = 8
	MaxPresentationCharts = 64
)

// A chart's labels use a light canvas independently of the surrounding slide.
// This remains readable when a user explicitly places it on a dark cover.
const chartCanvasXML = `<c:spPr><a:solidFill><a:srgbClr val="FFFFFF"/></a:solidFill><a:ln><a:noFill/></a:ln></c:spPr>`

const lineGroupingXML = `<c:lineChart><c:grouping val="standard"/>`
const lineSeriesColorsXML = lineGroupingXML + `<c:varyColors val="0"/>`

func managedChartBytesEqual(body, current []byte) bool {
	// Accept only exact versions emitted by our writer. Earlier versions did
	// not explicitly fix line-series colours or provide a light canvas.
	for _, variant := range [][]byte{current, bytes.Replace(current, []byte(lineSeriesColorsXML), []byte(lineGroupingXML), 1)} {
		if bytes.Equal(body, variant) || bytes.Equal(body, bytes.Replace(variant, []byte(chartCanvasXML), nil, 1)) {
			return true
		}
	}
	return false
}

func validateChart(c SlideChart) error {
	if c.Type != "column" && c.Type != "bar" && c.Type != "line" && c.Type != "pie" {
		return fmt.Errorf("%w: chart type", ErrFormat)
	}
	if len(c.Title) > 1024 || !validText(c.Title) || len(c.Categories) == 0 || len(c.Categories) > MaxChartCategories || len(c.Series) == 0 || len(c.Series) > MaxChartSeries {
		return ErrLimit
	}
	for _, label := range c.Categories {
		if !validText(label) || len(label) > 1024 {
			return ErrFormat
		}
	}
	positive := false
	for _, s := range c.Series {
		if !validText(s.Name) || len(s.Name) > 1024 || strings.TrimSpace(s.Name) == "" || len(s.Values) != len(c.Categories) {
			return ErrFormat
		}
		for _, value := range s.Values {
			if len(value) > 64 {
				return ErrLimit
			}
			if _, err := typedCellXML(Cell{Type: "number", Value: value}); err != nil {
				return err
			}
			if c.Type == "pie" && strings.HasPrefix(value, "-") && strings.Trim(strings.TrimPrefix(value, "-"), "0.") != "" {
				return fmt.Errorf("%w: pie values must be nonnegative", ErrFormat)
			}
			if strings.Trim(strings.TrimPrefix(value, "-"), "0.") != "" {
				positive = true
			}
		}
	}
	if c.Type == "pie" && (len(c.Series) != 1 || !positive) {
		return fmt.Errorf("%w: pie chart requires one nonzero series", ErrFormat)
	}
	return nil
}

// chartWorkbook never converts source decimals through a floating-point value.
// The base workbook is created with text cells, then only numeric cell XML is
// replaced using the same explicit typed writer used by range patches.
func chartWorkbook(c SlideChart) ([]byte, error) {
	rows := make([][]Cell, len(c.Categories)+1)
	for i := range rows {
		rows[i] = make([]Cell, len(c.Series)+1)
		for j := range rows[i] {
			rows[i][j] = Cell{Type: "text"}
		}
	}
	rows[0][0].Value = "Category"
	writes := map[string]Cell{}
	for j, s := range c.Series {
		rows[0][j+1].Value = s.Name
		for i, v := range s.Values {
			address, _ := excelize.CoordinatesToCellName(j+2, i+2)
			writes[address] = Cell{Type: "number", Value: v}
		}
	}
	for i, v := range c.Categories {
		rows[i+1][0].Value = v
	}
	data, err := generateXLSX([]Sheet{{Name: "Data", Rows: rows}})
	if err != nil {
		return nil, err
	}
	p, err := readPackage(data)
	if err != nil {
		return nil, err
	}
	body, err := editSheet(p.parts["xl/worksheets/sheet1.xml"], writes, false)
	if err != nil {
		return nil, err
	}
	return rewritePackage(p, map[string][]byte{"xl/worksheets/sheet1.xml": body})
}

func chartCache(tag string, values []string, numeric bool) string {
	var b strings.Builder
	b.WriteString("<c:" + tag + ">")
	if numeric {
		b.WriteString("<c:formatCode>General</c:formatCode>")
	}
	fmt.Fprintf(&b, `<c:ptCount val="%d"/>`, len(values))
	for i, v := range values {
		fmt.Fprintf(&b, `<c:pt idx="%d"><c:v>%s</c:v></c:pt>`, i, escapeText(v))
	}
	b.WriteString("</c:" + tag + ">")
	return b.String()
}

func chartXML(c SlideChart) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, `<c:chartSpace xmlns:c="%s" xmlns:a="%s" xmlns:r="%s"><c:lang val="zh-CN"/><c:chart>`, chartNS, drawingNS, officeRelNS)
	if c.Title != "" {
		fmt.Fprintf(&b, `<c:title><c:tx><c:rich><a:bodyPr/><a:lstStyle/><a:p><a:pPr/><a:r><a:rPr lang="zh-CN"/><a:t>%s</a:t></a:r></a:p></c:rich></c:tx><c:overlay val="0"/></c:title>`, escapeText(c.Title))
	}
	b.WriteString(`<c:autoTitleDeleted val="0"/><c:plotArea><c:layout/>`)
	tag := "barChart"
	switch c.Type {
	case "line":
		tag = "lineChart"
		b.WriteString(lineSeriesColorsXML)
	case "pie":
		tag = "pieChart"
		b.WriteString(`<c:pieChart><c:varyColors val="1"/>`)
	default:
		dir := "col"
		if c.Type == "bar" {
			dir = "bar"
		}
		fmt.Fprintf(&b, `<c:barChart><c:barDir val="%s"/><c:grouping val="clustered"/><c:varyColors val="0"/>`, dir)
	}
	for i, s := range c.Series {
		col, _ := excelize.ColumnNumberToName(i + 2)
		fmt.Fprintf(&b, `<c:ser><c:idx val="%d"/><c:order val="%d"/><c:tx><c:strRef><c:f>Data!$%s$1</c:f>%s</c:strRef></c:tx>`, i, i, col, chartCache("strCache", []string{s.Name}, false))
		fmt.Fprintf(&b, `<c:cat><c:strRef><c:f>Data!$A$2:$A$%d</c:f>%s</c:strRef></c:cat>`, len(c.Categories)+1, chartCache("strCache", c.Categories, false))
		fmt.Fprintf(&b, `<c:val><c:numRef><c:f>Data!$%s$2:$%s$%d</c:f>%s</c:numRef></c:val>`, col, col, len(c.Categories)+1, chartCache("numCache", s.Values, true))
		if c.Type == "line" {
			b.WriteString(`<c:smooth val="0"/>`)
		}
		b.WriteString(`</c:ser>`)
	}
	if c.Type == "pie" {
		b.WriteString(`<c:firstSliceAng val="0"/>`)
	} else {
		if c.Type == "bar" || c.Type == "column" {
			b.WriteString(`<c:gapWidth val="150"/>`)
		}
		b.WriteString(`<c:axId val="100001"/><c:axId val="100002"/>`)
	}
	b.WriteString(`</c:` + tag + `>`)
	if c.Type != "pie" {
		catPos, valPos := "b", "l"
		if c.Type == "bar" {
			catPos, valPos = "l", "b"
		}
		fmt.Fprintf(&b, `<c:catAx><c:axId val="100001"/><c:scaling><c:orientation val="minMax"/></c:scaling><c:delete val="0"/><c:axPos val="%s"/><c:tickLblPos val="nextTo"/><c:crossAx val="100002"/><c:crosses val="autoZero"/><c:auto val="1"/><c:lblAlgn val="ctr"/><c:lblOffset val="100"/></c:catAx>`, catPos)
		fmt.Fprintf(&b, `<c:valAx><c:axId val="100002"/><c:scaling><c:orientation val="minMax"/></c:scaling><c:delete val="0"/><c:axPos val="%s"/><c:majorGridlines/><c:numFmt formatCode="General" sourceLinked="1"/><c:tickLblPos val="nextTo"/><c:crossAx val="100001"/><c:crosses val="autoZero"/><c:crossBetween val="between"/></c:valAx>`, valPos)
	}
	b.WriteString(`</c:plotArea>`)
	if c.Legend {
		b.WriteString(`<c:legend><c:legendPos val="b"/><c:overlay val="0"/></c:legend>`)
	}
	b.WriteString(`<c:plotVisOnly val="1"/><c:dispBlanksAs val="gap"/><c:showDLblsOverMax val="0"/></c:chart>` + chartCanvasXML + `<c:externalData r:id="rIdWorkbook"><c:autoUpdate val="0"/></c:externalData>`)
	meta, _ := json.Marshal(c)
	fmt.Fprintf(&b, `<c:extLst><c:ext uri="%s"><os:spec xmlns:os="%s">%s</os:spec></c:ext></c:extLst></c:chartSpace>`, studioChartNS, studioChartNS, escapeText(string(meta)))
	return []byte(b.String())
}

func chartFrameXML(id int64, name, rel string, c SlideChart) string {
	return fmt.Sprintf(`<p:graphicFrame xmlns:p="%s" xmlns:a="%s" xmlns:r="%s"><p:nvGraphicFramePr><p:cNvPr id="%d" name="%s"/><p:cNvGraphicFramePr/><p:nvPr/></p:nvGraphicFramePr><p:xfrm><a:off x="%d" y="%d"/><a:ext cx="%d" cy="%d"/></p:xfrm><a:graphic><a:graphicData uri="%s"><c:chart xmlns:c="%s" r:id="%s"/></a:graphicData></a:graphic></p:graphicFrame>`, presentationNS, drawingNS, officeRelNS, id, escapeText(name), c.X, c.Y, c.Width, c.Height, chartNS, chartNS, escapeText(rel))
}

func addPartContentType(p packageData, replaced map[string][]byte, name, mime string) error {
	body := p.parts["[Content_Types].xml"]
	if v, ok := replaced["[Content_Types].xml"]; ok {
		body = v
	}
	d := xml.NewDecoder(bytes.NewReader(body))
	for {
		tok, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return ErrFormat
		}
		if s, ok := tok.(xml.StartElement); ok && s.Name.Space == contentTypeNS && s.Name.Local == "Override" && xmlAttr(s, "PartName") == "/"+name {
			if xmlAttr(s, "ContentType") != mime {
				return ErrConflict
			}
			return nil
		}
	}
	updated, err := appendXMLChild(body, contentTypeNS, "Types", fmt.Sprintf(`<Override xmlns="%s" PartName="/%s" ContentType="%s"/>`, contentTypeNS, escapeText(name), mime))
	if err != nil {
		return err
	}
	replaced["[Content_Types].xml"] = updated
	return nil
}

func addChartRelationship(p packageData, replaced map[string][]byte, part, chart string) (string, error) {
	rp := slideRels(part)
	body := p.parts[rp]
	if v, ok := replaced[rp]; ok {
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
		id := "rIdStudioChart" + strconv.Itoa(i)
		if _, ok := rels[id]; ok {
			continue
		}
		updated, err := appendXMLChild(body, packageRelNS, "Relationships", fmt.Sprintf(`<Relationship xmlns="%s" Id="%s" Type="%s/chart" Target="../charts/%s"/>`, packageRelNS, id, officeRelNS, path.Base(chart)))
		if err != nil {
			return "", err
		}
		replaced[rp] = updated
		return id, nil
	}
	return "", ErrLimit
}

func addNativeChartParts(p packageData, replaced map[string][]byte, c SlideChart) (string, error) {
	if err := validateChart(c); err != nil {
		return "", err
	}
	workbook, err := chartWorkbook(c)
	if err != nil {
		return "", err
	}
	chart := chartXML(c)
	// Include both hashes: metadata may be identical while workbook ZIP metadata
	// differs. A new graph never overwrites a shared source graph.
	key := digest(append(append([]byte(nil), chart...), []byte(digest(workbook))...))
	chartPart := "ppt/charts/studio-" + key + ".xml"
	workbookPart := "ppt/embeddings/studio-" + key + ".xlsx"
	rels := []byte(fmt.Sprintf(`<Relationships xmlns="%s"><Relationship Id="rIdWorkbook" Type="%s/package" Target="../embeddings/%s"/></Relationships>`, packageRelNS, officeRelNS, path.Base(workbookPart)))
	for name, body := range map[string][]byte{chartPart: chart, workbookPart: workbook, slideRels(chartPart): rels} {
		if old, ok := p.parts[name]; ok {
			if !bytes.Equal(old, body) {
				return "", ErrConflict
			}
		} else {
			if old, ok := replaced[name]; ok && !bytes.Equal(old, body) {
				return "", ErrConflict
			}
			replaced[name] = body
		}
	}
	if err := addPartContentType(p, replaced, chartPart, "application/vnd.openxmlformats-officedocument.drawingml.chart+xml"); err != nil {
		return "", err
	}
	if err := addPartContentType(p, replaced, workbookPart, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"); err != nil {
		return "", err
	}
	return chartPart, nil
}

func addSlideCharts(data []byte, slides []Slide) ([]byte, error) {
	count := 0
	for _, s := range slides {
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
	sw, sh, err := deckSize(p.parts)
	if err != nil {
		return nil, err
	}
	replaced := map[string][]byte{}
	for i, s := range slides {
		if len(s.Charts) == 0 {
			continue
		}
		part := fmt.Sprintf("ppt/slides/slide%d.xml", i+1)
		body := p.parts[part]
		id, err := nextShapeID(body)
		if err != nil {
			return nil, err
		}
		for j, c := range s.Charts {
			if !validImageBox(c.X, c.Y, c.Width, c.Height, sw, sh) {
				return nil, fmt.Errorf("%w: chart exceeds slide bounds", ErrFormat)
			}
			chart, err := addNativeChartParts(p, replaced, c)
			if err != nil {
				return nil, err
			}
			rel, err := addChartRelationship(p, replaced, part, chart)
			if err != nil {
				return nil, err
			}
			body, err = appendXMLChild(body, presentationNS, "spTree", chartFrameXML(id+int64(j), "Chart "+strconv.Itoa(j+1), rel, c))
			if err != nil {
				return nil, err
			}
		}
		replaced[part] = body
	}
	return rewritePackage(p, replaced)
}
