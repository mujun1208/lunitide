package modelfit

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"
)

func TestCompileParametersMatrix(t *testing.T) {
	glm := mustLoadProfile(t, "glm", "standard")
	eff, err := CompileParameters(glm, ModelIntent{Mode: "quick"})
	if err != nil {
		t.Fatal(err)
	}
	if eff.ClearThinking == nil || *eff.ClearThinking != false {
		t.Fatal("glm explicit false clearThinking must survive")
	}
	body, err := EncodePrepared(glm, eff)
	if err != nil || !bytes.Contains(body, []byte(`"clear_thinking":false`)) && !bytes.Contains(body, []byte(`"clearThinking":false`)) {
		t.Fatalf("omitempty dropped clearThinking: %s", body)
	}
	if _, err = CompileParameters(glm, ModelIntent{Mode: "nope"}); err == nil {
		t.Fatal("unknown mode must error")
	}
}

func mustLoadProfile(t *testing.T, family, purpose string) ModelProfile {
	t.Helper()
	raw, err := os.ReadFile("testdata/profiles-v2.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Profiles []ModelProfile `json:"profiles"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	p := FindProfile(doc.Profiles, family, purpose)
	if p.ProfileID == "" {
		t.Fatalf("missing profile family=%s purpose=%s", family, purpose)
	}
	return p
}
