package officetools

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

const (
	docxInk     = "1F2937"
	docxNavy    = "0B1F3A"
	docxTeal    = "0D9488"
	docxMuted   = "64748B"
	docxBodyPt  = 24 // 12pt — not Word's 11pt default wall
	docxH1Pt    = 32 // 16pt
	docxH2Pt    = 28 // 14pt
	docxTitlePt = 44 // 22pt

	minDocxBodyRunes   = 40
	minReportBodyRunes = 200
	minNovelBodyRunes  = 400
	minNovelChapters   = 2
)

// DocxDoc is one Word document. Kind selects cover/title-page rules:
// report gets a cover; novel requires author + chapter Heading 1.
type DocxDoc struct {
	Title    string
	Subtitle string
	Author   string
	Kind     string // report | novel | document | ""
	Blocks   []DocxBlock
}

// SampleStyledDocxBlocks is a headings+body fixture that passes validation.
func SampleStyledDocxBlocks() []DocxBlock {
	p := sampleDocxProse
	return []DocxBlock{
		{Type: "heading", Text: "概述"},
		{Type: "paragraph", Text: p},
		{Type: "heading2", Text: "说明"},
		{Type: "paragraph", Text: p},
	}
}

// SampleReportDocxBlocks is a production-shaped report (cover is added by GenDocxDoc).
func SampleReportDocxBlocks() []DocxBlock {
	p := sampleDocxProse + sampleDocxProse
	return []DocxBlock{
		{Type: "heading", Text: "摘要"},
		{Type: "paragraph", Text: p},
		{Type: "heading", Text: "背景与目的"},
		{Type: "paragraph", Text: p},
		{Type: "heading2", Text: "问题与分析"},
		{Type: "paragraph", Text: p},
		{Type: "quote", Text: "公开检索仅作旁证，缺口标待确认，不编造数字。"},
		{Type: "heading", Text: "结论与待办"},
		{Type: "paragraph", Text: p},
	}
}

// SampleNovelDocxDoc is a title+author+chapters fixture that passes novel validation.
func SampleNovelDocxDoc() DocxDoc {
	ch := sampleDocxProse + sampleDocxProse
	return DocxDoc{
		Title:  "潮声",
		Author: "阿潮",
		Kind:   "novel",
		Blocks: []DocxBlock{
			{Type: "heading", Text: "第一章 起潮"},
			{Type: "paragraph", Text: ch},
			{Type: "heading", Text: "第二章 转港"},
			{Type: "paragraph", Text: ch},
		},
	}
}

const sampleDocxProse = "本节交代背景、数据口径与可执行结论，避免只有提纲没有论证。读者应能直接引用其中的事实和建议，而不是面对空话或未展开的目录。论证要落到具体约束、出处和下一步动作上。夜色压上码头的时候，潮水先碰到石阶，再碰到鞋面，对岸灯火稀落像有人把城市的呼吸调小。"

func docxKindOf(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "report":
		return "report"
	case "novel":
		return "novel"
	case "document":
		return "document"
	default:
		return ""
	}
}

func normalizeDocxBlockType(b DocxBlock) string {
	t := strings.ToLower(strings.TrimSpace(b.Type))
	if t == "" {
		t = "paragraph"
	}
	if (t == "heading" || t == "h1") && b.Level == 2 {
		return "heading2"
	}
	switch t {
	case "h1":
		return "heading"
	case "h2":
		return "heading2"
	}
	return t
}

func docxStyleID(blockType string) string {
	switch blockType {
	case "heading":
		return "Heading1"
	case "heading2":
		return "Heading2"
	case "bullet":
		return "ListParagraph"
	case "quote":
		return "Quote"
	case "caption":
		return "Caption"
	default:
		return "Normal"
	}
}

func countDocxSpecStats(blocks []DocxBlock) (headings, h1, bodyRunes int) {
	for _, b := range blocks {
		text := strings.TrimSpace(b.Text)
		if text == "" {
			continue
		}
		switch normalizeDocxBlockType(b) {
		case "heading":
			headings++
			h1++
		case "heading2":
			headings++
		case "paragraph", "quote":
			bodyRunes += utf8.RuneCountInString(text)
		}
	}
	return headings, h1, bodyRunes
}

