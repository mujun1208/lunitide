package webviewhost

import (
	"encoding/json"
	"testing"
)

func TestMicrophoneGrantCDPParametersPinsGranted(t *testing.T) {
	var payload map[string]any
	if err := json.Unmarshal([]byte(MicrophoneGrantCDPParameters(TrustedOrigin)), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["origin"] != TrustedOrigin || payload["setting"] != "granted" {
		t.Fatalf("unexpected payload: %#v", payload)
	}
	descriptor, _ := payload["permission"].(map[string]any)
	if descriptor["name"] != "microphone" {
		t.Fatalf("unexpected permission descriptor: %#v", payload["permission"])
	}
}

func TestResetDeniedMicrophoneCacheDropsOnlyTrustedDenials(t *testing.T) {
	raw := []byte(`{"browser":{"enabled_labs_experiments":["x"]},` +
		`"profile":{"content_settings":{"exceptions":{"media_stream_mic":{` +
		`"https://app.lunitide.local:443,*":{"setting":2,"last_modified":"13431184145736957"},` +
		`"https://APP.LUNITIDE.LOCAL,*":{"setting":2},` +
		`"https://app.lunitide.local,*":{"setting":1},` +
		`"https://evil.example,*":{"setting":2},` +
		`"https://app.lunitide.local:444,*":{"setting":2}` +
		`}}}}}`)
	updated, changed := ResetDeniedMicrophoneCache(raw, TrustedVirtualHost)
	if !changed {
		t.Fatal("trusted denials were not removed")
	}
	var preferences struct {
		Browser map[string]any `json:"browser"`
		Profile struct {
			ContentSettings struct {
				Exceptions struct {
					MediaStreamMic map[string]any `json:"media_stream_mic"`
				} `json:"exceptions"`
			} `json:"content_settings"`
		} `json:"profile"`
	}
	if err := json.Unmarshal(updated, &preferences); err != nil {
		t.Fatalf("updated preferences are not valid JSON: %v", err)
	}
	if len(preferences.Browser) != 1 {
		t.Fatal("unrelated preferences content was lost")
	}
	remaining := preferences.Profile.ContentSettings.Exceptions.MediaStreamMic
	if _, ok := remaining["https://app.lunitide.local:443,*"]; ok {
		t.Fatal("default-port trusted denial survived")
	}
	if _, ok := remaining["https://APP.LUNITIDE.LOCAL,*"]; ok {
		t.Fatal("portless trusted denial survived")
	}
	for _, key := range []string{"https://app.lunitide.local,*", "https://evil.example,*", "https://app.lunitide.local:444,*"} {
		if _, ok := remaining[key]; !ok {
			t.Fatalf("unrelated entry %q was dropped", key)
		}
	}
	if len(remaining) != 3 {
		t.Fatalf("unexpected remaining entries: %#v", remaining)
	}
}

func TestResetDeniedMicrophoneCacheLeavesUnrelatedInputAlone(t *testing.T) {
	for name, raw := range map[string][]byte{
		"empty":            {},
		"malformed":        []byte(`{"profile":`),
		"no exceptions":    []byte(`{"profile":{"name":"Local State"}}`),
		"empty exceptions": []byte(`{"profile":{"content_settings":{"exceptions":{"media_stream_mic":{}}}}}`),
		"allowed only":     []byte(`{"profile":{"content_settings":{"exceptions":{"media_stream_mic":{"https://app.lunitide.local:443,*":{"setting":1}}}}}}`),
	} {
		updated, changed := ResetDeniedMicrophoneCache(raw, TrustedVirtualHost)
		if changed || string(updated) != string(raw) {
			t.Fatalf("%s: input was modified: changed=%v", name, changed)
		}
	}
}
