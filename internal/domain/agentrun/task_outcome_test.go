package agentrun

import (
	"context"
	"testing"
)

func TestTaskOutcomeRejectsModelAuthoredL0(t *testing.T) {
	ctx := context.Background()
	scope := VerificationScope{OwnerScope: "owner", TaskID: "task-l0", GoalRevision: 2}
	specs := []StepSpec{{
		ID: "write-report", GoalRevision: 2, Required: true,
		Checks: []AcceptanceCheck{{ID: "l0-receipt", Kind: "l0", Required: true, ExpectedDigest: "digest-good"}},
	}}

	claimed, err := EvaluateTask(ctx, scope, specs, []StepOutcome{{
		StepID: "write-report", AttemptID: "a1", GoalRevision: 2, State: TaskSucceeded,
		Claim: `{"success":true,"l0":"passed","test":"passed"}`,
	}}, modelAuthoredReceipts{})
	if err != nil {
		t.Fatal(err)
	}
	if claimed.State == TaskSucceeded {
		t.Fatalf("model body must not self-author L0: %+v", claimed)
	}

	wrongScope, err := EvaluateTask(ctx, scope, specs, []StepOutcome{{
		StepID: "write-report", AttemptID: "a2", GoalRevision: 2, State: TaskSucceeded,
		ReceiptRefs: []string{"receipt-wrong-scope"},
	}}, modelAuthoredReceipts{})
	if err != nil {
		t.Fatal(err)
	}
	if wrongScope.State == TaskSucceeded {
		t.Fatalf("wrong-scope receipt must not verify: %+v", wrongScope)
	}

	stale, err := EvaluateTask(ctx, scope, specs, []StepOutcome{{
		StepID: "write-report", AttemptID: "a3", GoalRevision: 2, State: TaskSucceeded,
		ReceiptRefs: []string{"receipt-stale"},
	}}, modelAuthoredReceipts{})
	if err != nil {
		t.Fatal(err)
	}
	if stale.State == TaskSucceeded {
		t.Fatalf("stale digest must not verify: %+v", stale)
	}

	modelEvidence, err := EvaluateTask(ctx, scope, specs, []StepOutcome{{
		StepID: "write-report", AttemptID: "a4", GoalRevision: 2, State: TaskSucceeded,
		ReceiptRefs: []string{"receipt-ok"}, EvidenceRefs: []string{"ev-model"},
	}}, modelAuthoredReceipts{})
	if err != nil {
		t.Fatal(err)
	}
	if modelEvidence.State == TaskSucceeded {
		t.Fatalf("model-authored check evidence must not verify: %+v", modelEvidence)
	}
}

func TestTaskOutcomeMissingRequiredStep(t *testing.T) {
	ctx := context.Background()
	scope := VerificationScope{OwnerScope: "owner", TaskID: "task-miss", GoalRevision: 1}
	specs := []StepSpec{
		{ID: "file-a", GoalRevision: 1, Required: true, Checks: []AcceptanceCheck{{ID: "a", Kind: "artifact", Required: true}}},
		{ID: "file-b", GoalRevision: 1, Required: true, Checks: []AcceptanceCheck{{ID: "b", Kind: "artifact", Required: true}}},
	}
	got, err := EvaluateTask(ctx, scope, specs, []StepOutcome{{
		StepID: "file-a", AttemptID: "a1", GoalRevision: 1, State: TaskSucceeded,
		ReceiptRefs: []string{"receipt-a"}, ArtifactRefs: []string{"art-a"},
	}}, twoFileReceipts{})
	if err != nil {
		t.Fatal(err)
	}
	if got.State == TaskSucceeded || len(got.RemainingSteps) != 1 || got.RemainingSteps[0] != "file-b" {
		t.Fatalf("missing required file-b must stay incomplete: %+v", got)
	}
}

