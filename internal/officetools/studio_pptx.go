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
func GenStudioPptxWithTables(title string, slides []SlideSpec, tables map[int][][]string) ([]byte, error) {
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
		case "cover", "section", "content", "two-column", "comparison", "quote", "metrics", "timeline", "agenda", "closing", "table":
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
		if (layout == "metrics" || layout == "timeline") && len(s.Bullets) > 6 {
			return nil, fmt.Errorf("%w: %s supports at most six items", ErrLimit, layout)
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
	replacements := map[string]string{}
	for i, s := range slides {
		xml := studioSlideXML(i, len(slides), title, s)
		if s.Layout == "table" {
			xml = strings.Replace(xml, `</p:spTree>`, studioTableXML(100, tables[i])+`</p:spTree>`, 1)
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

func studioTableXML(id int, rows [][]string) string {
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
			fill, color := clrWhite, clrInk
			if r == 0 {
				fill, color = clrNavy, clrWhite
			} else if r%2 == 0 {
				fill = clrSoft
			}
			b.WriteString(`<a:tc><a:txBody><a:bodyPr wrap="square"/><a:lstStyle/>`)
			b.WriteString(pptxPara("l", pptxFontRun(v, color, 1500, r == 0)))
			fmt.Fprintf(&b, `</a:txBody><a:tcPr marL="130000" marR="130000" marT="60000" marB="60000" anchor="ctr"><a:solidFill><a:srgbClr val="%s"/></a:solidFill></a:tcPr></a:tc>`, fill)
		}
		b.WriteString(`</a:tr>`)
	}
	b.WriteString(`</a:tbl></a:graphicData></a:graphic></p:graphicFrame>`)
	return b.String()
}

func studioSlideXML(index, total int, deckTitle string, s SlideSpec) string {
	if s.Layout == "cover" || s.Layout == "closing" {
		all := []string{}
		if s.Subtitle != "" {
			all = append(all, s.Subtitle)
		}
		all = append(all, s.Bullets...)
		s.Subtitle = strings.Join(all, "  ·  ")
		return pptxTitleSlide(s, deckTitle)
	}
	if s.Layout == "section" {
		all := []string{}
		if s.Subtitle != "" {
			all = append(all, s.Subtitle)
		}
		all = append(all, s.Bullets...)
		s.Bullets = []string{strings.Join(all, "  ·  ")}
		return pptxSectionSlide(index, total, s)
	}
	if s.Layout == "content" {
		if s.Subtitle != "" {
			s.Bullets = append([]string{s.Subtitle}, s.Bullets...)
		}
		return pptxContentSlide(index, total, deckTitle, s)
	}
	var b strings.Builder
	b.WriteString(pptxTreeOpen())
	b.WriteString(pptxBg(clrPaper))
	b.WriteString(pptxSpTreeOpen())
	b.WriteString(pptxRect(2, "Header", clrNavy, 0, 0, pptxW, 1150000))
	b.WriteString(pptxTextBox(3, "Title", 520000, 260000, 11000000, 720000, pptxPara("l", pptxFontRun(s.Title, clrWhite, 2700, true))))
	id := 4
	textBox := func(name, text string, x, y, w, h, sz int, bold bool, color string) {
		b.WriteString(pptxTextBox(id, name, x, y, w, h, pptxPara("l", pptxFontRun(text, color, sz, bold))))
		id++
	}
	if s.Subtitle != "" {
		textBox("Subtitle", s.Subtitle, 520000, 1280000, 11000000, 600000, 1700, false, clrMuted)
	}
	y := 1900000
	switch s.Layout {
	case "two-column", "comparison":
		mid := (len(s.Bullets) + 1) / 2
		for col := 0; col < 2; col++ {
			x := 520000 + col*5670000
			b.WriteString(pptxRect(id, fmt.Sprintf("Column%d", col+1), clrWhite, x, y, 5340000, 3940000))
			id++
			start, end := 0, mid
			if col == 1 {
				start, end = mid, len(s.Bullets)
			}
			for j := start; j < end; j++ {
				textBox(fmt.Sprintf("Item%d", j+1), s.Bullets[j], x+240000, y+260000+(j-start)*570000, 4860000, 520000, 1800, j == start && s.Layout == "comparison", clrInk)
			}
		}
	case "quote":
		textBox("Quote", strings.Join(s.Bullets, "\n"), 1100000, 2200000, 9900000, 3200000, 2800, false, clrNavy)
	case "metrics":
		for j, item := range s.Bullets {
			x := 520000 + (j%3)*3770000
			yy := y + (j/3)*1860000
			b.WriteString(pptxRect(id, fmt.Sprintf("MetricCard%d", j+1), clrWhite, x, yy, 3440000, 1580000))
			id++
			pair := strings.SplitN(item, "|", 2)
			textBox(fmt.Sprintf("Metric%d", j+1), pair[0], x+220000, yy+260000, 3000000, 600000, 3200, true, clrTeal)
			if len(pair) > 1 {
				textBox(fmt.Sprintf("MetricLabel%d", j+1), pair[1], x+220000, yy+980000, 3000000, 440000, 1500, false, clrMuted)
			}
		}
	case "timeline":
		for j, item := range s.Bullets {
			yy := y + j*650000
			b.WriteString(pptxRect(id, fmt.Sprintf("Step%d", j+1), clrTeal, 650000, yy, 130000, 520000))
			id++
			textBox(fmt.Sprintf("Timeline%d", j+1), item, 1080000, yy, 10400000, 560000, 2000, false, clrInk)
		}
	case "agenda":
		for j, item := range s.Bullets {
			col, row := j/6, j%6
			x := 520000 + col*5670000
			yy := y + row*600000
			textBox(fmt.Sprintf("Number%d", j+1), fmt.Sprintf("%02d", j+1), x, yy, 620000, 520000, 2000, true, clrTeal)
			textBox(fmt.Sprintf("Agenda%d", j+1), item, x+750000, yy, 4350000, 520000, 1800, false, clrInk)
		}
	}
	b.WriteString(pptxRect(id, "FooterRule", clrSoft, 520000, 6280000, 11000000, 12700))
	id++
	textBox("Footer", deckTitle, 520000, 6380000, 8000000, 320000, 1100, false, clrMuted)
	textBox("Page", fmt.Sprintf("%d / %d", index+1, total), 10600000, 6380000, 1000000, 320000, 1100, false, clrMuted)
	b.WriteString(pptxTreeClose())
	return b.String()
}
