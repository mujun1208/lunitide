// m9_tts_handlers_test.go pins the MC-05 real-machine degradation
// acceptance: a Windows N / stripped-install / broken-COM machine must
// surface M95-001 on every tts.* method while the chat pipeline itself
// keeps working (subtitle-only degradation, never a hard failure).
package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
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
	e.SetM9TtsService(tts.NewService(tts.NewRouterEngineWithEngines(&okSapiEngine{}, nil)))

	// natural and legacy edge share the platform catalogue.
	for _, engine := range []string{"natural", "edge", "sapi", ""} {
		payload := fmt.Sprintf(`{"engine":%q}`, engine)
		resp := e.Handle(context.Background(), validRequest("tts.voices", payload))
		if !resp.OK {
			t.Fatalf("tts.voices %q = %+v, want ok", engine, resp)
		}
		voices := resp.Payload.(map[string]any)["voices"].([]tts.Voice)
		if len(voices) == 0 || voices[0].VoiceID != "v" {
			t.Fatalf("tts.voices %q voices = %+v", engine, voices)
		}
	}

	ref := e.Handle(context.Background(), validRequest("tts.voices", `{"engine":"ref"}`))
	if !ref.OK {
		t.Fatalf("tts.voices ref = %+v, want ok", ref)
	}
	refVoices := ref.Payload.(map[string]any)["voices"].([]tts.Voice)
	// 18 role-play presets + 32 hot-pack presets = 50 selectable timbres.
	if len(refVoices) != 50 || refVoices[0].VoiceID != tts.RefDefaultVoiceID() || refVoices[0].Group == "" {
		t.Fatalf("ref voices = %d entries, want 50 grouped presets (first = %+v)", len(refVoices), refVoices[0])
	}
	if _, ok := ref.Payload.(map[string]any)["ref_meta"].(tts.RefMeta); !ok {
		t.Fatalf("tts.voices ref payload missing ref_meta: %+v", ref.Payload)
	}

	bad := e.Handle(context.Background(), validRequest("tts.voices", `{"engine":"mega"}`))
	if bad.OK || bad.Error == nil || bad.Error.Code != "BRIDGE_SCHEMA_INVALID" {
		t.Fatalf("tts.voices bogus engine = %+v, want schema invalid", bad)
	}
}

func TestTtsVoicesEnginelessIsM95_001(t *testing.T) {
	e := NewEngine(providerRepositoryStub{}, "test")
	e.SetM9TtsService(tts.NewService(tts.NewRouterEngineWithEngines(nil, nil)))
	sapiResp := e.Handle(context.Background(), validRequest("tts.voices", `{}`))
	if sapiResp.OK || sapiResp.Error == nil || sapiResp.Error.Code != "M95-001" {
		t.Fatalf("tts.voices sapi = %+v, want M95-001", sapiResp)
	}
}

func TestTtsEnsureRefEngine(t *testing.T) {
	e := NewEngine(providerRepositoryStub{}, "test")
	// A live /docs stub makes the host report online without spawning
	// anything; the endpoint is deliberately not the default so the
	// auto-host never touches a real launcher.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	resp := e.Handle(context.Background(), validRequest("tts.ensureRefEngine", `{"refEndpoint":`+quoteJSON(srv.URL)+`}`))
	if !resp.OK {
		t.Fatalf("tts.ensureRefEngine = %+v, want ok", resp)
	}
	body := resp.Payload.(map[string]any)
	if body["state"] != tts.RefHostOnline {
		t.Fatalf("state = %v, want online", body["state"])
	}
	if body["endpoint"] != srv.URL {
		t.Fatalf("endpoint = %v", body["endpoint"])
	}

	bad := e.Handle(context.Background(), validRequest("tts.ensureRefEngine", `{"refEndpoint":123}`))
	if bad.OK || bad.Error == nil || bad.Error.Code != "BRIDGE_SCHEMA_INVALID" {
		t.Fatalf("tts.ensureRefEngine bad payload = %+v, want schema invalid", bad)
	}
	// NOTE: no default-endpoint case here — on a dev machine with a real
	// E:\GPT-SoVITS install the launch path would spawn the actual model
	// server from inside the test process.
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
	e.SetM9TtsService(tts.NewService(tts.NewRouterEngineWithEngines(nil, nil)))

	// A custom (non-refpack) voice without a reference file stays
	// schema-invalid; empty voiceId now means the default preset.
	noFile := e.Handle(context.Background(), validRequest("tts.synthesize", `{"text":"段","engine":"ref","voiceId":"HKEY_LOCAL_MACHINE_X","refEndpoint":"http://127.0.0.1:9880"}`))
	if noFile.OK || noFile.Error == nil || noFile.Error.Code != "BRIDGE_SCHEMA_INVALID" {
		t.Fatalf("ref without file = %+v, want schema invalid", noFile)
	}

	badEndpoint := e.Handle(context.Background(), validRequest("tts.synthesize", `{"text":"段","engine":"ref","refWavPath":"x.wav","refEndpoint":"ftp://x"}`))
	if badEndpoint.OK || badEndpoint.Error == nil || badEndpoint.Error.Code != "BRIDGE_SCHEMA_INVALID" {
		t.Fatalf("ref with bad endpoint = %+v, want schema invalid", badEndpoint)
	}

	// Preset voices skip the path checks (the engine resolves the pack
	// folder), so validation passes and the nil ref engine answers
	// M95-001 — proving the request really reached the router.
	preset := e.Handle(context.Background(), validRequest("tts.synthesize", `{"text":"段","engine":"ref","voiceId":"refpack:甜心少女.wav"}`))
	if preset.OK || preset.Error == nil || preset.Error.Code != "M95-001" {
		t.Fatalf("ref preset = %+v, want M95-001 from nil engine", preset)
	}
}

type okSapiEngine struct{}

func (okSapiEngine) Voices() ([]tts.Voice, error) {
	return []tts.Voice{{VoiceID: "v", DisplayName: "V", Gender: "neutral", Lang: "zh-CN"}}, nil
}
func (okSapiEngine) Synthesize(in tts.SynthesizeInput) (tts.SynthesizeResult, bool, error) {
	return tts.SynthesizeResult{WavBase64: "sapi-wav", DurationHint: 1}, false, nil
}

func TestTtsSynthesizeNaturalRoutesToPlatform(t *testing.T) {
	e := NewEngine(providerRepositoryStub{}, "test")
	e.SetM9TtsService(tts.NewService(tts.NewRouterEngineWithEngines(&failingSynthEngine{}, &okSapiEngine{})))

	// natural and legacy edge both land on the platform engine; a
	// synthesis failure there is a plain M95-002, no engine fallback
	// (the ok ref engine proves the router does not silently reroute).
	for _, engine := range []string{"natural", "edge"} {
		payload := fmt.Sprintf(`{"text":"段","engine":%q}`, engine)
		resp := e.Handle(context.Background(), validRequest("tts.synthesize", payload))
		if resp.OK {
			t.Fatalf("tts.synthesize %q = %+v, want failure", engine, resp)
		}
		if resp.Error == nil || resp.Error.Code != "M95-002" {
			t.Fatalf("tts.synthesize %q code = %+v, want M95-002", engine, resp.Error)
		}
	}
}

func quoteJSON(value string) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return string(encoded)
}
