package doctext

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/ledongthuc/pdf"
)

// PDFPageText is one page of a PDF text layer. Empty Text means that page
// has no extractable layer and still needs OCR.
type PDFPageText struct {
	Page int
	Text string
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
		fonts := map[string]*pdf.Font{}
		for _, name := range page.Fonts() {
			if _, ok := fonts[name]; !ok {
				font := page.Font(name)
				fonts[name] = &font
			}
		}
		text, pageErr := pdfPageText(page, fonts)
		if pageErr != nil {
			text = ""
		}
		pages = append(pages, PDFPageText{Page: i, Text: strings.TrimSpace(text)})
	}
	if len(pages) == 0 {
		return nil, ErrNoTextLayer
	}
	return pages, nil
}

func PDFPagesNeedOCR(pages []PDFPageText) bool {
	for _, p := range pages {
		if strings.TrimSpace(p.Text) == "" {
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
