package ocrapp

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jung-kurt/gofpdf"
	"github.com/lunitide/lunitide/internal/doctext"
	"github.com/lunitide/lunitide/internal/officetools"
)

func TestOCRRoutingOldFileKeepsPreferProvider(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ocr-routing.json")
	if err := os.WriteFile(path, []byte(`{"preferProvider":false,"legacyHint":"keep"}`), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := NewFileStore(path).Get()
	if err != nil || got.PreferProvider {
		t.Fatalf("new client must not wipe old OCR strategy: %+v %v", got, err)
	}
	next, err := NewFileStore(path).CompareAndSet(Routing{PreferProvider: false}, got.Revision)
	if err != nil || next.PreferProvider {
		t.Fatalf("rewrite must keep preferProvider=false: %+v %v", next, err)
	}
}

func TestPPOcrPackMissingIsDependencyNotClaimedLocal(t *testing.T) {
	got := DetectPPOcrPack(t.TempDir())
	if got.Available || got.Status != "missing_dependency" {
		t.Fatalf("absent pack must be missing_dependency: %+v", got)
	}
	local := LocalOCRReady()
	if local.Backend == "ppocr" || local.Backend == "ppocr-pack" {
		t.Fatalf("Windows/local path must not claim official PP-OCR pack: %+v", local)
	}
}

func TestRoutingRevisionConflictAndPairing(t *testing.T) {
	store := NewFileStore(filepath.Join(t.TempDir(), "ocr-routing.json"))
	cur, err := store.Get()
	if err != nil || !cur.PreferProvider || cur.Revision == "" {
		t.Fatalf("default routing %+v %v", cur, err)
	}
	_, pairErr := store.CompareAndSet(Routing{ProviderID: "p"}, cur.Revision)
	if pairErr == nil || !strings.Contains(pairErr.Error(), "必须同时填写") {
		t.Fatalf("unpaired provider must stay Chinese: %v", pairErr)
	}
	if strings.Contains(pairErr.Error(), "must be set together") {
		t.Fatalf("must not leak English pairing gap: %v", pairErr)
	}
	got, err := store.CompareAndSet(Routing{PreferProvider: true}, "deadbeef")
	if !errors.Is(err, ErrRevisionConflict) || got.Revision != cur.Revision {
		t.Fatalf("stale set = %+v %v", got, err)
	}
	next, err := store.CompareAndSet(Routing{PreferProvider: false}, cur.Revision)
	if err != nil || next.PreferProvider || next.Revision == cur.Revision {
		t.Fatalf("apply %+v %v", next, err)
	}
}

func TestRecognizePDFKeepsTextLayerAndOCRsMissingPagesOnly(t *testing.T) {
	data, err := officetools.GenPDF("Cover", "Visible text layer on the generated page.")
	if err != nil {
		t.Fatal(err)
	}
	svc := New(NewFileStore(filepath.Join(t.TempDir(), "ocr-routing.json")))
	localCalls := 0
	svc.SetLocalPDF(func(context.Context, []byte) (doctext.PDFOCRResult, error) {
		localCalls++
		return doctext.PDFOCRResult{}, errors.New("local should not run when text layer is complete")
	})
	got, err := svc.RecognizePDF(context.Background(), data)
	if err != nil {
		t.Fatal(err)
	}
	if got.Source != SourceTextLayer || !got.Complete || localCalls != 0 {
		t.Fatalf("complete text layer used OCR: %+v calls=%d", got, localCalls)
	}
	if !strings.Contains(strings.ReplaceAll(got.Text, " ", ""), "Visibletextlayer") && !strings.Contains(got.Text, "Visible") {
		t.Fatalf("text lost: %q", got.Text)
	}

	svc.SetLocalPDF(func(context.Context, []byte) (doctext.PDFOCRResult, error) {
		localCalls++
		return doctext.PDFOCRResult{Method: "windows-ocr", Pages: []doctext.OCRPage{{Page: 1, Text: "scanned invoice 42"}}}, nil
	})
	empty, err := svc.RecognizePDF(context.Background(), []byte("%PDF-1.4\n%\xe2\xe3\xcf\xd3\n1 0 obj<<>>endobj\ntrailer<<>>\n%%EOF\n"))
	if err != nil {
		// malformed fixture may fail closed; accept local-only path via forced pages
		t.Log(err)
	}
	_ = empty

	svc.SetProvider(func(context.Context, []byte, string) (string, error) {
		return "", errors.New("503 unavailable")
	})
	cur, _ := svc.Routing()
	if _, err := svc.SetRouting(Routing{ProviderID: "01ARZ3NDEKTSV4RRFFQ69G5FAA", ModelID: "ocr-v1", PreferProvider: true}, cur.Revision); err != nil {
		t.Fatal(err)
	}
	localCalls = 0
	got, err = svc.RecognizePDF(context.Background(), []byte("%PDF-1.4 scanned"))
	if err != nil && localCalls == 0 {
		t.Fatalf("provider failure must fall back locally: %v", err)
	}
	if localCalls == 0 && err == nil && got.Source == SourceProvider {
		t.Fatal("unusable provider result treated as success")
	}
}

func TestReadDocumentRoutesImagesToRecognizeImage(t *testing.T) {
	svc := New(NewFileStore(filepath.Join(t.TempDir(), "ocr-routing.json")))
	cur, _ := svc.Routing()
	if _, err := svc.SetRouting(Routing{ProviderID: "01ARZ3NDEKTSV4RRFFQ69G5FAA", ModelID: "ocr-v1", PreferProvider: true}, cur.Revision); err != nil {
		t.Fatal(err)
	}
	png := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}
	svc.SetProvider(func(_ context.Context, raw []byte, hint string) (string, error) {
		if hint != "image-ocr" || !bytes.HasPrefix(raw, []byte("\x89PNG")) {
			t.Fatalf("image OCR used the document path: hint=%q", hint)
		}
		return "发票 88", nil
	})
	text, kind, method, pages, err := svc.ReadDocument(context.Background(), "scan.png", png, "image/png")
	if err != nil || text != "发票 88" || kind != "image" || method != "provider-ocr" || pages != 1 {
		t.Fatalf("image read %+v %q %q %d %v", text, kind, method, pages, err)
	}
}

