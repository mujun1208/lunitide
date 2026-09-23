package doctext

import (
	"bytes"
	"fmt"
	"strings"
	"unicode"

	"github.com/ledongthuc/pdf"
)

// PDFPageText is one page of a PDF text layer. Empty Text means that page
// has no extractable layer and still needs OCR. ParseFailed means the page
// object or content stream could not be read; that is not a blank page.
type PDFPageText struct {
	Page        int
	Text        string
	ParseFailed bool
}

// ExtractPDFPages returns per-page text-layer coverage. It does not OCR.
// A page with only whitespace is reported as empty so callers can OCR it
// without re-processing pages that already have a confirmed layer.
func ExtractPDFPages(raw []byte) (pages []PDFPageText, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			pages = nil
			err = ErrNoTextLayer
		}
	}()
	if len(raw) > MaxInputBytes {
		return nil, ErrBudgetExceeded
	}
	if !bytes.HasPrefix(raw, []byte("%PDF-")) {
		return nil, ErrUnsupportedFormat
	}
	reader, err := pdf.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return nil, fmt.Errorf("doctext: pdf: %w", err)
	}
	n := reader.NumPage()
	if n > 500 {
		return nil, ErrBudgetExceeded
	}
	pages = make([]PDFPageText, 0, n)
	for i := 1; i <= n; i++ {
		page := reader.Page(i)
		if page.V.IsNull() || page.V.Key("Contents").IsNull() {
			pages = append(pages, PDFPageText{Page: i, ParseFailed: true})
			continue
		}
		fonts := map[string]*pdf.Font{}
		for _, name := range page.Fonts() {
			if _, ok := fonts[name]; !ok {
				font := page.Font(name)
				fonts[name] = &font
			}
		}
		text, pageErr := pdfPageText(page, fonts)
		if pageErr != nil {
			pages = append(pages, PDFPageText{Page: i, ParseFailed: true})
			continue
		}
		pages = append(pages, PDFPageText{Page: i, Text: strings.TrimSpace(text)})
	}
	if len(pages) == 0 {
		return nil, ErrNoTextLayer
	}
	return pages, nil
}

func PDFPagesParseFailed(pages []PDFPageText) bool {
	for _, p := range pages {
		if p.ParseFailed {
			return true
		}
	}
	return false
}

// PDFTextLayerReadable reports whether extracted PDF text can be shown to a
// model. Control-character soup from a font with no Unicode map is not text.
// Letters and digits count, including a schedule that is mostly dates and amounts.
func PDFTextLayerReadable(text string) bool {
	var controls, readable, total int
	for _, r := range text {
		if unicode.IsSpace(r) {
			continue
		}
		total++
		switch {
		case r < 0x20 || (r >= 0x7f && r <= 0x9f):
			controls++
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			readable++
		}
	}
	if total == 0 || controls*5 > total {
		return false
	}
	return readable*2 > total
}

func PDFPageNeedsOCR(p PDFPageText) bool {
	return !p.ParseFailed && strings.TrimSpace(p.Text) == ""
}

func PDFPagesNeedOCR(pages []PDFPageText) bool {
	for _, p := range pages {
		if PDFPageNeedsOCR(p) {
			return true
		}
	}
	return false
}

func JoinPDFPages(pages []PDFPageText) string {
	var b strings.Builder
	for _, p := range pages {
		fmt.Fprintf(&b, "\n[PDF page %d]\n%s\n", p.Page, p.Text)
	}
	return b.String()
}
