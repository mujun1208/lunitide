package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/ocrapp"
	"github.com/oklog/ulid/v2"
)

func TestOCRPackGetReportsDisabledGate(t *testing.T) {
	e := NewEngine(providerRepositoryStub{}, "test")
	resp := e.Handle(context.Background(), validRequest("ocr.pack.get", `{"packId":"paddleocr-vl-1.6"}`))
	if !resp.OK {
		t.Fatalf("ocr.pack.get %+v", resp.Error)
	}
	raw, err := json.Marshal(resp.Payload)
	if err != nil {
		t.Fatal(err)
	}
	var snap struct {
		Pack struct {
			PackID       string  `json:"packId"`
			Availability string  `json:"availability"`
			Revision     int64   `json:"revision"`
			Manifest     *string `json:"manifestDigest"`
		} `json:"pack"`
		Gate struct {
			InstallAllowed   bool    `json:"installAllowed"`
			AutoRouteAllowed bool    `json:"autoRouteAllowed"`
			ReasonCode       *string `json:"reasonCode"`
			ProfileDigest    *string `json:"verifiedRuntimeProfileDigest"`
			Revision         int64   `json:"revision"`
		} `json:"gate"`
		Operation *struct{} `json:"operation"`
		Release   *struct{} `json:"release"`
	}
	if err := json.Unmarshal(raw, &snap); err != nil {
		t.Fatal(err)
	}
	if snap.Pack.PackID != "paddleocr-vl-1.6" || snap.Pack.Availability != "not_installed" || snap.Pack.Revision != 1 || snap.Pack.Manifest != nil {
		t.Fatalf("pack %+v", snap.Pack)
	}
	if snap.Gate.InstallAllowed || snap.Gate.AutoRouteAllowed || snap.Gate.ReasonCode == nil || *snap.Gate.ReasonCode != "NO_VERIFIED_RUNTIME_PROFILE" || snap.Gate.ProfileDigest != nil {
		t.Fatalf("gate %+v", snap.Gate)
	}
	if snap.Operation != nil || snap.Release != nil {
		t.Fatalf("operation/release must be null %+v", snap)
	}
}

func TestOCRPackMutationsAreZeroSideEffect(t *testing.T) {
	root := t.TempDir()
	svc := ocrapp.New(ocrapp.NewFileStore(filepath.Join(root, "ocr-routing.json")))
	svc.SetInstallRoot(root)
	svc.SetInstallBundle(func(context.Context, ocrapp.Bundle, func(ocrapp.Progress)) error {
		t.Fatal("legacy installer must not run")
		return nil
	})
	e := NewEngine(providerRepositoryStub{}, "test")
	e.SetOCR(svc)

	before, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	op := ulid.Make().String()
	digest := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	install := e.Handle(context.Background(), memoryItemRequest("ocr.pack.install",
		`{"packId":"paddleocr-vl-1.6","catalogRevision":"1","acceptedManifestDigest":"`+digest+`","expectedRevision":1,"operationId":"`+op+`"}`, op))
	if install.OK || install.Error == nil || install.Error.Code != "NO_VERIFIED_RUNTIME_PROFILE" || install.Error.Retryable {
		t.Fatalf("install %+v", install)
	}
	cancel := e.Handle(context.Background(), memoryItemRequest("ocr.pack.cancel",
		`{"operationId":"`+op+`","expectedRevision":1}`, ulid.Make().String()))
	if cancel.OK || cancel.Error == nil || cancel.Error.Code != "NO_VERIFIED_RUNTIME_PROFILE" {
		t.Fatalf("cancel %+v", cancel)
	}
	uninstall := e.Handle(context.Background(), memoryItemRequest("ocr.pack.uninstall",
		`{"packId":"paddleocr-vl-1.6","expectedRevision":1,"confirmed":true,"operationId":"`+ulid.Make().String()+`"}`, ulid.Make().String()))
	if uninstall.OK || uninstall.Error == nil || uninstall.Error.Code != "NO_VERIFIED_RUNTIME_PROFILE" {
		t.Fatalf("uninstall %+v", uninstall)
	}
	after, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Fatalf("pack mutation created files before=%d after=%d", len(before), len(after))
	}
	list := e.Handle(context.Background(), validRequest("ocr.pack.notice.list", `{"packId":"paddleocr-vl-1.6"}`))
	if !list.OK {
		t.Fatalf("notice list %+v", list.Error)
	}
	var listPayload struct {
		Items      []any   `json:"items"`
		NextCursor *string `json:"nextCursor"`
	}
	if err := json.Unmarshal(mustJSON(list.Payload), &listPayload); err != nil || len(listPayload.Items) != 0 || listPayload.NextCursor != nil {
		t.Fatalf("notice list payload %+v err=%v", listPayload, err)
	}
	read := e.Handle(context.Background(), validRequest("ocr.pack.notice.read",
		`{"packId":"paddleocr-vl-1.6","manifestDigest":"`+digest+`","offset":0,"limit":4096}`))
	if read.OK || read.Error == nil || read.Error.Code != "OCR_PACK_NOTICE_UNAVAILABLE" || read.Error.Retryable {
		t.Fatalf("notice read %+v", read)
	}
}

