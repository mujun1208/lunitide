// router_test.go pins the engine fan-out: edge payloads reach the cloud
// engine, ref payloads reach the ref engine, every natural/sapi payload
// keeps hitting the platform SAPI engine, and a missing engine surfaces
// M95-001 semantics.
package tts

import (
	"errors"
	"testing"
)

type recordingEngine struct {
	label string
	err   error
	calls []string
}

func (e *recordingEngine) Voices() ([]Voice, error) {
	return []Voice{{VoiceID: e.label}}, nil
}

func (e *recordingEngine) Synthesize(in SynthesizeInput) (SynthesizeResult, bool, error) {
	e.calls = append(e.calls, in.Text)
	if e.err != nil {
		return SynthesizeResult{}, false, e.err
	}
	return SynthesizeResult{WavBase64: e.label, DurationHint: 1}, false, nil
}

func TestRouterRoutesByEngine(t *testing.T) {
	sapi := &recordingEngine{label: "sapi-wav"}
	ref := &recordingEngine{label: "ref-wav"}
	edge := &recordingEngine{label: "edge-wav"}
	router := NewRouterEngineWithAll(sapi, ref, edge)

	cases := []struct {
		engine string
		want   string
	}{
		{"", "sapi-wav"},
		{EngineSapi, "sapi-wav"},
		{EngineNatural, "sapi-wav"},
		{EngineEdge, "edge-wav"},
		{EngineRef, "ref-wav"},
	}
	for _, tc := range cases {
		res, _, err := router.Synthesize(SynthesizeInput{Text: "段", Engine: tc.engine})
		if err != nil {
			t.Fatalf("engine %q: %v", tc.engine, err)
		}
		if res.WavBase64 != tc.want {
			t.Fatalf("engine %q routed to %q, want %q", tc.engine, res.WavBase64, tc.want)
		}
	}
	if len(sapi.calls) != 3 || len(ref.calls) != 1 || len(edge.calls) != 1 {
		t.Fatalf("call fan-out wrong: sapi=%d ref=%d edge=%d", len(sapi.calls), len(ref.calls), len(edge.calls))
	}
}

func TestRouterNilEngineIsM95_001(t *testing.T) {
	router := NewRouterEngineWithEngines(nil, nil)
	if _, err := router.Voices(); !errors.Is(err, ErrEngineUnavailable) {
		t.Fatalf("voices err = %v, want ErrEngineUnavailable", err)
	}
	for _, engine := range []string{"", EngineSapi, EngineNatural, EngineEdge, EngineRef} {
		if _, _, err := router.Synthesize(SynthesizeInput{Text: "段", Engine: engine}); !errors.Is(err, ErrEngineUnavailable) {
			t.Fatalf("engine %q err = %v, want ErrEngineUnavailable", engine, err)
		}
	}
}

func TestRouterVoicesUsesPlatformEngine(t *testing.T) {
	router := NewRouterEngineWithEngines(&recordingEngine{label: "sapi"}, nil)
	voices, err := router.Voices()
	if err != nil || len(voices) != 1 || voices[0].VoiceID != "sapi" {
		t.Fatalf("voices = %+v err=%v", voices, err)
	}
}

func TestRouterVoicesForEdge(t *testing.T) {
	router := NewRouterEngineWithAll(&recordingEngine{label: "sapi"}, nil, &recordingEngine{label: "edge"})
	voices, err := router.VoicesFor(EngineEdge)
	if err != nil || len(voices) != 1 || voices[0].VoiceID != "edge" {
		t.Fatalf("edge voices = %+v err=%v", voices, err)
	}
	if _, err := NewRouterEngineWithEngines(&recordingEngine{label: "sapi"}, nil).VoicesFor(EngineEdge); !errors.Is(err, ErrEngineUnavailable) {
		t.Fatalf("nil edge voices err = %v, want ErrEngineUnavailable", err)
	}
}
