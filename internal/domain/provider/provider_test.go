package provider

import (
	"testing"
	"time"
)

func TestValidateRequiresExactlyOneDefaultModel(t *testing.T) {
	p := Provider{ID: "01K00000000000000000000000", Name: "DeepSeek", Protocol: ProtocolOpenAICompatible, BaseURL: "https://api.deepseek.com", Models: []Model{{ModelID: "deepseek-chat", DisplayName: "DeepSeek Chat"}}}
	if err := p.Validate(); err == nil {
		t.Fatal("expected missing default model to fail")
	}
	p.Models[0].IsDefault = true
	if err := p.Validate(); err != nil {
		t.Fatalf("expected provider to be valid: %v", err)
	}
}

func TestValidateAcceptsFourModelKindsAndOneKindDefault(t *testing.T) {
	p := Provider{ID: "01K00000000000000000000000", Name: "Mixed", Protocol: ProtocolOpenAICompatible, BaseURL: "https://example.com", Models: []Model{
		{ModelID: "chat", DisplayName: "Chat", IsDefault: true, Kind: KindLLM, KindDefault: true},
		{ModelID: "ocr", DisplayName: "OCR", Kind: KindVision, KindDefault: true},
	}}
	if err := p.Validate(); err != nil {
		t.Fatalf("mixed kinds: %v", err)
	}
	p.Models = append(p.Models, Model{ModelID: "ocr-2", DisplayName: "OCR 2", Kind: KindVision, KindDefault: true})
	if err := p.Validate(); err == nil {
		t.Fatal("two vision kind defaults on one provider must fail")
	}
}

func TestCatalogForKindOrdersDefaultThenBackups(t *testing.T) {
	a := Provider{ID: "01K00000000000000000000001", Name: "A", Status: StatusEnabled, CredentialState: CredentialConfigured, CredentialRef: "ref", CreatedAt: mustTime("2026-01-02T00:00:00Z"), Models: []Model{
		{ModelID: "backup", DisplayName: "Backup", Kind: KindVision},
	}}
	b := Provider{ID: "01K00000000000000000000002", Name: "B", Status: StatusEnabled, CredentialState: CredentialConfigured, CredentialRef: "ref", CreatedAt: mustTime("2026-01-01T00:00:00Z"), Models: []Model{
		{ModelID: "primary", DisplayName: "Primary", Kind: KindVision, KindDefault: true},
	}}
	disabled := Provider{ID: "01K00000000000000000000003", Name: "Off", Status: StatusDisabled, CredentialState: CredentialConfigured, CredentialRef: "ref", Models: []Model{
		{ModelID: "skip", DisplayName: "Skip", Kind: KindVision, KindDefault: true, IsDefault: true},
	}}
	got := CatalogForKind([]Provider{a, b, disabled}, KindVision)
	if len(got) != 2 || got[0].Model.ModelID != "primary" || got[1].Model.ModelID != "backup" {
		t.Fatalf("catalog = %#v", got)
	}
}

func TestVisionDescribeCatalogPrefersKindThenVisionLLM(t *testing.T) {
	chat := Provider{ID: "01K00000000000000000000004", Name: "Chat", Status: StatusEnabled, CredentialState: CredentialConfigured, CredentialRef: "ref", Models: []Model{
		{ModelID: "deepseek-v4-pro", DisplayName: "DeepSeek", IsDefault: true, Kind: KindLLM},
		{ModelID: "gpt-4o", DisplayName: "4o", Kind: KindLLM, SupportsVision: true},
	}}
	vlm := Provider{ID: "01K00000000000000000000005", Name: "VLM", Status: StatusEnabled, CredentialState: CredentialConfigured, CredentialRef: "ref", Models: []Model{
		{ModelID: "qwen2-vl-8b", DisplayName: "Qwen VL", Kind: KindVision, KindDefault: true},
	}}
	got := VisionDescribeCatalog([]Provider{chat, vlm}, "deepseek-v4-pro")
	if len(got) != 2 || got[0].Model.ModelID != "qwen2-vl-8b" || got[1].Model.ModelID != "gpt-4o" {
		t.Fatalf("vision describe catalog = %#v", got)
	}
}

