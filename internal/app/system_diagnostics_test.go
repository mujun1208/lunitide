package app

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/audit"
	"github.com/lunitide/lunitide/internal/ocrapp"
	"github.com/lunitide/lunitide/internal/storage/sqlite"
	"github.com/lunitide/lunitide/internal/toolruntime"
)

func TestSystemDiagnosticsTextChatAndGUIFollowReadiness(t *testing.T) {
	e := NewEngine(providerRepositoryStub{}, "test")
	r := validRequest("system.diagnostics", `{}`)
	resp := e.Handle(context.Background(), r)
	if !resp.OK {
		t.Fatalf("%+v", resp.Error)
	}
	var out struct {
		Components []diagnosticComponent `json:"components"`
	}
	raw, _ := json.Marshal(resp.Payload)
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	items := map[string]diagnosticComponent{}
	for _, item := range out.Components {
		items[item.ID] = item
	}
	chat := items["text_chat"]
	if chat.State != "not_configured" || chat.Detail != "请先配置并启用供应商、凭据和模型" {
		t.Fatalf("empty catalog must use chat readiness, not wired-as-configured: %+v", chat)
	}
	gui := items["gui"]
	if gui.State != "not_configured" || !strings.Contains(gui.Detail, "GUI") {
		t.Fatalf("gui must come from capabilityReadiness: %+v", gui)
	}
}

func TestSystemDiagnosticsIdentifiesStorageAndAuditFailure(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.OpenTemplated(ctx, filepath.Join(t.TempDir(), "health.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	runtime, err := toolruntime.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	e := NewEngine(providerRepositoryStub{}, "test")
	e.SetStorageReadiness(store)
	e.SetAuditChainVerifier(store)
	e.SetToolRuntime(runtime)
	check := func() map[string]diagnosticComponent {
		t.Helper()
		r := validRequest("system.diagnostics", `{}`)
		resp := e.Handle(ctx, r)
		if !resp.OK {
			t.Fatalf("%+v", resp.Error)
		}
		var out struct {
			TraceID    string                `json:"traceId"`
			Components []diagnosticComponent `json:"components"`
		}
		raw, _ := json.Marshal(resp.Payload)
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatal(err)
		}
		if out.TraceID != r.TraceID {
			t.Fatal("trace correlation lost")
		}
		items := map[string]diagnosticComponent{}
		for _, item := range out.Components {
			items[item.ID] = item
		}
		return items
	}
	items := check()
	if items["storage"].State != "healthy" || items["audit"].State != "healthy" || items["tool_policy"].State != "healthy" {
		t.Fatalf("healthy check: %+v", items)
	}
	if items["voice_local"].State != "not_configured" || items["computer"].State != "not_configured" {
		t.Fatal("unconfigured runtimes claimed ready")
	}
	if items["ocr"].State != "not_configured" {
		t.Fatalf("unwired OCR must not look configured: %+v", items["ocr"])
	}
	e.SetOCR(ocrapp.New(ocrapp.NewFileStore(filepath.Join(t.TempDir(), "ocr-routing.json"))))
	ocr := check()["ocr"]
	if strings.Contains(ocr.Detail, "PP-OCR") {
		t.Fatalf("diagnostics must not claim the ONNX pack: %+v", ocr)
	}
	local := ocrapp.LocalOCRReady()
	if local.PDF || local.Image {
		if ocr.State != "configured" || ocr.Detail != "本机识别可用" {
			t.Fatalf("unbound local OCR worker: %+v", ocr)
		}
	} else if ocr.State == "configured" {
		t.Fatalf("no local backend and no provider must not be configured: %+v", ocr)
	}
	e.SetAuditChainVerifier(stubAuditVerifier{err: audit.ErrChainBroken})
	if check()["audit"].Code != "AUDIT_CHAIN_BROKEN" {
		t.Fatal("audit tamper not identified")
	}
	store.Close()
	if check()["storage"].State != "degraded" {
		t.Fatal("closed database reported healthy")
	}
}
