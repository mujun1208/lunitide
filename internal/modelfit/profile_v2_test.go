package modelfit

import (
	"encoding/json"
	"os"
	"testing"
)

func TestTargetDigestCanonicalAndRejectsSecrets(t *testing.T) {
	a := TargetIdentity{Protocol: "openai_compatible", EndpointURL: "https://API.example.com:443/v1#frag", EndpointPurpose: "standard", ModelRequested: "glm-5", Family: "glm"}
	b := TargetIdentity{Protocol: "openai_compatible", EndpointURL: "https://api.example.com/v1", EndpointPurpose: "standard", ModelRequested: "glm-5", Family: "glm"}
	da, err := TargetDigest(a)
	if err != nil {
		t.Fatal(err)
	}
	db, err := TargetDigest(b)
	if err != nil || da != db {
		t.Fatalf("canonical mismatch %s %s %v", da, db, err)
	}
	if _, err = TargetDigest(TargetIdentity{EndpointURL: "https://user:pass@api.example.com/v1"}); err == nil {
		t.Fatal("userinfo must be rejected")
	}
	if _, err = TargetDigest(TargetIdentity{EndpointURL: "https://api.example.com/v1?api_key=secret"}); err == nil {
		t.Fatal("key query must be rejected")
	}
	coding := TargetIdentity{Protocol: "openai_compatible", EndpointURL: "https://api.example.com/v1", EndpointPurpose: "coding", ModelRequested: "glm-5", Family: "glm"}
	dc, err := TargetDigest(coding)
	if err != nil || dc == da {
		t.Fatal("coding purpose must not share standard digest")
	}
}

func TestModelProfileBindingAlias(t *testing.T) {
	raw, err := os.ReadFile("testdata/profiles-v2.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Profiles []ModelProfile `json:"profiles"`
	}
	if json.Unmarshal(raw, &doc) != nil || len(doc.Profiles) < 3 {
		t.Fatal(doc)
	}
	glm := FindProfile(doc.Profiles, "glm", "standard")
	coding := FindProfile(doc.Profiles, "glm", "coding")
	if glm.ProfileID == "" || coding.ProfileID == "" || glm.ProfileID == coding.ProfileID {
		t.Fatal("glm standard and coding must be distinct declared profiles")
	}
	if glm.Modes["quick"].ClearThinking == nil {
		t.Fatal("glm clearThinking false must be present, not omitted")
	}
	d1, _ := ProfileDigest(glm)
	glm2 := glm
	// field order must not matter — marshal via canonical encoder
	d2, _ := ProfileDigest(glm2)
	if d1 != d2 {
		t.Fatal("canonical profile digest unstable")
	}
}