func validateDocxSpec(doc DocxDoc) error {
	if strings.TrimSpace(doc.Title) == "" {
		return fmt.Errorf("officetools: document title is required")
	}
	if len(doc.Blocks) == 0 || len(doc.Blocks) > MaxDocxBlocks {
		return fmt.Errorf("%w: blocks must be 1-%d", ErrLimit, MaxDocxBlocks)
	}
	for _, b := range doc.Blocks {
		switch normalizeDocxBlockType(b) {
		case "heading", "heading2", "paragraph", "bullet", "quote", "caption":
		default:
			return fmt.Errorf("officetools: docx block type %q not supported (heading|heading2|paragraph|bullet|quote|caption)", b.Type)
		}
	}
	headings, h1, bodyRunes := countDocxSpecStats(doc.Blocks)
	if headings == 0 {
		return fmt.Errorf("officetools: document needs Heading 1/2 styles, not a single-style body")
	}
	if bodyRunes < minDocxBodyRunes {
		return fmt.Errorf("officetools: document body is trivial")
	}
	switch docxKindOf(doc.Kind) {
	case "report":
		if headings < 2 || bodyRunes < minReportBodyRunes {
			return fmt.Errorf("officetools: report needs section headings and substantial chapters")
		}
	case "novel":
		if strings.TrimSpace(doc.Author) == "" {
			return fmt.Errorf("officetools: novel needs an author")
		}
		if h1 < minNovelChapters {
			return fmt.Errorf("officetools: novel needs chapter Heading 1")
		}
		if bodyRunes < minNovelBodyRunes {
			return fmt.Errorf("officetools: novel is an outline dump, not chapter prose")
		}
	}
	return nil
}

func buildDocxBody(doc DocxDoc) string {
	var b strings.Builder
	kind := docxKindOf(doc.Kind)
	if kind == "report" || kind == "novel" {
		b.WriteString(docxCoverPage(doc, kind))
	} else {
		b.WriteString(docxParagraph("Title", doc.Title, `<w:jc w:val="center"/><w:spacing w:before="240" w:after="200"/>`))
		if sub := strings.TrimSpace(doc.Subtitle); sub != "" {
			b.WriteString(docxParagraph("Subtitle", sub, `<w:jc w:val="center"/>`))
		}
	}
	for _, block := range doc.Blocks {
		text := block.Text
		bt := normalizeDocxBlockType(block)
		style := docxStyleID(bt)
		extra := ""
		if bt == "bullet" {
			extra = `<w:numPr><w:ilvl w:val="0"/><w:numId w:val="1"/></w:numPr>`
		}
		b.WriteString(docxParagraph(style, text, extra))
	}
	return b.String()
}

func docxCoverPage(doc DocxDoc, kind string) string {
	var b strings.Builder
	b.WriteString(docxParagraph("Title", doc.Title, `<w:jc w:val="center"/><w:spacing w:before="2400" w:after="240"/>`))
	if sub := strings.TrimSpace(doc.Subtitle); sub != "" {
		b.WriteString(docxParagraph("Subtitle", sub, `<w:jc w:val="center"/>`))
	}
	author := strings.TrimSpace(doc.Author)
	if kind == "novel" {
		if author == "" {
			author = "佚名"
		}
		b.WriteString(docxParagraph("Author", "作者　"+author, `<w:jc w:val="center"/>`))
	} else if author != "" {
		b.WriteString(docxParagraph("Author", author, `<w:jc w:val="center"/>`))
	}
	if kind == "report" {
		b.WriteString(docxParagraph("Caption", "月汐报告 · 可打印定稿", `<w:jc w:val="center"/>`))
	}
	b.WriteString(`<w:p><w:r><w:br w:type="page"/></w:r></w:p>`)
	return b.String()
}

func docxParagraph(style, text, extraPPr string) string {
	rPr := docxRunProps(style)
	pPr := `<w:pPr><w:pStyle w:val="` + style + `"/>` + extraPPr + `</w:pPr>`
	return `<w:p>` + pPr + `<w:r>` + rPr + `<w:t xml:space="preserve">` + xmlEscape(text) + `</w:t></w:r></w:p>`
}

