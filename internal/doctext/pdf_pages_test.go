package doctext_test

import (
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/doctext"
	"github.com/lunitide/lunitide/internal/officetools"
)

func TestPDFTextLayerReadableRejectsControlSoup(t *testing.T) {
	if !doctext.PDFTextLayerReadable("销售助手需求：客户跟进、报价和下一步。") {
		t.Fatal("chinese text must stay readable")
	}
	if !doctext.PDFTextLayerReadable("Night of the Living Dead is a public-domain film.") {
		t.Fatal("english text must stay readable")
	}
	if !doctext.PDFTextLayerReadable("2026-09-23 09:00 100") {
		t.Fatal("a dated schedule must stay readable")
	}
	soup := strings.Repeat("\x01", 40) + "FMJSD" + strings.Repeat("\x02", 40)
	if doctext.PDFTextLayerReadable(soup) {
		t.Fatal("control-character text layer must not be treated as readable")
	}
}

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
