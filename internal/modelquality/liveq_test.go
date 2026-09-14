package modelquality

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/officestudio"
)

func TestLiveQSelectsAuthorizedTargets(t *testing.T) {
	rows := []InstalledProvider{
		{
			ID: AuthorizedDeepSeekProviderID, Name: "DeepSeek", Protocol: "openai_compatible",
			BaseURL: "https://api.deepseek.com/v1", CredentialRef: "cred-ds-must-not-leak",
			Models: []InstalledModel{
				{ModelID: "deepseek-v4-pro", IsDefault: true, Kind: "llm"},
				{ModelID: "deepseek-v4-flash", Kind: "llm"},
				{ModelID: "deepseek-flash", Kind: "llm"},
			},
		},
		{
			ID: AuthorizedGLMProviderID, Name: "HSYQ", Protocol: "openai_compatible",
			BaseURL: "https://ark.cn-beijing.volces.com/api/plan/v3", CredentialRef: "cred-glm-must-not-leak",
			Models: []InstalledModel{{ModelID: "glm-5.3", IsDefault: true, Kind: "llm"}},
		},
		{
			ID: "01LMSTUDIOLOCAL00000000001", Name: "LM Studio", Protocol: "openai_compatible",
			BaseURL: "http://127.0.0.1:1234/v1",
			Models:  []InstalledModel{{ModelID: "local-qwen", IsDefault: true, Kind: "llm"}},
		},
		{
			ID: "01VOLCSPEECH00000000000001", Name: "volc speech", Protocol: "volc_speech",
			BaseURL: "https://openspeech.bytedance.com",
			Models:  []InstalledModel{{ModelID: "seed-tts", Kind: "speech"}},
		},
		{
			ID: "01OCRIMAGEVIDEO00000000001", Name: "media", Protocol: "openai_compatible",
			BaseURL: "https://ark.cn-beijing.volces.com/api/v3",
			Models: []InstalledModel{
				{ModelID: "doubao-ocr", Kind: "ocr"},
				{ModelID: "seedream", Kind: "image"},
				{ModelID: "seedance", Kind: "video"},
			},
		},
	}

	sel := SelectAuthorizedTargets(rows)
	if len(sel.Targets) == 0 {
		t.Fatal("authorized DeepSeek and GLM targets missing")
	}

	seen := map[string]AuthorizedTarget{}
	for _, tgt := range sel.Targets {
		if tgt.ProviderID != AuthorizedDeepSeekProviderID && tgt.ProviderID != AuthorizedGLMProviderID {
			t.Fatalf("unauthorized provider selected: %s %s", tgt.ProviderID, tgt.ProviderName)
		}
		if strings.Contains(strings.ToLower(tgt.ProviderName), "lm studio") || strings.Contains(tgt.OriginHost, "127.0.0.1") {
			t.Fatalf("LM Studio / localhost leaked through: %+v", tgt)
		}
		key := tgt.ProviderID + "/" + tgt.ModelID + "/" + tgt.Role
		seen[key] = tgt
	}

	dsPro := seen[AuthorizedDeepSeekProviderID+"/deepseek-v4-pro/suite"]
	if dsPro.ModelID == "" {
		t.Fatal("DeepSeek suite target must be installed default deepseek-v4-pro")
	}
	if dsPro.OriginHost != "api.deepseek.com" {
		t.Fatalf("DeepSeek origin host=%q", dsPro.OriginHost)
	}
	flash := seen[AuthorizedDeepSeekProviderID+"/deepseek-flash/probe"]
	if flash.ModelID == "" {
		t.Fatal("DeepSeek probe must use verbatim model id deepseek-flash")
	}
	glm := seen[AuthorizedGLMProviderID+"/glm-5.3/suite"]
	if glm.ModelID == "" {
		t.Fatal("GLM suite target must be glm-5.3")
	}
	if glm.OriginHost != "ark.cn-beijing.volces.com" {
		t.Fatalf("GLM origin host=%q", glm.OriginHost)
	}

	if _, ok := seen[AuthorizedDeepSeekProviderID+"/deepseek-v4-flash/suite"]; ok {
		t.Fatal("must not relabel installed deepseek-v4-pro as profile deepseek-v4-flash")
	}

	if len(sel.ProfileMismatch) == 0 {
		t.Fatal("must record deepseek-v4-flash profile vs installed deepseek-v4-pro")
	}
	mismatch := sel.ProfileMismatch[0]
	if mismatch.ProfileModelID != "deepseek-v4-flash" || mismatch.InstalledModelID != "deepseek-v4-pro" {
		t.Fatalf("mismatch=%+v", mismatch)
	}

	for _, rej := range sel.Rejected {
		if rej.ProviderID == AuthorizedDeepSeekProviderID || rej.ProviderID == AuthorizedGLMProviderID {
			t.Fatalf("authorized provider marked rejected: %+v", rej)
		}
	}
	if !rejectedName(sel, "LM Studio") {
		t.Fatal("LM Studio must be rejected")
	}
}

