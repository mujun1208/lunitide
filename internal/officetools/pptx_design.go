package officetools

import (
	"fmt"
	"strings"
)

const (
	pptxW    = 12192000
	pptxH    = 6858000
	clrNavy  = "0B1F3A"
	clrTeal  = "0D9488"
	clrGold  = "C9A227"
	clrPaper = "F4F6F8"
	clrInk   = "1F2937"
	clrMuted = "64748B"
	clrWhite = "FFFFFF"
	clrSoft  = "E2E8F0"
)

func pptxLayoutOf(index int, s SlideSpec) string {
	switch strings.ToLower(strings.TrimSpace(s.Layout)) {
	case "title", "cover":
		return "title"
	case "section":
		return "section"
	case "content":
		return "content"
	}
	if index == 0 {
		return "title"
	}
	if len(s.Bullets) == 0 {
		return "section"
	}
	return "content"
}

func pptxFontRun(text, color string, sz int, bold bool) string {
	return SlideTheme{}.fontRun(text, color, sz, bold)
}

func (theme SlideTheme) fontRun(text, color string, sz int, bold bool) string {
	weight := ""
	if bold {
		weight = ` b="1"`
	}
	latin, east := theme.Latin, theme.East
	if latin == "" {
		latin = "Calibri"
	}
	if east == "" {
		east = "Microsoft YaHei"
	}
	return fmt.Sprintf(`<a:r><a:rPr lang="zh-CN" altLang="en-US" sz="%d"%s dirty="0"><a:solidFill><a:srgbClr val="%s"/></a:solidFill><a:latin typeface="%s"/><a:ea typeface="%s"/><a:cs typeface="%s"/></a:rPr><a:t>%s</a:t></a:r>`, sz, weight, color, xmlEscape(latin), xmlEscape(east), xmlEscape(east), xmlEscape(text))
}

func pptxRect(id int, name, fill string, x, y, cx, cy int) string {
	return fmt.Sprintf(`<p:sp><p:nvSpPr><p:cNvPr id="%d" name="%s"/><p:cNvSpPr/><p:nvPr/></p:nvSpPr><p:spPr><a:xfrm><a:off x="%d" y="%d"/><a:ext cx="%d" cy="%d"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom><a:solidFill><a:srgbClr val="%s"/></a:solidFill><a:ln><a:noFill/></a:ln></p:spPr></p:sp>`, id, xmlEscape(name), x, y, cx, cy, fill)
}

func pptxTextBox(id int, name string, x, y, cx, cy int, paras string) string {
	return fmt.Sprintf(`<p:sp><p:nvSpPr><p:cNvPr id="%d" name="%s"/><p:cNvSpPr txBox="1"/><p:nvPr/></p:nvSpPr><p:spPr><a:xfrm><a:off x="%d" y="%d"/><a:ext cx="%d" cy="%d"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom><a:noFill/></p:spPr><p:txBody><a:bodyPr wrap="square" lIns="0" tIns="0" rIns="0" bIns="0"><a:spAutoFit/></a:bodyPr><a:lstStyle/>%s</p:txBody></p:sp>`, id, xmlEscape(name), x, y, cx, cy, paras)
}

func pptxPara(align, run string) string {
	algn := ""
	if align != "" {
		algn = ` algn="` + align + `"`
	}
	return `<a:p><a:pPr` + algn + `></a:pPr>` + run + `<a:endParaRPr lang="zh-CN"/></a:p>`
}

func pptxBulletPara(text string, theme SlideTheme) string {
	ink, teal := theme.Ink, theme.Teal
	if ink == "" {
		ink = clrInk
	}
	if teal == "" {
		teal = clrTeal
	}
	return `<a:p><a:pPr marL="342900" indent="-171450"><a:buFont typeface="Arial"/><a:buClr><a:srgbClr val="` + teal + `"/></a:buClr><a:buChar char="●"/><a:spcBef><a:spcPts val="1200"/></a:spcBef></a:pPr>` +
		theme.fontRun(text, ink, 1800, false) +
		`<a:endParaRPr lang="zh-CN"/></a:p>`
}

func pptxTreeOpen() string {
	return xmlDecl + `
<p:sld xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"><p:cSld>`
}

func pptxTreeClose() string {
	return `</p:spTree></p:cSld><p:clrMapOvr><a:masterClrMapping/></p:clrMapOvr></p:sld>`
}

func pptxBg(hex string) string {
	return `<p:bg><p:bgPr><a:solidFill><a:srgbClr val="` + hex + `"/></a:solidFill><a:effectLst/></p:bgPr></p:bg>`
}

func pptxSpTreeOpen() string {
	return `<p:spTree><p:nvGrpSpPr><p:cNvPr id="1" name=""/><p:cNvGrpSpPr/><p:nvPr/></p:nvGrpSpPr><p:grpSpPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="` + fmt.Sprint(pptxW) + `" cy="` + fmt.Sprint(pptxH) + `"/><a:chOff x="0" y="0"/><a:chExt cx="` + fmt.Sprint(pptxW) + `" cy="` + fmt.Sprint(pptxH) + `"/></a:xfrm></p:grpSpPr>`
}

func slideXML(index, total int, deckTitle string, s SlideSpec) string {
	theme := ClassicSlideTheme()
	switch pptxLayoutOf(index, s) {
	case "title":
		return pptxTitleSlide(s, deckTitle, theme)
	case "section":
		return pptxSectionSlide(index, total, s, theme)
	default:
		return pptxContentSlide(index, total, deckTitle, s, theme)
	}
}

