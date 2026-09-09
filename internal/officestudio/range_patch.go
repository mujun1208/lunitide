package officestudio

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/xuri/excelize/v2"
)

type xmlSpan struct {
	start, innerStart, innerEnd, end int
	element                          xml.StartElement
}
type byteEdit struct {
	start, end int
	body       []byte
}

func spans(body []byte, namespace, local string) ([]xmlSpan, error) {
	dec := xml.NewDecoder(bytes.NewReader(body))
	dec.DefaultSpace = namespace
	out := []xmlSpan{}
	depth, activeDepth := 0, 0
	var active *xmlSpan
	fragmentPrefix := ""
	for {
		before := int(dec.InputOffset())
		tok, err := dec.Token()
		after := int(dec.InputOffset())
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, ErrFormat
		}
		switch t := tok.(type) {
		case xml.StartElement:
			depth++
			if depth == 1 && t.Name.Space != "" && !strings.Contains(t.Name.Space, ":") && t.Name.Space != namespace {
				fragmentPrefix = t.Name.Space
			}
			if active == nil && (t.Name.Space == namespace || fragmentPrefix != "" && t.Name.Space == fragmentPrefix) && t.Name.Local == local {
				active = &xmlSpan{start: before, innerStart: after, element: t}
				activeDepth = depth
			}
		case xml.EndElement:
			if active != nil && depth == activeDepth {
				active.innerEnd = before
				active.end = after
				out = append(out, *active)
				active = nil
			}
			depth--
		}
	}
	return out, nil
}
func attr(e xml.StartElement, name string) string {
	for _, a := range e.Attr {
		if a.Name.Local == name {
			return a.Value
		}
	}
	return ""
}
func splice(body []byte, edits []byteEdit) ([]byte, error) {
	sort.SliceStable(edits, func(i, j int) bool {
		if edits[i].start == edits[j].start {
			return edits[i].end < edits[j].end
		}
		return edits[i].start < edits[j].start
	})
	var b bytes.Buffer
	last := 0
	for _, e := range edits {
		if e.start < last || e.end < e.start || e.end > len(body) {
			return nil, ErrConflict
		}
		b.Write(body[last:e.start])
		b.Write(e.body)
		last = e.end
		if b.Len() > MaxPartBytes {
			return nil, ErrLimit
		}
	}
	b.Write(body[last:])
	if b.Len() > MaxPartBytes {
		return nil, ErrLimit
	}
	return b.Bytes(), nil
}

type rectangle struct{ x1, y1, x2, y2 int }

func parseRange(value string) (rectangle, error) {
	parts := strings.Split(strings.ToUpper(value), ":")
	if len(parts) == 1 {
		parts = append(parts, parts[0])
	}
	if len(parts) != 2 {
		return rectangle{}, ErrFormat
	}
	x1, y1, err := excelize.CellNameToCoordinates(parts[0])
	if err != nil {
		return rectangle{}, ErrFormat
	}
	x2, y2, err := excelize.CellNameToCoordinates(parts[1])
	if err != nil || x2 < x1 || y2 < y1 {
		return rectangle{}, ErrFormat
	}
	return rectangle{x1, y1, x2, y2}, nil
}
func (r rectangle) contains(x, y int) bool { return x >= r.x1 && x <= r.x2 && y >= r.y1 && y <= r.y2 }

