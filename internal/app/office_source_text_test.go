package app

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jung-kurt/gofpdf"
	"github.com/lunitide/lunitide/internal/doctext"
	domain "github.com/lunitide/lunitide/internal/domain/officestudio"
	"github.com/lunitide/lunitide/internal/ocrapp"
)

func blankScanPDF(t *testing.T) []byte {
	t.Helper()
	doc := gofpdf.New("P", "mm", "A4", "")
	doc.AddPage()
	var buf bytes.Buffer
	if err := doc.Output(&buf); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func mixedLayerScanPDF(t *testing.T) []byte {
	t.Helper()
	doc := gofpdf.New("P", "mm", "A4", "")
	doc.AddPage()
	doc.SetFont("helvetica", "", 12)
	doc.Cell(40, 10, "Cover layer page one")
	doc.AddPage()
	doc.AddPage()
	doc.SetFont("helvetica", "", 12)
	doc.Cell(40, 10, "Closing layer page three")
	var buf bytes.Buffer
	if err := doc.Output(&buf); err != nil {
		t.Fatal(err)
	}
	pages, err := doctext.ExtractPDFPages(buf.Bytes())
	if err != nil {
		t.Fatalf("mixed fixture must have a readable text layer: %v", err)
	}
	if len(pages) != 3 || strings.TrimSpace(pages[0].Text) == "" || strings.TrimSpace(pages[1].Text) != "" || strings.TrimSpace(pages[2].Text) == "" {
		t.Fatalf("mixed fixture coverage = %+v", pages)
	}
	return buf.Bytes()
}

func TestOfficeSourceTextPagingPreservesAllRunesAndEvidence(t *testing.T) {
	source := officeText{text: strings.Repeat("中文商业计划书\"数据\"\n<引用>\\\t", 600), method: "windows-ocr", pages: 27, complete: true}
	version := domain.Version{ID: "v1", SHA256: "immutable-hash", Kind: "pdf"}
	var restored strings.Builder
	offset := 0
	for i := 0; i < 100; i++ {
		raw, err := officeSourceTextPage("task", version, source, offset)
		if err != nil {
			t.Fatal(err)
		}
		if len(raw) > officeToolPageLimit {
			t.Fatalf("oversized tool page: %d", len(raw))
		}
		var page struct {
			Text           string
			NextTextOffset int
			HasMore        bool
			Notice         string
			Method         string
			PageCount      int
		}
		if err = json.Unmarshal([]byte(raw), &page); err != nil {
			t.Fatal(err)
		}
		if page.Method != "windows-ocr" || page.PageCount != 27 || !strings.Contains(page.Notice, "misread") {
			t.Fatal("missing OCR uncertainty", raw)
		}
		restored.WriteString(page.Text)
		if !page.HasMore {
			break
		}
		if page.NextTextOffset <= offset {
			t.Fatal("paging made no progress")
		}
		offset = page.NextTextOffset
	}
	if restored.String() != source.text {
		t.Fatal("source text was truncated or duplicated")
	}
	if _, err := officeSourceTextPage("task", version, source, -1); err == nil {
		t.Fatal("negative offset accepted")
	}
}

func TestOfficeVersionTextUnwiredMixedPDFIsNotComplete(t *testing.T) {
	e := NewEngine(nil, "test")
	v := domain.Version{Kind: "pdf", SHA256: "unwired-mixed", Name: "mixed.pdf"}
	got, err := e.officeVersionText(context.Background(), v, mixedLayerScanPDF(t))
	if err != nil {
		t.Fatal(err)
	}
	if got.complete {
		t.Fatal("unwired mixed PDF must not claim complete coverage")
	}
	if !strings.Contains(got.text, "Cover") {
		t.Fatalf("known text-layer pages must remain: %+v", got)
	}
}

func TestOfficeVersionTextFailsClosedWhenOCRUnwired(t *testing.T) {
	e := NewEngine(nil, "test")
	v := domain.Version{Kind: "pdf", SHA256: "abc123deadbeef", Name: "scan.pdf"}
	_, err := e.officeVersionText(context.Background(), v, blankScanPDF(t))
	if err == nil || !strings.Contains(err.Error(), "OCR 未装配，无法处理扫描 PDF") {
		t.Fatalf("unwired OCR must fail closed, not use a second pipeline: %v", err)
	}
}

func TestOfficeVersionTextWiredOCRFailureIsChinese(t *testing.T) {
	e := NewEngine(nil, "test")
	svc := ocrapp.New(ocrapp.NewFileStore(filepath.Join(t.TempDir(), "ocr-routing.json")))
	svc.SetRenderPDF(nil)
	svc.SetLocalPDF(nil)
	e.SetOCR(svc)
	v := domain.Version{Kind: "pdf", SHA256: "wired-fail", Name: "scan.pdf"}
	_, err := e.officeVersionText(context.Background(), v, []byte("%PDF-1.4"))
	if err == nil || !strings.Contains(err.Error(), "识别未能完成") {
		t.Fatalf("wired OCR failure must be Chinese fail-closed: %v", err)
	}
	if strings.Contains(err.Error(), "PDF text unavailable") || strings.Contains(err.Error(), "OCR routing could not complete") {
		t.Fatalf("must not leak English office OCR gap: %v", err)
	}
}

func TestOfficeVersionTextCacheFollowsOCRRoutingRevision(t *testing.T) {
	e := NewEngine(nil, "test")
	svc := ocrapp.New(ocrapp.NewFileStore(filepath.Join(t.TempDir(), "ocr-routing.json")))
	label := "first-pass"
	svc.SetLocalPDF(func(context.Context, []byte) (doctext.PDFOCRResult, error) {
		return doctext.PDFOCRResult{Method: "windows-ocr", Pages: []doctext.OCRPage{{Page: 1, Text: label}}}, nil
	})
	e.SetOCR(svc)
	v := domain.Version{Kind: "pdf", SHA256: "abc123deadbeef", Name: "scan.pdf"}
	data := []byte("%PDF-1.4")
	first, err := e.officeVersionText(context.Background(), v, data)
	if err != nil || !strings.Contains(first.text, "first-pass") {
		t.Fatalf("first %+v %v", first, err)
	}
	label = "after-reroute"
	cur, _ := svc.Routing()
	if _, err := svc.SetRouting(ocrapp.Routing{PreferProvider: false}, cur.Revision); err != nil {
		t.Fatal(err)
	}
	second, err := e.officeVersionText(context.Background(), v, data)
	if err != nil || !strings.Contains(second.text, "after-reroute") {
		t.Fatalf("SHA cache survived OCR routing change: %+v %v", second, err)
	}
}

func TestOfficeVersionTextDoesNotTreatIncompleteOCRAsComplete(t *testing.T) {
	e := NewEngine(nil, "test")
	svc := ocrapp.New(ocrapp.NewFileStore(filepath.Join(t.TempDir(), "ocr-routing.json")))
	localPDF := 0
	svc.SetRenderPDF(nil)
	svc.SetLocalPDF(func(context.Context, []byte) (doctext.PDFOCRResult, error) {
		localPDF++
		t.Fatal("mixed PDF must not re-OCR the whole document")
		return doctext.PDFOCRResult{}, nil
	})
	e.SetOCR(svc)
	v := domain.Version{Kind: "pdf", SHA256: "mixed-incomplete-sha", Name: "mixed.pdf"}
	data := mixedLayerScanPDF(t)
	first, err := e.officeVersionText(context.Background(), v, data)
	if err != nil {
		t.Fatal(err)
	}
	if first.complete {
		t.Fatalf("unread scan page must not be cached as complete: %+v", first)
	}
	if !strings.Contains(first.text, "Cover") {
		t.Fatalf("known text-layer pages must remain: %+v", first)
	}
	page, err := officeSourceTextPage("task", v, first, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(page, "incomplete") || strings.Contains(page, `"coverageComplete":true`) {
		t.Fatalf("inspect must declare incomplete coverage: %s", page)
	}
	second, err := e.officeVersionText(context.Background(), v, data)
	if err != nil || second.complete || localPDF != 0 {
		t.Fatalf("cached incomplete must stay incomplete without a second OCR pass: %+v %v calls=%d", second, err, localPDF)
	}
}
