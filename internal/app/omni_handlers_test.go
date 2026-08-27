package app

import (
	"context"
	"net/http"
	"net/http/httptest"
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
