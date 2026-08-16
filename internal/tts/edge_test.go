// edge_test.go pins the offline parts of the edge engine: the DRM token
// derivation, SSML rate/volume mapping and XML escaping. The websocket
// session itself needs the live service and stays behind the synthesis
// integration path.
package tts

import (
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestEdgeGECDeterministic(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	a, b := edgeGEC(now), edgeGEC(now)
	if a != b {
		t.Fatalf("edgeGEC not deterministic: %s != %s", a, b)
	}
	if len(a) != 64 {
		t.Fatalf("edgeGEC length = %d, want 64 hex chars", len(a))
	}
	if !regexp.MustCompile(`^[0-9A-F]{64}$`).MatchString(a) {
		t.Fatalf("edgeGEC = %s, want uppercase hex", a)
	}
	// The tick floor is 5 minutes: two stamps inside one window match,
	// across a boundary they do not.
	if edgeGEC(now.Add(time.Minute)) != a {
		t.Fatalf("edgeGEC changed inside the same 5-minute window")
	}
}

func TestEdgeSSMLMapsRateAndVolume(t *testing.T) {
	fast := edgeSSML("你好", "zh-CN-YunxiNeural", 5, 60)
	if !strings.Contains(fast, "rate='+50%'") || !strings.Contains(fast, "volume='60%'") {
		t.Fatalf("ssml prosody missing: %s", fast)
	}
	if !strings.Contains(fast, "voice name='zh-CN-YunxiNeural'") {
		t.Fatalf("ssml voice missing: %s", fast)
	}
	slow := edgeSSML("你好", "zh-CN-XiaoxiaoNeural", -3, 100)
	if !strings.Contains(slow, "rate='-30%'") || !strings.Contains(slow, "volume='100%'") {
		t.Fatalf("ssml negative prosody missing: %s", slow)
	}
	flat := edgeSSML("你好", "zh-CN-XiaoxiaoNeural", 0, 80)
	if !strings.Contains(flat, "rate='0%'") {
		t.Fatalf("ssml zero rate missing: %s", flat)
	}
}

func TestEdgeSSMLEscapesXML(t *testing.T) {
	ssml := edgeSSML(`a<b>&"c'`, "zh-CN-XiaoxiaoNeural", 0, 80)
	if strings.Contains(ssml, "a<b>") || strings.Contains(ssml, `&"`) {
		t.Fatalf("ssml did not escape XML: %s", ssml)
	}
	for _, esc := range []string{"&lt;", "&gt;", "&amp;", "&quot;", "&apos;"} {
		if !strings.Contains(ssml, esc) {
			t.Fatalf("ssml escape %s missing: %s", esc, ssml)
		}
	}
}

func TestEdgeVoicesCatalogue(t *testing.T) {
	voices := EdgeVoices()
	if len(voices) < 10 {
		t.Fatalf("edge catalogue too small: %d", len(voices))
	}
	found := false
	for _, v := range voices {
		if v.VoiceID == edgeDefaultVoice {
			found = true
		}
	}
	if !found {
		t.Fatalf("default voice %s missing from catalogue", edgeDefaultVoice)
	}
	// Copies must not alias the package-level slice.
	voices[0].DisplayName = "mutated"
	if edgeVoices[0].DisplayName == "mutated" {
		t.Fatalf("EdgeVoices returns an aliased slice")
	}
}
