// Package officetools implements the P2-1 Office toolchain generators:
// XLSX (excelize, with charts), DOCX and PPTX (zero-dependency OOXML
// zip/XML), PDF (gofpdf) and XLSX parsing. All APIs are pure
// bytes-in/bytes-out so the toolruntime can gate them behind the same
// approval modes as workspace.write.
package officetools

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/jung-kurt/gofpdf"
	"github.com/xuri/excelize/v2"
)

// Guard rails shared by every generator (fail-closed before any work).
const (
	MaxSheets        = 16
	MaxRowsPerSheet  = 5000
	MaxColsPerSheet  = 128
	MaxCellsTotal    = 50000
	MaxDocxBlocks    = 500
	MaxPptxSlides    = 30
	MaxBulletsPerSld = 12
	MaxPDFBodyRunes  = 50000
	MaxParseCells    = 20000
	MaxParseRows     = 200
)

var ErrLimit = errors.New("officetools: input exceeds limits")

// SheetSpec is one excel.gen sheet.
type SheetSpec struct {
	Name    string          `json:"name"`
	Headers []string        `json:"headers"`
	Rows    [][]interface{} `json:"rows"`
	Chart   *ChartSpec      `json:"chart,omitempty"`
}

// ChartSpec draws a chart over the first label column and first numeric
// column of the sheet.
type ChartSpec struct {
	Type  string `json:"type"` // bar | col | line | pie
	Title string `json:"title,omitempty"`
}

// GenXLSX builds one workbook from the sheet specs. The first sheet wins
// the sheet-name defaults ("Sheet1").
func GenXLSX(sheets []SheetSpec) ([]byte, error) {
	if len(sheets) == 0 || len(sheets) > MaxSheets {
		return nil, fmt.Errorf("%w: sheets must be 1-%d", ErrLimit, MaxSheets)
	}
	f := excelize.NewFile()
	cells := 0
	for i, s := range sheets {
		name := s.Name
		if name == "" {
			name = fmt.Sprintf("Sheet%d", i+1)
		}
		if i == 0 {
			if err := f.SetSheetName("Sheet1", name); err != nil {
				return nil, err
			}
		} else {
			if _, err := f.NewSheet(name); err != nil {
				return nil, err
			}
		}
		width := len(s.Headers)
		for _, row := range s.Rows {
			if len(row) > width {
				width = len(row)
			}
		}
		if width > MaxColsPerSheet {
			return nil, fmt.Errorf("%w: sheet %s has %d columns (max %d)", ErrLimit, name, width, MaxColsPerSheet)
		}
		rows := len(s.Rows) + boolToInt(len(s.Headers) > 0)
		if rows > MaxRowsPerSheet {
			return nil, fmt.Errorf("%w: sheet %s has %d rows (max %d)", ErrLimit, name, rows, MaxRowsPerSheet)
		}
		cells += rows * width
		if cells > MaxCellsTotal {
			return nil, fmt.Errorf("%w: workbook exceeds %d cells", ErrLimit, MaxCellsTotal)
		}
		if len(s.Headers) > 0 {
			if err := f.SetSheetRow(name, "A1", &s.Headers); err != nil {
				return nil, err
			}
		}
		for ri, row := range s.Rows {
			if len(row) == 0 {
				continue
			}
			cell, _ := excelize.CoordinatesToCellName(1, ri+boolToInt(len(s.Headers) > 0)+1)
			if err := f.SetSheetRow(name, cell, &row); err != nil {
				return nil, err
			}
		}
		if s.Chart != nil && s.Chart.Type != "" {
			if err := addChart(f, name, s, len(s.Headers) > 0); err != nil {
				return nil, err
			}
		}
	}
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func addChart(f *excelize.File, sheet string, s SheetSpec, hasHeaders bool) error {
	var chartType excelize.ChartType
	switch s.Chart.Type {
	case "bar":
		chartType = excelize.Bar
	case "col", "column":
		chartType = excelize.Col
	case "line":
		chartType = excelize.Line
	case "pie":
		chartType = excelize.Pie
	default:
		return fmt.Errorf("officetools: chart type %q not supported (bar|col|line|pie)", s.Chart.Type)
	}
	dataRows := len(s.Rows)
	if dataRows < 2 {
		return fmt.Errorf("officetools: chart needs at least 2 data rows")
	}
	firstDataRow := 2
	if !hasHeaders {
		firstDataRow = 1
	}
	last := firstDataRow + dataRows - 1
	cats := fmt.Sprintf("%s!$A$%d:$A$%d", sheet, firstDataRow, last)
	vals := fmt.Sprintf("%s!$B$%d:$B$%d", sheet, firstDataRow, last)
	title := s.Chart.Title
	chart := excelize.Chart{
		Type: chartType,
		Series: []excelize.ChartSeries{{
			Name:       title,
			Categories: cats,
			Values:     vals,
		}},
		Title: excelize.ChartTitle{
			Paragraph: []excelize.RichTextRun{{Text: title}},
		},
	}
	anchor, err := excelize.CoordinatesToCellName(1, last+2)
	if err != nil {
		return err
	}
	return f.AddChart(sheet, anchor, &chart)
}

// XLSXSummary is the excel.parse answer.
type XLSXSummary struct {
	Sheets []SheetSummary `json:"sheets"`
}

// SheetSummary previews one sheet.
type SheetSummary struct {
	Name       string     `json:"name"`
	Rows       int        `json:"rows"`
	Cols       int        `json:"cols"`
	Header     []string   `json:"header,omitempty"`
	Preview    [][]string `json:"preview"`
	Truncated  bool       `json:"truncated"`
	CellErrors int        `json:"cellErrors"`
}

// ParseXLSX reads a workbook and answers sheet names, dimensions and a
// bounded preview (stringified cells).
func ParseXLSX(data []byte) (string, error) {
	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("officetools: open xlsx: %w", err)
	}
	defer f.Close()
	out := XLSXSummary{Sheets: []SheetSummary{}}
	names := f.GetSheetList()
	if len(names) > MaxSheets {
		names = names[:MaxSheets]
	}
	for _, name := range names {
		rows, err := f.GetRows(name)
		if err != nil {
			return "", err
		}
		ss := SheetSummary{Name: name, Preview: [][]string{}}
		ss.Rows = len(rows)
		budget := MaxParseCells
		truncRows := rows
		if len(truncRows) > MaxParseRows {
			truncRows = truncRows[:MaxParseRows]
			ss.Truncated = true
		}
		for ri, row := range truncRows {
			cells := row
			if ri == 0 {
				ss.Header = append([]string{}, row...)
			}
			if len(cells) > ss.Cols {
				ss.Cols = len(cells)
			}
			if budget-len(cells) < 0 {
				cells = cells[:budget]
				ss.Truncated = true
			}
			budget -= len(cells)
			ss.Preview = append(ss.Preview, append([]string{}, cells...))
			if budget <= 0 {
				ss.Truncated = true
				break
			}
		}
		out.Sheets = append(out.Sheets, ss)
	}
	blob, err := json.Marshal(out)
	if err != nil {
		return "", err
	}
	return string(blob), nil
}

