package ocrapp

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/lunitide/lunitide/internal/doctext"
)

func zipWithRapidOCR(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	f, err := w.Create("RapidOCR-json.exe")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte("MZ-fake-ocr")); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestInstallDownloadsZipAndMarksPackReady(t *testing.T) {
	archive := zipWithRapidOCR(t)
	sum := sha256.Sum256(archive)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", itoa(len(archive)))
		_, _ = w.Write(archive)
	}))
	t.Cleanup(server.Close)

	bundle := Bundle{
		ID:    "rapidocr-json",
		Title: "PP-OCR",
		Downloads: []Download{{
			Path:    ".",
			URLs:    []string{server.URL + "/RapidOCR-json.zip"},
			SHA256:  hex.EncodeToString(sum[:]),
			Bytes:   int64(len(archive)),
			Archive: ArchiveZip,
		}},
	}
	root := t.TempDir()
	installer := &Installer{Root: root}
	if installer.Installed(bundle) {
		t.Fatal("missing zip must not count as installed")
	}
	if err := installer.Install(context.Background(), bundle, nil); err != nil {
		t.Fatalf("install: %v", err)
	}
	if !installer.Installed(bundle) {
		t.Fatal("verified zip must count as installed")
	}
	got := DetectPPOcrPack(installer.BundleDir(bundle.ID))
	if !got.Available || got.Status != "ready" {
		t.Fatalf("installed RapidOCR must be selectable: %+v", got)
	}
}

func TestRecognizeUsesInstalledPackWhenAuto(t *testing.T) {
	root := filepath.Join(t.TempDir(), "rapidocr-json")
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "RapidOCR-json.exe"), []byte("MZ"), 0700); err != nil {
		t.Fatal(err)
	}
	store := NewFileStore(filepath.Join(t.TempDir(), "ocr-routing.json"))
	svc := New(store)
	svc.SetInstallRoot(filepath.Dir(root))
	svc.SetPackImage(func(context.Context, string, []byte) (doctext.PDFOCRResult, error) {
		return doctext.PDFOCRResult{Method: "ppocr", Pages: []doctext.OCRPage{{Page: 1, Text: "发票 88"}}}, nil
	})
	svc.SetLocalImage(localImageOK("windows-only"))
	cur, err := svc.Routing()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetRouting(Routing{PreferProvider: false, LocalEngine: "auto"}, cur.Revision); err != nil {
		t.Fatalf("auto must be selectable after install: %v", err)
	}
	got, err := svc.RecognizeImage(context.Background(), []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a})
	if err != nil {
		t.Fatal(err)
	}
	if got.Method != "ppocr" || got.Text != "发票 88" {
		t.Fatalf("auto must run the installed pack: %+v", got)
	}
}

func TestRecognizeUsesInstalledPackWithoutSavingRouting(t *testing.T) {
	root := filepath.Join(t.TempDir(), "rapidocr-json")
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "RapidOCR-json.exe"), []byte("MZ"), 0700); err != nil {
		t.Fatal(err)
	}
	svc := New(NewFileStore(filepath.Join(t.TempDir(), "ocr-routing.json")))
	svc.SetInstallRoot(filepath.Dir(root))
	svc.SetPackImage(func(context.Context, string, []byte) (doctext.PDFOCRResult, error) {
		return doctext.PDFOCRResult{Method: "ppocr", Pages: []doctext.OCRPage{{Page: 1, Text: "未保存也识别"}}}, nil
	})
	svc.SetLocalImage(localImageOK("windows-only"))
	got, err := svc.RecognizeImage(context.Background(), []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a})
	if err != nil {
		t.Fatal(err)
	}
	if got.Method != "ppocr" || got.Text != "未保存也识别" {
		t.Fatalf("download without an extra save must still run the pack: %+v", got)
	}
	snap := svc.HealthSnapshot()
	if !snap.Pack.Available || snap.Local.Backend != "ppocr" {
		t.Fatalf("get/health after download must show ppocr without saving: %+v", snap)
	}
}

func TestSetRoutingPPOcrFillsPackRootFromInstall(t *testing.T) {
	root := filepath.Join(t.TempDir(), "rapidocr-json")
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "RapidOCR-json.exe"), []byte("MZ"), 0700); err != nil {
		t.Fatal(err)
	}
	svc := New(NewFileStore(filepath.Join(t.TempDir(), "ocr-routing.json")))
	svc.SetInstallRoot(filepath.Dir(root))
	cur, err := svc.Routing()
	if err != nil {
		t.Fatal(err)
	}
	saved, err := svc.SetRouting(Routing{PreferProvider: false, LocalEngine: "ppocr"}, cur.Revision)
	if err != nil {
		t.Fatalf("selecting PP-OCR after download must not need a folder: %v", err)
	}
	if saved.LocalEngine != "ppocr" || saved.PackRoot != root {
		t.Fatalf("install root must be recorded as packRoot: %+v", saved)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}
