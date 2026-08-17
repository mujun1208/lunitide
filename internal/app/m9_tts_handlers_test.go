// m9_tts_handlers_test.go pins the MC-05 real-machine degradation
// acceptance: a Windows N / stripped-install / broken-COM machine must
// surface M95-001 on every tts.* method while the chat pipeline itself
// keeps working (subtitle-only degradation, never a hard failure).
package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
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

func TestTtsVoicesPerEngine(t *testing.T) {
	e := NewEngine(providerRepositoryStub{}, "test")
	e.SetM9TtsService(tts.NewService(tts.NewRouterEngineWithEngines(nil, nil, nil)))

	edge := e.Handle(context.Background(), validRequest("tts.voices", `{"engine":"edge"}`))
	if !edge.OK {
		t.Fatalf("tts.voices edge = %+v, want ok", edge)
	}
	voices := edge.Payload.(map[string]any)["voices"].([]tts.Voice)
	if len(voices) == 0 || voices[0].VoiceID != "zh-CN-XiaoxiaoNeural" {
		t.Fatalf("edge voices = %+v", voices)
	}

	ref := e.Handle(context.Background(), validRequest("tts.voices", `{"engine":"ref"}`))
	if !ref.OK {
		t.Fatalf("tts.voices ref = %+v, want ok", ref)
	}
	if got := ref.Payload.(map[string]any)["voices"].([]tts.Voice)[0].VoiceID; got != "ref" {
		t.Fatalf("ref voices = %v", got)
	}

	bad := e.Handle(context.Background(), validRequest("tts.voices", `{"engine":"mega"}`))
	if bad.OK || bad.Error == nil || bad.Error.Code != "BRIDGE_SCHEMA_INVALID" {
		t.Fatalf("tts.voices bogus engine = %+v, want schema invalid", bad)
	}

	// SAPI stays M95-001 on this engine-less machine.
	sapiResp := e.Handle(context.Background(), validRequest("tts.voices", `{}`))
	if sapiResp.OK || sapiResp.Error == nil || sapiResp.Error.Code != "M95-001" {
		t.Fatalf("tts.voices sapi = %+v, want M95-001", sapiResp)
	}
}

func TestTtsRefAudios(t *testing.T) {
	e := NewEngine(providerRepositoryStub{}, "test")
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "音色1.wav"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "note.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	resp := e.Handle(context.Background(), validRequest("tts.refAudios", `{"dir":`+quoteJSON(dir)+`}`))
	if !resp.OK {
		t.Fatalf("tts.refAudios = %+v, want ok", resp)
	}
	body := resp.Payload.(map[string]any)
	if body["exists"] != true {
		t.Fatalf("exists = %v", body["exists"])
	}
	entries := body["entries"].([]tts.RefAudioEntry)
	var audio, parent int
	for _, entry := range entries {
		if entry.Name == "音色1.wav" {
			audio++
		}
		if entry.Name == ".." {
			parent++
		}
		if entry.Name == "note.txt" {
			t.Fatalf("non-audio file leaked: %+v", entries)
		}
	}
	if audio != 1 || parent != 1 {
		t.Fatalf("entries = %+v, want 音色1.wav plus ..", entries)
	}

	missing := e.Handle(context.Background(), validRequest("tts.refAudios", `{"dir":`+quoteJSON(filepath.Join(dir, "nope"))+`}`))
	if !missing.OK || missing.Payload.(map[string]any)["exists"] != false {
		t.Fatalf("missing dir = %+v, want exists:false", missing)
	}

	empty := e.Handle(context.Background(), validRequest("tts.refAudios", `{}`))
	if empty.OK || empty.Error == nil || empty.Error.Code != "BRIDGE_SCHEMA_INVALID" {
		t.Fatalf("empty dir = %+v, want schema invalid", empty)
	}
}

func TestTtsSynthesizeRefValidation(t *testing.T) {
	e := NewEngine(providerRepositoryStub{}, "test")
	e.SetM9TtsService(tts.NewService(tts.NewRouterEngineWithEngines(nil, nil, nil)))

	noFile := e.Handle(context.Background(), validRequest("tts.synthesize", `{"text":"段","engine":"ref","refEndpoint":"http://127.0.0.1:9880"}`))
	if noFile.OK || noFile.Error == nil || noFile.Error.Code != "BRIDGE_SCHEMA_INVALID" {
		t.Fatalf("ref without file = %+v, want schema invalid", noFile)
	}

	badEndpoint := e.Handle(context.Background(), validRequest("tts.synthesize", `{"text":"段","engine":"ref","refWavPath":"x.wav"}`))
	if badEndpoint.OK || badEndpoint.Error == nil || badEndpoint.Error.Code != "BRIDGE_SCHEMA_INVALID" {
		t.Fatalf("ref without endpoint = %+v, want schema invalid", badEndpoint)
	}
}

func TestTtsSynthesizeEdgeFallsBackToSapi(t *testing.T) {
	e := NewEngine(providerRepositoryStub{}, "test")
	e.SetM9TtsService(tts.NewService(tts.NewRouterEngineWithEngines(&okSapiEngine{}, &failingSynthEngine{}, nil)))

	resp := e.Handle(context.Background(), validRequest("tts.synthesize", `{"text":"网断段","engine":"edge"}`))
	if !resp.OK {
		t.Fatalf("tts.synthesize edge fallback = %+v, want ok with notice", resp)
	}
	body := resp.Payload.(map[string]any)
	if body["notice"] != "TTS_ENGINE_FALLBACK" {
		t.Fatalf("notice = %v, want TTS_ENGINE_FALLBACK", body["notice"])
	}
	if body["wav_base64"] != "sapi-wav" {
		t.Fatalf("fallback audio = %v", body["wav_base64"])
	}
}

type okSapiEngine struct{}

func (okSapiEngine) Voices() ([]tts.Voice, error) {
	return []tts.Voice{{VoiceID: "v", DisplayName: "V", Gender: "neutral", Lang: "zh-CN"}}, nil
}
func (okSapiEngine) Synthesize(in tts.SynthesizeInput) (tts.SynthesizeResult, bool, error) {
	return tts.SynthesizeResult{WavBase64: "sapi-wav", DurationHint: 1}, false, nil
}

func quoteJSON(value string) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return string(encoded)
}