func TestLiveQDoesNotWriteSecrets(t *testing.T) {
	ev := LiveEvidence{
		SchemaVersion: 1,
		LiveQualified: true,
		LayersQ:       "live_qualified",
		Probes: []ProbeRecord{{
			ProviderName: "DeepSeek",
			OriginHost:   "api.deepseek.com",
			ModelID:      "deepseek-v4-pro",
			Status:       "failed",
			HTTPClass:    "4xx",
			ErrorCode:    "HTTP_401 leaked sk-live-secret-value api_key=AKIA and credential_ref=cred-ds-must-not-leak",
		}},
		Notes: []string{"Authorization: Bearer sk-another-secret", "api_key present", "raw ref cred-ds-must-not-leak"},
		Runs: []TargetRun{{
			ProviderName: "HSYQ",
			OriginHost:   "ark.cn-beijing.volces.com",
			ModelID:      "glm-5.3",
			Cases: []CaseRecord{{
				CaseID:    "D01",
				ErrorCode: "upstream sk-glm-secret",
			}},
		}},
	}

	raw, err := MarshalLiveEvidence(ev)
	if err != nil {
		t.Fatal(err)
	}
	low := strings.ToLower(string(raw))
	for _, banned := range []string{"sk-", "api_key", "credential_ref", "cred-ds-must-not-leak", "sk-live-secret", "sk-another-secret", "sk-glm-secret"} {
		if strings.Contains(low, strings.ToLower(banned)) {
			t.Fatalf("evidence leaked %q: %s", banned, raw)
		}
	}
	if bytes.Contains(raw, []byte("Bearer ")) {
		t.Fatalf("evidence leaked bearer material: %s", raw)
	}

	var parsed LiveEvidence
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed.LiveQualified {
		t.Fatal("marshal must force LiveQualified=false")
	}
	if parsed.LayersQ == "live_qualified" {
		t.Fatal("marshal must not persist live_qualified as layers.Q")
	}
}

func rejectedName(sel TargetSelection, name string) bool {
	for _, rej := range sel.Rejected {
		if strings.EqualFold(rej.ProviderName, name) {
			return true
		}
	}
	return false
}

func TestLiveQObserveC01IncrementsFromDateKeysOnly(t *testing.T) {
	suite, err := LoadEvalSuite()
	if err != nil {
		t.Fatal(err)
	}
	c := suite.MustCase("C01")
	d1 := IndependentNextDay(mustParseTime(t, "2026-01-31T12:00:00Z")).UTC().Format(time.RFC3339)
	d2 := IndependentNextDay(mustParseTime(t, "2024-02-28T12:00:00Z")).UTC().Format(time.RFC3339)
	modelText := fmt.Sprintf(`{"2026-01-31T12:00:00Z":"%s","2024-02-28T12:00:00Z":"%s"}`, d1, d2)

	obs, verdict, _ := ObserveLiveCase(suite, c, modelText)
	if obs.Values["testFilesUnchanged"] != "" {
		t.Fatalf("live C01 must not stuff testFilesUnchanged: %+v", obs.Values)
	}
	if !verdict.LiveSuccessIncrement {
		t.Fatal("correct date keys must increment live success without testFilesUnchanged")
	}
}

func TestLiveQObserveC01WrongDatesNoIncrement(t *testing.T) {
	suite, err := LoadEvalSuite()
	if err != nil {
		t.Fatal(err)
	}
	c := suite.MustCase("C01")
	modelText := `{"2026-01-31T12:00:00Z":"2026-02-01T00:00:00Z","2024-02-28T12:00:00Z":"2024-02-29T00:00:00Z"}`

	_, verdict, _ := ObserveLiveCase(suite, c, modelText)
	if verdict.LiveSuccessIncrement {
		t.Fatal("wrong dates must not increment live success")
	}
}

