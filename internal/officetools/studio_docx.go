package officetools

import (
	"fmt"
	"strings"
)

// StudioDocxBlock extends the same native writer for short office documents,
// real tables and numbered lists without weakening the report/novel tool's
// existing completeness checks.
type StudioDocxBlock struct {
	Type    string
	Text    string
	Rows    [][]string
	Section *StudioDocumentOptions
}

type StudioDocumentOptions struct {
	Header, Footer, PageSize, Orientation                         string
	PageNumbers                                                   bool
	PageNumberStart                                               int
	Latin, East, Navy, Ink, Muted, Soft, Teal, Gold, Paper, White string
	BodyHalfPt                                                    int
}

func GenStudioDocx(title string, blocks []StudioDocxBlock) ([]byte, error) {
	return GenStudioDocxWithOptions(title, blocks, nil)
}

func GenStudioDocxWithOptions(title string, blocks []StudioDocxBlock, options *StudioDocumentOptions) ([]byte, error) {
	if strings.TrimSpace(title) == "" || len(blocks) == 0 || len(blocks) > MaxDocxBlocks {
		return nil, fmt.Errorf("%w: document title and 1-%d blocks required", ErrLimit, MaxDocxBlocks)
	}
	var body strings.Builder
	sections := []StudioDocumentOptions{{}}
	latin, east, navy, ink, muted := "", "", "", "", ""
	if options != nil {
		sections[0] = *options
		latin, east, navy, ink, muted = options.Latin, options.East, options.Navy, options.Ink, options.Muted
	}
	bodyHalf := 0
	if options != nil {
		bodyHalf = options.BodyHalfPt
	}
	paint := func(style, text, extra string) string {
		return docxThemedParagraph(style, text, extra, latin, east, navy, ink, muted, bodyHalf)
	}
	pageParts := options != nil && (options.Header != "" || options.Footer != "" || options.PageNumbers)
	for _, b := range blocks {
		if b.Type == "section" {
			if b.Section == nil || b.Text != "" || len(b.Rows) > 0 {
				return nil, fmt.Errorf("officetools: a section block requires page settings and cannot contain text")
			}
			sections = append(sections, *b.Section)
			pageParts = true
		}
	}
	if len(sections) > 64 {
		return nil, ErrLimit
	}
	for _, s := range sections {
		if err := studioPageOptionsValid(s); err != nil {
			return nil, err
		}
	}
	sectionIndex := 0
	hasTOC := false
	body.WriteString(paint("Title", title, `<w:jc w:val="center"/><w:outlineLvl w:val="9"/>`))
	cells := 0
	for _, block := range blocks {
		switch block.Type {
		case "table":
			if len(block.Rows) == 0 || len(block.Rows) > 500 {
				return nil, ErrLimit
			}
			cols := len(block.Rows[0])
			if cols == 0 || cols > 16 {
				return nil, ErrLimit
			}
			pageWidth, _ := studioPageSize(sections[sectionIndex])
			tableWidth := pageWidth - 3600
			width := tableWidth / cols
			fmt.Fprintf(&body, `<w:tbl><w:tblPr><w:tblW w:w="%d" w:type="dxa"/><w:tblBorders><w:top w:val="single" w:sz="4" w:color="CBD5E1"/><w:left w:val="single" w:sz="4" w:color="CBD5E1"/><w:bottom w:val="single" w:sz="4" w:color="CBD5E1"/><w:right w:val="single" w:sz="4" w:color="CBD5E1"/><w:insideH w:val="single" w:sz="4" w:color="CBD5E1"/><w:insideV w:val="single" w:sz="4" w:color="CBD5E1"/></w:tblBorders><w:tblLayout w:type="fixed"/></w:tblPr><w:tblGrid>`, tableWidth)
			for range cols {
				fmt.Fprintf(&body, `<w:gridCol w:w="%d"/>`, width)
			}
			body.WriteString(`</w:tblGrid>`)
			for ri, row := range block.Rows {
				if len(row) != cols {
					return nil, fmt.Errorf("officetools: table rows must have equal columns")
				}
				cells += len(row)
				if cells > 10000 {
					return nil, ErrLimit
				}
				body.WriteString(`<w:tr>`)
				if ri == 0 {
					body.WriteString(`<w:trPr><w:tblHeader/></w:trPr>`)
				}
				for _, text := range row {
					fmt.Fprintf(&body, `<w:tc><w:tcPr><w:tcW w:w="%d" w:type="dxa"/>`, width)
					if ri == 0 {
						body.WriteString(`<w:shd w:fill="E2E8F0"/>`)
					}
					body.WriteString(`</w:tcPr>`)
					body.WriteString(paint("Normal", text, `<w:ind w:firstLine="0"/><w:spacing w:after="80"/>`))
					body.WriteString(`</w:tc>`)
				}
				body.WriteString(`</w:tr>`)
			}
			body.WriteString(`</w:tbl>`)
		case "pagebreak":
			body.WriteString(`<w:p><w:r><w:br w:type="page"/></w:r></w:p>`)
		case "numbered":
			body.WriteString(paint("ListParagraph", block.Text, `<w:numPr><w:ilvl w:val="0"/><w:numId w:val="2"/></w:numPr>`))
		case "heading", "heading2", "heading3", "paragraph", "bullet", "quote", "caption", "":
			body.WriteString(buildDocxBlockOnly(block.Type, block.Text, latin, east, navy, ink, muted, bodyHalf))
		case "toc":
			hasTOC = true
			label := block.Text
			if label == "" {
				label = "目录"
			}
			body.WriteString(paint("StudioTOCHeading", label, `<w:outlineLvl w:val="9"/>`))
			pageWidth, _ := studioPageSize(sections[sectionIndex])
			fmt.Fprintf(&body, `<w:p><w:pPr><w:tabs><w:tab w:val="right" w:leader="dot" w:pos="%d"/></w:tabs><w:ind w:firstLine="0"/></w:pPr><w:fldSimple w:instr="TOC \o &quot;1-3&quot; \h \z" w:dirty="true"><w:r><w:t>目录待实际排版后更新；当前未计算页码。</w:t></w:r></w:fldSimple></w:p>`, pageWidth-3600)
		case "section":
			body.WriteString(`<w:p><w:pPr>` + studioSectionXML(sections[sectionIndex], sectionIndex, pageParts, true) + `</w:pPr></w:p>`)
			sectionIndex++
		default:
			return nil, fmt.Errorf("officetools: unsupported studio document block %q", block.Type)
		}
		if body.Len() > 8<<20 {
			return nil, ErrLimit
		}
	}
	doc := xmlDecl + `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><w:body>` + body.String() + studioSectionXML(sections[sectionIndex], sectionIndex, pageParts, false) + `</w:body></w:document>`
	numbering := strings.Replace(docxNumberingXML, `</w:numbering>`, `<w:abstractNum w:abstractNumId="1"><w:multiLevelType w:val="singleLevel"/><w:lvl w:ilvl="0"><w:start w:val="1"/><w:numFmt w:val="decimal"/><w:lvlText w:val="%1."/><w:lvlJc w:val="left"/><w:pPr><w:ind w:left="720" w:hanging="360"/></w:pPr></w:lvl></w:abstractNum><w:num w:numId="2"><w:abstractNumId w:val="1"/></w:num></w:numbering>`, 1)
	contentTypes, rels, settings := contentTypesDocx(), wordRelsDocx, docxSettingsXML
	heading3Sz := 24
	heading3Color := "0B1F3A"
	if options != nil {
		if options.BodyHalfPt > 0 {
			heading3Sz = options.BodyHalfPt
			if heading3Sz < 20 {
				heading3Sz = 20
			}
		}
		if options.Navy != "" {
			heading3Color = options.Navy
		}
	}
	styles := strings.Replace(docxStylesXML, `</w:styles>`, fmt.Sprintf(`<w:style w:type="paragraph" w:styleId="Heading3"><w:name w:val="heading 3"/><w:basedOn w:val="Heading2"/><w:next w:val="Normal"/><w:qFormat/><w:pPr><w:keepNext/><w:outlineLvl w:val="2"/><w:spacing w:before="200" w:after="100"/></w:pPr><w:rPr><w:b/><w:sz w:val="%d"/><w:color w:val="%s"/></w:rPr></w:style></w:styles>`, heading3Sz, heading3Color), 1)
	if options != nil && options.Navy != "" {
		styles = replaceDocxStyleColor(styles, "Heading1", options.Navy)
		styles = replaceDocxStyleColor(styles, "Heading2", options.Navy)
	}
	parts := []zipPart{}
	styles = strings.Replace(styles, `</w:styles>`, `<w:style w:type="paragraph" w:styleId="StudioTOCHeading"><w:name w:val="Studio TOC heading"/><w:basedOn w:val="Heading1"/><w:next w:val="Normal"/><w:pPr><w:keepNext/><w:outlineLvl w:val="9"/></w:pPr></w:style></w:styles>`, 1)
	if pageParts {
		for i, s := range sections {
			for _, which := range []string{"header", "footer"} {
				partName := fmt.Sprintf("word/%s%d.xml", which, i+1)
				relID := fmt.Sprintf("rIdStudio%s%d", which, i+1)
				contentTypes = strings.Replace(contentTypes, `</Types>`, fmt.Sprintf(`<Override PartName="/%s" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.%s+xml"/></Types>`, partName, which), 1)
				rels = strings.Replace(rels, `</Relationships>`, fmt.Sprintf(`<Relationship Id="%s" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/%s" Target="%s%d.xml"/></Relationships>`, relID, which, which, i+1), 1)
				parts = append(parts, zipPart{partName, studioHeaderFooter(which, s)})
			}
		}
	}
	if hasTOC || pageParts {
		settings = strings.Replace(settings, `</w:settings>`, `<w:updateFields w:val="true"/></w:settings>`, 1)
	}
	themePart := themeXML
	if options != nil {
		themePart = ThemeXMLFor(options.Latin, options.East, options.Navy, options.Teal, options.Gold, options.Paper, options.Ink, options.White, options.Soft)
	}
	parts = append(parts, []zipPart{{"[Content_Types].xml", contentTypes}, {"_rels/.rels", relsDocx}, {"word/document.xml", doc}, {"word/_rels/document.xml.rels", rels}, {"word/styles.xml", styles}, {"word/numbering.xml", numbering}, {"word/fontTable.xml", docxFontTableXML}, {"word/settings.xml", settings}, {"word/theme/theme1.xml", themePart}, {"docProps/core.xml", coreXML(title)}}...)
	return zipBytes(parts)
}

