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
		case "cover", "section", "content", "two-column", "comparison", "quote", "metrics", "timeline", "agenda", "closing", "table", "conclusion", "trend", "structure", "process", "evidence":
		default:
			return nil, fmt.Errorf("officetools: unsupported studio slide layout %q", layout)
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
	replacements := map[string]string{
		"ppt/theme/theme1.xml": ThemeXMLFor(theme.Latin, theme.East, theme.Navy, theme.Teal, theme.Gold, theme.Paper, theme.Ink, theme.White, theme.Soft),
	}
	for i, s := range slides {
		xml := studioSlideXML(i, len(slides), title, s, theme)
		if s.Layout == "table" {
			xml = strings.Replace(xml, `</p:spTree>`, studioTableXML(100, tables[i], theme)+`</p:spTree>`, 1)
		}
		replacements[fmt.Sprintf("ppt/slides/slide%d.xml", i+1)] = xml
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
