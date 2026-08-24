package tts

import (
	"context"
	"encoding/binary"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestParseEdgeVoicesRanksXiaoxiaoFirst(t *testing.T) {
	raw := []byte(`[
		{"ShortName":"en-US-AriaNeural","FriendlyName":"Microsoft Aria Online (Natural) - English (United States)","Gender":"Female","Locale":"en-US"},
		{"ShortName":"zh-CN-YunxiNeural","FriendlyName":"Microsoft Yunxi Online (Natural) - Chinese (Mainland)","Gender":"Male","Locale":"zh-CN"},
		{"ShortName":"zh-CN-XiaoxiaoNeural","FriendlyName":"Microsoft Xiaoxiao Online (Natural) - Chinese (Mainland)","Gender":"Female","Locale":"zh-CN"},
		{"ShortName":"zh-CN-XiaoyiNeural","FriendlyName":"Microsoft Xiaoyi Online (Natural) - Chinese (Mainland)","Gender":"Female","Locale":"zh-CN"},
		{"ShortName":"zh-CN-YunjianNeural","FriendlyName":"Microsoft Yunjian Online (Natural) - Chinese (Mainland)","Gender":"Male","Locale":"zh-CN"},
		{"ShortName":"zh-CN-YunxiaNeural","FriendlyName":"Microsoft Yunxia Online (Natural) - Chinese (Mainland)","Gender":"Male","Locale":"zh-CN"},
		{"ShortName":"zh-CN-YunyangNeural","FriendlyName":"Microsoft Yunyang Online (Natural) - Chinese (Mainland)","Gender":"Male","Locale":"zh-CN"}
	]`)
	voices, err := parseEdgeVoices(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(voices) != 50 {
		t.Fatalf("len=%d, want 50 Mandarin presets", len(voices))
	}
	wantFirst := edgeVoiceStyleID(edgeDefaultVoice, "chat")
	if voices[0].VoiceID != wantFirst || voices[0].DisplayName != "晓晓 · 温柔日常（推荐）" {
		t.Fatalf("first = %+v, want Xiaoxiao chat preset", voices[0])
	}
}

func TestEdgeSSMLEscapesAndMapsRate(t *testing.T) {
	ssml := edgeSSML(SynthesizeInput{Text: `你好 <世界>`, VoiceID: "zh-CN-XiaoxiaoNeural", Rate: 4, Volume: 80})
	if !strings.Contains(ssml, "你好 &lt;世界&gt;") {
		t.Fatalf("unescaped ssml: %s", ssml)
	}
	if !strings.Contains(ssml, `rate="+40%"`) {
		t.Fatalf("rate mapping: %s", ssml)
	}
	if !strings.Contains(ssml, `xml:lang="zh-CN"`) {
		t.Fatalf("lang: %s", ssml)
	}
	if !strings.Contains(ssml, `style="chat"`) || !strings.Contains(ssml, "mstts:express-as") {
		t.Fatalf("chat style missing: %s", ssml)
	}
}

func TestEdgeSSMLFollowsTheSentenceMood(t *testing.T) {
	happy := edgeSSML(SynthesizeInput{Text: "太好了，那我们出发吧！", VoiceID: "zh-CN-XiaoxiaoNeural", Volume: 80})
	if !strings.Contains(happy, `style="cheerful"`) || !strings.Contains(happy, `styledegree="1.8"`) {
		t.Fatalf("cheerful line: %s", happy)
	}
	soft := edgeSSML(SynthesizeInput{Text: "别担心，慢慢来就好。", VoiceID: "zh-CN-XiaoxiaoNeural", Volume: 80})
	if !strings.Contains(soft, `style="gentle"`) || !strings.Contains(soft, `pitch="+2%"`) {
		t.Fatalf("gentle line: %s", soft)
	}
	ask := edgeSSML(SynthesizeInput{Text: "你今天过得怎么样？", VoiceID: "zh-CN-XiaoxiaoNeural", Volume: 80})
	if !strings.Contains(ask, `style="chat"`) || !strings.Contains(ask, `pitch="+9%"`) {
		t.Fatalf("question line: %s", ask)
	}
}

func TestEdgeSSMLKeepsAnExplicitPersonaAndUnsupportedStyles(t *testing.T) {
	// 「晓晓 · 新闻播报」 is the user's pick: an excited sentence must not
	// silently turn it into the cheerful persona.
	chosen := edgeSSML(SynthesizeInput{Text: "太好了！", VoiceID: "zh-CN-XiaoxiaoNeural", Style: "newscast", Volume: 80})
	if !strings.Contains(chosen, `style="newscast"`) {
		t.Fatalf("explicit style overridden: %s", chosen)
	}
	// Yunyang publishes no cheerful preset — the base style stays put.
	if edgeVoiceSupportsStyle("zh-CN-YunyangNeural", "cheerful") {
		t.Fatal("Yunyang should not advertise cheerful")
	}
	flat := edgeSSML(SynthesizeInput{Text: "太好了！", VoiceID: "zh-CN-YunyangNeural", Style: "chat", Volume: 80})
	if !strings.Contains(flat, `style="chat"`) {
		t.Fatalf("unsupported style used: %s", flat)
	}
}

func TestEdgeSecMSGECIsStableInsideThe300sWindow(t *testing.T) {
	a := time.Date(2026, 8, 19, 5, 0, 10, 0, time.UTC)
	b := a.Add(10 * time.Second)
	if edgeSecMSGEC(a) == "" || edgeSecMSGEC(a) != edgeSecMSGEC(b) {
		t.Fatalf("gec not stable: %s vs %s", edgeSecMSGEC(a), edgeSecMSGEC(b))
	}
	if edgeSecMSGEC(a) == edgeSecMSGEC(a.Add(6*time.Minute)) {
		t.Fatal("gec should rotate every 300s")
	}
}

func TestEdgeAudioPayloadStripsHeaders(t *testing.T) {
	header := []byte("Path:audio\r\n")
	frame := make([]byte, 2+len(header)+4)
	binary.BigEndian.PutUint16(frame[:2], uint16(len(header)))
	copy(frame[2:], header)
	copy(frame[2+len(header):], []byte("MP3."))
	got := edgeAudioPayload(frame)
	if string(got) != "MP3." {
		t.Fatalf("payload=%q", got)
	}
	legacy := []byte("X-RequestId:abc\r\nPath:audio\r\nRIFF....")
	if string(edgeAudioPayload(legacy)) != "RIFF...." {
		t.Fatal("legacy delimiter fallback broken")
	}
}

func TestEdgeVoicesUsesInjectedHTTP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("trustedclienttoken") != edgeTrustedToken {
			t.Fatalf("missing token: %s", r.URL.RawQuery)
		}
		if r.URL.Query().Get("Sec-MS-GEC") == "" {
			t.Fatal("missing Sec-MS-GEC")
		}
		_, _ = w.Write([]byte(`[
			{"ShortName":"zh-CN-XiaoxiaoNeural","FriendlyName":"Microsoft Xiaoxiao Online (Natural) - Chinese (Mainland)","Gender":"Female","Locale":"zh-CN"},
			{"ShortName":"zh-CN-XiaoyiNeural","FriendlyName":"Microsoft Xiaoyi Online (Natural) - Chinese (Mainland)","Gender":"Female","Locale":"zh-CN"},
			{"ShortName":"zh-CN-YunjianNeural","FriendlyName":"Microsoft Yunjian Online (Natural) - Chinese (Mainland)","Gender":"Male","Locale":"zh-CN"},
			{"ShortName":"zh-CN-YunxiNeural","FriendlyName":"Microsoft Yunxi Online (Natural) - Chinese (Mainland)","Gender":"Male","Locale":"zh-CN"},
			{"ShortName":"zh-CN-YunxiaNeural","FriendlyName":"Microsoft Yunxia Online (Natural) - Chinese (Mainland)","Gender":"Male","Locale":"zh-CN"},
			{"ShortName":"zh-CN-YunyangNeural","FriendlyName":"Microsoft Yunyang Online (Natural) - Chinese (Mainland)","Gender":"Male","Locale":"zh-CN"}
		]`))
	}))
	defer srv.Close()
	eng := NewEdgeEngine().(*edgeEngine)
	eng.voicesURL = srv.URL
	eng.client = srv.Client()
	voices, err := eng.Voices()
	if err != nil || len(voices) != 50 || voices[0].VoiceID != edgeVoiceStyleID(edgeDefaultVoice, "chat") {
		t.Fatalf("voices=%+v err=%v", voices, err)
	}
}