// patchRanges preserves OOXML unknown payloads and existing cell formatting.
// It invalidates cached formula results across the workbook conservatively;
// the returned report explicitly requires a real recalculation engine.
func patchRanges(data []byte, req PatchRequest) (PatchResult, error) {
	if req.Kind != XLSX {
		return PatchResult{}, ErrFormat
	}
	if len(req.Ranges) > 128 || len(req.Operations) > 1000 {
		return PatchResult{}, ErrLimit
	}
	before, err := Inspect(XLSX, data)
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
	writeSet := map[string]map[string]Cell{}
	total := 0
	add := func(part, address string, cell Cell) error {
		if writeSet[part] == nil {
			writeSet[part] = map[string]Cell{}
		}
		if _, ok := writeSet[part][address]; ok {
			return fmt.Errorf("%w: overlapping range writes", ErrConflict)
		}
		if cell.Format != "" {
			return fmt.Errorf("%w: range edits preserve existing cell styles; format changes require a managed spec", ErrReadOnly)
		}
		if _, err := typedCellXML(cell); err != nil {
			return err
		}
		writeSet[part][address] = cell
		total++
		if total > 50000 {
			return ErrLimit
		}
		return nil
	}
	for _, r := range req.Ranges {
		body, ok := p.parts[r.Part]
		if !ok || !isNodePart(XLSX, r.Part) {
			return PatchResult{}, ErrFormat
		}
		if r.ExpectedDigest == "" || digest(body) != r.ExpectedDigest {
			return PatchResult{}, ErrConflict
		}
		area, err := parseRange(r.Range)
		if err != nil {
			return PatchResult{}, err
		}
		rows, cols := area.y2-area.y1+1, area.x2-area.x1+1
		if rows > 5000 || cols > 128 || rows*cols > 50000 || len(r.Rows) != rows {
			return PatchResult{}, ErrLimit
		}
		for ri, row := range r.Rows {
			if len(row) != cols {
				return PatchResult{}, fmt.Errorf("%w: rows must match target range", ErrFormat)
			}
			for ci, cell := range row {
				address, _ := excelize.CoordinatesToCellName(area.x1+ci, area.y1+ri)
				if err := add(r.Part, address, cell); err != nil {
					return PatchResult{}, err
				}
			}
		}
	}
	byID := map[string]Node{}
	for _, n := range before.Nodes {
		byID[n.ID] = n
	}
	for _, op := range req.Operations {
		n, ok := byID[op.NodeID]
		if !ok || op.ExpectedDigest == "" || n.Digest != op.ExpectedDigest {
			return PatchResult{}, ErrConflict
		}
		if !n.Editable || n.Kind != "cell:text" {
			return PatchResult{}, ErrReadOnly
		}
		if err := add(n.Part, strings.TrimPrefix(n.Locator, "cell:"), Cell{Type: "text", Value: op.Text}); err != nil {
			return PatchResult{}, err
		}
	}
	if total == 0 {
		return PatchResult{}, ErrLimit
	}
	for _, n := range before.Nodes {
		if _, target := writeSet[n.Part][strings.TrimPrefix(n.Locator, "cell:")]; target && n.Kind == "cell:richtext" {
			return PatchResult{}, fmt.Errorf("%w: a rich-text cell requires a format-aware editor", ErrReadOnly)
		}
	}
	replaced := map[string][]byte{}
	for part, writes := range writeSet {
		updated, err := editSheet(p.parts[part], writes, len(p.parts["xl/calcChain.xml"]) > 0)
		if err != nil {
			return PatchResult{}, fmt.Errorf("%s: %w", part, err)
		}
		replaced[part] = updated
	}
	report := &CalculationReport{AffectedFormulaNodes: []string{}, DependencyCoverage: "conservative-workbook", Recalculation: "not-required", Notice: "保留原单元格样式与其他部件；尚未执行任何公式。"}
	for part, body := range p.parts {
		if !isNodePart(XLSX, part) {
			continue
		}
		if next, ok := replaced[part]; ok {
			body = next
		}
		updated, count, invalidated, err := clearFormulaCaches(body)
		if err != nil {
			return PatchResult{}, err
		}
		report.FormulaCount += count
		report.InvalidatedCaches += invalidated
		if !bytes.Equal(updated, body) {
			replaced[part] = updated
		}
	}
	if report.FormulaCount > 0 {
		report.Recalculation = "required"
		report.Notice = "已保守失效整个工作簿的显式公式缓存；未计算结果。共享、数组、命名区域及图表依赖仍须完整重算，不能把旧显示值当作新结果。"
		workbook, err := markWorkbookRecalculation(p.parts["xl/workbook.xml"])
		if err != nil {
			return PatchResult{}, err
		}
		replaced["xl/workbook.xml"] = workbook
	}
	chartChanges, err := refreshSheetChartCaches(p, replaced, report)
	if err != nil {
		return PatchResult{}, err
	}
	output, err := rewritePackage(p, replaced)
	if err != nil {
		return PatchResult{}, err
	}
	after, err := Inspect(XLSX, output)
	if err != nil {
		return PatchResult{}, err
	}
	if !after.RenderAllowed {
		return PatchResult{}, ErrReadOnly
	}
	for _, n := range after.Nodes {
		if n.Kind == "cell:formula" {
			report.AffectedFormulaNodes = append(report.AffectedFormulaNodes, n.ID)
		}
	}
	for _, n := range after.Nodes {
		old, exists := byID[n.ID]
		if !exists {
			continue
		}
		address := strings.TrimPrefix(n.Locator, "cell:")
		_, target := writeSet[n.Part][address]
		chartChanged := n.Chart != nil && chartChanges[n.Chart.ChartPart]
		if !target && old.Digest != n.Digest && n.Kind != "cell:formula" && !chartChanged {
			return PatchResult{}, fmt.Errorf("%w: unrelated cell changed", ErrConflict)
		}
	}
	changed := []string{}
	for _, part := range after.Parts {
		original := p.parts[part.Name]
		if digest(original) != part.SHA256 {
			if _, ok := replaced[part.Name]; !ok {
				return PatchResult{}, ErrConflict
			}
			changed = append(changed, part.Name)
		}
	}
	sort.Strings(changed)
	return PatchResult{Data: output, Inspection: after, ChangedParts: changed, Calculation: report}, nil
}

