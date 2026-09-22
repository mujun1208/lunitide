package officetools

import (
	"archive/zip"
	"bytes"
	"fmt"
	"strings"
	"unicode/utf8"
)

// GenQuietPptx is the no-template deck. Each slide is a different composition
// on one paper-and-ink theme. It does not reuse the navy header skeleton.
func GenQuietPptx(title string, slides []SlideSpec) ([]byte, error) {
	slides = append([]SlideSpec(nil), slides...)
	for i := range slides {
		slides[i].Layout = quietLayout(i, len(slides), slides[i])
	}
	baseSlides := make([]SlideSpec, len(slides))
	for i, s := range slides {
		baseSlides[i] = s
		baseSlides[i].Layout = "content"
		baseSlides[i].Metrics = nil
		baseSlides[i].Comparison = nil
	}
	base, err := GenPptx(title, baseSlides)
	if err != nil {
		return nil, err
	}
	theme := quietTheme()
	replacements := map[string]string{
		"ppt/theme/theme1.xml":              ThemeXMLFor(theme.Latin, theme.East, theme.Navy, theme.Teal, theme.Gold, theme.Paper, theme.Ink, theme.White, theme.Soft),
		"ppt/slideMasters/slideMaster1.xml": strings.Replace(slideMasterXML, `val="0B1F3A"`, `val="`+theme.Paper+`"`, 1),
	}
	for i, s := range slides {
		replacements[fmt.Sprintf("ppt/slides/slide%d.xml", i+1)] = quietSlideXML(i, len(slides), title, s, theme)
	}
	zr, err := zip.NewReader(bytes.NewReader(base), int64(len(base)))
	if err != nil {
		return nil, err
	}
	var b bytes.Buffer
	zw := zip.NewWriter(&b)
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

func quietTheme() SlideTheme {
	return SlideTheme{
		Navy: "1A1A1A", Teal: "1F4B45", Gold: "8A8178",
		Paper: "F6F4F1", Ink: "1A1A1A", Muted: "6B6560",
		White: "FFFCF9", Soft: "E6E1DA",
		Latin: "Calibri", East: "Microsoft YaHei",
	}
}

func quietLayout(index, total int, s SlideSpec) string {
	switch strings.ToLower(strings.TrimSpace(s.Layout)) {
	case "title", "cover":
		return "cover"
	case "section", "content", "agenda", "metrics", "comparison", "quote", "timeline", "closing":
		return strings.ToLower(strings.TrimSpace(s.Layout))
	}
	if len(s.Metrics) > 0 {
		return "metrics"
	}
	if s.Comparison != nil && (len(s.Comparison.Left) > 0 || len(s.Comparison.Right) > 0) {
		return "comparison"
	}
	if index == 0 {
		return "cover"
	}
	if index == total-1 && quietClosingTitle(s.Title) {
		return "closing"
	}
	if len(s.Bullets) >= 4 && quietShortItems(s.Bullets) {
		return "agenda"
	}
	return "content"
}

func quietClosingTitle(title string) bool {
	for _, word := range []string{"谢谢", "感谢", "结语", "下一步", "收尾"} {
		if strings.Contains(title, word) {
			return true
		}
	}
	return false
}

func quietShortItems(items []string) bool {
	for _, item := range items {
		if utf8.RuneCountInString(strings.TrimSpace(item)) > 18 {
			return false
		}
	}
	return true
}

func quietLines(items []string, max int) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		out = append(out, item)
		if max > 0 && len(out) >= max {
			break
		}
	}
	return out
}

func quietStack(n, top, bottom, prefer int) (slot, size int) {
	slot, size = prefer, 1800
	if n < 1 {
		return slot, size
	}
	if room := bottom - top; n*slot > room && room > 0 {
		slot = room / n
	}
	if slot < 280000 {
		slot = 280000
	}
	switch {
	case slot < 400000:
		size = 1400
	case slot < 520000:
		size = 1600
	}
	return slot, size
}

func quietSlideXML(index, total int, deckTitle string, s SlideSpec, theme SlideTheme) string {
	switch s.Layout {
	case "cover":
		return quietCover(s, deckTitle, theme, false)
	case "closing":
		if len(quietLines(s.Bullets, 12)) > 0 {
			return quietClosing(index, total, deckTitle, s, theme)
		}
		return quietCover(s, deckTitle, theme, true)
	case "section":
		return quietSection(index, total, s, theme)
	case "agenda":
		return quietAgenda(index, total, deckTitle, s, theme)
	case "metrics":
		return quietMetrics(index, total, deckTitle, s, theme)
	case "comparison":
		return quietComparison(index, total, deckTitle, s, theme)
	case "quote":
		return quietQuote(index, total, deckTitle, s, theme)
	case "timeline":
		return quietTimeline(index, total, deckTitle, s, theme)
	default:
		return quietContent(index, total, deckTitle, s, theme)
	}
}

