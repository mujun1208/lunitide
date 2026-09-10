package app

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/ccapp"
	"github.com/lunitide/lunitide/internal/ocrapp"
	"github.com/lunitide/lunitide/internal/toolruntime"
	"github.com/oklog/ulid/v2"
)

func TestCapabilityReadinessSeparatesConfigAndDependency(t *testing.T) {
	e := NewEngine(nil, "test")
	chat := e.capabilityReadiness(context.Background(), "chat")
	if chat.Availability != "missing_dependency" || chat.Code != "DEPENDENCY_MISSING" {
		t.Fatalf("unwired chat: %+v", chat)
	}
	ocr := e.capabilityReadiness(context.Background(), "ocr")
	if ocr.Availability != "missing_dependency" || ocr.Code != "DEPENDENCY_MISSING" {
		t.Fatalf("unwired ocr: %+v", ocr)
	}
	files := e.capabilityReadiness(context.Background(), "files")
	if files.Availability != "missing_dependency" || files.Code != "DEPENDENCY_MISSING" {
		t.Fatalf("unwired files: %+v", files)
	}

	e = NewEngine(providerRepositoryStub{}, "test")
	chat = e.capabilityReadiness(context.Background(), "chat")
	if chat.Availability != "needs_config" || chat.Code != "CAPABILITY_NOT_READY" {
		t.Fatalf("empty catalog must be config, not a missing install: %+v", chat)
	}
	if chat.Code == "DEPENDENCY_MISSING" || chat.Code == "SCOPE_DENIED" {
		t.Fatalf("config gap reused another class: %+v", chat)
	}
}

func TestOCRReadinessRequiresLocalOrBoundProvider(t *testing.T) {
	none := ocrCapabilityFrom(ocrapp.Routing{}, ocrapp.LocalReady{Backend: "unavailable"})
	if none.Availability == "ready" {
		t.Fatalf("no local backend and no provider must not be ready: %+v", none)
	}
	if none.Code != "CAPABILITY_NOT_READY" && none.Code != "DEPENDENCY_MISSING" {
		t.Fatalf("honest not-ready code: %+v", none)
	}
	local := ocrCapabilityFrom(ocrapp.Routing{}, ocrapp.LocalReady{PDF: true, Image: true, Backend: "windows-ocr"})
	if local.Availability != "ready" || strings.Contains(local.Detail, "PP-OCR") {
		t.Fatalf("windows-ocr local must be ready without claiming the ONNX pack: %+v", local)
	}
	cloud := ocrCapabilityFrom(ocrapp.Routing{ProviderID: "01ARZ3NDEKTSV4RRFFQ69G5FAA", ModelID: "ocr-v1"}, ocrapp.LocalReady{Backend: "unavailable"})
	if cloud.Availability != "ready" {
		t.Fatalf("bound provider is enough without local pack: %+v", cloud)
	}

	e := NewEngine(nil, "test")
	e.SetOCR(ocrapp.New(ocrapp.NewFileStore(filepath.Join(t.TempDir(), "ocr-routing.json"))))
	wired := e.capabilityReadiness(context.Background(), "ocr")
	if strings.Contains(wired.Detail, "PP-OCR") {
		t.Fatalf("must not claim the ONNX pack: %+v", wired)
	}
	localReady := ocrapp.LocalOCRReady()
	if localReady.PDF || localReady.Image {
		if wired.Availability != "ready" || wired.Detail != "本机识别可用" {
			t.Fatalf("unbound local worker: %+v", wired)
		}
	} else if wired.Availability == "ready" {
		t.Fatalf("unbound and no local backend must not be ready: %+v", wired)
	}
}

func TestCapabilityReadinessFilesFollowsWorkspace(t *testing.T) {
	runtime, err := toolruntime.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	e := NewEngine(nil, "test")
	e.SetToolRuntime(runtime)
	files := e.capabilityReadiness(context.Background(), "files")
	if files.Availability != "ready" {
		t.Fatalf("workspace-backed fileops reported missing: %+v", files)
	}
}

func TestCapabilityReadinessDesktopSeparatesConfigAndDependency(t *testing.T) {
	e := NewEngine(nil, "test")
	desktop := e.capabilityReadiness(context.Background(), "desktop")
	if desktop.Availability != "missing_dependency" || desktop.Code != "DEPENDENCY_MISSING" {
		t.Fatalf("unwired desktop: %+v", desktop)
	}
	gui := e.capabilityReadiness(context.Background(), "gui")
	if gui.Availability != "missing_dependency" || gui.Code != "DEPENDENCY_MISSING" {
		t.Fatalf("unwired gui: %+v", gui)
	}

	e, ccSvc := newCcEngine(t)
	desktop = e.capabilityReadiness(context.Background(), "desktop")
	if desktop.Availability != "needs_config" || desktop.Code != "CAPABILITY_NOT_READY" {
		t.Fatalf("disabled computer control must be config: %+v", desktop)
	}
	enabled := true
	if _, err := ccSvc.UpdateConfig(context.Background(), ccapp.SettingsPatch{ExpectedRevision: 1, Enabled: &enabled}); err != nil {
		t.Fatal(err)
	}
	desktop = e.capabilityReadiness(context.Background(), "desktop")
	if desktop.Availability != "ready" {
		t.Fatalf("enabled computer control: %+v", desktop)
	}
	if _, err := ccSvc.EmergencyStop(context.Background(), "operator", "readiness"); err != nil {
		t.Fatal(err)
	}
	desktop = e.capabilityReadiness(context.Background(), "desktop")
	if desktop.Availability != "unavailable" || desktop.Code != "SCOPE_DENIED" {
		t.Fatalf("emergency stop must be permission, not missing install: %+v", desktop)
	}

	e = NewEngine(providerRepositoryStub{}, "test")
	gui = e.capabilityReadiness(context.Background(), "gui")
	if gui.Availability != "needs_config" || gui.Code != "CAPABILITY_NOT_READY" {
		t.Fatalf("empty GUI/vision catalog must be config: %+v", gui)
	}
}

func TestDesktopTypeStopsWhenComputerNotReady(t *testing.T) {
	e, _ := newCcEngine(t)
	runtime, err := toolruntime.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	e.SetToolRuntime(runtime)
	session := ulid.Make().String()
	_, err = e.executeUserTool(context.Background(), executionModeFullAccess, session, "desktop.type", json.RawMessage(`{"text":"hi"}`))
	if err == nil || !strings.Contains(err.Error(), "CAPABILITY_NOT_READY") {
		t.Fatalf("desktop.type without computer control must fail closed: %v", err)
	}
}
