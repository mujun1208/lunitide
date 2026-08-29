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