func typedCellXML(c Cell) (string, error) {
	if !validText(c.Value) || len(c.Value) > 32767 {
		return "", ErrLimit
	}
	value := escapeText(c.Value)
	switch c.Type {
	case "text":
		return `<is xmlns="` + sheetNS + `"><t xml:space="preserve">` + value + `</t></is>`, nil
	case "number":
		if !decimalPattern.MatchString(c.Value) {
			return "", ErrFormat
		}
		digits := strings.TrimLeft(strings.ReplaceAll(strings.TrimPrefix(c.Value, "-"), ".", ""), "0")
		if len(digits) > 15 {
			return "", fmt.Errorf("%w: long identifiers must use text cells", ErrFormat)
		}
		if _, err := strconv.ParseFloat(c.Value, 64); err != nil {
			return "", ErrFormat
		}
		return `<v xmlns="` + sheetNS + `">` + value + `</v>`, nil
	case "boolean":
		if c.Value != "true" && c.Value != "false" {
			return "", ErrFormat
		}
		v := "0"
		if c.Value == "true" {
			v = "1"
		}
		return `<v xmlns="` + sheetNS + `">` + v + `</v>`, nil
	case "date":
		d, err := time.Parse("2006-01-02", c.Value)
		if err != nil || d.Year() < 1900 {
			return "", ErrFormat
		}
		return `<v xmlns="` + sheetNS + `">` + value + `</v>`, nil
	case "formula":
		formula, err := localFormula(c.Value)
		if err != nil {
			return "", err
		}
		return `<f xmlns="` + sheetNS + `">` + escapeText(formula) + `</f>`, nil
	case "blank":
		if c.Value != "" {
			return "", ErrFormat
		}
		return "", nil
	}
	return "", ErrFormat
}