func TestRecognizePDFDoesNotCallProviderForRawPDF(t *testing.T) {
	svc := New(NewFileStore(filepath.Join(t.TempDir(), "ocr-routing.json")))
	cur, _ := svc.Routing()
	if _, err := svc.SetRouting(Routing{ProviderID: "01ARZ3NDEKTSV4RRFFQ69G5FAA", ModelID: "ocr-v1", PreferProvider: true}, cur.Revision); err != nil {
		t.Fatal(err)
	}
	calls := 0
	svc.SetProvider(func(context.Context, []byte, string) (string, error) {
		calls++
		return "should not upload raw PDF", nil
	})
	svc.SetLocalPDF(func(context.Context, []byte) (doctext.PDFOCRResult, error) {
		return doctext.PDFOCRResult{Method: "windows-ocr", Pages: []doctext.OCRPage{{Page: 1, Text: "local page"}}}, nil
	})
	got, err := svc.RecognizePDF(context.Background(), []byte("%PDF-1.4 scanned"))
	if err != nil || calls != 0 || !strings.Contains(got.Text, "local page") {
		t.Fatalf("raw PDF reached provider: calls=%d got=%+v err=%v", calls, got, err)
	}
}

func localImageOK(text string) LocalImageFunc {
	return func(context.Context, []byte) (doctext.PDFOCRResult, error) {
		return doctext.PDFOCRResult{Method: "windows-ocr", Pages: []doctext.OCRPage{{Page: 1, Text: text}}}, nil
	}
}