func TestLiveQObservationFromArtifactF03NoSpecBodyGlyphProof(t *testing.T) {
	want := "人民币￥100；达成率％；长度μm；温度℃；生僻字𠮷。"
	spec := officestudio.Spec{Kind: officestudio.PDF, Body: want}
	obs := observationFromArtifact(EvalCase{CaseID: "F03"}, spec, []byte("%PDF-invalid"))
	if obs.Values["allGlyphsVisible"] == "true" || obs.Values["pdfTextExact"] == "true" {
		t.Fatalf("glyph flags must not come from spec.Body when inspect text empty: %+v", obs.Values)
	}
}

func TestLiveQParseSpecJSONRejectsNonJSON(t *testing.T) {
	if _, err := parseSpecJSON("not json at all"); err == nil {
		t.Fatal("parseSpecJSON must reject non-JSON")
	}
}

func TestLiveQOutputTokenCap(t *testing.T) {
	suite, err := LoadEvalSuite()
	if err != nil {
		t.Fatal(err)
	}
	base := suite.MustCase("D01")
	cap, raised := LiveOutputTokenCap(base)
	if cap != closeoutLiveOutputTokens || raised {
		t.Fatalf("default cap=%d raised=%v want %d false", cap, raised, closeoutLiveOutputTokens)
	}
	raisedCase := suite.MustCase("D02")
	cap, raised = LiveOutputTokenCap(raisedCase)
	if cap != raisedLiveOutputTokens || !raised {
		t.Fatalf("raised cap=%d raised=%v want %d true", cap, raised, raisedLiveOutputTokens)
	}
}

func TestLiveQLayersQTokenNeverLiveQualified(t *testing.T) {
	cases := []struct {
		name   string
		probes []ProbeRecord
		runs   []TargetRun
	}{
		{"probed_failed", nil, nil},
		{"partial_probes", []ProbeRecord{{Role: "suite", Status: "ok"}}, nil},
		{"partial_runs", []ProbeRecord{{Role: "suite", Status: "ok"}}, []TargetRun{{Cases: make([]CaseRecord, len(FixedCaseIDs)-1)}}},
		{"ran", []ProbeRecord{{Role: "suite", Status: "ok"}}, []TargetRun{{Cases: make([]CaseRecord, len(FixedCaseIDs))}}},
	}
	for _, tc := range cases {
		got := LayersQToken(tc.probes, tc.runs)
		if got == "live_qualified" {
			t.Fatalf("%s returned live_qualified", tc.name)
		}
	}
}

func TestLiveQWriteBaselineQOnlyTouchesQ(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "baseline.json")
	original := `{"layers":{"E":"pending","Q":"ran","D":"not_run"}}`
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteBaselineQ(path, "partial"); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	out := string(raw)
	if !strings.Contains(out, `"Q":"partial"`) {
		t.Fatalf("Q not updated: %s", out)
	}
	if !strings.Contains(out, `"E":"pending"`) || !strings.Contains(out, `"D":"not_run"`) {
		t.Fatalf("E/D must be untouched: %s", out)
	}
}

func TestLiveQNonIncrementCases(t *testing.T) {
	suite, err := LoadEvalSuite()
	if err != nil {
		t.Fatal(err)
	}
	fixture := suite.MustCase("R02")
	_, v, _ := ObserveLiveCase(suite, fixture, `{}`)
	if v.LiveSuccessIncrement {
		t.Fatal("fixture-only R02 must not increment live success")
	}
	host := suite.MustCase("R01")
	_, v, _ = ObserveLiveCase(suite, host, `{}`)
	if v.LiveSuccessIncrement {
		t.Fatal("host-runtime R01 must not increment live success")
	}
	f03 := suite.MustCase("F03")
	obs := Observation{
		ScenarioID: "missing-glyph",
		Values: map[string]string{
			"fontFixture": "known-missing-glyph", "missingGlyphDetected": "true",
			"formalBlocked": "true", "silentLoss": "false",
		},
	}
	v = suite.Evaluate("F03", "missing-glyph", obs)
	v = withLiveIncrement(f03, "missing-glyph", obs, v)
	if v.LiveSuccessIncrement {
		t.Fatal("missing-glyph F03 must not increment live success")
	}
}

func mustParseTime(t *testing.T, s string) time.Time {
	t.Helper()
	ts, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatal(err)
	}
	return ts
}
