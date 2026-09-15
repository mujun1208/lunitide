package agentrunapp_test

import (
	"context"
	"testing"

	"github.com/lunitide/lunitide/internal/domain/agentrun"
)

func TestModelFitAndOfficeOutcomeUI(t *testing.T) {
	scope := agentrun.VerificationScope{OwnerScope: "owner", TaskID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", GoalRevision: 1}
	specs := []agentrun.StepSpec{{
		ID: "write-report", GoalRevision: 1, Required: true,
		Checks: []agentrun.AcceptanceCheck{{ID: "report-file", Kind: "artifact", Required: true}},
	}}
	outcomes := []agentrun.StepOutcome{{
		StepID: "write-report", AttemptID: "try-1", GoalRevision: 1, State: "succeeded",
		ArtifactRefs: []string{"missing-ref"},
	}}
	got, err := agentrun.EvaluateTask(context.Background(), scope, specs, outcomes, missingArtifactReceipts{})
	if err != nil {
		t.Fatal(err)
	}
	if got.State == agentrun.TaskSucceeded {
		t.Fatalf("missing artifact must not evaluate as succeeded: %+v", got)
	}
	view := agentrun.PresentTaskOutcome(got, agentrun.StreamEnded, nil)
	if view.GreenComplete || view.Spinner {
		t.Fatalf("ended run with missing file: spinner=%v green=%v", view.Spinner, view.GreenComplete)
	}

	claimed := agentrun.TaskOutcome{
		TaskID: scope.TaskID, GoalRevision: 1, Version: 1,
		State: agentrun.TaskSucceeded, Completion: agentrun.CompletionVerified,
		ReasonCode: "bridge_ok", ArtifactRefs: []string{},
	}
	emptyList := agentrun.PresentTaskOutcome(claimed, agentrun.StreamEnded, nil)
	if emptyList.GreenComplete || emptyList.TaskComplete || emptyList.Tone == "complete" {
		t.Fatalf("empty artifactRefs list must not be complete: %+v", emptyList)
	}
	if emptyList.Spinner {
		t.Fatal("spinner must end when the run ended")
	}
}

type missingArtifactReceipts struct{}

func (missingArtifactReceipts) ResolveReceipt(_ context.Context, scope agentrun.VerificationScope, id string) (agentrun.TrustedReceipt, error) {
	return agentrun.TrustedReceipt{ID: id, TaskID: scope.TaskID, GoalRevision: scope.GoalRevision, State: "succeeded"}, nil
}

func (missingArtifactReceipts) ResolveArtifact(_ context.Context, _ agentrun.VerificationScope, _ string) (agentrun.ArtifactSnapshot, error) {
	return agentrun.ArtifactSnapshot{}, agentrun.ErrNotFound
}

func (missingArtifactReceipts) ResolveCheckEvidence(_ context.Context, _ agentrun.VerificationScope, _ string) (agentrun.CheckEvidence, error) {
	return agentrun.CheckEvidence{}, agentrun.ErrNotFound
}