func buildDocxBlockOnly(kind, text string, latin, east, navy, ink, muted string, bodyHalfPt int) string {
	bt := normalizeDocxBlockType(DocxBlock{Type: kind})
	extra := ""
	if bt == "bullet" {
		extra = `<w:numPr><w:ilvl w:val="0"/><w:numId w:val="1"/></w:numPr>`
	}
	style := docxStyleID(bt)
	if kind == "heading3" {
		style = "Heading3"
	}
	return docxThemedParagraph(style, text, extra, latin, east, navy, ink, muted, bodyHalfPt)
}

func studioPageOptionsValid(s StudioDocumentOptions) error {
	if s.PageSize != "" && s.PageSize != "A4" && s.PageSize != "Letter" || s.Orientation != "" && s.Orientation != "portrait" && s.Orientation != "landscape" || s.PageNumberStart < 0 || s.PageNumberStart > 32767 || len(s.Header) > 8192 || len(s.Footer) > 8192 {
		return fmt.Errorf("officetools: unsupported section page settings")
	}
	return nil
}

func studioPageSize(s StudioDocumentOptions) (int, int) {
	w, h := 11906, 16838
	if s.PageSize == "Letter" {
		w, h = 12240, 15840
	}
	if s.Orientation == "landscape" {
		w, h = h, w
	}
	return w, h
}

