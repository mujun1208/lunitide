package officeapp

import (
	"context"
	"testing"

	domain "github.com/lunitide/lunitide/internal/domain/officestudio"
)

func TestAssessDeliveryAppliesFontRequiredFlags(t *testing.T) {
	svc, store, task := studioServiceFixture(t)
	v := generatedWord(t, svc, task, "delivery-font")
	basic, err := svc.AssessDelivery(context.Background(), task.ID, v.ID, domain.DeliveryPolicy{Tier: "basic", Revision: "v2-basic"})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range basic.MissingChecks {
		if id == "font_actual_substitution" || id == "font-actual" {
			t.Fatal("basic must not require font_actual_substitution")
		}
	}

	assured, err := svc.AssessDelivery(context.Background(), task.ID, v.ID, domain.DeliveryPolicy{Tier: "assured", Revision: "v2-assured"})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, id := range assured.MissingChecks {
		if id == "font-actual" || id == "font_actual_substitution" {
			found = true
		}
	}
	if !found {
		t.Fatal("assured must require font-actual")
	}

	task.Checkpoint = WithTaskDeliveryPolicy(task.Checkpoint, domain.DeliveryPolicy{Tier: "assured", Revision: "v2-assured"})
	if _, err = store.UpdateOfficeTask(context.Background(), task, task.Revision); err != nil {
		t.Fatal(err)
	}
	assuredVer := generatedWord(t, svc, task, "delivery-font-assured")
	reports, err := store.ListOfficeValidations(context.Background(), assuredVer.ID)
	if err != nil || len(reports) == 0 {
		t.Fatal("assured font evidence not persisted", err)
	}
	for _, check := range reports[0].Checks {
		if check.ID == "font_actual_substitution" && !check.Required {
			t.Fatal("stored assured tier must require font_actual_substitution on new checks")
		}
		if check.ID == "font_actual_substitution" && check.Status == "passed" {
			t.Fatal("stored assured must not mint substitution proof")
		}
	}
}

func TestFormalDecisionRequiredCoverage(t *testing.T) {
	svc, store, task := studioServiceFixture(t)
	policy := domain.DeliveryPolicy{Tier: "assured", Revision: "v2", RequiredCheckIDs: []string{"file-integrity", "font-actual", "page-coverage"}}
	dec, err := svc.AssessDelivery(context.Background(), task.ID, "missing-version", policy)
	if err != nil {
		t.Fatal(err)
	}
	if dec.Allowed || dec.State == "verified" {
		t.Fatalf("missing evidence must not formal: %+v", dec)
	}
	_ = store
}