func TestEdgeSynthesizeUsesInjectedSynth(t *testing.T) {
	eng := NewEdgeEngine().(*edgeEngine)
	var got SynthesizeInput
	eng.synth = func(_ context.Context, in SynthesizeInput) (SynthesizeResult, bool, error) {
		got = in
		return SynthesizeResult{WavBase64: "d2F2", DurationHint: 0.2}, false, nil
	}
	res, _, err := eng.Synthesize(SynthesizeInput{Text: "你好", VoiceID: `HKEY_LOCAL_MACHINE\x`, Rate: 4, Volume: 80})
	if err != nil {
		t.Fatal(err)
	}
	if got.VoiceID != edgeDefaultVoice {
		t.Fatalf("HKEY voice should fall back to Xiaoxiao, got %s", got.VoiceID)
	}
	if got.Style != "chat" {
		t.Fatalf("default style = %q, want chat", got.Style)
	}
	if res.WavBase64 != "d2F2" {
		t.Fatalf("wav = %s", res.WavBase64)
	}
}

func TestEdgeSynthesizeNilSynthIsUnavailable(t *testing.T) {
	eng := &edgeEngine{}
	if _, _, err := eng.Synthesize(SynthesizeInput{Text: "段"}); !errors.Is(err, ErrEngineUnavailable) {
		t.Fatalf("err=%v", err)
	}
}
