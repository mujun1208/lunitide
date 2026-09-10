package doctext_test

import (
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/doctext"
	"github.com/lunitide/lunitide/internal/officetools"
)

func TestExtractPDFPagesReportsTextLayerCoverage(t *testing.T) {
	data, err := officetools.GenPDF("Title page", "Body of the only page with a real text layer.")
	if err != nil {
		t.Fatal(err)
	}
	pages, err := doctext.ExtractPDFPages(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(pages) != 1 || pages[0].Page != 1 {
		t.Fatalf("pages = %+v", pages)
	}
	if doctext.PDFPagesNeedOCR(pages) {
		t.Fatalf("generated PDF should not need OCR: %+v", pages)
	}
	joined := doctext.JoinPDFPages(pages)
	if !strings.Contains(joined, "Body") && !strings.Contains(strings.ReplaceAll(pages[0].Text, " ", ""), "Bodyoftheonlypage") {
		t.Fatalf("page text missing: %q", pages[0].Text)
	}
}

func TestExtractPDFPagesRejectsNonPDF(t *testing.T) {
	if _, err := doctext.ExtractPDFPages([]byte("not a pdf")); err != doctext.ErrUnsupportedFormat {
		t.Fatalf("err = %v", err)
	}
}
