package tts

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestVolcVoicesAreOfficialSpeakers(t *testing.T) {
	voices := VolcVoices()
	if len(voices) < 20 {
		t.Fatalf("volc voices = %d, want a documented 2.0 set (not padded to 50)", len(voices))
	}
	if len(voices) == 50 {
		t.Fatal("volc catalogue must not pretend to be the local 50-life pack")
	}
	seen := map[string]bool{}
	for _, v := range voices {
		if seen[v.VoiceID] {
			t.Fatalf("duplicate speaker %s", v.VoiceID)
		}
		seen[v.VoiceID] = true
		if !strings.HasSuffix(v.VoiceID, "_uranus_bigtts") {
			t.Fatalf("non-2.0 speaker leaked: %s", v.VoiceID)
		}
		if strings.Contains(v.DisplayName, "温柔桃子") || strings.HasPrefix(v.VoiceID, "refpack:") {
			t.Fatalf("doubao-app / local clone id leaked: %+v", v)
		}
		if v.Group == "" || v.DisplayName == "" {
			t.Fatalf("incomplete row: %+v", v)
		}
	}
	if !IsVolcSpeakerID(VolcDefaultVoiceID()) {
		t.Fatal("default speaker missing from catalogue")
	}
	if VolcDefaultVoiceID() != "zh_female_xiaohe_uranus_bigtts" {
		t.Fatalf("default = %s, want 小何 2.0", VolcDefaultVoiceID())
	}
}

func TestVolcSynthesizeHTTP(t *testing.T) {
	chunk := base64.StdEncoding.EncodeToString([]byte("ID3fake-mp3"))
	var sawKey, sawResource, speaker string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawKey = r.Header.Get("X-Api-Key")
		sawResource = r.Header.Get("X-Api-Resource-Id")
		body, _ := io.ReadAll(r.Body)
		var payload struct {
			ReqParams struct {
				Speaker string `json:"speaker"`
			} `json:"req_params"`
		}
		_ = json.Unmarshal(body, &payload)
		speaker = payload.ReqParams.Speaker
		w.Header().Set("X-Tt-Logid", "test-logid")
		_, _ = io.WriteString(w, `{"code":0,"data":"`+chunk+`"}`+"\n")
		_, _ = io.WriteString(w, `{"code":20000000}`+"\n")
	}))
	defer srv.Close()

	eng := &volcEngine{client: srv.Client(), url: srv.URL}
	res, fallback, err := eng.Synthesize(SynthesizeInput{
		Text: "你好", Engine: EngineVolc, VolcAPIKey: "ark-test-key", VoiceID: "zh_female_vv_uranus_bigtts",
	})
	if err != nil {
		t.Fatalf("synth: %v", err)
	}
	if fallback {
		t.Fatal("known speaker should not fall back")
	}
	if sawKey != "ark-test-key" || sawResource != "seed-tts-2.0" || speaker != "zh_female_vv_uranus_bigtts" {
		t.Fatalf("headers key=%s resource=%s speaker=%s", sawKey, sawResource, speaker)
	}
	raw, err := base64.StdEncoding.DecodeString(res.WavBase64)
	if err != nil || string(raw) != "ID3fake-mp3" {
		t.Fatalf("audio = %q err=%v", raw, err)
	}
}

func TestVolcSynthesizeMissingKeyIsM95_001(t *testing.T) {
	eng := NewVolcEngine()
	_, _, err := eng.Synthesize(SynthesizeInput{Text: "你好", Engine: EngineVolc})
	if !errors.Is(err, ErrEngineUnavailable) {
		t.Fatalf("err = %v, want ErrEngineUnavailable", err)
	}
	if !strings.Contains(err.Error(), "火山") {
		t.Fatalf("err = %v, want 火山", err)
	}
}

func TestVolcUnknownSpeakerFallsBackToXiaohe(t *testing.T) {
	var speaker string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var payload struct {
			ReqParams struct {
				Speaker string `json:"speaker"`
			} `json:"req_params"`
		}
		_ = json.Unmarshal(body, &payload)
		speaker = payload.ReqParams.Speaker
		_, _ = io.WriteString(w, `{"code":0,"data":"`+base64.StdEncoding.EncodeToString([]byte("x"))+`"}`+"\n")
		_, _ = io.WriteString(w, `{"code":20000000}`+"\n")
	}))
	defer srv.Close()
	eng := &volcEngine{client: srv.Client(), url: srv.URL}
	_, fallback, err := eng.Synthesize(SynthesizeInput{
		Text: "段", Engine: EngineVolc, VolcAPIKey: "k", VoiceID: "refpack:甜心少女.wav",
	})
	if err != nil {
		t.Fatalf("synth: %v", err)
	}
	if !fallback || speaker != volcDefaultSpeaker {
		t.Fatalf("speaker=%s fallback=%v", speaker, fallback)
	}
}

func TestParseVolcNDJSONError(t *testing.T) {
	_, err := parseVolcNDJSON([]byte(`{"code":45000000,"message":"speaker permission denied"}` + "\n"))
	if err == nil || !strings.Contains(err.Error(), "speaker permission denied") {
		t.Fatalf("err = %v", err)
	}
}

func TestVolcResourceID(t *testing.T) {
	if volcResourceID("seed-tts-2.0") != "seed-tts-2.0" {
		t.Fatal("resource id must stay seed-tts-2.0")
	}
	speaker, fallback := volcResolveSpeaker("seed-tts-2.0")
	if speaker != volcDefaultSpeaker || fallback {
		t.Fatalf("resource id should map to 小何 without fallback, got %s fallback=%v", speaker, fallback)
	}
	if volcResourceID("zh_female_xiaohe_uranus_bigtts") != "seed-tts-2.0" {
		t.Fatal("2.0 speaker must use seed-tts-2.0")
	}
	if volcResourceID("zh_female_shuangkuaisisi_moon_bigtts") != "seed-tts-1.0" {
		t.Fatal("1.0 speaker must use seed-tts-1.0")
	}
	for _, alias := range []string{"seedtts-2.0", "doubao-seed-tts-2.0", "seed-tts"} {
		if CanonicalTTSResourceID(alias) != "seed-tts-2.0" {
			t.Fatalf("%s must canonicalize to seed-tts-2.0", alias)
		}
		speaker, fallback := volcResolveSpeaker(alias)
		if speaker != volcDefaultSpeaker || fallback {
			t.Fatalf("%s speaker=%s fallback=%v", alias, speaker, fallback)
		}
		if volcResourceID(alias) != "seed-tts-2.0" {
			t.Fatalf("%s resource must stay 2.0", alias)
		}
	}
	if CanonicalTTSResourceID("seed-tts-1.0") != "seed-tts-1.0" {
		t.Fatal("1.0 resource alias")
	}
	if IsVolcTTSResourceID("zh_female_xiaohe_uranus_bigtts") {
		t.Fatal("speaker is not a resource id")
	}
}
