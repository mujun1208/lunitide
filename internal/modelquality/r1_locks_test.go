package modelquality

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/modelfit"
	"github.com/lunitide/lunitide/internal/officestudio"
)

func TestRuntimeEvidenceRedaction(t *testing.T) {
	secret := "sk-test-secret-r26"
	reasoning := "need the quarterly file before answering"
	raw := RuntimeEvidence{
		API: json.RawMessage(`{"error":"lease failed Bearer ` + secret + `","reasoning":"` + reasoning + `","reasoningContent":"` + reasoning + `"}`),
		Logs: []string{
			"attempt failed api_key=" + secret,
			`protocol private reasoning_content=` + reasoning,
		},
		BackupManifest: []string{"credential_ref=cred-r26-private", "key=" + secret},
		BackupCipher:   []byte("ciphertext-ok"),
		Protocol:       modelfit.ProtocolCapture{ReasoningContent: reasoning, Source: modelfit.SourceProvider, Complete: true},
	}
	got := RedactRuntimeEvidence(raw)
	blob, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(blob, []byte(secret)) || bytes.Contains(blob, []byte(reasoning)) {
		t.Fatalf("RedactRuntimeEvidence leaked secret or private reasoning: %s", blob)
	}
	for _, line := range got.Logs {
		if strings.Contains(line, secret) || strings.Contains(line, reasoning) {
			t.Fatalf("log leaked: %s", line)
		}
	}
	if got.Protocol.ReasoningContent != "" {
		t.Fatal("protocol private reasoning must be stripped from export")
	}
	if string(got.API) == string(raw.API) {
		t.Fatal("RedactRuntimeEvidence must rewrite the export API blob")
	}
	if !bytes.Equal(got.BackupCipher, []byte("ciphertext-ok")) {
		t.Fatal("encrypted backup ciphertext must remain")
	}
	if SanitizeLog("Bearer "+secret) == "Bearer "+secret {
		t.Fatal("SanitizeLog must redact secrets")
	}
	if SanitizeLog(reasoning) == "[redacted]" || !strings.Contains(SanitizeLog(reasoning), reasoning) {
		t.Fatal("ordinary reasoning is not a secret token; RedactRuntimeEvidence must strip the field")
	}
}

func TestTemplateCertificationAndBenchmarkAccounting(t *testing.T) {
	suite, err := LoadEvalSuite()
	if err != nil {
		t.Fatal(err)
	}
	frozen := NewFrozenAccounting()
	suite.RecordFirst(frozen, Outcome{CaseID: "R02", ScenarioID: "default", FixtureOnly: true, OraclePass: true})
	suite.RecordFirst(frozen, Outcome{CaseID: "R02", ScenarioID: "default", FixtureOnly: true, OraclePass: true, Verdict: &Verdict{Pass: true, LiveSuccessIncrement: true}})
	if frozen.Acc.LiveSuccess != 0 || frozen.Acc.FixturePass != 1 {
		t.Fatalf("fixture pass / repeat must not mint live or overwrite first: %+v", frozen.Acc)
	}

	f03 := Outcome{CaseID: "F03", ScenarioID: "missing-glyph", OraclePass: true, EvidenceKind: "live"}
	suite.RecordFirst(frozen, f03)
	if frozen.Acc.LiveSuccess != 0 {
		t.Fatalf("missing-glyph must not increment live or delivery success: %+v", frozen.Acc)
	}

	model := NewFrozenAccounting()
	office := NewFrozenAccounting()
	suite.RecordFirst(model, Outcome{CaseID: "C01", ScenarioID: "default", OraclePass: true, Verdict: &Verdict{Pass: true, LiveSuccessIncrement: true}})
	suite.RecordFirst(office, Outcome{CaseID: "D01", ScenarioID: "default", OraclePass: true, Verdict: &Verdict{Pass: true, LiveSuccessIncrement: true}})
	if model.Acc.LiveSuccess == 0 || office.Acc.LiveSuccess == 0 || model.Acc.LiveSuccess != 1 || office.Acc.LiveSuccess != 1 {
		t.Fatalf("model vs office must score separately: model=%+v office=%+v", model.Acc, office.Acc)
	}

	for _, v := range officestudio.EngineeredVariants() {
		if v.DesignerReviewed {
			t.Fatalf("designerReviewed must stay 0: %+v", v)
		}
	}
	tpl, ok := officestudio.LoadTemplate("ops-clear")
	if !ok || strings.TrimSpace(tpl.License) == "" || strings.TrimSpace(tpl.Version) == "" {
		t.Fatalf("template source not tracked: %+v ok=%v", tpl, ok)
	}
	if officestudio.FontEmbedPolicy(officestudio.DefaultBrand().Fonts) == "" {
		t.Fatal("font source policy missing")
	}
	if strings.Contains(officestudio.TemplateVariantCoverage(), "designerReviewed=1") {
		t.Fatal("must not mark designerReviewed=1")
	}
}