// DocxBlock is one docx body block.
type DocxBlock struct {
	Type  string `json:"type"` // heading | heading2 | paragraph | bullet | quote | caption
	Text  string `json:"text"`
	Level int    `json:"level,omitempty"` // 2 on type=heading → Heading2
}

// GenDocx writes a styled Word document (OOXML zip with styles, fonts,
// theme, numbering). Empty or unstyled single-style bodies are rejected.
func GenDocx(title string, blocks []DocxBlock) ([]byte, error) {
	return GenDocxDoc(DocxDoc{Title: title, Blocks: blocks})
}

// GenDocxDoc writes one production-shaped Word file. Reports get a cover
// page; novels get title + author then chapter Heading 1.
func GenDocxDoc(doc DocxDoc) ([]byte, error) {
	doc = normalizeDocxDoc(doc)
	if err := validateDocxSpec(doc); err != nil {
		return nil, err
	}
	body := xmlDecl + `
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><w:body>` +
		buildDocxBody(doc) +
		`<w:sectPr><w:pgSz w:w="11906" w:h="16838"/><w:pgMar w:top="1440" w:right="1800" w:bottom="1440" w:left="1800"/><w:pgNumType w:fmt="decimal"/></w:sectPr></w:body></w:document>`
	data, err := zipBytes([]zipPart{
		{"[Content_Types].xml", contentTypesDocx()},
		{"_rels/.rels", relsDocx},
		{"word/document.xml", body},
		{"word/_rels/document.xml.rels", wordRelsDocx},
		{"word/styles.xml", docxStylesXML},
		{"word/numbering.xml", docxNumberingXML},
		{"word/fontTable.xml", docxFontTableXML},
		{"word/settings.xml", docxSettingsXML},
		{"word/theme/theme1.xml", themeXML},
		{"docProps/core.xml", coreXML(doc.Title)},
	})
	if err != nil {
		return nil, err
	}
	if err := ValidateDocx(data); err != nil {
		return nil, err
	}
	return data, nil
}