func TestOutcomeGoalRevisionOrder(t *testing.T) {
	ctx := context.Background()
	specs := []StepSpec{{
		ID: "write", GoalRevision: 2, Required: true,
		Checks: []AcceptanceCheck{{ID: "file", Kind: "artifact", Required: true}},
	}}
	old := StepOutcome{
		StepID: "write", AttemptID: "old-1", GoalRevision: 1, State: TaskSucceeded,
		ReceiptRefs: []string{"receipt-old"}, ArtifactRefs: []string{"art-old"},
	}
	scope := VerificationScope{OwnerScope: "owner", TaskID: "task-rev", GoalRevision: 2}
	got, err := EvaluateTask(ctx, scope, specs, []StepOutcome{old}, twoFileReceipts{})
	if err != nil {
		t.Fatal(err)
	}
	if got.State == TaskSucceeded || got.GoalRevision != 2 {
		t.Fatalf("old terminal must not satisfy new goal: %+v", got)
	}

	led := ApplyCompletedEvent(OutcomeLedger{}, CompletedEvent{
		TaskID: "task-rev", GoalRevision: 2, OutcomeVersion: 1,
		Outcome: TaskOutcome{TaskID: "task-rev", GoalRevision: 2, Version: 1, State: TaskIncomplete},
	})
	led = ApplyCompletedEvent(led, CompletedEvent{
		TaskID: "task-rev", GoalRevision: 1, OutcomeVersion: 9,
		Outcome: TaskOutcome{TaskID: "task-rev", GoalRevision: 1, Version: 9, State: TaskSucceeded, Completion: CompletionVerified, ArtifactRefs: []string{"art-old"}},
	})
	if led.Current.GoalRevision != 2 || led.Current.State == TaskSucceeded {
		t.Fatalf("old completed event overwrote new goal: %+v", led.Current)
	}
	if len(led.History) != 1 || led.History[0].GoalRevision != 1 {
		t.Fatalf("old evidence must stay in history: %+v", led.History)
	}

	again := ApplyCompletedEvent(led, CompletedEvent{
		TaskID: "task-rev", GoalRevision: 2, OutcomeVersion: 1,
		Outcome: TaskOutcome{TaskID: "task-rev", GoalRevision: 2, Version: 1, State: TaskSucceeded},
	})
	if again.Current.Version != led.Current.Version || again.Current.State != led.Current.State {
		t.Fatalf("duplicate CompletedEvent must be idempotent: %+v vs %+v", again.Current, led.Current)
	}

	view := PresentTaskOutcome(got, StreamEnded, nil)
	if view.ReplyEnded && view.TaskComplete {
		t.Fatal("stream end must stay independent of task complete")
	}
	if !view.ReplyEnded || view.GreenComplete {
		t.Fatalf("ended stream with unfinished goal: %+v", view)
	}
}

type modelAuthoredReceipts struct{}

func (modelAuthoredReceipts) ResolveReceipt(_ context.Context, scope VerificationScope, id string) (TrustedReceipt, error) {
	switch id {
	case "receipt-wrong-scope":
		return TrustedReceipt{ID: id, OwnerScope: "other", TaskID: scope.TaskID, GoalRevision: scope.GoalRevision, ArgumentsDigest: "digest-good", State: "succeeded"}, nil
	case "receipt-stale":
		return TrustedReceipt{ID: id, OwnerScope: scope.OwnerScope, TaskID: scope.TaskID, GoalRevision: scope.GoalRevision, ArgumentsDigest: "digest-old", State: "succeeded"}, nil
	case "receipt-ok":
		return TrustedReceipt{ID: id, OwnerScope: scope.OwnerScope, TaskID: scope.TaskID, GoalRevision: scope.GoalRevision, ArgumentsDigest: "digest-good", State: "succeeded"}, nil
	default:
		return TrustedReceipt{}, ErrNotFound
	}
}

func (modelAuthoredReceipts) ResolveArtifact(context.Context, VerificationScope, string) (ArtifactSnapshot, error) {
	return ArtifactSnapshot{}, ErrNotFound
}

func (modelAuthoredReceipts) ResolveCheckEvidence(_ context.Context, scope VerificationScope, id string) (CheckEvidence, error) {
	if id == "ev-model" {
		return CheckEvidence{ID: id, CheckID: "l0-receipt", ValidatorID: "model", OwnerScope: scope.OwnerScope, TaskID: scope.TaskID, GoalRevision: scope.GoalRevision, Status: "passed"}, nil
	}
	return CheckEvidence{}, ErrNotFound
}

type twoFileReceipts struct{}

func (twoFileReceipts) ResolveReceipt(_ context.Context, scope VerificationScope, id string) (TrustedReceipt, error) {
	return TrustedReceipt{ID: id, OwnerScope: scope.OwnerScope, TaskID: scope.TaskID, GoalRevision: scope.GoalRevision, State: "succeeded"}, nil
}

func (twoFileReceipts) ResolveArtifact(_ context.Context, scope VerificationScope, id string) (ArtifactSnapshot, error) {
	if id == "art-a" {
		return ArtifactSnapshot{ID: id, Path: "/tmp/a.docx", SHA256: "aa", Bytes: 12}, nil
	}
	if id == "art-old" {
		return ArtifactSnapshot{ID: id, Path: "/tmp/old.docx", SHA256: "old", Bytes: 8}, nil
	}
	return ArtifactSnapshot{}, ErrNotFound
}

func (twoFileReceipts) ResolveCheckEvidence(context.Context, VerificationScope, string) (CheckEvidence, error) {
	return CheckEvidence{}, ErrNotFound
}