func cellType(c Cell) string {
	switch c.Type {
	case "text":
		return "inlineStr"
	case "boolean":
		return "b"
	case "date":
		return "d"
	default:
		return "n"
	}
}
func encodedCell(raw []byte, c Cell, address string, hasCalcChain bool) ([]byte, error) {
	inner, err := typedCellXML(c)
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 {
		if c.Type == "formula" && hasCalcChain {
			return nil, fmt.Errorf("%w: changing a workbook with calcChain formulas requires a calculation engine", ErrReadOnly)
		}
		return []byte(`<c xmlns="` + sheetNS + `" r="` + address + `" t="` + cellType(c) + `">` + inner + `</c>`), nil
	}
	cellSpans, err := spans(raw, sheetNS, "c")
	if err != nil || len(cellSpans) != 1 {
		return nil, ErrFormat
	}
	loc := cellSpans[0]
	if attr(loc.element, "cm") != "" || attr(loc.element, "vm") != "" {
		return nil, ErrReadOnly
	}
	_, kind, _ := cellText(raw, nil)
	if kind == "richtext" {
		return nil, ErrReadOnly
	}
	forms, err := spans(raw, sheetNS, "f")
	if err != nil {
		return nil, err
	}
	hasFormula := len(forms) > 0
	if hasCalcChain && (hasFormula || c.Type == "formula") {
		return nil, fmt.Errorf("%w: calcChain formula edits need a calculation engine", ErrReadOnly)
	}
	for _, f := range forms {
		if attr(f.element, "t") != "" && attr(f.element, "t") != "normal" {
			return nil, ErrReadOnly
		}
	}
	dec := xml.NewDecoder(bytes.NewReader(raw))
	dec.DefaultSpace = sheetNS
	depth := 0
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, ErrFormat
		}
		switch t := tok.(type) {
		case xml.StartElement:
			depth++
			if depth == 2 && (t.Name.Space != sheetNS && t.Name.Space != loc.element.Name.Space || (t.Name.Local != "v" && t.Name.Local != "f" && t.Name.Local != "is")) {
				return nil, ErrReadOnly
			}
		case xml.EndElement:
			depth--
		}
	}
	start := string(raw[:loc.innerStart])
	start = cellTypeAttr.ReplaceAllString(start, "")
	start = strings.TrimSuffix(strings.TrimSuffix(start, ">"), "/") + ` t="` + cellType(c) + `">`
	end := string(raw[loc.innerEnd:loc.end])
	if strings.HasSuffix(string(raw[:loc.innerStart]), "/>") {
		name := strings.Fields(strings.TrimPrefix(start, "<"))[0]
		end = "</" + name + ">"
	}
	return []byte(start + inner + end), nil
}

func editSheet(body []byte, writes map[string]Cell, hasCalcChain bool) ([]byte, error) {
	for _, kind := range []string{"mergeCell", "f"} {
		items, err := spans(body, sheetNS, kind)
		if err != nil {
			return nil, err
		}
		for _, item := range items {
			ref := attr(item.element, "ref")
			if ref == "" {
				continue
			}
			r, err := parseRange(ref)
			if err != nil {
				return nil, ErrFormat
			}
			for address := range writes {
				x, y, _ := excelize.CellNameToCoordinates(address)
				if r.contains(x, y) && (kind == "f" || x != r.x1 || y != r.y1) {
					return nil, fmt.Errorf("%w: range overlaps merged followers or an array/shared formula", ErrReadOnly)
				}
			}
		}
	}
	rows, err := spans(body, sheetNS, "row")
	if err != nil {
		return nil, err
	}
	byRow := map[int]map[string]Cell{}
	for address, c := range writes {
		_, y, err := excelize.CellNameToCoordinates(address)
		if err != nil {
			return nil, err
		}
		if byRow[y] == nil {
			byRow[y] = map[string]Cell{}
		}
		byRow[y][address] = c
	}
	edits := []byteEdit{}
	lastRow := 0
	for _, row := range rows {
		y, err := strconv.Atoi(attr(row.element, "r"))
		if err != nil || y <= lastRow {
			return nil, ErrFormat
		}
		lastRow = y
		if values := byRow[y]; len(values) > 0 {
			updated, err := editRow(body[row.start:row.end], values, hasCalcChain)
			if err != nil {
				return nil, err
			}
			edits = append(edits, byteEdit{row.start, row.end, updated})
			delete(byRow, y)
		}
	}
	sheetData, err := spans(body, sheetNS, "sheetData")
	if err != nil || len(sheetData) != 1 {
		return nil, ErrFormat
	}
	sd := sheetData[0]
	insertions := map[int][]int{}
	for y := range byRow {
		offset := sd.innerEnd
		for _, row := range rows {
			n, _ := strconv.Atoi(attr(row.element, "r"))
			if n > y {
				offset = row.start
				break
			}
		}
		insertions[offset] = append(insertions[offset], y)
	}
	selfClosed := strings.HasSuffix(string(body[sd.start:sd.innerStart]), "/>")
	if selfClosed && len(byRow) > 0 {
		indices := []int{}
		for y := range byRow {
			indices = append(indices, y)
		}
		sort.Ints(indices)
		var b bytes.Buffer
		tag := strings.TrimSuffix(string(body[sd.start:sd.innerStart]), "/>") + ">"
		b.WriteString(tag)
		for _, y := range indices {
			row, err := newRow(y, byRow[y], hasCalcChain)
			if err != nil {
				return nil, err
			}
			b.Write(row)
		}
		name := strings.Fields(strings.TrimPrefix(tag, "<"))[0]
		name = strings.TrimSuffix(name, ">")
		b.WriteString("</" + name + ">")
		edits = append(edits, byteEdit{sd.start, sd.end, b.Bytes()})
	} else {
		for offset, indices := range insertions {
			sort.Ints(indices)
			var b bytes.Buffer
			for _, y := range indices {
				row, err := newRow(y, byRow[y], hasCalcChain)
				if err != nil {
					return nil, err
				}
				b.Write(row)
			}
			edits = append(edits, byteEdit{offset, offset, b.Bytes()})
		}
	}
	updated, err := splice(body, edits)
	if err != nil {
		return nil, err
	}
	return updateDimension(updated, writes)
}

