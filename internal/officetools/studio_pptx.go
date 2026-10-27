package officetools

import (
	"archive/zip"
	"bytes"
	"fmt"
	"strings"
	"unicode/utf8"
)

// GenStudioPptx retains the existing package/master/theme/notes generator and
// adds ten bounded semantic layouts. All supplied strings survive generation;
// unsupported layouts and excessive content fail instead of disappearing.
func GenStudioPptx(title string, slides []SlideSpec) ([]byte, error) {
	return GenStudioPptxWithTables(title, slides, nil)
}

// Tables are actual editable DrawingML tables, keyed by zero-based slide.
type SlideTheme struct {
	Navy, Teal, Gold, Paper, Ink, Muted, White, Soft string
	Latin, East                                      string
	TitleSz, BodySz, NotesSz                         int
}

func (theme SlideTheme) titleSize() int {
	if theme.TitleSz > 0 {
		return theme.TitleSz
	}
	return 2700
}

func ClassicSlideTheme() SlideTheme {
	return SlideTheme{Navy: clrNavy, Teal: clrTeal, Gold: clrGold, Paper: clrPaper, Ink: clrInk, Muted: clrMuted, White: clrWhite, Soft: clrSoft, Latin: "Calibri", East: "Microsoft YaHei"}
}

func GenStudioPptxWithTables(title string, slides []SlideSpec, tables map[int][][]string) ([]byte, error) {
	return GenStudioPptxThemed(title, slides, tables, ClassicSlideTheme())
}

