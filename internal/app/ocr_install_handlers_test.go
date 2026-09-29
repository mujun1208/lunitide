package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/ocrapp"
)

func TestOCRInstallStartsDownloadAndReportsProgress(t *testing.T) {
	root := t.TempDir()
	svc := ocrapp.New(ocrapp.NewFileStore(filepath.Join(root, "ocr-routing.json")))
	svc.SetInstallRoot(root)
	started := make(chan struct{})
	var once sync.Once
	svc.SetInstallBundle(func(ctx context.Context, bundle ocrapp.Bundle, progress func(ocrapp.Progress)) error {
		once.Do(func() { close(started) })
		progress(ocrapp.Progress{BundleID: bundle.ID, File: "RapidOCR-json.zip", Done: 10, Total: 100})
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
		return nil
	})
	e := NewEngine(providerRepositoryStub{}, "test")
	e.SetOCR(svc)

	first := e.Handle(context.Background(), validRequest("ocr.install", `{}`))
	if !first.OK {
		t.Fatalf("ocr.install %#v", first.Error)
	}
	payload := first.Payload.(map[string]any)
	if payload["state"] != "downloading" && payload["state"] != "idle" && payload["state"] != "ready" {
		t.Fatalf("first snapshot %+v", payload)
	}
	<-started
	second := e.Handle(context.Background(), validRequest("ocr.install", `{}`))
	if !second.OK {
		t.Fatalf("progress poll %#v", second.Error)
	}
	got := second.Payload.(map[string]any)
	if got["state"] != "downloading" && got["state"] != "ready" {
		t.Fatalf("poll while installing %+v", got)
	}
}

func TestOCRInstallMissingServiceIsChinese(t *testing.T) {
	e := NewEngine(providerRepositoryStub{}, "test")
	resp := e.Handle(context.Background(), validRequest("ocr.install", `{}`))
	if resp.OK || resp.Error == nil || !strings.Contains(resp.Error.Message, "OCR") {
		t.Fatalf("unwired install %#v", resp)
	}
	if strings.Contains(resp.Error.Message, "not packaged") {
		t.Fatalf("must not leak English: %#v", resp.Error)
	}
}

func TestOCRInstallReadyUsesPackWithoutFolderPick(t *testing.T) {
	root := t.TempDir()
	bundleDir := filepath.Join(root, ocrapp.RuntimeID)
	if err := os.MkdirAll(bundleDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundleDir, "RapidOCR-json.exe"), []byte("MZ"), 0700); err != nil {
		t.Fatal(err)
	}
	svc := ocrapp.New(ocrapp.NewFileStore(filepath.Join(root, "ocr-routing.json")))
	svc.SetInstallRoot(root)
	e := NewEngine(providerRepositoryStub{}, "test")
	e.SetOCR(svc)
	got := e.Handle(context.Background(), validRequest("ocr.routing.get", `{}`))
	if !got.OK {
		t.Fatalf("get %#v", got.Error)
	}
	payload := got.Payload.(map[string]any)
	pack, _ := payload["pack"].(map[string]any)
	if pack["available"] != true {
		t.Fatalf("downloaded pack must be ready without a folder pick: %+v", payload)
	}
}