func TestHistoricalRequiredDoesNotPolluteCurrentPolicy(t *testing.T) {
	svc, store, task := studioServiceFixture(t)
	ctx := context.Background()
	v := generatedWord(t, svc, task, "history-required")
	legacy, err := store.AddOfficeValidation(ctx, domain.Validation{
		VersionID: v.ID, SHA256: v.SHA256, Validator: "legacy-required",
		Checks: []domain.Check{{ID: "font_actual_substitution", Status: "unsupported", Required: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !legacy.Checks[0].Required {
		t.Fatal("legacy row must keep the incoming Required bit")
	}
	current, err := store.AddOfficeValidation(ctx, domain.Validation{
		VersionID: v.ID, SHA256: v.SHA256, Validator: "current-basic",
		Checks: []domain.Check{{ID: "font_actual_substitution", Status: "unsupported", Required: false}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if current.Checks[0].Required {
		t.Fatal("current run must not inherit historical Required")
	}
	reports, err := store.ListOfficeValidations(ctx, v.ID)
	if err != nil || len(reports) == 0 {
		t.Fatal("validations", err)
	}
	if reports[0].Checks[0].ID != "font_actual_substitution" || reports[0].Checks[0].Required {
		t.Fatalf("latest stored required leaked: %+v", reports[0].Checks)
	}

	dec, err := svc.AssessDelivery(ctx, task.ID, v.ID, domain.DeliveryPolicy{Revision: "office-basic-v2"})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range dec.MissingChecks {
		if id == "font-actual" || id == "font_actual_substitution" {
			t.Fatalf("basic must not require font-actual: %+v", dec)
		}
	}
}

func TestFormalDecisionAcceptExportBundleShareDecision(t *testing.T) {
	svc, store, task := studioServiceFixture(t)
	ctx := context.Background()
	v := generatedWord(t, svc, task, "share-decision")
	seed := seedBasicFormalChecks(t, store, v)
	if seed.Quality == "passed" {
		t.Fatal("optional unsupported font-actual must keep historical Quality off passed")
	}

	basicPolicy := domain.DeliveryPolicy{Tier: "basic", Revision: "office-basic-v2"}
	basic, err := svc.AssessDelivery(ctx, task.ID, v.ID, basicPolicy)
	if err != nil {
		t.Fatal(err)
	}
	if !basic.Allowed || basic.State != "verified" || basic.DecisionID == "" {
		t.Fatalf("basic formal: %+v", basic)
	}
	again, err := svc.AssessDelivery(ctx, task.ID, v.ID, basicPolicy)
	if err != nil {
		t.Fatal(err)
	}
	if again.DecisionID != basic.DecisionID || again.Allowed != basic.Allowed || again.State != basic.State {
		t.Fatalf("unique decision drifted: %+v vs %+v", again, basic)
	}
	if !sameMissingChecks(again.MissingChecks, basic.MissingChecks) {
		t.Fatalf("unique missing checks drifted: %+v vs %+v", again.MissingChecks, basic.MissingChecks)
	}

	assured, err := svc.AssessDelivery(ctx, task.ID, v.ID, domain.DeliveryPolicy{Tier: "assured", Revision: "office-assured-v2"})
	if err != nil {
		t.Fatal(err)
	}
	if assured.Allowed || assured.State == "verified" {
		t.Fatalf("assured must block without font-actual/page-coverage: %+v", assured)
	}
	foundBlock := false
	for _, id := range assured.MissingChecks {
		if id == "font-actual" || id == "page-coverage" {
			foundBlock = true
		}
	}
	if !foundBlock {
		t.Fatalf("assured missing checks: %+v", assured.MissingChecks)
	}

	extra, err := svc.AssessDelivery(ctx, task.ID, v.ID, domain.DeliveryPolicy{
		Tier: "basic", Revision: "office-basic-v2", RequiredCheckIDs: []string{"visual-review"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if extra.DecisionID != basic.DecisionID || extra.Allowed != basic.Allowed || extra.State != basic.State || !sameMissingChecks(extra.MissingChecks, basic.MissingChecks) {
		t.Fatalf("client RequiredCheckIDs bypassed policy: %+v vs %+v", extra, basic)
	}

	_, acceptDec, err := svc.AcceptFormal(ctx, task.ID, v.ArtifactID, v.ID, 1, basicPolicy)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if _, exportDec, err := svc.ExportFormal(ctx, task.ID, v.ID, dir, basicPolicy); err != nil {
		t.Fatal(err)
	} else if exportDec.DecisionID != basic.DecisionID || exportDec.Allowed != basic.Allowed || exportDec.State != basic.State || !sameMissingChecks(exportDec.MissingChecks, basic.MissingChecks) {
		t.Fatalf("export decision: %+v vs %+v", exportDec, basic)
	}
	bundle, err := svc.CreateBundle(ctx, task.ID, "正式包", []string{v.ID}, "share-bundle")
	if err != nil {
		t.Fatal(err)
	}
	out, err := svc.ExportBundleFormal(ctx, task.ID, bundle.ID, dir, basicPolicy)
	if err != nil || !out.Complete {
		t.Fatalf("bundle formal: %+v %v", out, err)
	}
	if acceptDec.DecisionID != basic.DecisionID || acceptDec.Allowed != basic.Allowed || acceptDec.State != basic.State || !sameMissingChecks(acceptDec.MissingChecks, basic.MissingChecks) {
		t.Fatalf("accept decision: %+v vs %+v", acceptDec, basic)
	}
	if out.Decision.DecisionID != basic.DecisionID || out.Decision.Allowed != basic.Allowed || out.Decision.State != basic.State || !sameMissingChecks(out.Decision.MissingChecks, basic.MissingChecks) {
		t.Fatalf("bundle decision: %+v vs %+v", out.Decision, basic)
	}
}

func TestFormalDecisionFailedActualRenderNotHiddenByNativeRenderAlias(t *testing.T) {
	svc, store, task := studioServiceFixture(t)
	ctx := context.Background()
	v := generatedWord(t, svc, task, "alias-render-fail")
	if _, err := store.AddOfficeValidation(ctx, domain.Validation{
		VersionID: v.ID, SHA256: v.SHA256, Validator: "alias-render-fail",
		Checks: []domain.Check{
			{ID: "file-integrity", Status: "passed"},
			{ID: "source-content", Status: "passed"},
			{ID: "locked-facts", Status: "passed"},
			{ID: "font-availability", Status: "passed"},
			{ID: "native_render", Status: "unsupported"},
			{ID: "actual-render", Status: "failed"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	dec, err := svc.AssessDelivery(ctx, task.ID, v.ID, domain.DeliveryPolicy{Revision: "office-basic-v2"})
	if err != nil {
		t.Fatal(err)
	}
	if dec.State != "blocked" || dec.Allowed {
		t.Fatalf("failed actual-render must block, not hide behind native_render: %+v", dec)
	}
}

func TestCheckEmitsFormalRequiredIDs(t *testing.T) {
	svc, store, task := studioServiceFixture(t)
	v := generatedWord(t, svc, task, "formal-ids")
	reports, err := store.ListOfficeValidations(context.Background(), v.ID)
	if err != nil || len(reports) == 0 {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, c := range reports[0].Checks {
		seen[domain.CanonicalCheckID(c.ID)] = true
	}
	for _, id := range []string{"file-integrity", "source-content", "locked-facts", "font-availability"} {
		if !seen[id] {
			t.Fatalf("Check() missing %s in %+v", id, reports[0].Checks)
		}
	}
}

func TestFormalDecisionRecomputesWhenEvidenceImproves(t *testing.T) {
	svc, store, task := studioServiceFixture(t)
	v := generatedWord(t, svc, task, "recompute")
	first, err := svc.AssessDelivery(context.Background(), task.ID, v.ID, domain.DeliveryPolicy{Revision: "office-basic-v2"})
	if err != nil || first.Allowed {
		t.Fatalf("first assess should not allow incomplete evidence: %+v %v", first, err)
	}
	if _, err = store.AddOfficeValidation(context.Background(), domain.Validation{
		VersionID: v.ID, SHA256: v.SHA256, Validator: "later-complete",
		Checks: []domain.Check{
			{ID: "file-integrity", Status: "passed"},
			{ID: "source-content", Status: "passed"},
			{ID: "locked-facts", Status: "passed"},
			{ID: "font-availability", Status: "passed"},
			{ID: "actual-render", Status: "passed"},
			{ID: "font_actual_substitution", Status: "unsupported", Required: false},
		},
	}); err != nil {
		t.Fatal(err)
	}
	second, err := svc.AssessDelivery(context.Background(), task.ID, v.ID, domain.DeliveryPolicy{Revision: "office-basic-v2"})
	if err != nil {
		t.Fatal(err)
	}
	if !second.Allowed || second.State != "verified" {
		t.Fatalf("improved evidence must recompute, not reuse first persist: first=%+v second=%+v", first, second)
	}
	if second.DecisionID == "" || second.DecisionID == first.DecisionID {
		t.Fatalf("new evidence_digest must mint a new DecisionID: %q %q", first.DecisionID, second.DecisionID)
	}
}

func TestFormalDecisionFailedRequiredSetsBlockingCodes(t *testing.T) {
	svc, store, task := studioServiceFixture(t)
	v := generatedWord(t, svc, task, "blocked-codes")
	if _, err := store.AddOfficeValidation(context.Background(), domain.Validation{
		VersionID: v.ID, SHA256: v.SHA256, Validator: "fail-render",
		Checks: []domain.Check{
			{ID: "file-integrity", Status: "passed"},
			{ID: "source-content", Status: "passed"},
			{ID: "locked-facts", Status: "passed"},
			{ID: "font-availability", Status: "passed"},
			{ID: "actual-render", Status: "failed"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	dec, err := svc.AssessDelivery(context.Background(), task.ID, v.ID, domain.DeliveryPolicy{Revision: "office-basic-v2"})
	if err != nil || dec.State != "blocked" || dec.Allowed {
		t.Fatalf("%+v %v", dec, err)
	}
	found := false
	for _, id := range dec.BlockingCodes {
		if id == "actual-render" {
			found = true
		}
	}
	if !found {
		t.Fatalf("failed required must be BlockingCodes, not only MissingChecks: %+v", dec)
	}
}

func seedBasicFormalChecks(t *testing.T, store interface {
	AddOfficeValidation(context.Context, domain.Validation) (domain.Validation, error)
}, v domain.Version) domain.Validation {
	t.Helper()
	qa, err := store.AddOfficeValidation(context.Background(), domain.Validation{
		VersionID: v.ID, SHA256: v.SHA256, Validator: "formal-decision-seed",
		Checks: []domain.Check{
			{ID: "file-integrity", Status: "passed"},
			{ID: "source-content", Status: "passed"},
			{ID: "locked-facts", Status: "passed"},
			{ID: "font-availability", Status: "passed"},
			{ID: "actual-render", Status: "passed"},
			{ID: "font_actual_substitution", Status: "unsupported", Required: false},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return qa
}

func sameMissingChecks(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	seen := map[string]int{}
	for _, id := range want {
		seen[id]++
	}
	for _, id := range got {
		seen[id]--
		if seen[id] < 0 {
			return false
		}
	}
	return true
}
