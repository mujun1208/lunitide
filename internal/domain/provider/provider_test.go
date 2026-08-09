package provider

import "testing"

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
	for _, raw := range []string{
		"http://example.com", "https://例子.com", "https://exa_mple.com",
		"https://example.com.", "https://example.com:bad/v1", "https://example.com:/v1",
	} {
		if got, err := NormalizeBaseURL(raw); err == nil {
			t.Errorf("NormalizeBaseURL(%q) accepted as %q", raw, got)
		}
	}
	if got, err := NormalizeBaseURL("https://[2001:DB8::1]:443/v1"); err != nil || got != "https://[2001:db8::1]/v1" {
		t.Fatalf("valid bracketed IPv6 HTTPS = %q, %v", got, err)
	}
}