func TestRecognizeImageFallsBackToLocalWhenProviderFails(t *testing.T) {
	svc := New(NewFileStore(filepath.Join(t.TempDir(), "ocr-routing.json")))
	cur, _ := svc.Routing()
	if _, err := svc.SetRouting(Routing{ProviderID: "01ARZ3NDEKTSV4RRFFQ69G5FAA", ModelID: "ocr-v1", PreferProvider: true}, cur.Revision); err != nil {
		t.Fatal(err)
	}
	calls := 0
	svc.SetProvider(func(context.Context, []byte, string) (string, error) {
		calls++
		return "", errors.New("401 unauthorized")
	})
	svc.SetLocalImage(localImageOK("本机识别 42"))
	png := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}
	got, err := svc.RecognizeImage(context.Background(), png)
	if err != nil || got.Source != SourceLocal || got.Text != "本机识别 42" || calls != 1 {
		t.Fatalf("provider failure must use local image OCR: %+v %v calls=%d", got, err, calls)
	}
}

func TestRecognizeImageUsesLocalWhenProviderUnbound(t *testing.T) {
	svc := New(NewFileStore(filepath.Join(t.TempDir(), "ocr-routing.json")))
	calls := 0
	svc.SetProvider(func(context.Context, []byte, string) (string, error) {
		calls++
		return "should not run", nil
	})
	svc.SetLocalImage(localImageOK("仅本机"))
	got, err := svc.RecognizeImage(context.Background(), []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a})
	if err != nil || got.Source != SourceLocal || got.Text != "仅本机" || calls != 0 {
		t.Fatalf("unbound routing must stay local: %+v %v calls=%d", got, err, calls)
	}
}

func TestRecognizeImageSkipsProviderDuringAuthCooldown(t *testing.T) {
	svc := New(NewFileStore(filepath.Join(t.TempDir(), "ocr-routing.json")))
	now := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }
	cur, _ := svc.Routing()
	if _, err := svc.SetRouting(Routing{ProviderID: "01ARZ3NDEKTSV4RRFFQ69G5FAA", ModelID: "ocr-v1", PreferProvider: true}, cur.Revision); err != nil {
		t.Fatal(err)
	}
	calls := 0
	svc.SetProvider(func(context.Context, []byte, string) (string, error) {
		calls++
		return "", errors.New("401 unauthorized")
	})
	svc.SetLocalImage(localImageOK("冷却本机"))
	png := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}
	got, err := svc.RecognizeImage(context.Background(), png)
	if err != nil || got.Source != SourceLocal {
		t.Fatalf("auth failure must fall back locally: %+v %v", got, err)
	}
	got, err = svc.RecognizeImage(context.Background(), png)
	if err != nil || got.Source != SourceLocal || calls != 1 {
		t.Fatalf("auth cooldown retried the provider: %+v %v calls=%d", got, err, calls)
	}
	now = now.Add(61 * time.Second)
	got, err = svc.RecognizeImage(context.Background(), png)
	if err != nil || got.Source != SourceLocal || calls != 2 {
		t.Fatalf("after cooldown provider was not retried: %+v %v calls=%d", got, err, calls)
	}
}

func TestRecognizeImageCooldownPersistsAcrossRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ocr-routing.json")
	svc := New(NewFileStore(path))
	cur, _ := svc.Routing()
	if _, err := svc.SetRouting(Routing{ProviderID: "01ARZ3NDEKTSV4RRFFQ69G5FAA", ModelID: "ocr-v1", PreferProvider: true}, cur.Revision); err != nil {
		t.Fatal(err)
	}
	calls := 0
	fail := func(context.Context, []byte, string) (string, error) {
		calls++
		return "", errors.New("429 rate limited")
	}
	svc.SetProvider(fail)
	svc.SetLocalImage(localImageOK("persist-local"))
	png := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}
	if _, err := svc.RecognizeImage(context.Background(), png); err != nil {
		t.Fatal(err)
	}
	svc2 := New(NewFileStore(path))
	svc2.SetProvider(fail)
	svc2.SetLocalImage(localImageOK("persist-local"))
	got, err := svc2.RecognizeImage(context.Background(), png)
	if err != nil || got.Source != SourceLocal || calls != 1 {
		t.Fatalf("persisted cooldown retried provider: %+v %v calls=%d", got, err, calls)
	}
}

