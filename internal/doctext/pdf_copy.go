package doctext

import (
	"bytes"
	"errors"
	"strings"

	"github.com/jung-kurt/gofpdf"
)

var ErrUnsupportedForm = errors.New("PDF 表单或签名变更未支持")

const (
	PDFCopyMethod = "lossy_text_rerender"
	PDFCopyLossy  = true
)

func pdfHasProtectedStructure(raw []byte) bool {
	s := string(raw)
	return strings.Contains(s, "/AcroForm") || strings.Contains(s, "/XFA") || strings.Contains(s, "/Type /Sig") || strings.Contains(s, "/FT /Sig")
}

func SplitPDFCopies(raw []byte) ([][]byte, error) {
	if pdfHasProtectedStructure(raw) {
		return nil, ErrUnsupportedForm
	}
	pages, err := ExtractPDFPages(raw)
	if err != nil {
		return nil, err
	}
	out := make([][]byte, 0, len(pages))
	for _, p := range pages {
		part, err := renderTextPDF([]string{strings.TrimSpace(p.Text)})
		if err != nil {
			return nil, err
		}
		out = append(out, part)
	}
	return out, nil
}

func MergePDFCopies(parts [][]byte) ([]byte, error) {
	var texts []string
	for _, part := range parts {
		if pdfHasProtectedStructure(part) {
			return nil, ErrUnsupportedForm
		}
		pages, err := ExtractPDFPages(part)
		if err != nil {
			return nil, err
		}
		for _, p := range pages {
			texts = append(texts, strings.TrimSpace(p.Text))
		}
	}
	return renderTextPDF(texts)
}

func ApplyPDFForm([]byte, map[string]string) ([]byte, error) {
	return nil, ErrUnsupportedForm
}

func renderTextPDF(pages []string) ([]byte, error) {
	p := gofpdf.New("P", "mm", "A4", "")
	if len(pages) == 0 {
		p.AddPage()
	}
	for _, text := range pages {
		p.AddPage()
		p.SetFont("Helvetica", "", 12)
		p.Cell(40, 10, text)
	}
	var buf bytes.Buffer
	if err := p.Output(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