func editRow(raw []byte, writes map[string]Cell, hasCalcChain bool) ([]byte, error) {
	cells, err := spans(raw, sheetNS, "c")
	if err != nil {
		return nil, err
	}
	existing := map[string]bool{}
	edits := []byteEdit{}
	for _, span := range cells {
		address := attr(span.element, "r")
		if existing[address] {
			return nil, ErrFormat
		}
		existing[address] = true
		if c, ok := writes[address]; ok {
			updated, err := encodedCell(raw[span.start:span.end], c, address, hasCalcChain)
			if err != nil {
				return nil, err
			}
			edits = append(edits, byteEdit{span.start, span.end, updated})
		}
	}
	rows, err := spans(raw, sheetNS, "row")
	if err != nil || len(rows) != 1 {
		return nil, ErrFormat
	}
	row := rows[0]
	inserts := map[int][]string{}
	for address := range writes {
		if existing[address] {
			continue
		}
		x, _, _ := excelize.CellNameToCoordinates(address)
		offset := row.innerEnd
		for _, span := range cells {
			col, _, err := excelize.CellNameToCoordinates(attr(span.element, "r"))
			if err != nil {
				return nil, ErrFormat
			}
			if col > x {
				offset = span.start
				break
			}
		}
		inserts[offset] = append(inserts[offset], address)
	}
	if strings.HasSuffix(string(raw[:row.innerStart]), "/>") {
		y, err := strconv.Atoi(attr(row.element, "r"))
		if err != nil {
			return nil, ErrFormat
		}
		generated, err := newRow(y, writes, hasCalcChain)
		if err != nil {
			return nil, err
		}
		start := strings.TrimSuffix(string(raw[:row.innerStart]), "/>") + ">"
		endPos := bytes.IndexByte(generated, '>')
		tail := generated[endPos+1:]
		name := strings.TrimSuffix(strings.Fields(strings.TrimPrefix(start, "<"))[0], ">")
		tail = bytes.Replace(tail, []byte("</row>"), []byte("</"+name+">"), 1)
		return append([]byte(start), tail...), nil
	}
	for offset, addresses := range inserts {
		sort.Slice(addresses, func(i, j int) bool {
			a, _, _ := excelize.CellNameToCoordinates(addresses[i])
			b, _, _ := excelize.CellNameToCoordinates(addresses[j])
			return a < b
		})
		var b bytes.Buffer
		for _, address := range addresses {
			cell, err := encodedCell(nil, writes[address], address, hasCalcChain)
			if err != nil {
				return nil, err
			}
			b.Write(cell)
		}
		edits = append(edits, byteEdit{offset, offset, b.Bytes()})
	}
	return splice(raw, edits)
}
func newRow(y int, writes map[string]Cell, hasCalcChain bool) ([]byte, error) {
	addresses := []string{}
	for address := range writes {
		addresses = append(addresses, address)
	}
	sort.Slice(addresses, func(i, j int) bool {
		a, _, _ := excelize.CellNameToCoordinates(addresses[i])
		b, _, _ := excelize.CellNameToCoordinates(addresses[j])
		return a < b
	})
	var b bytes.Buffer
	fmt.Fprintf(&b, `<row xmlns="%s" r="%d">`, sheetNS, y)
	for _, address := range addresses {
		cell, err := encodedCell(nil, writes[address], address, hasCalcChain)
		if err != nil {
			return nil, err
		}
		b.Write(cell)
	}
	b.WriteString(`</row>`)
	return b.Bytes(), nil
}

var refAttr = regexp.MustCompile(`\s+ref\s*=\s*(?:"[^"]*"|'[^']*')`)

