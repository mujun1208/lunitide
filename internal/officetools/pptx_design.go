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
	weight := ""
	if bold {
		weight = ` b="1"`
	}
	return fmt.Sprintf(`<a:r><a:rPr lang="zh-CN" altLang="en-US" sz="%d"%s dirty="0"><a:solidFill><a:srgbClr val="%s"/></a:solidFill><a:latin typeface="Calibri"/><a:ea typeface="Microsoft YaHei"/><a:cs typeface="Microsoft YaHei"/></a:rPr><a:t>%s</a:t></a:r>`, sz, weight, color, xmlEscape(text))
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

func pptxBulletPara(text string) string {
	return `<a:p><a:pPr marL="342900" indent="-171450"><a:buFont typeface="Arial"/><a:buClr><a:srgbClr val="` + clrTeal + `"/></a:buClr><a:buChar char="●"/><a:spcBef><a:spcPts val="1200"/></a:spcBef></a:pPr>` +
		pptxFontRun(text, clrInk, 1800, false) +
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
	switch pptxLayoutOf(index, s) {
	case "title":
		return pptxTitleSlide(s, deckTitle)
	case "section":
		return pptxSectionSlide(index, total, s)
	default:
		return pptxContentSlide(index, total, deckTitle, s)
	}
}

func pptxTitleSlide(s SlideSpec, deckTitle string) string {
	sub := strings.TrimSpace(s.Subtitle)
	if sub == "" && len(s.Bullets) > 0 {
		sub = strings.Join(s.Bullets, "  ·  ")
	}
	if sub == "" {
		sub = deckTitle
	}
	var b strings.Builder
	b.WriteString(pptxTreeOpen())
	b.WriteString(pptxBg(clrNavy))
	b.WriteString(pptxSpTreeOpen())
	b.WriteString(pptxRect(2, "AccentBar", clrTeal, 0, 0, 280000, pptxH))
	b.WriteString(pptxRect(3, "GoldRule", clrGold, 720000, 3520000, 2400000, 36000))
	b.WriteString(pptxTextBox(4, "Title", 720000, 2100000, 10700000, 1300000, pptxPara("l", pptxFontRun(s.Title, clrWhite, 4000, true))))
	b.WriteString(pptxTextBox(5, "Subtitle", 720000, 3720000, 10700000, 900000, pptxPara("l", pptxFontRun(sub, "A5F3FC", 1800, false))))
	b.WriteString(pptxTextBox(6, "Brand", 720000, 6200000, 5000000, 360000, pptxPara("l", pptxFontRun("LUNITIDE  商务演示", clrGold, 1200, false))))
	b.WriteString(pptxTreeClose())
	return b.String()
}

func pptxSectionSlide(index, total int, s SlideSpec) string {
	var b strings.Builder
	b.WriteString(pptxTreeOpen())
	b.WriteString(pptxBg(clrNavy))
	b.WriteString(pptxSpTreeOpen())
	b.WriteString(pptxRect(2, "AccentBar", clrTeal, 0, 0, 280000, pptxH))
	b.WriteString(pptxTextBox(3, "Kicker", 720000, 2400000, 10700000, 400000, pptxPara("l", pptxFontRun(fmt.Sprintf("0%d  /  %02d", index+1, total), clrGold, 1400, false))))
	b.WriteString(pptxTextBox(4, "Title", 720000, 2880000, 10700000, 1400000, pptxPara("l", pptxFontRun(s.Title, clrWhite, 3600, true))))
	if len(s.Bullets) > 0 {
		b.WriteString(pptxTextBox(5, "Lead", 720000, 4400000, 10700000, 800000, pptxPara("l", pptxFontRun(s.Bullets[0], "A5F3FC", 1600, false))))
	}
	b.WriteString(pptxTreeClose())
	return b.String()
}

func pptxContentSlide(index, total int, deckTitle string, s SlideSpec) string {
	var bullets strings.Builder
	if len(s.Bullets) == 0 {
		bullets.WriteString(`<a:p><a:endParaRPr lang="zh-CN"/></a:p>`)
	}
	for _, item := range s.Bullets {
		bullets.WriteString(pptxBulletPara(item))
	}
	footer := strings.TrimSpace(deckTitle)
	if footer == "" {
		footer = "Lunitide"
	}
	var b strings.Builder
	b.WriteString(pptxTreeOpen())
	b.WriteString(pptxBg(clrPaper))
	b.WriteString(pptxSpTreeOpen())
	b.WriteString(pptxRect(2, "Header", clrNavy, 0, 0, pptxW, 1180000))
	b.WriteString(pptxRect(3, "AccentBar", clrTeal, 0, 0, 160000, pptxH))
	b.WriteString(pptxTextBox(4, "Title", 520000, 280000, 11000000, 700000, pptxPara("l", pptxFontRun(s.Title, clrWhite, 2400, true))))
	b.WriteString(pptxTextBox(5, "Body", 520000, 1480000, 11000000, 4500000, bullets.String()))
	b.WriteString(pptxRect(6, "FooterRule", clrSoft, 520000, 6280000, 11000000, 12700))
	b.WriteString(pptxTextBox(7, "Footer", 520000, 6380000, 8000000, 320000, pptxPara("l", pptxFontRun(footer, clrMuted, 1100, false))))
	b.WriteString(pptxTextBox(8, "Page", 9000000, 6380000, 2600000, 320000, pptxPara("r", pptxFontRun(fmt.Sprintf("%d / %d", index+1, total), clrMuted, 1100, false))))
	b.WriteString(pptxTreeClose())
	return b.String()
}
