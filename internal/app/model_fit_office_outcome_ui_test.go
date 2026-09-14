package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/agentrun"
	domain "github.com/lunitide/lunitide/internal/domain/officestudio"
	"github.com/lunitide/lunitide/internal/modelfit"
	content "github.com/lunitide/lunitide/internal/officestudio"
)

const outcomeTaskID = "01ARZ3NDEKTSV4RRFFQ69G5FAV"

func TestModelFitAndOfficeOutcomeUI(t *testing.T) {
	t.Run("missing_required_artifact_is_not_succeeded", func(t *testing.T) {
		scope := agentrun.VerificationScope{OwnerScope: "owner", TaskID: outcomeTaskID, GoalRevision: 1}
		specs := []agentrun.StepSpec{{
			ID: "deliver-pptx", GoalRevision: 1, Required: true,
			Checks: []agentrun.AcceptanceCheck{{ID: "pptx-file", Kind: "artifact", Required: true}},
		}}
		outcomes := []agentrun.StepOutcome{{
			StepID: "deliver-pptx", AttemptID: "a1", GoalRevision: 1, State: "succeeded",
			ArtifactRefs: []string{"art-empty"},
		}}
		got, err := agentrun.EvaluateTask(context.Background(), scope, specs, outcomes, outcomeReceipts{
			artifacts: map[string]agentrun.ArtifactSnapshot{
				"art-empty": {ID: "art-empty", Path: "", SHA256: ""},
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		if got.State == agentrun.TaskSucceeded {
			t.Fatalf("empty artifact path must not be succeeded: %+v", got)
		}
		view := agentrun.PresentTaskOutcome(got, agentrun.StreamEnded, nil)
		if view.GreenComplete {
			t.Fatal("missing file must not render green complete")
		}
		if view.Spinner {
			t.Fatal("spinner must end when the run ended")
		}
		if view.Tone != "partial" {
			t.Fatalf("tone=%q, want partial", view.Tone)
		}
	})

	t.Run("bridge_success_with_empty_path_is_not_complete", func(t *testing.T) {
		claimed := agentrun.TaskOutcome{
			TaskID: outcomeTaskID, GoalRevision: 1, Version: 1,
			State: agentrun.TaskSucceeded, Completion: agentrun.CompletionVerified,
			ReasonCode: "bridge_ok", ArtifactRefs: []string{""},
		}
		view := agentrun.PresentTaskOutcome(claimed, agentrun.StreamEnded, []string{""})
		if view.GreenComplete || view.TaskComplete {
			t.Fatalf("bridge success with empty path must not show complete: %+v", view)
		}
		if view.Spinner {
			t.Fatal("spinner must stop on stream end even when the artifact is missing")
		}
		if view.Tone == "complete" {
			t.Fatal("empty path must not use complete tone")
		}
	})

	t.Run("bridge_success_with_empty_artifact_list_is_not_complete", func(t *testing.T) {
		claimed := agentrun.TaskOutcome{
			TaskID: outcomeTaskID, GoalRevision: 1, Version: 1,
			State: agentrun.TaskSucceeded, Completion: agentrun.CompletionVerified,
			ReasonCode: "bridge_ok", ArtifactRefs: []string{},
		}
		view := agentrun.PresentTaskOutcome(claimed, agentrun.StreamEnded, nil)
		if view.GreenComplete || view.TaskComplete {
			t.Fatalf("succeeded+verified with empty artifactRefs must not show complete: %+v", view)
		}
		if view.Spinner {
			t.Fatal("spinner must stop on stream end even when artifactRefs is empty")
		}
		if view.Tone != "partial" {
			t.Fatalf("tone=%q, want partial", view.Tone)
		}
	})

	t.Run("spinner_stops_on_failed_stream", func(t *testing.T) {
		view := agentrun.PresentTaskOutcome(agentrun.TaskOutcome{
			TaskID: outcomeTaskID, GoalRevision: 1, Version: 1,
			State: agentrun.TaskFailed, Completion: agentrun.CompletionUnverified,
			ReasonCode: "stream_failed",
		}, agentrun.StreamFailed, nil)
		if view.Spinner {
			t.Fatal("spinner must stop when the run fails")
		}
		if view.GreenComplete {
			t.Fatal("failed run must not be green complete")
		}
	})

	t.Run("probe_fixture_is_not_live_qualified", func(t *testing.T) {
		for _, phase := range []modelfit.FitPhase{modelfit.FitInProgress, modelfit.FitFailed, modelfit.FitExpired, modelfit.FitActivated} {
			view := modelfit.PresentModelFit(phase, "fixture", "qualified")
			if view.LiveQualified {
				t.Fatalf("phase %s fixture must not show live qualified: %+v", phase, view)
			}
			if strings.Contains(strings.ToLower(view.Label), "qualified") || strings.Contains(view.Label, "高端商用") {
				t.Fatalf("phase %s fixture label leaked live qualified: %q", phase, view.Label)
			}
		}
	})

	t.Run("missing_renderer_is_needs_review_not_verified", func(t *testing.T) {
		e, _ := officeEngineFixture(t)
		task := officeCreatedTask(t, e, "ui-missing-renderer")
		v, err := e.officeStudio.Generate(context.Background(), task.ID, "缺渲染.docx", content.Spec{
			SchemaVersion: 1, Kind: content.DOCX, Title: "缺渲染", Blocks: []content.Block{{Type: "paragraph", Text: "正文"}},
		}, "ui-missing-renderer-doc")
		if err != nil {
			t.Fatal(err)
		}
		dec, err := e.officeStudio.AssessDelivery(context.Background(), task.ID, v.ID, domain.DeliveryPolicy{Revision: "office-basic-v2"})
		if err != nil {
			t.Fatal(err)
		}
		if dec.Allowed || dec.State == "verified" {
			t.Fatalf("missing renderer must not verify: %+v", dec)
		}
		if dec.State != "needs_review" {
			t.Fatalf("state=%q, want needs_review", dec.State)
		}
		found := false
		for _, id := range dec.MissingChecks {
			if id == "actual-render" {
				found = true
			}
		}
		if !found {
			t.Fatalf("missing checks must include actual-render: %+v", dec.MissingChecks)
		}
		view := PresentFormalDecision(dec)
		if view.Verified || view.State == "verified" {
			t.Fatalf("UI must not mark missing renderer verified: %+v", view)
		}
		if !strings.Contains(strings.Join(view.MissingChecks, ","), "actual-render") {
			t.Fatalf("UI missing checks: %+v", view.MissingChecks)
		}
	})

	t.Run("stale_evidence_not_reused_across_sha_or_policy", func(t *testing.T) {
		e, _ := officeEngineFixture(t)
		task := officeCreatedTask(t, e, "ui-stale-evidence")
		v1, err := e.officeStudio.Generate(context.Background(), task.ID, "旧源.docx", content.Spec{
			SchemaVersion: 1, Kind: content.DOCX, Title: "旧源", Blocks: []content.Block{{Type: "paragraph", Text: "一"}},
		}, "ui-stale-v1")
		if err != nil {
			t.Fatal(err)
		}
		v2, err := e.officeStudio.Generate(context.Background(), task.ID, "新源.docx", content.Spec{
			SchemaVersion: 1, Kind: content.DOCX, Title: "新源", Blocks: []content.Block{{Type: "paragraph", Text: "二"}},
		}, "ui-stale-v2")
		if err != nil {
			t.Fatal(err)
		}
		basic := domain.DeliveryPolicy{Revision: "office-basic-v2"}
		d1, err := e.officeStudio.AssessDelivery(context.Background(), task.ID, v1.ID, basic)
		if err != nil {
			t.Fatal(err)
		}
		d2, err := e.officeStudio.AssessDelivery(context.Background(), task.ID, v2.ID, basic)
		if err != nil {
			t.Fatal(err)
		}
		if v1.SHA256 == v2.SHA256 {
			t.Fatal("fixture must change source SHA")
		}
		if d1.DecisionID == "" || d1.DecisionID == d2.DecisionID {
			t.Fatalf("decision reused across source SHA: %q %q", d1.DecisionID, d2.DecisionID)
		}
		if d1.SourceSHA256 == d2.SourceSHA256 {
			t.Fatalf("source SHA leaked across versions: %q", d1.SourceSHA256)
		}
		assured, err := e.officeStudio.AssessDelivery(context.Background(), task.ID, v1.ID, domain.DeliveryPolicy{Revision: "office-assured-v2"})
		if err != nil {
			t.Fatal(err)
		}
		if assured.DecisionID == d1.DecisionID || assured.PolicyRevision == d1.PolicyRevision {
			t.Fatalf("decision reused across policy revision: %+v vs %+v", assured, d1)
		}
	})

	t.Run("formal_export_fails_copy_still_works", func(t *testing.T) {
		e, _ := officeEngineFixture(t)
		task := officeCreatedTask(t, e, "ui-formal-copy")
		v, err := e.officeStudio.Generate(context.Background(), task.ID, "草稿.docx", content.Spec{
			SchemaVersion: 1, Kind: content.DOCX, Title: "草稿", Blocks: []content.Block{{Type: "paragraph", Text: "正文"}},
		}, "ui-formal-copy-doc")
		if err != nil {
			t.Fatal(err)
		}
		assessed, err := e.officeStudio.AssessDelivery(context.Background(), task.ID, v.ID, domain.DeliveryPolicy{Revision: "office-basic-v2"})
		if err != nil {
			t.Fatal(err)
		}
		formal := officeCall(t, e, "office.artifact.export", "ui-formal-export", map[string]any{
			"taskId": task.ID, "versionId": v.ID, "deliveryMode": "formal",
		})
		if formal.OK || formal.Error == nil || formal.Error.Code != "OFFICE_DRAFT_REQUIRED" {
			t.Fatalf("formal export must fail: %+v", formal)
		}
		failDec := formalDecisionFromError(t, formal)
		if failDec.DecisionID != assessed.DecisionID || failDec.Allowed != assessed.Allowed || failDec.State != assessed.State {
			t.Fatalf("UI/export must share FormalDecision: %+v vs %+v", failDec, assessed)
		}
		if len(failDec.MissingChecks) == 0 {
			t.Fatal("formal failure must list missing check IDs")
		}
		for _, id := range failDec.MissingChecks {
			if !strings.Contains(formal.Error.Message, id) {
				t.Fatalf("error must list missing check %s: %q", id, formal.Error.Message)
			}
		}
		copyExport := officeCall(t, e, "office.artifact.export", "ui-copy-export", map[string]any{
			"taskId": task.ID, "versionId": v.ID, "deliveryMode": "copy",
		})
		if !copyExport.OK {
			t.Fatalf("copy/draft export must stay available: %+v", copyExport.Error)
		}
	})
}

func formalDecisionFromError(t *testing.T, r bridge.Response) domain.FormalDecision {
	t.Helper()
	if r.Error == nil || r.Error.Details == nil {
		t.Fatalf("missing FormalDecision details: %+v", r)
	}
	raw, err := json.Marshal(r.Error.Details["decision"])
	if err != nil {
		t.Fatal(err)
	}
	var dec domain.FormalDecision
	if err = json.Unmarshal(raw, &dec); err != nil || dec.DecisionID == "" {
		t.Fatalf("FormalDecision details: %s %v", raw, err)
	}
	return dec
}

type outcomeReceipts struct {
	artifacts map[string]agentrun.ArtifactSnapshot
}

func (s outcomeReceipts) ResolveReceipt(_ context.Context, scope agentrun.VerificationScope, id string) (agentrun.TrustedReceipt, error) {
	return agentrun.TrustedReceipt{ID: id, TaskID: scope.TaskID, GoalRevision: scope.GoalRevision, State: "succeeded"}, nil
}

func (s outcomeReceipts) ResolveArtifact(_ context.Context, _ agentrun.VerificationScope, id string) (agentrun.ArtifactSnapshot, error) {
	art, ok := s.artifacts[id]
	if !ok {
		return agentrun.ArtifactSnapshot{}, agentrun.ErrNotFound
	}
	return art, nil
}

func (s outcomeReceipts) ResolveCheckEvidence(_ context.Context, _ agentrun.VerificationScope, _ string) (agentrun.CheckEvidence, error) {
	return agentrun.CheckEvidence{}, agentrun.ErrNotFound
}
