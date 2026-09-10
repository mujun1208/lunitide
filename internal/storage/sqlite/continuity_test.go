package sqlite

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/modelfit"
	"github.com/oklog/ulid/v2"
)

func TestToolOperationCrashWindows(t *testing.T) {
	ctx := context.Background()
	store, err := OpenTemplated(ctx, filepath.Join(t.TempDir(), "continuity.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	now := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	intent := modelfit.ToolOperation{
		ID:              ulid.Make().String(),
		OwnerScope:      "session",
		SessionID:       "",
		TurnID:          ulid.Make().String(),
		ToolName:        "workspace.write",
		InputDigest:     strings.Repeat("ab", 32),
		EffectClass:     modelfit.EffectLocalReversible,
		State:           modelfit.OpPending,
		ExpectedVersion: 1,
		Attempt:         1,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if err := store.PutToolOperationIntent(ctx, intent); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetToolOperation(ctx, intent.OwnerScope, intent.ID)
	if err != nil || got.State != modelfit.OpPending {
		t.Fatalf("intent lost: %+v %v", got, err)
	}

	if err := store.MarkToolOperationRunning(ctx, intent.OwnerScope, intent.ID, 1, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := store.RecoverInterruptedToolOperations(ctx); err != nil {
		t.Fatal(err)
	}
	got, err = store.GetToolOperation(ctx, intent.OwnerScope, intent.ID)
	if err != nil || got.State != modelfit.OpUnknown {
		t.Fatalf("running crash must become unknown: %+v %v", got, err)
	}
	if Resume := modelfit.ResumeDecision(got.State, got.EffectClass); Resume != modelfit.ResumeVerifyUnknown {
		t.Fatalf("unknown must not auto-retry: %q", Resume)
	}

	if err := store.FinishToolOperation(ctx, intent.OwnerScope, intent.ID, got.ExpectedVersion, modelfit.OpSucceeded, "", now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	done, err := store.GetToolOperation(ctx, intent.OwnerScope, intent.ID)
	if err != nil || done.State != modelfit.OpSucceeded {
		t.Fatalf("receipt lost: %+v %v", done, err)
	}
	if err := store.FinishToolOperation(ctx, intent.OwnerScope, intent.ID, 1, modelfit.OpFailed, "stale", now.Add(3*time.Second)); err == nil {
		t.Fatal("stale version overwrote a committed receipt")
	}

	cancelled, err := done.RequestCancel()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RequestToolOperationCancel(ctx, cancelled.OwnerScope, cancelled.ID, cancelled.ExpectedVersion, now.Add(4*time.Second)); err != nil {
		t.Fatal(err)
	}
	after, err := store.GetToolOperation(ctx, intent.OwnerScope, intent.ID)
	if err != nil || after.State != modelfit.OpSucceeded || after.CancellationRequestedAt == "" {
		t.Fatalf("cancel must not erase succeeded work: %+v %v", after, err)
	}
}

func TestToolOperationRemoteCrashWindows(t *testing.T) {
	ctx := context.Background()
	store, err := OpenTemplated(ctx, filepath.Join(t.TempDir(), "remote-crash.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 9, 10, 4, 0, 0, 0, time.UTC)

	t.Run("intent-before-send", func(t *testing.T) {
		op := remoteMediaOp(now, "intent")
		if err := store.PutToolOperationIntent(ctx, op); err != nil {
			t.Fatal(err)
		}
		if err := store.RecoverInterruptedToolOperations(ctx); err != nil {
			t.Fatal(err)
		}
		got, err := store.GetToolOperation(ctx, op.OwnerScope, op.ID)
		if err != nil || got.State != modelfit.OpUnknown || got.ExternalID != "" {
			t.Fatalf("intent crash stays unknown without inventing a job: %+v %v", got, err)
		}
		if action := modelfit.ResumeDecision(got.State, got.EffectClass); action != modelfit.ResumeVerifyUnknown {
			t.Fatalf("must not auto-resubmit: %q", action)
		}
	})

	t.Run("external-id-before-receipt", func(t *testing.T) {
		op := remoteMediaOp(now.Add(time.Minute), "id-before-receipt")
		if err := store.PutToolOperationIntent(ctx, op); err != nil {
			t.Fatal(err)
		}
		if err := store.MarkToolOperationRunning(ctx, op.OwnerScope, op.ID, 1, now.Add(61*time.Second)); err != nil {
			t.Fatal(err)
		}
		if err := store.BindToolOperationTrack(ctx, op.OwnerScope, op.ID, 2, "supplier-job-3", "submitted", nil, now.Add(62*time.Second)); err != nil {
			t.Fatal(err)
		}
		running, err := store.GetToolOperation(ctx, op.OwnerScope, op.ID)
		if err != nil || running.ExternalID != "supplier-job-3" || running.State != modelfit.OpRunning {
			t.Fatalf("id must land before receipt: %+v %v", running, err)
		}
		if action := modelfit.ResumeDecision(running.State, running.EffectClass); action != modelfit.ResumeQueryExisting {
			t.Fatalf("running remote job is query-existing: %q", action)
		}
		if err := store.RecoverInterruptedToolOperations(ctx); err != nil {
			t.Fatal(err)
		}
		got, err := store.GetToolOperation(ctx, op.OwnerScope, op.ID)
		if err != nil || got.State != modelfit.OpUnknown || got.ExternalID != "supplier-job-3" {
			t.Fatalf("recover must keep the supplier id: %+v %v", got, err)
		}
		if err := store.FinishToolOperation(ctx, op.OwnerScope, op.ID, 1, modelfit.OpSucceeded, "", now.Add(3*time.Minute)); err == nil {
			t.Fatal("stale receipt must not overwrite a bound job id")
		}
	})

	t.Run("artifact-before-receipt", func(t *testing.T) {
		op := remoteMediaOp(now.Add(2*time.Minute), "artifact-before-receipt")
		if err := store.PutToolOperationIntent(ctx, op); err != nil {
			t.Fatal(err)
		}
		if err := store.MarkToolOperationRunning(ctx, op.OwnerScope, op.ID, 1, now.Add(121*time.Second)); err != nil {
			t.Fatal(err)
		}
		if err := store.BindToolOperationTrack(ctx, op.OwnerScope, op.ID, 2, "supplier-job-4", "submitted", []string{"generated.png"}, now.Add(122*time.Second)); err != nil {
			t.Fatal(err)
		}
		if err := store.RecoverInterruptedToolOperations(ctx); err != nil {
			t.Fatal(err)
		}
		got, err := store.GetToolOperation(ctx, op.OwnerScope, op.ID)
		if err != nil || got.State != modelfit.OpUnknown || got.ExternalID != "supplier-job-4" || len(got.ArtifactRefs) != 1 || got.ArtifactRefs[0] != "generated.png" {
			t.Fatalf("artifact on disk plus missing receipt must stay queryable: %+v %v", got, err)
		}
		if action := modelfit.ResumeDecision(got.State, got.EffectClass); action != modelfit.ResumeVerifyUnknown {
			t.Fatalf("unknown artifact window is verify, not regenerate: %q", action)
		}
	})
}

func remoteMediaOp(now time.Time, name string) modelfit.ToolOperation {
	return modelfit.ToolOperation{
		ID:              ulid.Make().String(),
		OwnerScope:      "session",
		TurnID:          ulid.Make().String(),
		ToolName:        "image.generate",
		TargetRef:       name,
		InputDigest:     strings.Repeat("ab", 32),
		EffectClass:     modelfit.EffectRemoteTrackable,
		State:           modelfit.OpPending,
		ExpectedVersion: 1,
		Attempt:         1,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
}

func TestModelCallAttemptIntentSurvivesMissingUsage(t *testing.T) {
	ctx := context.Background()
	store, err := OpenTemplated(ctx, filepath.Join(t.TempDir(), "calls.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	rec := CallAttemptRecord{
		ID:         ulid.Make().String(),
		OwnerScope: "session",
		TaskID:     "task-1",
		TurnID:     "turn-1",
		CallID:     "call-1",
		AttemptID:  "attempt-1",
		Purpose:    "chat",
		Status:     string(modelfit.CallIntent),
		Integrity:  string(modelfit.UsageUnknown),
		CostStatus: "unknown",
		StartedAt:  now,
	}
	if err := store.PutCallAttemptIntent(ctx, rec); err != nil {
		t.Fatal(err)
	}
	if err := store.FinishCallAttempt(ctx, rec.OwnerScope, rec.CallID, rec.AttemptID, CallAttemptReceipt{
		Status:     string(modelfit.CallSucceeded),
		Integrity:  string(modelfit.UsageUnknown),
		CostStatus: "unknown",
		EndedAt:    now.Add(time.Second),
	}); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetCallAttempt(ctx, rec.OwnerScope, rec.CallID, rec.AttemptID)
	if err != nil || got.Status != string(modelfit.CallSucceeded) || got.Integrity != string(modelfit.UsageUnknown) {
		t.Fatalf("empty usage became zero-reported: %+v %v", got, err)
	}
	sum, err := store.SumCallAttemptsByTask(ctx, rec.OwnerScope, rec.TaskID)
	if err != nil || sum.Calls != 1 || sum.InputTokens != 0 || sum.Integrity != string(modelfit.UsageUnknown) {
		t.Fatalf("task rollup: %+v %v", sum, err)
	}
}

func TestCallAttemptPersistsEfficiencySnapshot(t *testing.T) {
	ctx := context.Background()
	store, err := OpenTemplated(ctx, filepath.Join(t.TempDir(), "eff.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	rec := CallAttemptRecord{
		ID: ulid.Make().String(), OwnerScope: "session", TaskID: "task-1", CallID: "call-1", AttemptID: "attempt-1",
		Purpose: "chat", Status: string(modelfit.CallIntent), Integrity: string(modelfit.UsageUnknown),
		CostStatus: "unknown", StartedAt: now,
		PolicyVersion: "token-efficiency-v1", BytesBefore: 120, BytesAfter: 90,
	}
	if err := store.PutCallAttemptIntent(ctx, rec); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetCallAttempt(ctx, rec.OwnerScope, rec.CallID, rec.AttemptID)
	if err != nil || got.PolicyVersion != "token-efficiency-v1" || got.BytesBefore != 120 || got.BytesAfter != 90 {
		t.Fatalf("efficiency snapshot lost: %+v %v", got, err)
	}
	listed, err := store.ListCallAttempts(ctx, rec.OwnerScope, rec.TaskID, 10)
	if err != nil || len(listed) != 1 || listed[0].BytesBefore != 120 || listed[0].BytesAfter != 90 {
		t.Fatalf("list lost snapshot: %+v %v", listed, err)
	}
	blank := rec
	blank.ID, blank.CallID, blank.AttemptID = ulid.Make().String(), "call-2", "attempt-2"
	blank.PolicyVersion, blank.BytesBefore, blank.BytesAfter = "", 0, 0
	if err := store.PutCallAttemptIntent(ctx, blank); err != nil {
		t.Fatal(err)
	}
	old, err := store.GetCallAttempt(ctx, blank.OwnerScope, blank.CallID, blank.AttemptID)
	if err != nil || old.PolicyVersion != "" || old.BytesBefore != 0 || old.BytesAfter != 0 {
		t.Fatalf("legacy row must stay unknown, not invented zeros-as-savings: %+v %v", old, err)
	}
}

func TestFinishToolOperationDoesNotReviveCancelled(t *testing.T) {
	ctx := context.Background()
	store, err := OpenTemplated(ctx, filepath.Join(t.TempDir(), "finish-cancel.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 9, 10, 8, 0, 0, 0, time.UTC)
	op := modelfit.ToolOperation{
		ID: ulid.Make().String(), OwnerScope: "session", ToolName: "desktop.type",
		InputDigest: strings.Repeat("ab", 32), EffectClass: modelfit.EffectDesktopInteractive,
		State: modelfit.OpCancelled, ExpectedVersion: 2, Attempt: 1, CreatedAt: now, UpdatedAt: now,
	}
	if err := store.PutToolOperationIntent(ctx, op); err != nil {
		t.Fatal(err)
	}
	if err := store.FinishToolOperation(ctx, op.OwnerScope, op.ID, 2, modelfit.OpSucceeded, "", now.Add(time.Second)); err == nil {
		t.Fatal("late success must not revive a cancelled operation")
	}
	got, err := store.GetToolOperation(ctx, op.OwnerScope, op.ID)
	if err != nil || got.State != modelfit.OpCancelled {
		t.Fatalf("cancelled must stay cancelled: %+v %v", got, err)
	}
	if action := modelfit.ResumeDecision(got.State, got.EffectClass); action != modelfit.ResumeKeepStopped {
		t.Fatalf("cancelled must not revive: %q", action)
	}
}

func TestFinishCallAttemptDoesNotZeroCommittedReceipt(t *testing.T) {
	ctx := context.Background()
	store, err := OpenTemplated(ctx, filepath.Join(t.TempDir(), "finish-zero.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 9, 10, 8, 10, 0, 0, time.UTC)
	rec := CallAttemptRecord{
		ID: ulid.Make().String(), OwnerScope: "session", TaskID: "task-1",
		CallID: "call-1", AttemptID: "attempt-1", Purpose: "chat",
		Status: string(modelfit.CallIntent), Integrity: string(modelfit.UsageUnknown),
		CostStatus: "unknown", StartedAt: now,
	}
	if err := store.PutCallAttemptIntent(ctx, rec); err != nil {
		t.Fatal(err)
	}
	if err := store.FinishCallAttempt(ctx, rec.OwnerScope, rec.CallID, rec.AttemptID, CallAttemptReceipt{
		Status: string(modelfit.CallSucceeded), Integrity: string(modelfit.UsageReported),
		InputTokens: 9, OutputTokens: 2, CostStatus: "unknown", EndedAt: now.Add(time.Second),
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.FinishCallAttempt(ctx, rec.OwnerScope, rec.CallID, rec.AttemptID, CallAttemptReceipt{
		Status: string(modelfit.CallSucceeded), Integrity: string(modelfit.UsageUnknown),
		InputTokens: 0, OutputTokens: 0, CostStatus: "unknown", EndedAt: now.Add(2 * time.Second),
	}); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetCallAttempt(ctx, rec.OwnerScope, rec.CallID, rec.AttemptID)
	if err != nil || got.Status != string(modelfit.CallSucceeded) || got.InputTokens != 9 || got.OutputTokens != 2 || got.Integrity != string(modelfit.UsageReported) {
		t.Fatalf("committed receipt must not be zeroed: %+v %v", got, err)
	}
	sum, err := store.SumCallAttemptsByTask(ctx, rec.OwnerScope, rec.TaskID)
	if err != nil || sum.InputTokens != 9 || sum.OutputTokens != 2 {
		t.Fatalf("rollup must keep committed tokens: %+v %v", sum, err)
	}
}

func TestRecoverInterruptedToolOperationsLeavesCancelled(t *testing.T) {
	ctx := context.Background()
	store, err := OpenTemplated(ctx, filepath.Join(t.TempDir(), "cancel-recover.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 9, 10, 7, 0, 0, 0, time.UTC)
	op := modelfit.ToolOperation{
		ID: ulid.Make().String(), OwnerScope: "session", ToolName: "desktop.type",
		InputDigest: strings.Repeat("ab", 32), EffectClass: modelfit.EffectDesktopInteractive,
		State: modelfit.OpCancelled, ExpectedVersion: 2, Attempt: 1, CreatedAt: now, UpdatedAt: now,
	}
	if err := store.PutToolOperationIntent(ctx, op); err != nil {
		t.Fatal(err)
	}
	if err := store.RecoverInterruptedToolOperations(ctx); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetToolOperation(ctx, op.OwnerScope, op.ID)
	if err != nil || got.State != modelfit.OpCancelled {
		t.Fatalf("cancelled work must stay cancelled across recover: %+v %v", got, err)
	}
	if action := modelfit.ResumeDecision(got.State, got.EffectClass); action != modelfit.ResumeKeepStopped {
		t.Fatalf("cancelled must not revive: %q", action)
	}
}

func TestRecoverInterruptedCallAttempts(t *testing.T) {
	ctx := context.Background()
	store, err := OpenTemplated(ctx, filepath.Join(t.TempDir(), "calls-recover.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 9, 10, 6, 0, 0, 0, time.UTC)

	intent := CallAttemptRecord{
		ID: ulid.Make().String(), OwnerScope: "session", TaskID: "task-1",
		CallID: "call-intent", AttemptID: "attempt-1", Purpose: "chat",
		Status: string(modelfit.CallIntent), Integrity: string(modelfit.UsageUnknown),
		CostStatus: "unknown", StartedAt: now,
	}
	sent := CallAttemptRecord{
		ID: ulid.Make().String(), OwnerScope: "session", TaskID: "task-1",
		CallID: "call-sent", AttemptID: "attempt-2", Purpose: "chat",
		Status: string(modelfit.CallSent), Integrity: string(modelfit.UsageUnknown),
		CostStatus: "unknown", StartedAt: now.Add(time.Second),
	}
	done := CallAttemptRecord{
		ID: ulid.Make().String(), OwnerScope: "session", TaskID: "task-1",
		CallID: "call-done", AttemptID: "attempt-3", Purpose: "chat",
		Status: string(modelfit.CallSucceeded), Integrity: string(modelfit.UsageReported),
		CostStatus: "unknown", InputTokens: 9, OutputTokens: 2,
		StartedAt: now.Add(2 * time.Second), EndedAt: now.Add(3 * time.Second),
	}
	for _, rec := range []CallAttemptRecord{intent, sent, done} {
		if err := store.PutCallAttemptIntent(ctx, rec); err != nil {
			t.Fatal(err)
		}
	}

	if err := store.RecoverInterruptedCallAttempts(ctx); err != nil {
		t.Fatal(err)
	}
	gotIntent, err := store.GetCallAttempt(ctx, intent.OwnerScope, intent.CallID, intent.AttemptID)
	if err != nil || gotIntent.Status != string(modelfit.CallUnknown) || gotIntent.InputTokens != 0 || gotIntent.OutputTokens != 0 {
		t.Fatalf("intent crash must become unknown with no invented tokens: %+v %v", gotIntent, err)
	}
	if gotIntent.EndedAt.IsZero() || gotIntent.Integrity != string(modelfit.UsageUnknown) {
		t.Fatalf("recovered intent must keep unknown integrity and close the window: %+v", gotIntent)
	}
	gotSent, err := store.GetCallAttempt(ctx, sent.OwnerScope, sent.CallID, sent.AttemptID)
	if err != nil || gotSent.Status != string(modelfit.CallUnknown) || gotSent.InputTokens != 0 {
		t.Fatalf("sent-before-receipt must become unknown, not resend: %+v %v", gotSent, err)
	}
	gotDone, err := store.GetCallAttempt(ctx, done.OwnerScope, done.CallID, done.AttemptID)
	if err != nil || gotDone.Status != string(modelfit.CallSucceeded) || gotDone.InputTokens != 9 || gotDone.OutputTokens != 2 {
		t.Fatalf("committed receipt must stay: %+v %v", gotDone, err)
	}

	if err := store.RecoverInterruptedCallAttempts(ctx); err != nil {
		t.Fatal(err)
	}
	again, err := store.GetCallAttempt(ctx, intent.OwnerScope, intent.CallID, intent.AttemptID)
	if err != nil || again.Status != string(modelfit.CallUnknown) || again.InputTokens != 0 {
		t.Fatalf("second recover must not invent success: %+v %v", again, err)
	}
	sum, err := store.SumCallAttemptsByTask(ctx, "session", "task-1")
	if err != nil || sum.Calls != 3 || sum.InputTokens != 9 || sum.OutputTokens != 2 || sum.Integrity != string(modelfit.UsageUnknown) {
		t.Fatalf("rollup must keep known tokens and unknown integrity: %+v %v", sum, err)
	}
	if err := store.FinishCallAttempt(ctx, sent.OwnerScope, sent.CallID, sent.AttemptID, CallAttemptReceipt{
		Status: string(modelfit.CallSucceeded), Integrity: string(modelfit.UsageReported),
		InputTokens: 4, OutputTokens: 1, CostStatus: "unknown", EndedAt: now.Add(4 * time.Second),
	}); err != nil {
		t.Fatal(err)
	}
	late, err := store.GetCallAttempt(ctx, sent.OwnerScope, sent.CallID, sent.AttemptID)
	if err != nil || late.Status != string(modelfit.CallSucceeded) || late.InputTokens != 4 || late.OutputTokens != 1 {
		t.Fatalf("late receipt after recover must still land: %+v %v", late, err)
	}
}

func TestFinishToolOperationDoesNotReviveAfterCancelRequested(t *testing.T) {
	ctx := context.Background()
	store, err := OpenTemplated(ctx, filepath.Join(t.TempDir(), "finish-cancel-unknown.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 9, 10, 9, 0, 0, 0, time.UTC)
	op := modelfit.ToolOperation{
		ID: ulid.Make().String(), OwnerScope: "session", ToolName: "image.generate",
		InputDigest: strings.Repeat("ab", 32), EffectClass: modelfit.EffectRemoteTrackable,
		State: modelfit.OpUnknown, ExpectedVersion: 3, Attempt: 1, CreatedAt: now, UpdatedAt: now,
	}
	if err := store.PutToolOperationIntent(ctx, op); err != nil {
		t.Fatal(err)
	}
	if err := store.RequestToolOperationCancel(ctx, op.OwnerScope, op.ID, 3, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetToolOperation(ctx, op.OwnerScope, op.ID)
	if err != nil || got.State != modelfit.OpUnknown || got.CancellationRequestedAt == "" {
		t.Fatalf("cancel must keep unknown outcome: %+v %v", got, err)
	}
	if err := store.FinishToolOperation(ctx, op.OwnerScope, op.ID, got.ExpectedVersion, modelfit.OpSucceeded, "", now.Add(2*time.Second)); err == nil {
		t.Fatal("late success must not revive a cancel-requested unknown operation")
	}
	after, err := store.GetToolOperation(ctx, op.OwnerScope, op.ID)
	if err != nil || after.State != modelfit.OpUnknown || after.CancellationRequestedAt == "" {
		t.Fatalf("cancel-requested unknown must stay unknown: %+v %v", after, err)
	}
	if action := modelfit.ResumeDecisionFor(after); action != modelfit.ResumeKeepStopped {
		t.Fatalf("must not verify-and-continue after cancel: %q", action)
	}
}

func TestBindToolOperationTrackDoesNotReviveCancelled(t *testing.T) {
	ctx := context.Background()
	store, err := OpenTemplated(ctx, filepath.Join(t.TempDir(), "bind-cancel.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 9, 10, 8, 20, 0, 0, time.UTC)
	op := modelfit.ToolOperation{
		ID: ulid.Make().String(), OwnerScope: "session", ToolName: "image.generate",
		InputDigest: strings.Repeat("ab", 32), EffectClass: modelfit.EffectRemoteTrackable,
		State: modelfit.OpCancelled, ExpectedVersion: 2, Attempt: 1, CreatedAt: now, UpdatedAt: now,
	}
	if err := store.PutToolOperationIntent(ctx, op); err != nil {
		t.Fatal(err)
	}
	if err := store.BindToolOperationTrack(ctx, op.OwnerScope, op.ID, 2, "late-supplier-job", "submitted", []string{"out.png"}, now.Add(time.Second)); err == nil {
		t.Fatal("late bind must not attach a supplier id to a cancelled operation")
	}
	got, err := store.GetToolOperation(ctx, op.OwnerScope, op.ID)
	if err != nil || got.State != modelfit.OpCancelled || got.ExternalID != "" || len(got.ArtifactRefs) != 0 {
		t.Fatalf("cancelled must stay stopped without a new track: %+v %v", got, err)
	}
	if action := modelfit.ResumeDecision(got.State, got.EffectClass); action != modelfit.ResumeKeepStopped {
		t.Fatalf("cancelled must not become query-existing: %q", action)
	}
}

func TestContinuityListsNewestFirst(t *testing.T) {
	ctx := context.Background()
	store, err := OpenTemplated(ctx, filepath.Join(t.TempDir(), "list.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	older := modelfit.ToolOperation{
		ID: ulid.Make().String(), OwnerScope: "sess-a", ToolName: "workspace.read",
		InputDigest: strings.Repeat("ab", 32), EffectClass: modelfit.EffectReadOnly,
		State: modelfit.OpSucceeded, ExpectedVersion: 1, Attempt: 1, CreatedAt: now, UpdatedAt: now,
	}
	newer := older
	newer.ID = ulid.Make().String()
	newer.ToolName = "files.apply"
	newer.UpdatedAt = now.Add(time.Minute)
	if err := store.PutToolOperationIntent(ctx, older); err != nil {
		t.Fatal(err)
	}
	if err := store.PutToolOperationIntent(ctx, newer); err != nil {
		t.Fatal(err)
	}
	ops, err := store.ListToolOperations(ctx, "sess-a", 10)
	if err != nil || len(ops) != 2 || ops[0].ID != newer.ID {
		t.Fatalf("ops newest first: %+v %v", ops, err)
	}

	first := CallAttemptRecord{
		ID: ulid.Make().String(), OwnerScope: "sess-a", TaskID: "task-1", CallID: "c1", AttemptID: "a1",
		Purpose: "chat", Status: string(modelfit.CallSucceeded), Integrity: string(modelfit.UsageReported),
		InputTokens: 10, OutputTokens: 4, CostStatus: "unknown", StartedAt: now,
	}
	second := first
	second.ID, second.CallID, second.AttemptID = ulid.Make().String(), "c2", "a2"
	second.StartedAt = now.Add(time.Minute)
	second.InputTokens, second.OutputTokens = 20, 6
	if err := store.PutCallAttemptIntent(ctx, first); err != nil {
		t.Fatal(err)
	}
	if err := store.PutCallAttemptIntent(ctx, second); err != nil {
		t.Fatal(err)
	}
	calls, err := store.ListCallAttempts(ctx, "sess-a", "", 10)
	if err != nil || len(calls) != 2 || calls[0].CallID != "c2" {
		t.Fatalf("calls newest first: %+v %v", calls, err)
	}
	sum, err := store.SumCallAttemptsByOwner(ctx, "sess-a", "")
	if err != nil || sum.Calls != 2 || sum.InputTokens != 30 || sum.OutputTokens != 10 {
		t.Fatalf("owner rollup: %+v %v", sum, err)
	}
}

func TestProtocolMessageGroupPersistsCompletePairsOnly(t *testing.T) {
	ctx := context.Background()
	store, err := OpenTemplated(ctx, filepath.Join(t.TempDir(), "groups.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	sessionID := ulid.Make().String()
	turnID := ulid.Make().String()
	complete := modelfit.MessageGroup{
		ID:       ulid.Make().String(),
		Sequence: 0,
		Assistant: modelfit.ProtocolMessage{
			Role: "assistant",
			ToolCalls: []modelfit.ProtocolToolCall{
				{ID: "c1", Name: "workspace.read", Arguments: []byte(`{"path":"a.md"}`)},
			},
		},
		Tools:    []modelfit.ProtocolMessage{{Role: "tool", ToolCallID: "c1", Content: "ok"}},
		Complete: true,
	}
	incomplete := modelfit.MessageGroup{
		ID:       ulid.Make().String(),
		Sequence: 1,
		Assistant: modelfit.ProtocolMessage{
			Role:      "assistant",
			ToolCalls: []modelfit.ProtocolToolCall{{ID: "c2", Name: "workspace.write", Arguments: []byte(`{"path":`)}},
		},
	}
	if err := store.PutProtocolMessageGroup(ctx, "sess", sessionID, turnID, complete); err != nil {
		t.Fatal(err)
	}
	if err := store.PutProtocolMessageGroup(ctx, "sess", sessionID, turnID, incomplete); err != nil {
		t.Fatal(err)
	}
	got, err := store.ListCompleteProtocolMessageGroups(ctx, "sess", sessionID, turnID)
	if err != nil || len(got) != 1 || got[0].ID != complete.ID || !got[0].Complete || got[0].Tools[0].Content != "ok" {
		t.Fatalf("complete pairing lost: %+v %v", got, err)
	}
	if !modelfit.MessageGroupComplete(got[0]) {
		t.Fatal("stored complete group failed validate")
	}

	plain := []byte("need file")
	blob, digest, err := modelfit.SealProtocolPrivate(plain, bytes.Repeat([]byte{9}, 32))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.PutProtocolPrivate(ctx, "sess", "ref-1", blob, digest, "g1"); err != nil {
		t.Fatal(err)
	}
	gotBlob, gotDigest, err := store.GetProtocolPrivate(ctx, "sess", "ref-1")
	if err != nil || gotDigest != digest {
		t.Fatalf("private blob lost: %v", err)
	}
	opened, err := modelfit.OpenProtocolPrivate(gotBlob, bytes.Repeat([]byte{9}, 32))
	if err != nil || string(opened) != "need file" {
		t.Fatalf("private open: %q %v", opened, err)
	}
}
