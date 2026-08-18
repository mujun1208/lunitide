// refcatalog_test.go covers the built-in 18-voice character catalogue:
// integrity (unique files, four style groups), voice-ID resolution,
// speed-factor mapping, service health probing and preset-driven
// synthesis through the ref engine.
package tts

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRefPresetCatalogIntegrity(t *testing.T) {
	// 18 role-play presets + 32 hot-pack presets = 50 timbres across
	// 10 style groups (4 role-play + 6 hot).
	if len(refPresets) != 50 {
		t.Fatalf("preset count = %d, want 50", len(refPresets))
	}
	seen := map[string]bool{}
	groups := map[string]bool{}
	for _, p := range refPresets {
		if seen[p.File] {
			t.Fatalf("duplicate preset file %q", p.File)
		}
		seen[p.File] = true
		if p.Gender != "male" && p.Gender != "female" {
			t.Fatalf("preset %q gender = %q", p.File, p.Gender)
		}
		groups[p.Group] = true
	}
	if len(groups) != 10 {
		t.Fatalf("style groups = %d, want 10 (%v)", len(groups), groups)
	}

	voices := RefVoices()
	if len(voices) != 50 {
		t.Fatalf("RefVoices len = %d, want 50", len(voices))
	}
	for _, v := range voices {
		if !IsRefPresetVoiceID(v.VoiceID) || v.Group == "" || v.Lang != "zh-CN" {
			t.Fatalf("bad preset voice %+v", v)
		}
	}
}

func TestRefResolveVoice(t *testing.T) {
	path, ok := RefResolveVoice("refpack:甜心少女.wav", "")
	if !ok || !strings.HasSuffix(path, filepath.Join("角色扮演", "甜心少女.wav")) {
		t.Fatalf("resolve default pack = %q ok=%v", path, ok)
	}
	custom, ok := RefResolveVoice("refpack:甜心少女.wav", `D:\voices`)
	if !ok || custom != `D:\voices\甜心少女.wav` {
		t.Fatalf("resolve custom pack = %q ok=%v", custom, ok)
	}
	if _, ok := RefResolveVoice("refpack:不存在.wav", ""); ok {
		t.Fatal("unknown preset file must not resolve")
	}
	if _, ok := RefResolveVoice("HKEY_LOCAL_MACHINE\\Speech", ""); ok {
		t.Fatal("non-refpack voice id must not resolve")
	}
}

func TestRefSpeedFactor(t *testing.T) {
	cases := map[int]float64{-10: 0.5, -8: 0.52, 0: 1, 10: 1.6, 20: 2}
	for rate, want := range cases {
		if got := refSpeedFactor(rate); got < want-0.001 || got > want+0.001 {
			t.Fatalf("speed(%d) = %v, want %v", rate, got, want)
		}
	}
}

func TestRefPackMetaProbesEndpointAndPack(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()
	meta := RefPackMeta(srv.URL)
	if !meta.ServerOnline {
		t.Fatal("probe must see the test server online")
	}
	if meta.Endpoint != srv.URL || meta.PackDir != DefaultRefPackDir+" + "+DefaultRefPackDirHot {
		t.Fatalf("meta echo = %+v", meta)
	}

	dead := RefPackMeta("http://127.0.0.1:1")
	if dead.ServerOnline {
		t.Fatal("closed port must report offline")
	}
	if dead.MissingFiles == nil {
		t.Fatal("missing_files must always be a list")
	}
}

func TestRefSynthesizePresetVoice(t *testing.T) {
	var gotPath string
	var gotSpeed float64
	wav := wavFixture(32000, 16000)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		gotPath, _ = body["ref_audio_path"].(string)
		gotSpeed, _ = body["speed_factor"].(float64)
		w.Header().Set("Content-Type", "audio/wav")
		_, _ = w.Write(wav)
	}))
	defer srv.Close()

	engine := NewRefEngine()
	// Point the pack at an empty temp dir first: the preset file is
	// missing, so synthesis must fail naming the pack dir (the real
	// collection may exist on a dev machine, which would otherwise make
	// this branch machine-dependent).
	pack := t.TempDir()
	orig := DefaultRefPackDir
	DefaultRefPackDir = pack
	defer func() { DefaultRefPackDir = orig }()

	_, _, err := engine.Synthesize(SynthesizeInput{
		Text: "段", Engine: EngineRef, VoiceID: "refpack:甜心少女.wav",
		RefEndpoint: srv.URL,
	})
	if err == nil || !errors.Is(err, ErrSynthesisFailed) {
		t.Fatalf("missing pack file err = %v, want ErrSynthesisFailed", err)
	}
	if !strings.Contains(err.Error(), pack) {
		t.Fatalf("error should name the pack dir: %v", err)
	}

	// Drop the preset file in: synthesis must resolve and succeed.
	if err := os.WriteFile(filepath.Join(pack, "冷面霸总.wav"), wav, 0o600); err != nil {
		t.Fatal(err)
	}

	res, fallback, err := engine.Synthesize(SynthesizeInput{
		Text: "晚上好", Engine: EngineRef, VoiceID: "refpack:冷面霸总.wav", Rate: 10,
		RefEndpoint: srv.URL,
	})
	if err != nil || fallback {
		t.Fatalf("preset synthesize err=%v fallback=%v", err, fallback)
	}
	if want := filepath.Join(pack, "冷面霸总.wav"); gotPath != want {
		t.Fatalf("service ref path = %q, want %q", gotPath, want)
	}
	if gotSpeed < 1.59 || gotSpeed > 1.61 {
		t.Fatalf("speed factor = %v, want ~1.6", gotSpeed)
	}
	if !strings.HasPrefix(res.WavBase64, "UklGR") {
		t.Fatal("result is not RIFF base64")
	}
}
