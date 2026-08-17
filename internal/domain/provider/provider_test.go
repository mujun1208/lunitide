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