func mustTime(v string) time.Time {
	t, err := time.Parse(time.RFC3339, v)
	if err != nil {
		panic(err)
	}
	return t
}

func TestValidateRejectsUnknownProtocol(t *testing.T) {
	p := Provider{ID: "01K00000000000000000000000", Name: "Example", Protocol: "openai", BaseURL: "https://example.com", Models: []Model{{ModelID: "model", DisplayName: "Model", IsDefault: true}}}
	if err := p.Validate(); err == nil {
		t.Fatal("legacy protocol must be migrated, not accepted by domain")
	}
}

func TestValidateAcceptsVolcSpeechVoiceModels(t *testing.T) {
	p := Provider{ID: "01K00000000000000000000000", Name: "Volc", Protocol: ProtocolVolcSpeech, BaseURL: "https://openspeech.bytedance.com", Models: []Model{
		{ModelID: "seed-asr-2.0", DisplayName: "seed-asr 2.0", IsDefault: true, Kind: KindVoice},
	}}
	if err := p.Validate(); err != nil {
		t.Fatalf("volc voice: %v", err)
	}
	p.Models[0].Kind = KindLLM
	if err := p.Validate(); err == nil {
		t.Fatal("volc speech must not carry an llm model")
	}
}

func TestValidateAcceptsVolcSpeechAsrAndTts(t *testing.T) {
	p := Provider{ID: "01K00000000000000000000000", Name: "Volc", Protocol: ProtocolVolcSpeech, BaseURL: "https://openspeech.bytedance.com", Models: []Model{
		{ModelID: "seed-asr-2.0", DisplayName: "seed-asr 2.0", IsDefault: true, Kind: KindASR, KindDefault: true},
		{ModelID: "zh_female_xiaohe_uranus_bigtts", DisplayName: "小何", Kind: KindTTS, KindDefault: true},
	}}
	if err := p.Validate(); err != nil {
		t.Fatalf("volc asr+tts: %v", err)
	}
	p.Models = append(p.Models, Model{ModelID: "seed-asr-backup", DisplayName: "backup", Kind: KindASR, KindDefault: true})
	if err := p.Validate(); err == nil {
		t.Fatal("two asr kind defaults on one provider must fail")
	}
}

func TestNormalizeKindMapsVoiceToAsr(t *testing.T) {
	if NormalizeKind("voice") != KindASR {
		t.Fatalf("voice = %q", NormalizeKind("voice"))
	}
	if NormalizeKind("asr") != KindASR || NormalizeKind("tts") != KindTTS {
		t.Fatal("asr/tts must persist")
	}
	if !ValidKind("asr") || !ValidKind("tts") || !ValidKind("voice") || ValidKind("nope") {
		t.Fatal("valid kinds")
	}
	if (Model{Kind: KindVoice}).EffectiveKind() != KindASR {
		t.Fatal("leftover voice is listen")
	}
	if !IsSpeechKind(KindASR) || !IsSpeechKind(KindTTS) || !IsSpeechKind(KindVoice) || IsSpeechKind(KindLLM) {
		t.Fatal("speech kinds")
	}
}

func TestCatalogForKindVoiceIncludesAsrAndTts(t *testing.T) {
	p := Provider{ID: "01K00000000000000000000006", Name: "Volc", Status: StatusEnabled, CredentialState: CredentialConfigured, CredentialRef: "ref", Models: []Model{
		{ModelID: "seed-asr-2.0", DisplayName: "asr", IsDefault: true, Kind: KindASR},
		{ModelID: "zh_female_xiaohe_uranus_bigtts", DisplayName: "tts", Kind: KindTTS, KindDefault: true},
	}}
	speech := CatalogForKind([]Provider{p}, KindVoice)
	if len(speech) != 2 || speech[0].Model.ModelID != "zh_female_xiaohe_uranus_bigtts" {
		t.Fatalf("voice catalog = %#v", speech)
	}
	listen := CatalogForKind([]Provider{p}, KindASR)
	if len(listen) != 1 || listen[0].Model.ModelID != "seed-asr-2.0" {
		t.Fatalf("asr catalog = %#v", listen)
	}
	speak := CatalogForKind([]Provider{p}, KindTTS)
	if len(speak) != 1 || speak[0].Model.ModelID != "zh_female_xiaohe_uranus_bigtts" {
		t.Fatalf("tts catalog = %#v", speak)
	}
}

