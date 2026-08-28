package app

import (
	"archive/zip"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/omni"
)

func TestOmniStatusUnwired(t *testing.T) {
	e := NewEngine(providerRepositoryStub{}, "test")
	resp := e.Handle(context.Background(), validRequest("omni.status", `{}`))
	if !resp.OK {
		t.Fatalf("omni.status = %+v", resp)
	}
	payload := resp.Payload.(map[string]any)
	if payload["supported"] != false {
		t.Fatalf("supported = %v", payload["supported"])
	}
}

func TestOmniStartWithoutService(t *testing.T) {
	e := NewEngine(providerRepositoryStub{}, "test")
	resp := e.Handle(context.Background(), validRequest("omni.start", `{}`))
	if resp.OK || resp.Error == nil || resp.Error.Code != "OMNI-001" {
		t.Fatalf("omni.start = %+v", resp)
	}
}

func TestOmniStartWithoutModel(t *testing.T) {
	e := NewEngine(providerRepositoryStub{}, "test")
	e.SetOmniService(NewOmniService(t.TempDir()))
	resp := e.Handle(context.Background(), validRequest("omni.start", `{"personaId":"refpack:甜心少女.wav"}`))
	if resp.OK || resp.Error == nil || resp.Error.Code != "OMNI-001" {
		t.Fatalf("omni.start = %+v; want OMNI-001 for a missing model", resp)
	}
}

func TestOmniStatusReportsCatalogue(t *testing.T) {
	e := NewEngine(providerRepositoryStub{}, "test")
	e.SetOmniService(NewOmniService(t.TempDir()))
	resp := e.Handle(context.Background(), validRequest("omni.status", `{}`))
	if !resp.OK {
		t.Fatalf("omni.status = %+v", resp)
	}
	payload := resp.Payload.(map[string]any)
	if payload["supported"] != true {
		t.Fatalf("supported = %v", payload["supported"])
	}
	if payload["installed"] != false {
		t.Fatalf("installed = %v", payload["installed"])
	}
	if payload["hostState"] != omni.HostMissingModel {
		t.Fatalf("hostState = %v", payload["hostState"])
	}
	bytes, _ := payload["downloadBytes"].(int64)
	if bytes == 0 {
		if n, ok := payload["downloadBytes"].(float64); !ok || n < float64(8<<30) {
			t.Fatalf("downloadBytes = %v", payload["downloadBytes"])
		}
	}
}

func TestOmniStopWithoutSession(t *testing.T) {
	e := NewEngine(providerRepositoryStub{}, "test")
	resp := e.Handle(context.Background(), validRequest("omni.stop", `{}`))
	if !resp.OK {
		t.Fatalf("omni.stop = %+v", resp)
	}
}

func TestOmniServiceCloseIsSafeToRepeat(t *testing.T) {
	svc := NewOmniService(t.TempDir())
	svc.Close()
	svc.Close()
}

func TestOmniStartReturnsBeforeInit(t *testing.T) {
	initGate := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			w.WriteHeader(http.StatusOK)
		case "/v1/stream/omni_init":
			<-initGate
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(func() {
		close(initGate)
		srv.Close()
	})

	e := NewEngine(providerRepositoryStub{}, "test")
	svc := NewOmniService(t.TempDir())
	t.Cleanup(svc.Close)
	svc.host.Present = func() bool { return true }
	svc.host.Finder = func() string { return "llama-omni-server" }
	svc.host.Endpoint = srv.URL
	svc.host.HTTP = srv.Client()
	e.SetOmniService(svc)

	started := time.Now()
	resp := e.Handle(context.Background(), validRequest("omni.start", `{"personaId":""}`))
	if elapsed := time.Since(started); elapsed > 800*time.Millisecond {
		t.Fatalf("omni.start blocked %s waiting on omni_init", elapsed)
	}
	if !resp.OK {
		t.Fatalf("omni.start = %+v", resp)
	}
}

func TestOmniAppendRejectsBadPayload(t *testing.T) {
	e := NewEngine(providerRepositoryStub{}, "test")
	e.SetOmniService(NewOmniService(t.TempDir()))
	resp := e.Handle(context.Background(), validRequest("omni.append", `{"sessionId":"gone","pcm":"AAAA"}`))
	if resp.OK || resp.Error == nil || resp.Error.Code != "OMNI-004" {
		t.Fatalf("omni.append = %+v; want OMNI-004", resp)
	}
}

func isolateOmniLoopback(host *omni.Host) {
	host.HTTP = &http.Client{
		Timeout: time.Second,
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("isolated from live MiniCPM-o")
		}),
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestOmniStatusReportsRuntimeWithoutDownloadingModel(t *testing.T) {
	root := t.TempDir()
	payload := filepath.Join(t.TempDir(), omni.BundledRuntimeZip)
	writeOmniStubZip(t, payload)
	svc := NewOmniService(root)
	svc.host.Payload = payload
	isolateOmniLoopback(svc.host)
	if err := svc.host.EnsureRuntime(); err != nil {
		t.Fatal(err)
	}
	e := NewEngine(providerRepositoryStub{}, "test")
	e.SetOmniService(svc)
	resp := e.Handle(context.Background(), validRequest("omni.status", `{}`))
	if !resp.OK {
		t.Fatalf("omni.status = %+v", resp)
	}
	out := resp.Payload.(map[string]any)
	if out["runtimeFound"] != true {
		t.Fatalf("runtimeFound = %v", out["runtimeFound"])
	}
	if out["installed"] != false {
		t.Fatalf("installed = %v; model must stay downloadable", out["installed"])
	}
	if out["hostState"] != omni.HostMissingModel {
		t.Fatalf("hostState = %v; missing model is not missing runtime", out["hostState"])
	}
}

func TestOmniStartWithoutRuntimeDoesNotAskToCopyFiles(t *testing.T) {
	t.Setenv("LUNITIDE_OMNI_PAYLOAD", "")
	e := NewEngine(providerRepositoryStub{}, "test")
	svc := NewOmniService(t.TempDir())
	svc.host.Present = func() bool { return true }
	svc.host.Finder = func() string { return "" }
	isolateOmniLoopback(svc.host)
	e.SetOmniService(svc)
	resp := e.Handle(context.Background(), validRequest("omni.start", `{}`))
	if resp.OK || resp.Error == nil || resp.Error.Code != "OMNI-002" {
		t.Fatalf("omni.start = %+v; want OMNI-002", resp)
	}
	if strings.Contains(resp.Error.Message, "omni/runtime") || strings.Contains(resp.Error.Message, "放到") {
		t.Fatalf("must not tell users to copy files: %s", resp.Error.Message)
	}
}

func writeOmniStubZip(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	writer := zip.NewWriter(file)
	entry, err := writer.Create("llama-omni-server.exe")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write([]byte("stub")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
}
