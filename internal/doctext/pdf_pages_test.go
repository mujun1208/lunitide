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

func TestPDFPagesNeedOCRIgnoresParseFailed(t *testing.T) {
	pages := []doctext.PDFPageText{{Page: 1, ParseFailed: true}}
	if doctext.PDFPagesNeedOCR(pages) {
		t.Fatal("parse-failed pages must not be treated as blank OCR targets")
	}
	if !doctext.PDFPagesParseFailed(pages) {
		t.Fatal("parse-failed flag must stay visible")
	}
	pages = append(pages, doctext.PDFPageText{Page: 2})
	if !doctext.PDFPagesNeedOCR(pages) {
		t.Fatal("a blank readable page still needs OCR")
	}
}

func TestExtractPDFPagesRejectsNonPDF(t *testing.T) {
	if _, err := doctext.ExtractPDFPages([]byte("not a pdf")); err != doctext.ErrUnsupportedFormat {
		t.Fatalf("err = %v", err)
	}
}