func TestOCRPackBridge(t *testing.T) {
	e := NewEngine(providerRepositoryStub{}, "test")
	root := t.TempDir()
	svc := ocrapp.New(ocrapp.NewFileStore(filepath.Join(root, "ocr-routing.json")))
	svc.SetInstallRoot(root)
	svc.SetInstallBundle(func(context.Context, ocrapp.Bundle, func(ocrapp.Progress)) error {
		t.Fatal("legacy installer must not run")
		return nil
	})
	wired := NewEngine(providerRepositoryStub{}, "test")
	wired.SetOCR(svc)
	digest := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	op := ulid.Make().String()

	t.Run("get_exact_snapshot", func(t *testing.T) {
		TestOCRPackGetReportsDisabledGate(t)
	})
	t.Run("install_receipt", func(t *testing.T) {
		install := wired.Handle(context.Background(), memoryItemRequest("ocr.pack.install",
			`{"packId":"paddleocr-vl-1.6","catalogRevision":"1","acceptedManifestDigest":"`+digest+`","expectedRevision":1,"operationId":"`+op+`"}`, op))
		if install.OK || install.Error == nil || install.Error.Code != "NO_VERIFIED_RUNTIME_PROFILE" || install.Error.Retryable {
			t.Fatalf("%+v", install)
		}
	})
	t.Run("cancel_target_operation", func(t *testing.T) {
		cancel := wired.Handle(context.Background(), memoryItemRequest("ocr.pack.cancel",
			`{"operationId":"`+op+`","expectedRevision":1}`, ulid.Make().String()))
		if cancel.OK || cancel.Error == nil || cancel.Error.Code != "NO_VERIFIED_RUNTIME_PROFILE" {
			t.Fatalf("%+v", cancel)
		}
	})
	t.Run("uninstall_receipt", func(t *testing.T) {
		uninstall := wired.Handle(context.Background(), memoryItemRequest("ocr.pack.uninstall",
			`{"packId":"paddleocr-vl-1.6","expectedRevision":1,"confirmed":true,"operationId":"`+ulid.Make().String()+`"}`, ulid.Make().String()))
		if uninstall.OK || uninstall.Error == nil || uninstall.Error.Code != "NO_VERIFIED_RUNTIME_PROFILE" {
			t.Fatalf("%+v", uninstall)
		}
	})
	t.Run("stale_sha_revision", func(t *testing.T) {
		staleReq := wired.Handle(context.Background(), memoryItemRequest("ocr.pack.install",
			`{"packId":"paddleocr-vl-1.6","catalogRevision":"1","acceptedManifestDigest":"`+digest+`","expectedRevision":99,"operationId":"`+ulid.Make().String()+`"}`, ulid.Make().String()))
		if staleReq.OK || staleReq.Error == nil || staleReq.Error.Code != "NO_VERIFIED_RUNTIME_PROFILE" {
			t.Fatalf("gate fails before CAS %+v", staleReq)
		}
	})
	t.Run("notice_list_stable_cursor", func(t *testing.T) {
		list := e.Handle(context.Background(), validRequest("ocr.pack.notice.list", `{"packId":"paddleocr-vl-1.6"}`))
		if !list.OK {
			t.Fatalf("%+v", list.Error)
		}
		raw, _ := json.Marshal(list.Payload)
		var payload struct {
			Items      []any   `json:"items"`
			NextCursor *string `json:"nextCursor"`
		}
		if err := json.Unmarshal(raw, &payload); err != nil || len(payload.Items) != 0 || payload.NextCursor != nil {
			t.Fatalf("%s", raw)
		}
	})
	t.Run("notice_bounded", func(t *testing.T) {
		tooBig := e.Handle(context.Background(), validRequest("ocr.pack.notice.read",
			`{"packId":"paddleocr-vl-1.6","manifestDigest":"`+digest+`","offset":0,"limit":65537}`))
		if tooBig.OK || tooBig.Error == nil || tooBig.Error.Code != "BRIDGE_SCHEMA_INVALID" {
			t.Fatalf("%+v", tooBig)
		}
	})
	t.Run("notice_retained_after_uninstall_offline", func(t *testing.T) {
		read := e.Handle(context.Background(), validRequest("ocr.pack.notice.read",
			`{"packId":"paddleocr-vl-1.6","manifestDigest":"`+digest+`","offset":0,"limit":4096}`))
		if read.OK || read.Error == nil || read.Error.Code != "OCR_PACK_NOTICE_UNAVAILABLE" {
			t.Fatalf("%+v", read)
		}
	})
	t.Run("notice_unavailable_zero_mutation", func(t *testing.T) {
		before, err := os.ReadDir(root)
		if err != nil {
			t.Fatal(err)
		}
		read := wired.Handle(context.Background(), validRequest("ocr.pack.notice.read",
			`{"packId":"paddleocr-vl-1.6","manifestDigest":"`+digest+`","offset":0,"limit":4096}`))
		if read.OK || read.Error == nil || read.Error.Code != "OCR_PACK_NOTICE_UNAVAILABLE" || read.Error.Retryable {
			t.Fatalf("%+v", read)
		}
		after, err := os.ReadDir(root)
		if err != nil {
			t.Fatal(err)
		}
		if len(after) != len(before) {
			t.Fatalf("notice read mutated files")
		}
	})
	t.Run("no_verified_profile_zero_mutation", func(t *testing.T) {
		TestOCRPackMutationsAreZeroSideEffect(t)
	})
	t.Run("no_absolute_path", func(t *testing.T) {
		resp := e.Handle(context.Background(), validRequest("ocr.pack.get", `{"packId":"paddleocr-vl-1.6"}`))
		if !resp.OK {
			t.Fatalf("%+v", resp.Error)
		}
		raw, _ := json.Marshal(resp.Payload)
		if strings.Contains(string(raw), `C:`) || strings.Contains(string(raw), `\\`) || strings.Contains(string(raw), "packRoot") {
			t.Fatalf("absolute path leaked %s", raw)
		}
	})
}

func TestOCRPackInstall(t *testing.T) {
	TestOCRPackMutationsAreZeroSideEffect(t)
}