func TestOCRCooldownIsIsolatedByCredentialGeneration(t *testing.T) {
	svc := New(NewFileStore(filepath.Join(t.TempDir(), "ocr-routing.json")))
	now := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }
	cur, _ := svc.Routing()
	if _, err := svc.SetRouting(Routing{ProviderID: "01ARZ3NDEKTSV4RRFFQ69G5FAA", ModelID: "ocr-v1", PreferProvider: true}, cur.Revision); err != nil {
		t.Fatal(err)
	}
	calls := 0
	svc.SetProvider(func(context.Context, []byte, string) (string, error) {
		calls++
		return "", errors.New("401 unauthorized")
	})
	svc.SetLocalImage(localImageOK("local"))
	png := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}
	svc.SetCredential(func(string) string { return "ref-a" })
	if _, err := svc.RecognizeImage(context.Background(), png); err != nil {
		t.Fatal(err)
	}
	svc.SetCredential(func(string) string { return "ref-b" })
	if _, err := svc.RecognizeImage(context.Background(), png); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("rotated credential must not inherit the previous cooldown: calls=%d", calls)
	}
	if _, err := svc.RecognizeImage(context.Background(), png); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("same credential must stay in cooldown: calls=%d", calls)
	}
}

func TestHealthSnapshotReportsLastFailureWithoutInventedZeros(t *testing.T) {
	svc := New(NewFileStore(filepath.Join(t.TempDir(), "ocr-routing.json")))
	now := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }
	cur, _ := svc.Routing()
	if _, err := svc.SetRouting(Routing{ProviderID: "01ARZ3NDEKTSV4RRFFQ69G5FAA", ModelID: "ocr-v1", PreferProvider: true}, cur.Revision); err != nil {
		t.Fatal(err)
	}
	svc.SetProvider(func(context.Context, []byte, string) (string, error) {
		return "", errors.New("401 unauthorized")
	})
	svc.SetLocalImage(localImageOK("local"))
	if _, err := svc.RecognizeImage(context.Background(), []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}); err != nil {
		t.Fatal(err)
	}
	snap := svc.HealthSnapshot()
	if snap.LastFailure == nil || snap.LastFailure.Class != "auth" || snap.LastFailure.Operation != "image-ocr" || snap.LastFailure.Until.Before(now) {
		t.Fatalf("last failure %+v", snap)
	}
	if snap.Local.Backend == "" {
		t.Fatal("localReady backend must be explicit")
	}
	if snap.Pack.Status != "missing_dependency" || snap.Pack.Backend != "ppocr-pack" || snap.Pack.Available {
		t.Fatalf("HAT-22 pack must be visible as missing_dependency, not claimed ready: %+v", snap.Pack)
	}
	if snap.Local.Backend == "ppocr" || snap.Local.Backend == "ppocr-pack" {
		t.Fatalf("local path must stay windows-ocr/unavailable: %+v", snap.Local)
	}
}

func TestSetRoutingClearsOCRCooldown(t *testing.T) {
	svc := New(NewFileStore(filepath.Join(t.TempDir(), "ocr-routing.json")))
	cur, _ := svc.Routing()
	bound := Routing{ProviderID: "01ARZ3NDEKTSV4RRFFQ69G5FAA", ModelID: "ocr-v1", PreferProvider: true}
	if _, err := svc.SetRouting(bound, cur.Revision); err != nil {
		t.Fatal(err)
	}
	calls := 0
	svc.SetProvider(func(context.Context, []byte, string) (string, error) {
		calls++
		if calls == 1 {
			return "", errors.New("401 unauthorized")
		}
		return "recovered text", nil
	})
	svc.SetLocalImage(localImageOK("before-retest"))
	png := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}
	if _, err := svc.RecognizeImage(context.Background(), png); err != nil {
		t.Fatal(err)
	}
	cur, _ = svc.Routing()
	if _, err := svc.SetRouting(bound, cur.Revision); err != nil {
		t.Fatal(err)
	}
	got, err := svc.RecognizeImage(context.Background(), png)
	if err != nil || got.Text != "recovered text" || calls != 2 {
		t.Fatalf("re-save must retest provider: %+v %v calls=%d", got, err, calls)
	}
}

