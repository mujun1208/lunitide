package officetools

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/jung-kurt/gofpdf"
)

// The regular-weight font and its glyph coverage are bundled with the program,
// not discovered on the user's machine. See fonts/README.md and fonts/OFL.txt.
//
//go:embed fonts/LunitideSansSC-Regular.ttf.gz
var pdfFontCompressed []byte

//go:embed fonts/LunitideSansSC-Regular.cmap
var pdfFontCoverage []byte

const pdfFontSHA256 = "82b7b44a0e060caaa7bfc21cf10936ddf7452aef44c0f1ecda969403ac6d8fb8"

var loadPDFFont = sync.OnceValues(func() ([]byte, error) {
	r, err := gzip.NewReader(bytes.NewReader(pdfFontCompressed))
	if err != nil {
		return nil, err
	}
	defer r.Close()
	data, err := io.ReadAll(io.LimitReader(r, 16<<20))
	if err != nil {
		return nil, err
	}
	if len(data) >= 16<<20 || len(pdfFontCoverage) != 8192 {
		return nil, errors.New("embedded PDF font is invalid")
	}
	if fmt.Sprintf("%x", sha256.Sum256(data)) != pdfFontSHA256 {
		return nil, errors.New("embedded PDF font integrity check failed")
	}
	return data, nil
})

func pdfSupportsRune(r rune) bool {
	return r >= 0 && r <= 0xffff && len(pdfFontCoverage) == 8192 && pdfFontCoverage[r/8]&(1<<uint(r%8)) != 0
}

func pdfNeedsUnicode(text string) (bool, error) {
	if !utf8.ValidString(text) {
		return false, errors.New("PDF 内容不是有效的 UTF-8 文本")
	}
	unicodeFont := false
	for _, r := range text {
		if r == '\n' || r == '\r' || r == '\t' {
			continue
		}
		if r < 32 || r == 127 {
			return false, fmt.Errorf("PDF 内容含不可打印字符 U+%04X", r)
		}
		if r > 127 {
			if !pdfSupportsRune(r) {
				return false, fmt.Errorf("PDF 内置字体暂不支持字符 U+%04X；请替换该字符或生成 Word，未生成乱码文件", r)
			}
			unicodeFont = true
		}
	}
	return unicodeFont, nil
}

// GenPDF renders complete A4 title/body text. ASCII keeps the existing
// Helvetica rendering; Unicode uses an embedded, subsetted Chinese font.
func GenPDF(title, body string) ([]byte, error) {
	return genPDF(title, body, false)
}

// GenStablePDF uses fixed document metadata and sorted resources for managed
// snapshots. The same input can safely retry under an idempotency key.
func GenStablePDF(title, body string) ([]byte, error) { return genPDF(title, body, true) }

func genPDF(title, body string, stable bool) ([]byte, error) {
	if utf8.RuneCountInString(body) > MaxPDFBodyRunes {
		return nil, fmt.Errorf("%w: pdf body exceeds %d runes", ErrLimit, MaxPDFBodyRunes)
	}
	unicodeFont, err := pdfNeedsUnicode(title + body)
	if err != nil {
		return nil, err
	}
	pdf := gofpdf.New("P", "mm", "A4", "")
	if stable {
		pdf.SetCatalogSort(true)
		pdf.SetCreationDate(time.Date(2000, time.January, 1, 0, 0, 0, 0, time.UTC))
		pdf.SetModificationDate(time.Date(2000, time.January, 1, 0, 0, 0, 0, time.UTC))
	}
	family, titleStyle := "helvetica", "B"
	if unicodeFont {
		font, err := loadPDFFont()
		if err != nil {
			return nil, fmt.Errorf("PDF 中文字体不可用，未生成文件: %w", err)
		}
		family, titleStyle = "lunitide-sans-sc", ""
		pdf.AddUTF8FontFromBytes(family, "", font)
	}
	pdf.SetTitle(title, true)
	pdf.AddPage()
	pdf.SetFont(family, titleStyle, 18)
	writePDFParagraph(pdf, title, 10, unicodeFont)
	pdf.Ln(2)
	pdf.SetFont(family, "", 11)
	for _, para := range strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n") {
		if strings.TrimSpace(para) == "" {
			pdf.Ln(3)
			continue
		}
		writePDFParagraph(pdf, para, 6, unicodeFont)
		pdf.Ln(1)
	}
	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func writePDFParagraph(pdf *gofpdf.Fpdf, text string, height float64, unicodeFont bool) {
	text = strings.ReplaceAll(strings.ReplaceAll(text, "\r", ""), "\t", "    ")
	if !unicodeFont {
		pdf.MultiCell(0, height, text, "", "L", false)
		return
	}
	// gofpdf.MultiCell treats a Chinese line-break character like a space and
	// skips it (i = sep + 1). Lay out Unicode explicitly so every glyph survives.
	pageWidth, _ := pdf.GetPageSize()
	_, _, right, _ := pdf.GetMargins()
	maxWidth := pageWidth - right - pdf.GetX() - 2*pdf.GetCellMargin()
	for _, explicitLine := range strings.Split(text, "\n") {
		chars := []rune(explicitLine)
		if len(chars) == 0 {
			pdf.Ln(height)
		}
		for start := 0; start < len(chars); {
			end, lastSpace, width := start, -1, 0.0
			for end < len(chars) {
				next := pdf.GetStringWidth(string(chars[end]))
				if width+next > maxWidth && end > start {
					break
				}
				width += next
				if chars[end] == ' ' {
					lastSpace = end
				}
				end++
			}
			if end < len(chars) && lastSpace > start && lastSpace > end-16 {
				end = lastSpace + 1 // include the space; never skip a content glyph
			}
			pdf.CellFormat(0, height, string(chars[start:end]), "", 1, "L", false, 0, "")
			start = end
		}
	}
}