type quietPen struct {
	b     strings.Builder
	id    int
	theme SlideTheme
}

func (p *quietPen) open(paper string) {
	p.b.WriteString(pptxTreeOpen())
	p.b.WriteString(pptxBg(paper))
	p.b.WriteString(pptxSpTreeOpen())
	p.id = 2
}

func (p *quietPen) close() string {
	p.b.WriteString(pptxTreeClose())
	return p.b.String()
}

func (p *quietPen) rect(name, fill string, x, y, w, h int) {
	p.b.WriteString(pptxRect(p.id, name, fill, x, y, w, h))
	p.id++
}

func (p *quietPen) text(name, text string, x, y, w, h, sz int, bold bool, color, align string) {
	if strings.TrimSpace(text) == "" {
		return
	}
	p.b.WriteString(pptxTextBoxFixed(p.id, name, x, y, w, h, pptxPara(align, p.theme.fontRun(text, color, sz, bold))))
	p.id++
}

func (p *quietPen) rule(name string, x, y, w int) {
	p.rect(name, p.theme.Teal, x, y, w, 19050)
}

func (p *quietPen) footer(deck string, index, total int, source string) {
	p.rect("FooterRule", p.theme.Soft, 640000, 6320000, 10900000, 9525)
	left := strings.TrimSpace(source)
	if left != "" {
		left = "来源：" + left
	} else {
		left = deck
	}
	p.text("Footer", left, 640000, 6420000, 7800000, 280000, 1100, false, p.theme.Muted, "l")
	p.text("Page", fmt.Sprintf("%d / %d", index+1, total), 9000000, 6420000, 2540000, 280000, 1100, false, p.theme.Muted, "r")
}

func quietTitleSize(text string, base int) int {
	n := utf8.RuneCountInString(strings.TrimSpace(text))
	switch {
	case n > 28:
		return base - 1400
	case n > 16:
		return base - 600
	default:
		return base
	}
}

func quietCover(s SlideSpec, deckTitle string, theme SlideTheme, closing bool) string {
	var p quietPen
	p.theme = theme
	p.open(theme.Paper)
	kicker := deckTitle
	if closing {
		kicker = "收尾"
	}
	p.text("Kicker", kicker, 860000, 1680000, 10000000, 360000, 1400, false, theme.Teal, "l")
	p.text("Title", s.Title, 860000, 2140000, 10400000, 1400000, quietTitleSize(s.Title, 4000), true, theme.Ink, "l")
	p.rule("Rule", 860000, 3680000, 1680000)
	sub := strings.TrimSpace(s.Subtitle)
	p.text("Subtitle", sub, 860000, 3900000, 10000000, 520000, 1800, false, theme.Muted, "l")
	lineY := 3900000
	if sub != "" {
		lineY = 4520000
	}
	lines := quietLines(s.Bullets, 12)
	perCol := len(lines)
	if perCol > 4 {
		perCol = (len(lines) + 1) / 2
	}
	slot, size := quietStack(perCol, lineY, 6000000, 380000)
	for i, item := range lines {
		col, row := 0, i
		if len(lines) > 4 && i >= perCol {
			col, row = 1, i-perCol
		}
		p.text(fmt.Sprintf("CoverLine%d", i+1), item, 860000+col*5200000, lineY+row*slot, 4800000, slot-40000, size, false, theme.Muted, "l")
	}
	if strings.TrimSpace(s.Source) != "" {
		p.text("Source", "来源："+strings.TrimSpace(s.Source), 860000, 6200000, 10000000, 320000, 1200, false, theme.Muted, "l")
	}
	return p.close()
}

