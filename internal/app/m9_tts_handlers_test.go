// m9_tts_handlers_test.go pins the MC-05 real-machine degradation
// acceptance: a Windows N / stripped-install / broken-COM machine must
// surface M95-001 on every tts.* method while the chat pipeline itself
// keeps working (subtitle-only degradation, never a hard failure).
package app

import (
	"context"
	"testing"

	"github.com/lunitide/lunitide/internal/tts"
)

// brokenComEngine simulates Windows N / COM registration damage: every
// SAPI entry point reports the engine as unavailable.
type brokenComEngine struct{}

func (brokenComEngine) Voices() ([]tts.Voice, error) {
	return nil, tts.ErrEngineUnavailable
}
func (brokenComEngine) Synthesize(in tts.SynthesizeInput) (tts.SynthesizeResult, bool, error) {
	return tts.SynthesizeResult{}, false, tts.ErrEngineUnavailable
}

func TestTtsWindowsNServiceNotWired(t *testing.T) {
	// The full Windows N simulation: host.go never wires SetM9TtsService,
	// so the engine carries a nil service and every tts.* call must
	// degrade with M95-001 instead of panicking or blocking chat.
	e := NewEngine(providerRepositoryStub{}, "test")
	cases := []struct{ method, payload string }{
		{"tts.voices", `{}`},
		{"tts.synthesize", `{"text":"段"}`},
		{"tts.cancel", `{}`},
	}
	for _, tc := range cases {
		resp := e.Handle(context.Background(), validRequest(tc.method, tc.payload))
		if resp.OK {
			t.Fatalf("%s on an engine-less machine returned ok", tc.method)
		}
		if resp.Error == nil || resp.Error.Code != "M95-001" {
			t.Fatalf("%s error = %+v, want M95-001", tc.method, resp.Error)
		}
		if !resp.Error.Retryable {
			t.Fatalf("%s M95-001 must stay retryable (a later engine install heals it)", tc.method)
		}
	}
}

func TestTtsBrokenComRegistration(t *testing.T) {
	// COM damage simulation: the service is wired but SAPI reports the
	// engine unavailable — same M95-001 surface, chat keeps flowing.
	e := NewEngine(providerRepositoryStub{}, "test")
	e.SetM9TtsService(tts.NewService(brokenComEngine{}))

	resp := e.Handle(context.Background(), validRequest("tts.voices", `{}`))
	if resp.OK || resp.Error == nil || resp.Error.Code != "M95-001" {
		t.Fatalf("tts.voices = %+v, want M95-001", resp)
	}

	resp = e.Handle(context.Background(), validRequest("tts.synthesize", `{"text":"降级段"}`))
	if resp.OK || resp.Error == nil || resp.Error.Code != "M95-001" {
		t.Fatalf("tts.synthesize = %+v, want M95-001", resp)
	}

	// Chat itself must stay alive on the degraded machine.
	if health := e.Handle(context.Background(), validRequest("system.health", `{}`)); !health.OK {
		t.Fatalf("system.health on degraded machine = %+v, want ok", health)
	}
}

func TestTtsSegmentFailureIsolatedFromEngineLoss(t *testing.T) {
	// M95-002 (one segment failed) must not be confused with M95-001:
	// the segment failure is non-retryable at the segment level and the
	// renderer skips the segment instead of degrading the whole stage.
	e := NewEngine(providerRepositoryStub{}, "test")
	e.SetM9TtsService(tts.NewService(failingSynthEngine{}))

	resp := e.Handle(context.Background(), validRequest("tts.synthesize", `{"text":"失败段"}`))
	if resp.OK || resp.Error == nil {
		t.Fatalf("tts.synthesize = %+v, want failure", resp)
	}
	if resp.Error.Code != "M95-002" {
		t.Fatalf("code = %s, want M95-002", resp.Error.Code)
	}
	if resp.Error.Retryable {
		t.Fatalf("M95-002 is a per-segment skip, not a retryable engine error")
	}
}

type failingSynthEngine struct{}

func (failingSynthEngine) Voices() ([]tts.Voice, error) {
	return []tts.Voice{{VoiceID: "v", DisplayName: "V", Gender: "neutral", Lang: "zh-CN"}}, nil
}
func (failingSynthEngine) Synthesize(in tts.SynthesizeInput) (tts.SynthesizeResult, bool, error) {
	return tts.SynthesizeResult{}, false, tts.ErrSynthesisFailed
}