func TestOriginFingerprintTreatsVolcDocPathsAsSameOrigin(t *testing.T) {
	origin, err := OriginFingerprint(ProtocolVolcSpeech, VolcSpeechOrigin)
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{
		"wss://openspeech.bytedance.com/api/v3/plan/sauc/bigmodel_async",
		"wss://openspeech.bytedance.com/api/v3/plan/tts/bidirection",
		"https://openspeech.bytedance.com/api/v3/plan/tts/unidirectional",
	} {
		got, err := OriginFingerprint(ProtocolVolcSpeech, raw)
		if err != nil || got != origin {
			t.Fatalf("OriginFingerprint(%q)=%q,%v want %q", raw, got, err, origin)
		}
	}
}

func TestCanonicalVolcSpeechURLAcceptsOfficialFullPaths(t *testing.T) {
	cases := []string{
		"https://openspeech.bytedance.com",
		"https://openspeech.bytedance.com/api/v3/plan/tts/unidirectional",
		"wss://openspeech.bytedance.com/api/v3/plan/sauc/bigmodel_async",
		"wss://openspeech.bytedance.com/api/v3/plan/tts/bidirection",
		"https://ark.cn-beijing.volces.com/api/plan/v3",
		"",
	}
	for _, raw := range cases {
		got, err := CanonicalVolcSpeechURL(raw)
		if err != nil || got != VolcSpeechOrigin {
			t.Fatalf("CanonicalVolcSpeechURL(%q)=%q,%v", raw, got, err)
		}
	}
	if _, err := CanonicalVolcSpeechURL("https://evil.example/v1"); err == nil {
		t.Fatal("foreign host must fail")
	}
}

func TestNormalizeBaseURLIPv6AndEscapedPath(t *testing.T) {
	for raw, want := range map[string]string{
		"HTTPS://[2001:DB8::1]:443/a/../v1":   "https://[2001:db8::1]/v1",
		"https://example.test/a%2Fb/../c%20d": "https://example.test/c%20d",
	} {
		got, err := NormalizeBaseURL(raw)
		if err != nil || got != want {
			t.Errorf("NormalizeBaseURL(%q)=%q,%v want %q", raw, got, err, want)
		}
	}
}

func TestNormalizeBaseURLHTTPSAuthorityPolicy(t *testing.T) {
	tests := []struct {
		in   string
		want string // empty means error
	}{
		{"https://example.com", "https://example.com"},
		{"http://example.com", "http://example.com"}, // Now allowed
		{"https://192.168.1.1:8080", "https://192.168.1.1:8080"},
		{"http://192.168.1.1:8080", "http://192.168.1.1:8080"},
		{"https://[::1]:8443/v1", "https://[::1]:8443/v1"},
		{"ftp://example.com", ""},
		{"https://user:pass@example.com", ""},
		{"https://example.com/?q=1", ""},
		{"https://example.com/#frag", ""},
		{"https://example.com:443", "https://example.com"},
		{"http://example.com:80", "http://example.com"},
		{"https://EXAMPLE.COM", "https://example.com"},
	}
	for _, tc := range tests {
		got, err := NormalizeBaseURL(tc.in)
		if tc.want == "" {
			if err == nil {
				t.Errorf("NormalizeBaseURL(%q) accepted as %q", tc.in, got)
			}
		} else {
			if err != nil || got != tc.want {
				t.Errorf("NormalizeBaseURL(%q) = %q, %v; want %q", tc.in, got, err, tc.want)
			}
		}
	}
}