func quietClosing(index, total int, deckTitle string, s SlideSpec, theme SlideTheme) string {
	var p quietPen
	p.theme = theme
	p.open(theme.Paper)
	p.text("Kicker", "收尾", 720000, 420000, 4000000, 320000, 1400, false, theme.Teal, "l")
	p.text("Title", s.Title, 720000, 780000, 10700000, 720000, quietTitleSize(s.Title, 3200), true, theme.Ink, "l")
	p.rule("Rule", 720000, 1600000, 1680000)
	y := 1860000
	if strings.TrimSpace(s.Subtitle) != "" {
		p.text("Lead", s.Subtitle, 720000, y, 10700000, 400000, 1600, false, theme.Muted, "l")
		y += 480000
	}
	items := quietLines(s.Bullets, 12)
	slot, size := quietStack(len(items), y, 6100000, 560000)
	for i, item := range items {
		yy := y + i*slot
		p.text(fmt.Sprintf("Number%d", i+1), fmt.Sprintf("%02d", i+1), 720000, yy, 700000, slot-60000, 1800, true, theme.Teal, "l")
		p.text(fmt.Sprintf("Step%d", i+1), item, 1560000, yy, 9800000, slot-60000, size, false, theme.Ink, "l")
	}
	p.footer(deckTitle, index, total, s.Source)
	return p.close()
}

func quietSection(index, total int, s SlideSpec, theme SlideTheme) string {
	var p quietPen
	p.theme = theme
	p.open(theme.Paper)
	p.text("Index", fmt.Sprintf("%02d", index+1), 860000, 1680000, 4000000, 780000, 4400, true, theme.Gold, "l")
	p.text("Kicker", fmt.Sprintf("%02d  /  %02d", index+1, total), 860000, 2520000, 10000000, 360000, 1400, false, theme.Teal, "l")
	p.text("Title", s.Title, 860000, 3000000, 10400000, 1100000, quietTitleSize(s.Title, 3600), true, theme.Ink, "l")
	lead := strings.TrimSpace(s.Subtitle)
	if lead == "" {
		if lines := quietLines(s.Bullets, 1); len(lines) > 0 {
			lead = lines[0]
		}
	}
	p.text("Lead", lead, 860000, 4240000, 10000000, 700000, 1800, false, theme.Muted, "l")
	return p.close()
}

func quietContent(index, total int, deckTitle string, s SlideSpec, theme SlideTheme) string {
	var p quietPen
	p.theme = theme
	p.open(theme.Paper)
	p.text("Kicker", fmt.Sprintf("%02d", index+1), 720000, 420000, 2000000, 320000, 1400, false, theme.Teal, "l")
	p.text("Title", s.Title, 720000, 780000, 10700000, 800000, quietTitleSize(s.Title, 2800), true, theme.Ink, "l")
	p.rule("TitleRule", 720000, 1680000, 2200000)
	y := 1960000
	if strings.TrimSpace(s.Subtitle) != "" {
		p.text("Lead", s.Subtitle, 720000, y, 10700000, 480000, 1600, false, theme.Muted, "l")
		y += 560000
	}
	items := quietLines(s.Bullets, 0)
	slot, sz := 620000, 1800
	if n := len(items); n > 0 && y+n*slot > 6100000 {
		slot = (6100000 - y) / n
		if slot < 280000 {
			slot = 280000
		}
		if slot < 520000 {
			sz = 1400
		}
		if slot < 400000 {
			sz = 1200
		}
	}
	boxH := slot - 40000
	if boxH < 180000 {
		boxH = 180000
	}
	for i, item := range items {
		yy := y + i*slot
		p.rect(fmt.Sprintf("Mark%d", i+1), theme.Teal, 760000, yy+80000, 90000, 90000)
		p.text(fmt.Sprintf("Point%d", i+1), item, 1040000, yy, 10000000, boxH, sz, false, theme.Ink, "l")
	}
	p.footer(deckTitle, index, total, s.Source)
	return p.close()
}

func quietAgenda(index, total int, deckTitle string, s SlideSpec, theme SlideTheme) string {
	var p quietPen
	p.theme = theme
	p.open(theme.Paper)
	p.text("Title", s.Title, 720000, 480000, 10700000, 700000, 2800, true, theme.Ink, "l")
	p.rule("TitleRule", 720000, 1280000, 1600000)
	items := quietLines(s.Bullets, 0)
	rows := 4
	if n := len(items); n > 8 {
		rows = (n + 1) / 2
	}
	gap := 1000000
	if rows > 4 {
		gap = 3600000 / rows
	}
	for i, item := range items {
		col, row := 0, i
		if i >= rows {
			col, row = 1, i-rows
		}
		x := 720000 + col*5600000
		y := 1680000 + row*gap
		h := 420000
		if gap < h+80000 {
			h = gap - 80000
		}
		p.text(fmt.Sprintf("Number%d", i+1), fmt.Sprintf("%02d", i+1), x, y, 700000, h, 2000, true, theme.Teal, "l")
		p.text(fmt.Sprintf("Item%d", i+1), item, x+820000, y, 4200000, h, 1800, false, theme.Ink, "l")
	}
	p.footer(deckTitle, index, total, s.Source)
	return p.close()
}