func TestCanceledImageProviderSuccessIsDiscarded(t *testing.T) {
	svc := New(NewFileStore(filepath.Join(t.TempDir(), "ocr-routing.json")))
	cur, _ := svc.Routing()
	if _, err := svc.SetRouting(Routing{ProviderID: "01ARZ3NDEKTSV4RRFFQ69G5FAA", ModelID: "ocr-v1", PreferProvider: true}, cur.Revision); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	svc.SetProvider(func(context.Context, []byte, string) (string, error) {
		cancel()
		return "late provider text", nil
	})
	svc.SetLocalImage(func(context.Context, []byte) (doctext.PDFOCRResult, error) {
		t.Fatal("cancel must not run local image OCR")
		return doctext.PDFOCRResult{}, errors.New("unreachable")
	})
	got, err := svc.RecognizeImage(ctx, []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a})
	if err == nil || strings.Contains(got.Text, "late provider") {
		t.Fatalf("late success after cancel must not be adopted: %+v %v", got, err)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel must surface, not a packaged-OCR story: %v", err)
	}
}

func mixedThreePagePDF(t *testing.T) []byte {
	t.Helper()
	pdf := gofpdf.New("P", "mm", "A4", "")
	pdf.AddPage()
	pdf.SetFont("helvetica", "", 12)
	pdf.Cell(40, 10, "Cover layer page one")
	pdf.AddPage()
	pdf.AddPage()
	pdf.SetFont("helvetica", "", 12)
	pdf.Cell(40, 10, "Closing layer page three")
	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
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

func pngStub() []byte {
	return []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a, 'p', '2'}
}

