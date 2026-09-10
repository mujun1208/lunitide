package doctext

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/jung-kurt/gofpdf"
)

func TestPDFCopySplitMergeDoesNotTouchForms(t *testing.T) {
	raw := miniPDF(t, []string{"page-one", "page-two"})
	parts, err := SplitPDFCopies(raw)
	if err != nil || len(parts) != 2 {
		t.Fatalf("split %+v %v", parts, err)
	}
	merged, err := MergePDFCopies([][]byte{parts[1], parts[0]})
	if err != nil {
		t.Fatal(err)
	}
	pages, err := ExtractPDFPages(merged)
	if err != nil || len(pages) != 2 {
		t.Fatalf("merged pages %+v %v", pages, err)
	}
	joined := pages[0].Text + pages[1].Text
	if !strings.Contains(joined, "page-two") || !strings.Contains(joined, "page-one") {
		t.Fatalf("page order copy lost: %q", joined)
	}
	if _, err := ApplyPDFForm(raw, map[string]string{"x": "y"}); err == nil {
		t.Fatal("form/signature edits must stay unsupported")
	}
}

func TestPDFCopyRefusesFormsAndDeclaresLossy(t *testing.T) {
	if !PDFCopyLossy || PDFCopyMethod != "lossy_text_rerender" {
		t.Fatal("HAT-10 must declare split/merge as lossy text rerender")
	}
	form := []byte("%PDF-1.4\n1 0 obj<</AcroForm 2 0 R>>endobj\n")
	if _, err := SplitPDFCopies(form); !errors.Is(err, ErrUnsupportedForm) {
		t.Fatalf("form split: %v", err)
	}
	signed := []byte("%PDF-1.4\n/Type /Sig\n")
	if _, err := MergePDFCopies([][]byte{signed}); !errors.Is(err, ErrUnsupportedForm) {
		t.Fatalf("signature merge: %v", err)
	}
}

func miniPDF(t *testing.T, pages []string) []byte {
	t.Helper()
	p := gofpdf.New("P", "mm", "A4", "")
	for _, text := range pages {
		p.AddPage()
		p.SetFont("Helvetica", "", 12)
		p.Cell(40, 10, text)
	}
	var buf bytes.Buffer
	if err := p.Output(&buf); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
