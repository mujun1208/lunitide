package llmadapter

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"

	"github.com/lunitide/lunitide/internal/modelfit"
)

func TestCompileParametersMatrix(t *testing.T) {
	glm := loadDeclaredProfile(t, "glm", "standard")
	eff, err := modelfit.CompileParameters(glm, modelfit.ModelIntent{Mode: "quick"})
	if err != nil {
		t.Fatal(err)
	}
	if eff.ClearThinking == nil || *eff.ClearThinking != false {
		t.Fatal("glm explicit false clearThinking must survive")
	}
	req := Request{Model: "glm-5.3", Mode: "quick", Messages: []Message{{Role: RoleUser, Content: "hi"}}}
	prepared, err := PrepareChat(req, modelfit.TargetIdentity{Family: "glm", ModelRequested: "glm-5.3"}, modelfit.ReplayDecision{}, glm, false)
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Effective.ClearThinking == nil || *prepared.Effective.ClearThinking != false {
		t.Fatal("prepared effective must keep explicit false")
	}
	if !bytes.Contains(prepared.Body, []byte(`"clear_thinking":false`)) {
		t.Fatalf("frozen body omitted vendor clear_thinking: %s", prepared.Body)
	}
	if bytes.Contains(prepared.Body, []byte(`"clearThinking"`)) {
		t.Fatalf("do not invent a third/camel vendor name: %s", prepared.Body)
	}
	wantDigest := sha256Hex(prepared.Body)
	if prepared.Digest != wantDigest {
		t.Fatalf("digest=%s want sha256(body)=%s", prepared.Digest, wantDigest)
	}
	hooks := &fakeAttemptHooks{}
	id, err := hooks.BeforeSend(context.Background(), prepared)
	if err != nil || id == "" {
		t.Fatalf("before send: id=%s err=%v", id, err)
	}
	if err = hooks.MarkDispatched(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	if hooks.httpSends != 0 {
		t.Fatal("hooks must not send HTTP")
	}
	if _, err = PrepareChat(Request{Mode: "nope"}, modelfit.TargetIdentity{}, modelfit.ReplayDecision{}, glm, false); err == nil {
		t.Fatal("unknown mode must error")
	}
}

func loadDeclaredProfile(t *testing.T, family, purpose string) modelfit.ModelProfile {
	t.Helper()
	raw, err := os.ReadFile("../modelfit/testdata/profiles-v2.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Profiles []modelfit.ModelProfile `json:"profiles"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	p := modelfit.FindProfile(doc.Profiles, family, purpose)
	if p.ProfileID == "" {
		t.Fatalf("missing profile family=%s purpose=%s", family, purpose)
	}
	return p
}

func sha256Hex(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

type fakeAttemptHooks struct {
	httpSends int
}

func (f *fakeAttemptHooks) BeforeSend(context.Context, PreparedRequest) (string, error) {
	return "attempt-test-1", nil
}

func (f *fakeAttemptHooks) MarkDispatched(context.Context, string) error {
	return nil
}

func (f *fakeAttemptHooks) AfterAttempt(context.Context, string, AttemptResult) error {
	return nil
}
