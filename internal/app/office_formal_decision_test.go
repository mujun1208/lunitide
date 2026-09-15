package app

import (
	"context"
	"testing"

	"github.com/lunitide/lunitide/internal/bridge"
	domain "github.com/lunitide/lunitide/internal/domain/officestudio"
	content "github.com/lunitide/lunitide/internal/officestudio"
)

func TestFormalDecisionAcceptExportBundleShareDecision(t *testing.T) {
	e, store := officeEngineFixture(t)
	ctx := context.Background()
	task := officeCreatedTask(t, e, "formal-share")
	v, err := e.officeStudio.Generate(ctx, task.ID, "正式.docx", content.Spec{
		SchemaVersion: 1, Kind: content.DOCX, Title: "正式", Blocks: []content.Block{{Type: "paragraph", Text: "正文"}},
	}, "formal-share-doc")
	if err != nil {
		t.Fatal(err)
	}
	seed, err := store.AddOfficeValidation(ctx, domain.Validation{
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
	if seed.Quality == "passed" {
		t.Fatal("historical Quality must stay off passed when font-actual is unsupported")
	}

	policy := domain.DeliveryPolicy{Tier: "basic", Revision: "office-basic-v2"}
	want, err := e.officeStudio.AssessDelivery(ctx, task.ID, v.ID, policy)
	if err != nil {
		t.Fatal(err)
	}
	if !want.Allowed || want.State != "verified" || want.DecisionID == "" {
		t.Fatalf("basic formal: %+v", want)
	}

	accept := officeCall(t, e, "office.artifact.accept", "share-accept", map[string]any{
		"taskId": task.ID, "artifactId": v.ArtifactID, "versionId": v.ID, "expectedRevision": 1, "formal": true,
	})
	if !accept.OK {
		t.Fatalf("formal accept: %+v", accept.Error)
	}
	acceptDec := formalDecisionFrom(t, accept)
	export := officeCall(t, e, "office.artifact.export", "share-export", map[string]any{
		"taskId": task.ID, "versionId": v.ID, "deliveryMode": "formal",
	})
	if !export.OK {
		t.Fatalf("formal export: %+v", export.Error)
	}
	exportDec := formalDecisionFrom(t, export)
	created := officeCall(t, e, "office.bundle.create", "share-bundle", map[string]any{
		"taskId": task.ID, "title": "正式包", "versionIds": []string{v.ID},
	})
	if !created.OK {
		t.Fatalf("bundle create: %+v", created.Error)
	}
	var bundle struct{ ID string }
	if err = decodeResponsePayload(created.Payload, &bundle); err != nil || bundle.ID == "" {
		t.Fatalf("bundle: %#v %v", created.Payload, err)
	}
	bundled := officeCall(t, e, "office.bundle.export", "share-bundle-export", map[string]any{
		"taskId": task.ID, "bundleId": bundle.ID, "deliveryMode": "formal",
	})
	if !bundled.OK {
		t.Fatalf("formal bundle: %+v", bundled.Error)
	}
	bundleDec := formalDecisionFrom(t, bundled)
	for _, got := range []domain.FormalDecision{acceptDec, exportDec, bundleDec} {
		if got.DecisionID != want.DecisionID || got.Allowed != want.Allowed || got.State != want.State {
			t.Fatalf("shared decision: %+v vs %+v", got, want)
		}
	}
}

func formalDecisionFrom(t *testing.T, r bridge.Response) domain.FormalDecision {
	t.Helper()
	var out struct {
		Decision domain.FormalDecision `json:"decision"`
	}
	if err := decodeResponsePayload(r.Payload, &out); err != nil || out.Decision.DecisionID == "" {
		t.Fatalf("missing FormalDecision in payload: %+v %v", r.Payload, err)
	}
	return out.Decision
}
