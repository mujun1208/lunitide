package secret

import "testing"

func TestRefValidateUsesProviderHTTPSAuthorityPolicy(t *testing.T) {
	base := Ref{CredentialRef: "credential", ProviderID: "provider", Protocol: "openai_compatible"}
	for _, origin := range []string{"http://example.com", "https://例子.com", "https://exa_mple.com", "https://example.com.", "https://example.com:bad"} {
		ref := base
		ref.Origin = origin
		if _, err := ref.Validate(); err == nil {
			t.Errorf("Validate accepted %q", origin)
		}
	}
	ref := base
	ref.Origin = "HTTPS://[2001:DB8::1]:443/v1"
	got, err := ref.Validate()
	if err != nil || got.Origin != "https://[2001:db8::1]" {
		t.Fatalf("valid bracketed IPv6 ref = %#v, %v", got, err)
	}
}