func docxRunProps(style string) string {
	east, sz, bold, color := "SimSun", docxBodyPt, false, docxInk
	switch style {
	case "Title":
		east, sz, bold, color = "SimHei", docxTitlePt, true, docxNavy
	case "Subtitle":
		east, sz, color = "Microsoft YaHei", docxH2Pt, docxMuted
	case "Heading1":
		east, sz, bold, color = "SimHei", docxH1Pt, true, docxNavy
	case "Heading2":
		east, sz, bold, color = "SimHei", docxH2Pt, true, docxNavy
	case "Quote":
		east, sz, color = "SimSun", docxBodyPt, docxMuted
	case "Caption", "Author":
		east, sz, color = "Microsoft YaHei", 20, docxMuted
	}
	b := ""
	if bold {
		b = `<w:b/><w:bCs/>`
	}
	italic := ""
	if style == "Quote" {
		italic = `<w:i/><w:iCs/>`
	}
	return `<w:rPr><w:rFonts w:ascii="Calibri" w:hAnsi="Calibri" w:eastAsia="` + east + `" w:cs="Calibri" w:hint="eastAsia"/>` +
		b + italic +
		`<w:color w:val="` + color + `"/><w:sz w:val="` + itoa(sz) + `"/><w:szCs w:val="` + itoa(sz) + `"/><w:lang w:val="en-US" w:eastAsia="zh-CN"/></w:rPr>`
}

func itoa(n int) string {
	return fmt.Sprintf("%d", n)
}

const wordRelsDocx = xmlDecl + `
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/><Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/numbering" Target="numbering.xml"/><Relationship Id="rId3" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/fontTable" Target="fontTable.xml"/><Relationship Id="rId4" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/settings" Target="settings.xml"/><Relationship Id="rId5" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/theme" Target="theme/theme1.xml"/></Relationships>`

const docxSettingsXML = xmlDecl + `
<w:settings xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:characterSpacingControl w:val="compressPunctuation"/><w:compat><w:compatSetting w:name="compatibilityMode" w:uri="http://schemas.microsoft.com/office/word" w:val="15"/></w:compat></w:settings>`

const docxNumberingXML = xmlDecl + `
<w:numbering xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:abstractNum w:abstractNumId="0"><w:multiLevelType w:val="hybridMultilevel"/><w:lvl w:ilvl="0"><w:start w:val="1"/><w:numFmt w:val="bullet"/><w:lvlText w:val="•"/><w:lvlJc w:val="left"/><w:pPr><w:ind w:left="720" w:hanging="360"/></w:pPr><w:rPr><w:rFonts w:ascii="Times New Roman" w:eastAsia="SimSun" w:hAnsi="Times New Roman" w:hint="eastAsia"/></w:rPr></w:lvl></w:abstractNum><w:num w:numId="1"><w:abstractNumId w:val="0"/></w:num></w:numbering>`

const docxFontTableXML = xmlDecl + `
<w:fonts xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:font w:name="Calibri"><w:charset w:val="00"/><w:family w:val="swiss"/><w:pitch w:val="variable"/></w:font><w:font w:name="SimSun"><w:altName w:val="宋体"/><w:charset w:val="86"/><w:family w:val="auto"/><w:pitch w:val="variable"/></w:font><w:font w:name="SimHei"><w:altName w:val="黑体"/><w:charset w:val="86"/><w:family w:val="auto"/><w:pitch w:val="variable"/></w:font><w:font w:name="Microsoft YaHei"><w:altName w:val="微软雅黑"/><w:charset w:val="86"/><w:family w:val="swiss"/><w:pitch w:val="variable"/></w:font></w:fonts>`