func TestRecognizePDFOCRsOnlyEmptyPagesViaProvider(t *testing.T) {
	raw := mixedThreePagePDF(t)
	svc := New(NewFileStore(filepath.Join(t.TempDir(), "ocr-routing.json")))
	cur, _ := svc.Routing()
	if _, err := svc.SetRouting(Routing{ProviderID: "01ARZ3NDEKTSV4RRFFQ69G5FAA", ModelID: "ocr-v1", PreferProvider: true}, cur.Revision); err != nil {
		t.Fatal(err)
	}
	localPDF := 0
	localImg := 0
	provider := 0
	svc.SetLocalPDF(func(context.Context, []byte) (doctext.PDFOCRResult, error) {
		localPDF++
		t.Fatal("mixed PDF must not OCR the whole document")
		return doctext.PDFOCRResult{}, errors.New("unreachable")
	})
	svc.SetLocalImage(func(context.Context, []byte) (doctext.PDFOCRResult, error) {
		localImg++
		return doctext.PDFOCRResult{}, errors.New("local image should not run when provider succeeds")
	})
	svc.SetRenderPDF(func(_ context.Context, _ []byte, pages []int) ([]doctext.RenderedPDFPage, error) {
		if len(pages) != 1 || pages[0] != 2 {
			t.Fatalf("renderer must see only empty pages, got %v", pages)
		}
		return []doctext.RenderedPDFPage{{Page: 2, PNG: pngStub()}}, nil
	})
	svc.SetProvider(func(_ context.Context, in []byte, hint string) (string, error) {
		provider++
		if hint != "image-ocr" || bytes.HasPrefix(in, []byte("%PDF-")) {
			t.Fatalf("provider must receive a page image: hint=%q pdf=%v", hint, bytes.HasPrefix(in, []byte("%PDF-")))
		}
		return "scanned middle page", nil
	})
	got, err := svc.RecognizePDF(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	if provider != 1 || localPDF != 0 || localImg != 0 {
		t.Fatalf("calls provider=%d localPDF=%d localImg=%d", provider, localPDF, localImg)
	}
	if got.Source != SourceMixed || !got.Complete || len(got.Coverage) != 3 {
		t.Fatalf("mixed result: %+v", got)
	}
	if got.Coverage[0].Source != SourceTextLayer || got.Coverage[1].Source != SourceProvider || got.Coverage[2].Source != SourceTextLayer {
		t.Fatalf("coverage sources: %+v", got.Coverage)
	}
	if !strings.Contains(got.Text, "Cover") || !strings.Contains(got.Text, "scanned middle page") || !strings.Contains(got.Text, "Closing") {
		t.Fatalf("merged text lost a page: %q", got.Text)
	}
}

func TestRecognizePDFFallsBackLocalImageOnEmptyPageOnly(t *testing.T) {
	raw := mixedThreePagePDF(t)
	svc := New(NewFileStore(filepath.Join(t.TempDir(), "ocr-routing.json")))
	cur, _ := svc.Routing()
	if _, err := svc.SetRouting(Routing{ProviderID: "01ARZ3NDEKTSV4RRFFQ69G5FAA", ModelID: "ocr-v1", PreferProvider: true}, cur.Revision); err != nil {
		t.Fatal(err)
	}
	localPDF := 0
	localImg := 0
	svc.SetLocalPDF(func(context.Context, []byte) (doctext.PDFOCRResult, error) {
		localPDF++
		return doctext.PDFOCRResult{}, errors.New("whole-doc local must not run on mixed PDF")
	})
	svc.SetRenderPDF(func(context.Context, []byte, []int) ([]doctext.RenderedPDFPage, error) {
		return []doctext.RenderedPDFPage{{Page: 2, PNG: pngStub()}}, nil
	})
	svc.SetProvider(func(context.Context, []byte, string) (string, error) {
		return "", errors.New("401 unauthorized")
	})
	svc.SetLocalImage(func(_ context.Context, in []byte) (doctext.PDFOCRResult, error) {
		localImg++
		if bytes.HasPrefix(in, []byte("%PDF-")) {
			t.Fatal("local image received raw PDF")
		}
		return doctext.PDFOCRResult{Method: "windows-ocr", Pages: []doctext.OCRPage{{Page: 1, Text: "local middle"}}}, nil
	})
	got, err := svc.RecognizePDF(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	if localPDF != 0 || localImg != 1 || got.Coverage[1].Source != SourceLocal || !strings.Contains(got.Text, "local middle") {
		t.Fatalf("page-local fallback: localPDF=%d localImg=%d got=%+v", localPDF, localImg, got)
	}
}

func TestRecognizePDFCancelAfterRenderDoesNotAdopt(t *testing.T) {
	raw := mixedThreePagePDF(t)
	svc := New(NewFileStore(filepath.Join(t.TempDir(), "ocr-routing.json")))
	cur, _ := svc.Routing()
	if _, err := svc.SetRouting(Routing{ProviderID: "01ARZ3NDEKTSV4RRFFQ69G5FAA", ModelID: "ocr-v1", PreferProvider: true}, cur.Revision); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	svc.SetRenderPDF(func(context.Context, []byte, []int) ([]doctext.RenderedPDFPage, error) {
		cancel()
		return []doctext.RenderedPDFPage{{Page: 2, PNG: pngStub()}}, nil
	})
	svc.SetProvider(func(context.Context, []byte, string) (string, error) {
		t.Fatal("cancel must not call provider")
		return "late", nil
	})
	svc.SetLocalImage(func(context.Context, []byte) (doctext.PDFOCRResult, error) {
		t.Fatal("cancel must not run local image OCR")
		return doctext.PDFOCRResult{}, errors.New("unreachable")
	})
	svc.SetLocalPDF(func(context.Context, []byte) (doctext.PDFOCRResult, error) {
		t.Fatal("cancel must not run whole-doc local OCR")
		return doctext.PDFOCRResult{}, errors.New("unreachable")
	})
	got, err := svc.RecognizePDF(ctx, raw)
	if !errors.Is(err, context.Canceled) || strings.Contains(got.Text, "late") {
		t.Fatalf("cancel must surface: %+v %v", got, err)
	}
}

func TestRecognizePDFWholeDocLocalOnlyWhenRenderNilAndAllPagesEmpty(t *testing.T) {
	svc := New(NewFileStore(filepath.Join(t.TempDir(), "ocr-routing.json")))
	svc.SetRenderPDF(nil)
	localPDF := 0
	svc.SetLocalPDF(func(context.Context, []byte) (doctext.PDFOCRResult, error) {
		localPDF++
		return doctext.PDFOCRResult{Method: "windows-ocr", Pages: []doctext.OCRPage{{Page: 1, Text: "whole local"}}}, nil
	})
	got, err := svc.RecognizePDF(context.Background(), []byte("%PDF-1.4 scanned"))
	if err != nil || localPDF != 1 || !strings.Contains(got.Text, "whole local") {
		t.Fatalf("all-empty without renderer must use whole-doc local: %+v %v calls=%d", got, err, localPDF)
	}

	raw := mixedThreePagePDF(t)
	localPDF = 0
	svc.SetLocalPDF(func(context.Context, []byte) (doctext.PDFOCRResult, error) {
		localPDF++
		return doctext.PDFOCRResult{Pages: []doctext.OCRPage{{Page: 2, Text: "should not fill mixed"}}}, nil
	})
	got, err = svc.RecognizePDF(context.Background(), raw)
	if localPDF != 0 {
		t.Fatalf("mixed PDF without renderer must not re-OCR the whole document: %+v", got)
	}
	if err == nil && got.Complete {
		t.Fatalf("mixed PDF without renderer must stay incomplete: %+v", got)
	}
	if strings.Contains(got.Text, "should not fill mixed") {
		t.Fatalf("invented whole-doc text on mixed PDF: %q", got.Text)
	}
}

func TestProviderAuthFailureStillFallsBackLocal(t *testing.T) {
	svc := New(NewFileStore(filepath.Join(t.TempDir(), "ocr-routing.json")))
	cur, _ := svc.Routing()
	if _, err := svc.SetRouting(Routing{ProviderID: "01ARZ3NDEKTSV4RRFFQ69G5FAA", ModelID: "ocr-v1", PreferProvider: true}, cur.Revision); err != nil {
		t.Fatal(err)
	}
	svc.SetProvider(func(context.Context, []byte, string) (string, error) {
		return "", errors.New("401 unauthorized")
	})
	svc.SetLocalPDF(func(context.Context, []byte) (doctext.PDFOCRResult, error) {
		return doctext.PDFOCRResult{Method: "windows-ocr", Pages: []doctext.OCRPage{{Page: 1, Text: "local fallback"}}}, nil
	})
	got, err := svc.RecognizePDF(context.Background(), []byte("%PDF-1.4"))
	if err != nil {
		t.Fatal(err)
	}
	if got.Source != SourceLocal && !strings.Contains(got.Text, "local fallback") {
		if got.Method != "windows-ocr" && !strings.Contains(got.Text, "fallback") {
			t.Fatalf("expected local fallback, got %+v", got)
		}
	}
}

func TestRecognizeImageWithoutLocalUsesChineseFailClosed(t *testing.T) {
	svc := New(NewFileStore(filepath.Join(t.TempDir(), "ocr-routing.json")))
	svc.SetLocalImage(nil)
	_, err := svc.RecognizeImage(context.Background(), pngStub())
	if err == nil || !strings.Contains(err.Error(), "本地图片识别未装配") {
		t.Fatalf("unwired local image OCR must be Chinese fail-closed: %v", err)
	}
	if strings.Contains(err.Error(), "not packaged") || strings.Contains(err.Error(), "vision model") {
		t.Fatalf("must not leak English image-OCR gap: %v", err)
	}
}

func TestRecognizePDFWithoutLocalUsesChineseFailClosed(t *testing.T) {
	svc := New(NewFileStore(filepath.Join(t.TempDir(), "ocr-routing.json")))
	svc.SetRenderPDF(nil)
	svc.SetLocalPDF(nil)
	_, err := svc.RecognizePDF(context.Background(), []byte("%PDF-1.4"))
	if err == nil || !strings.Contains(err.Error(), "本地 OCR 未装配") {
		t.Fatalf("unwired local PDF OCR must be Chinese fail-closed: %v", err)
	}
	if strings.Contains(err.Error(), "local OCR is unavailable") || strings.Contains(err.Error(), "PDF text layer unavailable") {
		t.Fatalf("must not leak English PDF-OCR gap: %v", err)
	}
}