func pptxTitleSlide(s SlideSpec, deckTitle string, theme SlideTheme) string {
	if theme.Navy == "" {
		theme = ClassicSlideTheme()
	}
	sub := strings.TrimSpace(s.Subtitle)
	if sub == "" && len(s.Bullets) > 0 {
		sub = strings.Join(s.Bullets, "  ·  ")
	}
	if sub == "" {
		sub = deckTitle
	}
	var b strings.Builder
	b.WriteString(pptxTreeOpen())
	b.WriteString(pptxBg(theme.Navy))
	b.WriteString(pptxSpTreeOpen())
	b.WriteString(pptxRect(2, "AccentBar", theme.Teal, 0, 0, 280000, pptxH))
	b.WriteString(pptxRect(3, "GoldRule", theme.Gold, 720000, 3520000, 2400000, 36000))
	b.WriteString(pptxTextBox(4, "Title", 720000, 2100000, 10700000, 1300000, pptxPara("l", theme.fontRun(s.Title, theme.White, 4000, true))))
	b.WriteString(pptxTextBox(5, "Subtitle", 720000, 3720000, 10700000, 900000, pptxPara("l", theme.fontRun(sub, "A5F3FC", 1800, false))))
	b.WriteString(pptxTextBox(6, "Brand", 720000, 6200000, 5000000, 360000, pptxPara("l", theme.fontRun("LUNITIDE  商务演示", theme.Gold, 1200, false))))
	b.WriteString(pptxMetricValueBoxes(7, s, theme, 4700000))
	b.WriteString(pptxTreeClose())
	return b.String()
}

func pptxSectionSlide(index, total int, s SlideSpec, theme SlideTheme) string {
	if theme.Navy == "" {
		theme = ClassicSlideTheme()
	}
	var b strings.Builder
	b.WriteString(pptxTreeOpen())
	b.WriteString(pptxBg(theme.Navy))
	b.WriteString(pptxSpTreeOpen())
	b.WriteString(pptxRect(2, "AccentBar", theme.Teal, 0, 0, 280000, pptxH))
	b.WriteString(pptxTextBox(3, "Kicker", 720000, 2400000, 10700000, 400000, pptxPara("l", theme.fontRun(fmt.Sprintf("0%d  /  %02d", index+1, total), theme.Gold, 1400, false))))
	b.WriteString(pptxTextBox(4, "Title", 720000, 2880000, 10700000, 1400000, pptxPara("l", theme.fontRun(s.Title, theme.White, 3600, true))))
	if len(s.Bullets) > 0 {
		b.WriteString(pptxTextBox(5, "Lead", 720000, 4400000, 10700000, 800000, pptxPara("l", theme.fontRun(s.Bullets[0], "A5F3FC", 1600, false))))
	}
	b.WriteString(pptxMetricValueBoxes(6, s, theme, 5300000))
	b.WriteString(pptxTreeClose())
	return b.String()
}

func pptxContentSlide(index, total int, deckTitle string, s SlideSpec, theme SlideTheme) string {
	if theme.Navy == "" {
		theme = ClassicSlideTheme()
	}
	var bullets strings.Builder
	if len(s.Bullets) == 0 {
		bullets.WriteString(`<a:p><a:endParaRPr lang="zh-CN"/></a:p>`)
	}
	for _, item := range s.Bullets {
		bullets.WriteString(pptxBulletPara(item, theme))
	}
	footer := strings.TrimSpace(deckTitle)
	if footer == "" {
		footer = "Lunitide"
	}
	var b strings.Builder
	b.WriteString(pptxTreeOpen())
	b.WriteString(pptxBg(theme.Paper))
	b.WriteString(pptxSpTreeOpen())
	b.WriteString(pptxRect(2, "Header", theme.Navy, 0, 0, pptxW, 1180000))
	b.WriteString(pptxRect(3, "AccentBar", theme.Teal, 0, 0, 160000, pptxH))
	b.WriteString(pptxTextBox(4, "Title", 520000, 280000, 11000000, 700000, pptxPara("l", theme.fontRun(s.Title, theme.White, 2400, true))))
	b.WriteString(pptxTextBox(5, "Body", 520000, 1480000, 11000000, 4500000, bullets.String()))
	b.WriteString(pptxRect(6, "FooterRule", theme.Soft, 520000, 6280000, 11000000, 12700))
	b.WriteString(pptxTextBox(7, "Footer", 520000, 6380000, 8000000, 320000, pptxPara("l", theme.fontRun(footer, theme.Muted, 1100, false))))
	b.WriteString(pptxTextBox(8, "Page", 9000000, 6380000, 2600000, 320000, pptxPara("r", theme.fontRun(fmt.Sprintf("%d / %d", index+1, total), theme.Muted, 1100, false))))
	b.WriteString(pptxMetricValueBoxes(9, s, theme, 5600000))
	b.WriteString(pptxTreeClose())
	return b.String()
}

func pptxMetricValueBoxes(startID int, s SlideSpec, theme SlideTheme, y int) string {
	if len(s.Metrics) == 0 {
		return ""
	}
	var b strings.Builder
	id := startID
	for j, m := range s.Metrics {
		if strings.TrimSpace(m.Value) == "" {
			continue
		}
		x := 720000 + (j%4)*2800000
		yy := y + (j/4)*380000
		b.WriteString(pptxTextBox(id, fmt.Sprintf("Metric%d", j+1), x, yy, 2600000, 340000, pptxPara("l", theme.fontRun(m.Value, theme.Gold, 1400, true))))
		id++
	}
	return b.String()
}
