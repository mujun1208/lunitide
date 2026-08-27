package app

import (
	"context"
	"testing"

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

func TestOmniAppendRejectsBadPayload(t *testing.T) {
	e := NewEngine(providerRepositoryStub{}, "test")
	e.SetOmniService(NewOmniService(t.TempDir()))
	resp := e.Handle(context.Background(), validRequest("omni.append", `{"sessionId":"gone","pcm":"AAAA"}`))
	if resp.OK || resp.Error == nil || resp.Error.Code != "OMNI-004" {
		t.Fatalf("omni.append = %+v; want OMNI-004", resp)
	}
}