func updateDimension(body []byte, writes map[string]Cell) ([]byte, error) {
	dimensions, err := spans(body, sheetNS, "dimension")
	if err != nil {
		return nil, err
	}
	if len(dimensions) == 0 {
		return body, nil
	}
	if len(dimensions) > 1 {
		return nil, ErrFormat
	}
	dim := dimensions[0]
	area, err := parseRange(attr(dim.element, "ref"))
	if err != nil {
		return nil, err
	}
	for address := range writes {
		x, y, _ := excelize.CellNameToCoordinates(address)
		area.x1 = min(area.x1, x)
		area.y1 = min(area.y1, y)
		area.x2 = max(area.x2, x)
		area.y2 = max(area.y2, y)
	}
	cells, err := spans(body, sheetNS, "c")
	if err != nil {
		return nil, err
	}
	for _, cell := range cells {
		x, y, err := excelize.CellNameToCoordinates(attr(cell.element, "r"))
		if err != nil {
			return nil, ErrFormat
		}
		area.x1 = min(area.x1, x)
		area.y1 = min(area.y1, y)
		area.x2 = max(area.x2, x)
		area.y2 = max(area.y2, y)
	}
	a, _ := excelize.CoordinatesToCellName(area.x1, area.y1)
	b, _ := excelize.CoordinatesToCellName(area.x2, area.y2)
	replacement := refAttr.ReplaceAllString(string(body[dim.start:dim.end]), ` ref="`+a+":"+b+`"`)
	return splice(body, []byteEdit{{dim.start, dim.end, []byte(replacement)}})
}

func clearFormulaCaches(body []byte) ([]byte, int, int, error) {
	cells, err := spans(body, sheetNS, "c")
	if err != nil {
		return nil, 0, 0, err
	}
	edits := []byteEdit{}
	count, caches := 0, 0
	for _, cell := range cells {
		raw := body[cell.start:cell.end]
		formulas, err := spans(raw, sheetNS, "f")
		if err != nil {
			return nil, 0, 0, err
		}
		if len(formulas) == 0 {
			continue
		}
		count++
		values, err := spans(raw, sheetNS, "v")
		if err != nil {
			return nil, 0, 0, err
		}
		for _, v := range values {
			edits = append(edits, byteEdit{cell.start + v.start, cell.start + v.end, nil})
			caches++
		}
	}
	updated, err := splice(body, edits)
	return updated, count, caches, err
}

var recalcAttrs = regexp.MustCompile(`\s+(?:fullCalcOnLoad|forceFullCalc)\s*=\s*(?:"[^"]*"|'[^']*')`)

func markWorkbookRecalculation(body []byte) ([]byte, error) {
	items, err := spans(body, sheetNS, "calcPr")
	if err != nil {
		return nil, err
	}
	if len(items) > 1 {
		return nil, ErrFormat
	}
	if len(items) == 1 {
		p := items[0]
		start := recalcAttrs.ReplaceAllString(string(body[p.start:p.innerStart]), "")
		selfClosed := strings.HasSuffix(start, "/>")
		start = strings.TrimSuffix(strings.TrimSuffix(start, ">"), "/") + ` fullCalcOnLoad="1" forceFullCalc="1"`
		tail := body[p.innerStart:p.end]
		if selfClosed {
			start += "/>"
			tail = nil
		} else {
			start += ">"
		}
		return splice(body, []byteEdit{{p.start, p.end, append([]byte(start), tail...)}})
	}
	workbook, err := spans(body, sheetNS, "workbook")
	if err != nil || len(workbook) != 1 {
		return nil, ErrFormat
	}
	offset := workbook[0].innerEnd
	for _, later := range []string{"oleSize", "customWorkbookViews", "pivotCaches", "smartTagPr", "smartTagTypes", "webPublishing", "fileRecoveryPr", "webPublishObjects", "extLst"} {
		items, err := spans(body, sheetNS, later)
		if err != nil {
			return nil, err
		}
		if len(items) > 0 {
			offset = min(offset, items[0].start)
		}
	}
	return splice(body, []byteEdit{{offset, offset, []byte(`<calcPr xmlns="` + sheetNS + `" fullCalcOnLoad="1" forceFullCalc="1"/>`)}})
}