func quietMetrics(index, total int, deckTitle string, s SlideSpec, theme SlideTheme) string {
	var p quietPen
	p.theme = theme
	p.open(theme.Paper)
	p.text("Title", s.Title, 720000, 480000, 10700000, 700000, 2800, true, theme.Ink, "l")
	cards := metricCards(s)
	cols := 2
	if len(cards) == 1 {
		cols = 1
	} else if len(cards) > 4 {
		cols = 3
	}
	rows := 1
	if cols > 0 && len(cards) > 0 {
		rows = (len(cards) + cols - 1) / cols
	}
	rowH := 2000000
	if rows > 0 && 1680000+rows*rowH > 6100000 {
		rowH = (6100000 - 1680000) / rows
	}
	valueSz := 4000
	if cols == 3 || rowH < 1600000 {
		valueSz = 2800
	}
	for i, card := range cards {
		span := 5600000
		if cols == 3 {
			span = 3700000
		}
		x := 720000 + (i%cols)*span
		y := 1680000 + (i/cols)*rowH
		p.rule(fmt.Sprintf("MetricRule%d", i+1), x, y, 1200000)
		p.text(fmt.Sprintf("Value%d", i+1), card.value, x, y+160000, span-300000, 700000, valueSz, true, theme.Ink, "l")
		p.text(fmt.Sprintf("Label%d", i+1), card.label, x, y+rowH/2, span-300000, 360000, 1600, false, theme.Muted, "l")
	}
	p.footer(deckTitle, index, total, s.Source)
	return p.close()
}

func quietComparison(index, total int, deckTitle string, s SlideSpec, theme SlideTheme) string {
	var p quietPen
	p.theme = theme
	p.open(theme.Paper)
	p.text("Title", s.Title, 720000, 420000, 10700000, 640000, 2800, true, theme.Ink, "l")
	p.rect("Divider", theme.Soft, 6000000, 1500000, 9525, 4500000)
	left, right := comparisonColumns(s)
	left, right = quietLines(left, 6), quietLines(right, 6)
	for col, items := range [][]string{left, right} {
		x := 720000 + col*5900000
		slot, size := quietStack(len(items), 1600000, 6100000, 700000)
		for i, item := range items {
			sz, bold, color := size, false, theme.Ink
			if i == 0 {
				sz, bold, color = size+200, true, theme.Teal
			}
			p.text(fmt.Sprintf("Col%dItem%d", col+1, i+1), item, x, 1600000+i*slot, 5000000, slot-80000, sz, bold, color, "l")
		}
	}
	p.footer(deckTitle, index, total, s.Source)
	return p.close()
}

func quietQuote(index, total int, deckTitle string, s SlideSpec, theme SlideTheme) string {
	var p quietPen
	p.theme = theme
	p.open(theme.Paper)
	p.rule("QuoteMark", 900000, 1700000, 900000)
	quote := strings.Join(s.Bullets, " ")
	who := strings.TrimSpace(s.Subtitle)
	if strings.TrimSpace(quote) == "" {
		quote = who
		who = ""
	}
	p.text("Quote", quote, 900000, 2000000, 10200000, 2200000, quietTitleSize(quote, 2800), false, theme.Ink, "l")
	p.text("Attribution", who, 900000, 4400000, 10200000, 400000, 1600, false, theme.Muted, "l")
	p.footer(deckTitle, index, total, s.Source)
	return p.close()
}

func quietTimeline(index, total int, deckTitle string, s SlideSpec, theme SlideTheme) string {
	var p quietPen
	p.theme = theme
	p.open(theme.Paper)
	p.text("Title", s.Title, 720000, 420000, 10700000, 640000, 2800, true, theme.Ink, "l")
	items := quietLines(s.Bullets, 12)
	gap, size := quietStack(len(items), 1600000, 6100000, 860000)
	if len(items) > 0 {
		spine := len(items)*gap - 200000
		if spine < 400000 {
			spine = 400000
		}
		p.rect("Spine", theme.Soft, 980000, 1680000, 9525, spine)
	}
	for i, item := range items {
		y := 1600000 + i*gap
		p.rect(fmt.Sprintf("Dot%d", i+1), theme.Teal, 900000, y+60000, 140000, 140000)
		p.text(fmt.Sprintf("Step%d", i+1), item, 1300000, y, 9800000, gap-60000, size, false, theme.Ink, "l")
	}
	p.footer(deckTitle, index, total, s.Source)
	return p.close()
}