func studioSectionXML(s StudioDocumentOptions, index int, pageParts, sectionBreak bool) string {
	var b strings.Builder
	b.WriteString(`<w:sectPr>`)
	if pageParts {
		fmt.Fprintf(&b, `<w:headerReference w:type="default" r:id="rIdStudioheader%d"/><w:footerReference w:type="default" r:id="rIdStudiofooter%d"/>`, index+1, index+1)
	}
	if sectionBreak {
		b.WriteString(`<w:type w:val="nextPage"/>`)
	}
	w, h := studioPageSize(s)
	orientation := ""
	if s.Orientation == "landscape" {
		orientation = ` w:orient="landscape"`
	}
	fmt.Fprintf(&b, `<w:pgSz w:w="%d" w:h="%d"%s/><w:pgMar w:top="1440" w:right="1800" w:bottom="1440" w:left="1800" w:header="720" w:footer="720"/>`, w, h, orientation)
	if s.PageNumberStart > 0 {
		fmt.Fprintf(&b, `<w:pgNumType w:fmt="decimal" w:start="%d"/>`, s.PageNumberStart)
	}
	b.WriteString(`</w:sectPr>`)
	return b.String()
}

func studioHeaderFooter(which string, s StudioDocumentOptions) string {
	tag, text := "hdr", s.Header
	if which == "footer" {
		tag, text = "ftr", s.Footer
	}
	var b strings.Builder
	b.WriteString(xmlDecl)
	fmt.Fprintf(&b, `<w:%s xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">`, tag)
	b.WriteString(docxThemedParagraph("Caption", text, `<w:jc w:val="left"/>`, s.Latin, s.East, s.Navy, s.Ink, s.Muted, s.BodyHalfPt))
	if which == "footer" && s.PageNumbers {
		b.WriteString(`<w:p><w:pPr><w:jc w:val="right"/></w:pPr><w:fldSimple w:instr="PAGE" w:dirty="true"><w:r><w:t>页码待更新</w:t></w:r></w:fldSimple></w:p>`)
	}
	fmt.Fprintf(&b, `</w:%s>`, tag)
	return b.String()
}

func replaceDocxStyleColor(styles, styleID, color string) string {
	idx := strings.Index(styles, `w:styleId="`+styleID+`"`)
	if idx < 0 {
		return styles
	}
	end := strings.Index(styles[idx:], `</w:style>`)
	if end < 0 {
		return styles
	}
	block := styles[idx : idx+end]
	block = strings.Replace(block, `w:color w:val="0B1F3A"`, `w:color w:val="`+color+`"`, 1)
	return styles[:idx] + block + styles[idx+end:]
}