func GenStudioPptxThemed(title string, slides []SlideSpec, tables map[int][][]string, theme SlideTheme) ([]byte, error) {
	slides = append([]SlideSpec(nil), slides...)
	baseSlides := make([]SlideSpec, len(slides))
	for i, s := range slides {
		layout := s.Layout
		if layout == "" {
			if i == 0 {
				layout = "cover"
			} else {
				layout = "content"
			}
		}
		s.Layout = layout
		slides[i] = s
		switch layout {
		case "cover", "section", "content", "two-column", "comparison", "quote", "metrics", "timeline", "agenda", "closing", "table", "conclusion", "trend", "structure", "process", "evidence", "diagram":
		default:
			return nil, fmt.Errorf("officetools: unsupported studio slide layout %q", layout)
		}
		if layout == "diagram" {
			if err := validateSlideDiagram(s.Diagram); err != nil {
				return nil, err
			}
			if len(s.Bullets) > 0 || len(s.Metrics) > 0 || s.Comparison != nil {
				return nil, fmt.Errorf("officetools: diagram slide cannot mix bullets, metrics, or comparison payloads")
			}
		} else if s.Diagram != nil {
			return nil, fmt.Errorf("officetools: diagram payload requires the diagram layout")
		}
		if layout == "table" {
			rows := tables[i]
			if len(rows) == 0 || len(rows) > 9 || len(rows[0]) == 0 || len(rows[0]) > 6 || len(s.Bullets) > 0 {
				return nil, fmt.Errorf("officetools: table slide requires 1–9 equal rows, 1–6 columns, and no bullet payload")
			}
			for _, row := range rows {
				if len(row) != len(rows[0]) {
					return nil, fmt.Errorf("officetools: table rows have unequal widths")
				}
				for _, v := range row {
					if utf8.RuneCountInString(v) > 160 {
						return nil, ErrLimit
					}
				}
			}
		} else if len(tables[i]) > 0 {
			return nil, fmt.Errorf("officetools: table data requires table layout")
		}
		if utf8.RuneCountInString(s.Title) > 100 || utf8.RuneCountInString(s.Subtitle) > 300 {
			return nil, ErrLimit
		}
		for _, item := range s.Bullets {
			if utf8.RuneCountInString(item) > 300 {
				return nil, fmt.Errorf("%w: slide text needs splitting", ErrLimit)
			}
		}
		metricCount := len(s.Bullets)
		if layout == "metrics" && len(s.Metrics) > 0 {
			metricCount = len(s.Metrics)
		}
		if (layout == "metrics" || layout == "timeline") && metricCount > 6 {
			return nil, fmt.Errorf("%w: %s supports at most six items", ErrLimit, layout)
		}
		if layout == "metrics" && len(s.Metrics) == 0 && len(s.Bullets) == 0 {
			return nil, fmt.Errorf("officetools: metrics slide needs metrics or bullets")
		}
		baseSlides[i] = s
		baseSlides[i].Layout = "content"
	}
	base, err := GenPptx(title, baseSlides)
	if err != nil {
		return nil, err
	}
	zr, err := zip.NewReader(bytes.NewReader(base), int64(len(base)))
	if err != nil {
		return nil, err
	}
	var b bytes.Buffer
	zw := zip.NewWriter(&b)
	layouts := len(studioLayoutDefs)
	replacements := map[string]string{
		"ppt/theme/theme1.xml":  ThemeXMLFor(theme.Latin, theme.East, theme.Navy, theme.Teal, theme.Gold, theme.Paper, theme.Ink, theme.White, theme.Soft),
		"[Content_Types].xml":   contentTypesPptx(len(slides), layouts, noteSlideIndexes(slides)...),
		"ppt/slideMasters/slideMaster1.xml":               slideMasterXMLFor(theme, layouts),
		"ppt/slideMasters/_rels/slideMaster1.xml.rels":    slideMasterRelsFor(layouts),
		"ppt/slideLayouts/slideLayout1.xml":               slideLayoutXMLFor(studioLayoutDefs[0], theme),
		"ppt/slideLayouts/_rels/slideLayout1.xml.rels":    slideLayoutRels,
	}
	for i, s := range slides {
		xml := studioSlideXML(i, len(slides), title, s, theme)
		if s.Layout == "table" {
			xml = strings.Replace(xml, `</p:spTree>`, studioTableXML(100, tables[i], theme)+`</p:spTree>`, 1)
		}
		replacements[fmt.Sprintf("ppt/slides/slide%d.xml", i+1)] = xml
		// Each slide points at its own branded layout part so PowerPoint's
		// layout editor reflects the real per-page design.
		rels := fmt.Sprintf(`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slideLayout" Target="../slideLayouts/slideLayout%d.xml"/>`, StudioLayoutIndex(s.Layout))
		if strings.TrimSpace(s.Notes) != "" {
			rels += fmt.Sprintf(`<Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/notesSlide" Target="../notesSlides/notesSlide%d.xml"/>`, i+1)
		}
		replacements[fmt.Sprintf("ppt/slides/_rels/slide%d.xml.rels", i+1)] = xmlDecl + `
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` + rels + `</Relationships>`
	}
	for _, f := range zr.File {
		body, ok := replacements[f.Name]
		if !ok {
			if err := zw.Copy(f); err != nil {
				return nil, err
			}
			continue
		}
		w, err := zw.Create(f.Name)
		if err != nil {
			return nil, err
		}
		if _, err = w.Write([]byte(body)); err != nil {
			return nil, err
		}
	}
	// Layout parts beyond the first do not exist in the classic base package
	// and are appended after the copy loop.
	for i, def := range studioLayoutDefs {
		if i == 0 {
			continue
		}
		body := slideLayoutXMLFor(def, theme)
		w, err := zw.Create(fmt.Sprintf("ppt/slideLayouts/slideLayout%d.xml", i+1))
		if err != nil {
			return nil, err
		}
		if _, err = w.Write([]byte(body)); err != nil {
			return nil, err
		}
		w, err = zw.Create(fmt.Sprintf("ppt/slideLayouts/_rels/slideLayout%d.xml.rels", i+1))
		if err != nil {
			return nil, err
		}
		if _, err = w.Write([]byte(slideLayoutRels)); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	if err := ValidatePptx(b.Bytes()); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func studioTableXML(id int, rows [][]string, theme SlideTheme) string {
	const x, y, w, h = 520000, 1950000, 11150000, 4100000
	cols := len(rows[0])
	rowHeight := h / len(rows)
	var b strings.Builder
	fmt.Fprintf(&b, `<p:graphicFrame><p:nvGraphicFramePr><p:cNvPr id="%d" name="DataTable"/><p:cNvGraphicFramePr/><p:nvPr/></p:nvGraphicFramePr><p:xfrm><a:off x="%d" y="%d"/><a:ext cx="%d" cy="%d"/></p:xfrm><a:graphic><a:graphicData uri="http://schemas.openxmlformats.org/drawingml/2006/table"><a:tbl><a:tblPr firstRow="1" bandRow="1"/><a:tblGrid>`, id, x, y, w, h)
	for c := 0; c < cols; c++ {
		width := w / cols
		if c == cols-1 {
			width = w - (cols-1)*(w/cols)
		}
		fmt.Fprintf(&b, `<a:gridCol w="%d"/>`, width)
	}
	b.WriteString(`</a:tblGrid>`)
	for r, row := range rows {
		fmt.Fprintf(&b, `<a:tr h="%d">`, rowHeight)
		for _, v := range row {
			fill, color := theme.White, theme.Ink
			if r == 0 {
				fill, color = theme.Navy, theme.White
			} else if r%2 == 0 {
				fill = theme.Soft
			}
			b.WriteString(`<a:tc><a:txBody><a:bodyPr wrap="square"/><a:lstStyle/>`)
			b.WriteString(pptxPara("l", theme.fontRun(v, color, 1500, r == 0)))
			fmt.Fprintf(&b, `</a:txBody><a:tcPr marL="130000" marR="130000" marT="60000" marB="60000" anchor="ctr"><a:solidFill><a:srgbClr val="%s"/></a:solidFill></a:tcPr></a:tc>`, fill)
		}
		b.WriteString(`</a:tr>`)
	}
	b.WriteString(`</a:tbl></a:graphicData></a:graphic></p:graphicFrame>`)
	return b.String()
}

func studioSlideXML(index, total int, deckTitle string, s SlideSpec, theme SlideTheme) string {
	if s.Layout == "cover" || s.Layout == "closing" {
		all := []string{}
		if s.Subtitle != "" {
			all = append(all, s.Subtitle)
		}
		all = append(all, s.Bullets...)
		s.Subtitle = strings.Join(all, "  ·  ")
		return pptxTitleSlide(s, deckTitle, theme)
	}
	if s.Layout == "section" {
		all := []string{}
		if s.Subtitle != "" {
			all = append(all, s.Subtitle)
		}
		all = append(all, s.Bullets...)
		s.Bullets = []string{strings.Join(all, "  ·  ")}
		return pptxSectionSlide(index, total, s, theme)
	}
	if s.Layout == "content" {
		if s.Subtitle != "" {
			s.Bullets = append([]string{s.Subtitle}, s.Bullets...)
		}
		return pptxContentSlide(index, total, deckTitle, s, theme)
	}
	var b strings.Builder
	b.WriteString(pptxTreeOpen())
	b.WriteString(pptxBg(theme.Paper))
	b.WriteString(pptxSpTreeOpen())
	b.WriteString(pptxRect(2, "Header", theme.Navy, 0, 0, pptxW, 1150000))
	b.WriteString(pptxTextBox(3, "Title", 520000, 260000, 11000000, 720000, pptxPara("l", theme.fontRun(s.Title, theme.White, theme.titleSize(), true))))
	id := 4
	textBox := func(name, text string, x, y, w, h, sz int, bold bool, color string) {
		b.WriteString(pptxTextBox(id, name, x, y, w, h, pptxPara("l", theme.fontRun(text, color, sz, bold))))
		id++
	}
	if s.Subtitle != "" {
		textBox("Subtitle", s.Subtitle, 520000, 1280000, 11000000, 600000, 1700, false, theme.Muted)
	}
	y := 1900000
	switch s.Layout {
	case "two-column", "comparison":
		left, right := comparisonColumns(s)
		for col, items := range [][]string{left, right} {
			x := 520000 + col*5670000
			b.WriteString(pptxRect(id, fmt.Sprintf("Column%d", col+1), theme.White, x, y, 5340000, 3940000))
			id++
			for j, item := range items {
				textBox(fmt.Sprintf("Item%d", j+1+col*len(left)), item, x+240000, y+260000+j*570000, 4860000, 520000, 1800, j == 0 && s.Layout == "comparison", theme.Ink)
			}
		}
	case "quote":
		textBox("Quote", strings.Join(s.Bullets, "\n"), 1100000, 2200000, 9900000, 3200000, 2800, false, theme.Navy)
	case "metrics":
		for j, metric := range metricCards(s) {
			x := 520000 + (j%3)*3770000
			yy := y + (j/3)*1860000
			b.WriteString(pptxRect(id, fmt.Sprintf("MetricCard%d", j+1), theme.White, x, yy, 3440000, 1580000))
			id++
			textBox(fmt.Sprintf("Metric%d", j+1), metric.value, x+220000, yy+260000, 3000000, 600000, 3200, true, theme.Teal)
			if metric.label != "" {
				textBox(fmt.Sprintf("MetricLabel%d", j+1), metric.label, x+220000, yy+980000, 3000000, 440000, 1500, false, theme.Muted)
			}
		}
	case "timeline":
		for j, item := range s.Bullets {
			yy := y + j*650000
			b.WriteString(pptxRect(id, fmt.Sprintf("Step%d", j+1), theme.Teal, 650000, yy, 130000, 520000))
			id++
			textBox(fmt.Sprintf("Timeline%d", j+1), item, 1080000, yy, 10400000, 560000, 2000, false, theme.Ink)
		}
	case "agenda":
		for j, item := range s.Bullets {
			col, row := j/6, j%6
			x := 520000 + col*5670000
			yy := y + row*600000
			textBox(fmt.Sprintf("Number%d", j+1), fmt.Sprintf("%02d", j+1), x, yy, 620000, 520000, 2000, true, theme.Teal)
			textBox(fmt.Sprintf("Agenda%d", j+1), item, x+750000, yy, 4350000, 520000, 1800, false, theme.Ink)
		}
	case "diagram":
		b.WriteString(studioDiagramXMLShapes(s.Diagram, theme, &id))
	}
	b.WriteString(pptxRect(id, "FooterRule", theme.Soft, 520000, 6280000, 11000000, 12700))
	id++
	textBox("Footer", deckTitle, 520000, 6380000, 8000000, 320000, 1100, false, theme.Muted)
	textBox("Page", fmt.Sprintf("%d / %d", index+1, total), 10600000, 6380000, 1000000, 320000, 1100, false, theme.Muted)
	b.WriteString(pptxTreeClose())
	return b.String()
}

func comparisonColumns(s SlideSpec) (left, right []string) {
	if s.Comparison != nil {
		return s.Comparison.Left, s.Comparison.Right
	}
	mid := (len(s.Bullets) + 1) / 2
	return s.Bullets[:mid], s.Bullets[mid:]
}

// validateSlideDiagram is the renderer's defense line: the payload must be
// positioned and internally consistent before any shape is emitted.
func validateSlideDiagram(d *SlideDiagram) error {
	if d == nil {
		return fmt.Errorf("officetools: diagram slide requires a diagram payload")
	}
	if d.Direction != "down" && d.Direction != "right" {
		return fmt.Errorf("officetools: diagram direction must be down or right")
	}
	if len(d.Boxes) < 2 || len(d.Boxes) > 12 {
		return fmt.Errorf("officetools: diagram needs 2-12 boxes")
	}
	if len(d.Links) < 1 || len(d.Links) > 16 {
		return fmt.Errorf("officetools: diagram needs 1-16 links")
	}
	ids := make(map[string]bool, len(d.Boxes))
	for _, box := range d.Boxes {
		if box.ID == "" {
			return fmt.Errorf("officetools: diagram box id is required")
		}
		if box.W <= 0 || box.H <= 0 {
			return fmt.Errorf("officetools: diagram box %q has invalid geometry", box.ID)
		}
		if ids[box.ID] {
			return fmt.Errorf("officetools: duplicate diagram box id %q", box.ID)
		}
		ids[box.ID] = true
	}
	for _, link := range d.Links {
		if !ids[link.From] {
			return fmt.Errorf("officetools: diagram link references unknown source %q", link.From)
		}
		if !ids[link.To] {
			return fmt.Errorf("officetools: diagram link references unknown target %q", link.To)
		}
		if link.FromSide < 0 || link.FromSide > 3 || link.ToSide < 0 || link.ToSide > 3 {
			return fmt.Errorf("officetools: diagram link side must be 0-3")
		}
	}
	return nil
}

// diagramAnchor returns connection-site coordinates for a box side using the
// ECMA-376 preset order: 0 left, 1 top, 2 right, 3 bottom.
func diagramAnchor(b SlideDiagramBox, side int) (int64, int64) {
	switch side {
	case 1:
		return b.X + b.W/2, b.Y
	case 2:
		return b.X + b.W, b.Y + b.H/2
	case 3:
		return b.X + b.W/2, b.Y + b.H
	default:
		return b.X, b.Y + b.H/2
	}
}

// diagramTextSize picks a font that keeps the label on one readable line for
// the box width available at this lane size.
func diagramTextSize(box SlideDiagramBox) int {
	sz := 1200
	if box.H >= 500000 {
		switch runes := utf8.RuneCountInString(strings.TrimSpace(box.Label)); {
		case runes <= 6:
			sz = 1600
		case runes <= 8:
			sz = 1400
		}
	}
	if box.Kind == "decision" && sz > 1000 {
		sz = 1000
	}
	return sz
}

// studioDiagramXMLShapes emits node boxes as plain p:sp, edges as snapped
// p:cxnSp connectors, and edge labels as small chips. Everything stays a
// native editable shape; the layout already guarantees zero overlap.
func studioDiagramXMLShapes(d *SlideDiagram, theme SlideTheme, id *int) string {
	if d == nil {
		return ""
	}
	boxes := make(map[string]SlideDiagramBox, len(d.Boxes))
	shapeIDs := make(map[string]int, len(d.Boxes))
	var b strings.Builder
	for _, box := range d.Boxes {
		boxes[box.ID] = box
		prst, fill, text, line := "roundRect", theme.White, theme.Ink, fmt.Sprintf(`<a:ln w="15875"><a:solidFill><a:srgbClr val="%s"/></a:solidFill></a:ln>`, theme.Navy)
		bold := false
		switch box.Kind {
		case "start":
			fill, text, bold, line = theme.Teal, theme.White, true, `<a:ln><a:noFill/></a:ln>`
		case "end":
			fill, text, bold, line = theme.Navy, theme.White, true, `<a:ln><a:noFill/></a:ln>`
		case "decision":
			prst = "flowChartDecision"
			line = fmt.Sprintf(`<a:ln w="19050"><a:solidFill><a:srgbClr val="%s"/></a:solidFill></a:ln>`, theme.Gold)
		}
		sz := diagramTextSize(box)
		fmt.Fprintf(&b, `<p:sp><p:nvSpPr><p:cNvPr id="%d" name="Node_%s"/><p:cNvSpPr/><p:nvPr/></p:nvSpPr><p:spPr><a:xfrm><a:off x="%d" y="%d"/><a:ext cx="%d" cy="%d"/></a:xfrm><a:prstGeom prst="%s"><a:avLst/></a:prstGeom><a:solidFill><a:srgbClr val="%s"/></a:solidFill>%s</p:spPr><p:txBody><a:bodyPr wrap="square" lIns="22860" tIns="22860" rIns="22860" bIns="22860" anchor="ctr"><a:noAutofit/></a:bodyPr><a:lstStyle/>%s</p:txBody></p:sp>`,
			*id, xmlEscape(box.ID), box.X, box.Y, box.W, box.H, prst, fill, line, pptxPara("ctr", theme.fontRun(box.Label, text, sz, bold)))
		shapeIDs[box.ID] = *id
		*id++
	}
	for _, link := range d.Links {
		from, to := boxes[link.From], boxes[link.To]
		x1, y1 := diagramAnchor(from, link.FromSide)
		x2, y2 := diagramAnchor(to, link.ToSide)
		flipH, flipV := "", ""
		x, y := x1, y1
		if x2 < x1 {
			flipH = ` flipH="1"`
			x = x2
		}
		if y2 < y1 {
			flipV = ` flipV="1"`
			y = y2
		}
		cx, cy := x2-x1, y2-y1
		if cx < 0 {
			cx = -cx
		}
		if cy < 0 {
			cy = -cy
		}
		fmt.Fprintf(&b, `<p:cxnSp><p:nvCxnSpPr><p:cNvPr id="%d" name="Link_%s_%s"/><p:cNvCxnSpPr><a:stCxn id="%d" idx="%d"/><a:endCxn id="%d" idx="%d"/></p:cNvCxnSpPr><p:nvPr/></p:nvCxnSpPr><p:spPr><a:xfrm%s%s><a:off x="%d" y="%d"/><a:ext cx="%d" cy="%d"/></a:xfrm><a:prstGeom prst="straightConnector1"><a:avLst/></a:prstGeom><a:ln w="15875"><a:solidFill><a:srgbClr val="%s"/></a:solidFill><a:tailEnd type="triangle"/></a:ln></p:spPr></p:cxnSp>`,
			*id, xmlEscape(link.From), xmlEscape(link.To), shapeIDs[link.From], link.FromSide, shapeIDs[link.To], link.ToSide, flipH, flipV, x, y, cx, cy, theme.Muted)
		*id++
		if strings.TrimSpace(link.Label) == "" {
			continue
		}
		runes := int64(utf8.RuneCountInString(strings.TrimSpace(link.Label)))
		chipW := runes*160000 + 120000
		if chipW < 640000 {
			chipW = 640000
		}
		if chipW > 1440000 {
			chipW = 1440000
		}
		var chipY int64
		if d.Direction == "down" {
			chipY = (y1+y2)/2 - 140000
		} else {
			chipY = from.Y - 296000
		}
		chipX := (x1+x2)/2 - chipW/2
		if chipX < 200000 {
			chipX = 200000
		}
		if max := pptxW - 200000 - chipW; chipX > max {
			chipX = max
		}
		fmt.Fprintf(&b, `<p:sp><p:nvSpPr><p:cNvPr id="%d" name="LinkLabel_%s_%s"/><p:cNvSpPr txBox="1"/><p:nvPr/></p:nvSpPr><p:spPr><a:xfrm><a:off x="%d" y="%d"/><a:ext cx="%d" cy="280000"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom><a:solidFill><a:srgbClr val="%s"/></a:solidFill><a:ln><a:noFill/></a:ln></p:spPr><p:txBody><a:bodyPr wrap="square" lIns="22860" tIns="22860" rIns="22860" bIns="22860" anchor="ctr"><a:noAutofit/></a:bodyPr><a:lstStyle/>%s</p:txBody></p:sp>`,
			*id, xmlEscape(link.From), xmlEscape(link.To), chipX, chipY, chipW, theme.Paper, pptxPara("ctr", theme.fontRun(strings.TrimSpace(link.Label), theme.Muted, 1150, false)))
		*id++
	}
	return b.String()
}

type metricCard struct{ value, label string }

func metricCards(s SlideSpec) []metricCard {
	if len(s.Metrics) > 0 {
		out := make([]metricCard, len(s.Metrics))
		for i, m := range s.Metrics {
			label := m.Label
			if m.Unit != "" && !strings.Contains(label, m.Unit) {
				label = strings.TrimSpace(m.Label + "（" + m.Unit + "）")
			}
			out[i] = metricCard{value: m.Value, label: label}
		}
		return out
	}
	out := make([]metricCard, 0, len(s.Bullets))
	for _, item := range s.Bullets {
		pair := strings.SplitN(item, "|", 2)
		card := metricCard{value: pair[0]}
		if len(pair) > 1 {
			card.label = pair[1]
		}
		out = append(out, card)
	}
	return out
}