const docxStylesXML = xmlDecl + `
<w:styles xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
<w:docDefaults><w:rPrDefault><w:rPr><w:rFonts w:ascii="Calibri" w:hAnsi="Calibri" w:eastAsia="SimSun" w:cs="Calibri"/><w:sz w:val="24"/><w:szCs w:val="24"/><w:color w:val="1F2937"/><w:lang w:val="en-US" w:eastAsia="zh-CN"/></w:rPr></w:rPrDefault><w:pPrDefault><w:pPr><w:spacing w:after="160" w:line="360" w:lineRule="auto"/></w:pPr></w:pPrDefault></w:docDefaults>
<w:style w:type="paragraph" w:default="1" w:styleId="Normal"><w:name w:val="Normal"/><w:qFormat/><w:pPr><w:spacing w:after="160" w:line="360" w:lineRule="auto"/><w:ind w:firstLineChars="200" w:firstLine="480"/></w:pPr><w:rPr><w:rFonts w:ascii="Calibri" w:hAnsi="Calibri" w:eastAsia="SimSun"/><w:sz w:val="24"/><w:szCs w:val="24"/><w:color w:val="1F2937"/></w:rPr></w:style>
<w:style w:type="paragraph" w:styleId="Title"><w:name w:val="Title"/><w:basedOn w:val="Normal"/><w:next w:val="Normal"/><w:qFormat/><w:pPr><w:keepNext/><w:jc w:val="center"/><w:spacing w:before="240" w:after="200" w:line="276" w:lineRule="auto"/><w:ind w:firstLine="0"/><w:outlineLvl w:val="0"/></w:pPr><w:rPr><w:rFonts w:ascii="Calibri" w:hAnsi="Calibri" w:eastAsia="SimHei"/><w:b/><w:sz w:val="44"/><w:szCs w:val="44"/><w:color w:val="0B1F3A"/></w:rPr></w:style>
<w:style w:type="paragraph" w:styleId="Subtitle"><w:name w:val="Subtitle"/><w:basedOn w:val="Normal"/><w:next w:val="Normal"/><w:qFormat/><w:pPr><w:jc w:val="center"/><w:spacing w:after="200"/><w:ind w:firstLine="0"/></w:pPr><w:rPr><w:rFonts w:ascii="Calibri" w:hAnsi="Calibri" w:eastAsia="Microsoft YaHei"/><w:sz w:val="28"/><w:szCs w:val="28"/><w:color w:val="64748B"/></w:rPr></w:style>
<w:style w:type="paragraph" w:styleId="Author"><w:name w:val="Author"/><w:basedOn w:val="Normal"/><w:next w:val="Normal"/><w:pPr><w:jc w:val="center"/><w:ind w:firstLine="0"/></w:pPr><w:rPr><w:rFonts w:ascii="Calibri" w:hAnsi="Calibri" w:eastAsia="Microsoft YaHei"/><w:sz w:val="22"/><w:szCs w:val="22"/><w:color w:val="64748B"/></w:rPr></w:style>
<w:style w:type="paragraph" w:styleId="Heading1"><w:name w:val="heading 1"/><w:basedOn w:val="Normal"/><w:next w:val="Normal"/><w:uiPriority w:val="9"/><w:qFormat/><w:pPr><w:keepNext/><w:keepLines/><w:spacing w:before="360" w:after="160"/><w:outlineLvl w:val="0"/><w:pBdr><w:bottom w:val="single" w:sz="12" w:space="4" w:color="0D9488"/></w:pBdr><w:ind w:firstLine="0"/></w:pPr><w:rPr><w:rFonts w:ascii="Calibri" w:hAnsi="Calibri" w:eastAsia="SimHei"/><w:b/><w:sz w:val="32"/><w:szCs w:val="32"/><w:color w:val="0B1F3A"/></w:rPr></w:style>
<w:style w:type="paragraph" w:styleId="Heading2"><w:name w:val="heading 2"/><w:basedOn w:val="Normal"/><w:next w:val="Normal"/><w:uiPriority w:val="9"/><w:qFormat/><w:pPr><w:keepNext/><w:keepLines/><w:spacing w:before="280" w:after="120"/><w:outlineLvl w:val="1"/><w:ind w:firstLine="0"/></w:pPr><w:rPr><w:rFonts w:ascii="Calibri" w:hAnsi="Calibri" w:eastAsia="SimHei"/><w:b/><w:sz w:val="28"/><w:szCs w:val="28"/><w:color w:val="0B1F3A"/></w:rPr></w:style>
<w:style w:type="paragraph" w:styleId="Quote"><w:name w:val="Quote"/><w:basedOn w:val="Normal"/><w:next w:val="Normal"/><w:qFormat/><w:pPr><w:pBdr><w:left w:val="single" w:sz="18" w:space="10" w:color="0D9488"/></w:pBdr><w:ind w:left="720" w:firstLine="0"/><w:spacing w:before="120" w:after="120"/></w:pPr><w:rPr><w:rFonts w:ascii="Calibri" w:hAnsi="Calibri" w:eastAsia="SimSun"/><w:i/><w:color w:val="64748B"/></w:rPr></w:style>
<w:style w:type="paragraph" w:styleId="Caption"><w:name w:val="Caption"/><w:basedOn w:val="Normal"/><w:next w:val="Normal"/><w:qFormat/><w:pPr><w:jc w:val="center"/><w:ind w:firstLine="0"/><w:spacing w:before="80" w:after="80"/></w:pPr><w:rPr><w:rFonts w:ascii="Calibri" w:hAnsi="Calibri" w:eastAsia="Microsoft YaHei"/><w:sz w:val="18"/><w:szCs w:val="18"/><w:color w:val="64748B"/></w:rPr></w:style>
<w:style w:type="paragraph" w:styleId="ListParagraph"><w:name w:val="List Paragraph"/><w:basedOn w:val="Normal"/><w:qFormat/><w:pPr><w:ind w:left="720" w:hanging="360" w:firstLine="0"/><w:spacing w:after="80"/></w:pPr></w:style>
</w:styles>`