// SlideSpec is one pptx slide. Layout is title (cover), section, or content.
type SlideSpec struct {
	Title    string   `json:"title"`
	Subtitle string   `json:"subtitle,omitempty"`
	Bullets  []string `json:"bullets"`
	Layout   string   `json:"layout,omitempty"`
}

// GenPptx writes a minimal-but-valid PowerPoint deck (OOXML zip:
// presentation, slide master, one layout reused by every slide, theme).
func GenPptx(title string, slides []SlideSpec) ([]byte, error) {
	if len(slides) == 0 || len(slides) > MaxPptxSlides {
		return nil, fmt.Errorf("%w: slides must be 1-%d", ErrLimit, MaxPptxSlides)
	}
	if err := validateSlideSpecs(title, slides); err != nil {
		return nil, err
	}
	parts := []zipPart{
		{"[Content_Types].xml", contentTypesPptx(len(slides))},
		{"_rels/.rels", relsPptx},
		{"ppt/presentation.xml", presentationXML(len(slides))},
		{"ppt/_rels/presentation.xml.rels", presentationRels(len(slides))},
		{"ppt/slideMasters/slideMaster1.xml", slideMasterXML},
		{"ppt/slideMasters/_rels/slideMaster1.xml.rels", slideMasterRels},
		{"ppt/slideLayouts/slideLayout1.xml", slideLayoutXML},
		{"ppt/slideLayouts/_rels/slideLayout1.xml.rels", slideLayoutRels},
		{"ppt/theme/theme1.xml", themeXML},
	}
	for i, s := range slides {
		if len(s.Bullets) > MaxBulletsPerSld {
			s.Bullets = s.Bullets[:MaxBulletsPerSld]
		}
		name := fmt.Sprintf("ppt/slides/slide%d.xml", i+1)
		parts = append(parts, zipPart{name, slideXML(i, len(slides), title, s)})
		rels := xmlDecl + `
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slideLayout" Target="../slideLayouts/slideLayout1.xml"/></Relationships>`
		parts = append(parts, zipPart{path.Dir(name) + "/_rels/" + path.Base(name) + ".rels", rels})
	}
	// Deck title lands in the presentation properties (core.xml).
	parts = append(parts,
		zipPart{"docProps/core.xml", coreXML(title)},
		zipPart{"docProps/app.xml", appXML(len(slides))},
	)
	data, err := zipBytes(parts)
	if err != nil {
		return nil, err
	}
	if err := ValidatePptx(data); err != nil {
		return nil, err
	}
	return data, nil
}

// GenPDF renders title + body paragraphs through gofpdf (A4, built-in
// helvetica; CJK bodies are not shaped - callers should keep PDF bodies
// Latin or accept gofpdf substitution).
func GenPDF(title, body string) ([]byte, error) {
	if len([]rune(body)) > MaxPDFBodyRunes {
		return nil, fmt.Errorf("%w: pdf body exceeds %d runes", ErrLimit, MaxPDFBodyRunes)
	}
	pdf := gofpdf.New("P", "mm", "A4", "")
	pdf.SetTitle(title, true)
	pdf.AddPage()
	pdf.SetFont("helvetica", "B", 18)
	pdf.MultiCell(0, 10, title, "", "L", false)
	pdf.Ln(2)
	pdf.SetFont("helvetica", "", 11)
	for _, para := range strings.Split(body, "\n") {
		if strings.TrimSpace(para) == "" {
			pdf.Ln(3)
			continue
		}
		pdf.MultiCell(0, 6, para, "", "L", false)
		pdf.Ln(1)
	}
	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// --- OOXML plumbing -----------------------------------------------------

type zipPart struct {
	name string
	body string
}

func zipBytes(parts []zipPart) ([]byte, error) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	names := make([]string, 0, len(parts))
	for _, p := range parts {
		names = append(names, p.name)
	}
	sort.Strings(names)
	for _, name := range names {
		for _, p := range parts {
			if p.name != name {
				continue
			}
			w, err := zw.Create(name)
			if err != nil {
				return nil, err
			}
			if _, err := w.Write([]byte(p.body)); err != nil {
				return nil, err
			}
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func xmlEscape(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '&':
			b.WriteString("&amp;")
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		case '"':
			b.WriteString("&quot;")
		case '\'':
			b.WriteString("&apos;")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// --- Preview extraction (P2-2 workspace.artifact.preview) ---------------

// MaxPreviewRunes bounds every extracted text preview.
const MaxPreviewRunes = 20000

var (
	reDocxText = regexp.MustCompile(`<w:t(?:\s[^>]*)?>(.*?)</w:t>`)
	reDocxPara = regexp.MustCompile(`</w:p>`)
	rePptxText = regexp.MustCompile(`<a:t(?:\s[^>]*)?>(.*?)</a:t>`)
)

// ExtractDocxText pulls a bounded plain-text preview out of a .docx body
// (word/document.xml). Paragraph boundaries become newlines; XML entities
// are unescaped. Bytes outside the OOXML zip or oversized parts fail closed.
func ExtractDocxText(data []byte) (string, error) {
	body, err := readZipPart(data, "word/document.xml", 4<<20)
	if err != nil {
		return "", fmt.Errorf("officetools: docx preview: %w", err)
	}
	var out strings.Builder
	// Split on paragraph ends first so runs inside one paragraph join.
	for _, para := range reDocxPara.Split(body, -1) {
		texts := reDocxText.FindAllStringSubmatch(para, -1)
		if len(texts) == 0 {
			continue
		}
		for _, m := range texts {
			out.WriteString(xmlUnescape(m[1]))
		}
		out.WriteByte('\n')
		if out.Len() > MaxPreviewRunes {
			return truncateRunes(out.String(), MaxPreviewRunes), nil
		}
	}
	return truncateRunes(out.String(), MaxPreviewRunes), nil
}

// ExtractPptxText pulls a bounded plain-text preview out of a .pptx deck
// (ppt/slides/slideN.xml in numeric order, one slide per line group).
func ExtractPptxText(data []byte) (string, error) {
	names, err := zipEntryNames(data)
	if err != nil {
		return "", fmt.Errorf("officetools: pptx preview: %w", err)
	}
	slides := make([]string, 0, MaxPptxSlides)
	for _, name := range names {
		if !strings.HasPrefix(name, "ppt/slides/slide") || !strings.HasSuffix(name, ".xml") {
			continue
		}
		slides = append(slides, name)
	}
	sort.Slice(slides, func(i, j int) bool {
		return slideIndex(slides[i]) < slideIndex(slides[j])
	})
	if len(slides) > MaxPptxSlides {
		slides = slides[:MaxPptxSlides]
	}
	var out strings.Builder
	for i, name := range slides {
		body, err := readZipPart(data, name, 1<<20)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(&out, "—— 幻灯片 %d ——\n", i+1)
		for _, m := range rePptxText.FindAllStringSubmatch(body, -1) {
			out.WriteString(xmlUnescape(m[1]))
			out.WriteByte('\n')
		}
		if out.Len() > MaxPreviewRunes {
			break
		}
	}
	return truncateRunes(out.String(), MaxPreviewRunes), nil
}

func slideIndex(name string) int {
	digits := strings.TrimSuffix(strings.TrimPrefix(name, "ppt/slides/slide"), ".xml")
	n, _ := strconv.Atoi(digits)
	return n
}

func readZipPart(data []byte, name string, limit int64) (string, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", err
	}
	for _, f := range zr.File {
		if f.Name != name || f.UncompressedSize64 > uint64(limit) {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return "", err
		}
		defer rc.Close()
		b, err := io.ReadAll(io.LimitReader(rc, limit))
		if err != nil {
			return "", err
		}
		return string(b), nil
	}
	return "", fmt.Errorf("part %q not found", name)
}

func zipEntryNames(data []byte) ([]string, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(zr.File))
	for _, f := range zr.File {
		names = append(names, f.Name)
	}
	return names, nil
}

func xmlUnescape(s string) string {
	// html.UnescapeString covers the five predefined XML entities plus
	// common numeric forms without following external DTDs.
	return html.UnescapeString(s)
}

func truncateRunes(s string, max int) string {
	if len(s) <= max {
		return s
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}
