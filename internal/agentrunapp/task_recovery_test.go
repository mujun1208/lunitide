package agentrunapp_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/lunitide/lunitide/internal/agentrunapp"
	"github.com/lunitide/lunitide/internal/domain/agentrun"
	"github.com/oklog/ulid/v2"
)

func TestExecutionResumeUnknownEffect(t *testing.T) {
	cases := []struct {
		name           string
		fault          string
		expectConsumed bool
	}{
		{"write_succeeded_ack_lost", agentrunapp.TaskWriteFaultACKLost, false},
		{"crash_before_settlement", agentrunapp.TaskWriteFaultBeforeSettle, false},
		{"completion_event_lost", agentrunapp.TaskWriteFaultCompleteEventLost, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			budget, store, sessionID := executionBudgetHarness(t)
			recovery := agentrunapp.NewTaskRecovery(store.AgentRuntimeRepository())
			var writes atomic.Int32
			recovery.SetWriteFile(func(path string, data []byte) error {
				writes.Add(1)
				if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
					return err
				}
				return os.WriteFile(path, data, 0o600)
			})

			taskID := ulid.Make().String()
			scope, err := budget.EnsureExecutionBinding(ctx, "owner", sessionID, taskID, "task_root", taskID, "", executionBudgetPolicy(1000))
			if err != nil {
				t.Fatal(err)
			}
			permit, err := budget.AdmitCall(ctx, scope, agentrun.CallEstimate{
				CallID:           ulid.Make().String(),
				AttemptID:        ulid.Make().String(),
				RequestDigest:    ulid.Make().String(),
				InputTokensUpper: 400,
				OutputTokenCap:   300,
				OutputBytesCap:   4096,
				Purpose:          "write",
			})
			if err != nil {
				t.Fatal(err)
			}
			if err = budget.MarkDispatched(ctx, permit); err != nil {
				t.Fatal(err)
			}
			before, err := budget.Snapshot(ctx, scope)
			if err != nil {
				t.Fatal(err)
			}
			if before.ReservedTotal != 700 {
				t.Fatalf("reserved before write=%d, want 700", before.ReservedTotal)
			}

			content := []byte("artifact-once\n")
			digest := sha256HexBytes(content)
			path := filepath.Join(t.TempDir(), "out", "note.txt")
			native := "native-sess-alive-but-unproven"
			cleanup := agentrunapp.InstallTaskWriteFault(func(point string) error {
				if point == tc.fault {
					return errors.New("injected " + tc.fault)
				}
				return nil
			})
			defer cleanup()

			attempt, err := recovery.AttemptWriteEffect(ctx, agentrunapp.WriteEffectInput{
				Scope:           scope,
				Permit:          permit,
				StepID:          "write-step",
				AttemptID:       permit.AttemptID,
				EffectKey:       "artifact.write/" + permit.AttemptID,
				Path:            path,
				Content:         content,
				NativeSessionID: native,
				Usage:           agentrun.UsageTotals{InputTokens: 120, OutputTokens: 80},
				ReceiptID:       "receipt-" + permit.AttemptID,
				PayloadDigest:   digest,
			})
			if err == nil {
				t.Fatal("fault injection must stop the write pipeline")
			}
			if attempt.OutcomeState != "outcome_unknown" || attempt.EffectStatus != agentrun.EffectOutcomeUnknown && attempt.EffectStatus != agentrun.EffectPrepared {
				t.Fatalf("faulted attempt invented a result: %+v", attempt)
			}
			if writes.Load() != 1 {
				t.Fatalf("writes after fault=%d, want 1", writes.Load())
			}
			if got, readErr := os.ReadFile(path); readErr != nil || string(got) != string(content) {
				t.Fatalf("artifact after fault=%q err=%v", got, readErr)
			}

			cleanup()
			resumed, err := recovery.ResumeUnknownEffect(ctx, agentrunapp.ResumeUnknownInput{
				TaskID:          taskID,
				NativeSessionID: native,
			})
			if err != nil {
				t.Fatal(err)
			}
			if resumed.OutcomeState != "outcome_unknown" {
				t.Fatalf("resume invented success: %+v", resumed)
			}
			if resumed.EffectStatus != agentrun.EffectOutcomeUnknown {
				t.Fatalf("unknown effect did not stay unknown: %s", resumed.EffectStatus)
			}
			if resumed.CompletionEventPresent {
				t.Fatal("resume invented a completion event")
			}
			if resumed.ClaimedNativeResume {
				t.Fatal("forbidden substitute: looks resumed from native_session_id")
			}
			if writes.Load() != 1 {
				t.Fatalf("resume double-wrote: writes=%d", writes.Load())
			}
			if got, readErr := os.ReadFile(path); readErr != nil || string(got) != string(content) {
				t.Fatalf("resume changed artifact=%q err=%v", got, readErr)
			}

			after, err := budget.Snapshot(ctx, scope)
			if err != nil {
				t.Fatal(err)
			}
			if after.ReservedTotal == 0 && after.IsolatedTotal == 0 && after.ConsumedTotal == 0 {
				t.Fatal("budget reservation/consumed totals were cleared")
			}
			if tc.expectConsumed && after.ConsumedTotal != 200 {
				t.Fatalf("settled-before-crash consumed=%d, want 200 (not reset)", after.ConsumedTotal)
			}
			if !tc.expectConsumed && after.ConsumedTotal != 0 {
				t.Fatalf("unsettled resume consumed=%d, want 0 (must isolate, not invent settle)", after.ConsumedTotal)
			}
			if !tc.expectConsumed && after.IsolatedTotal+after.ReservedTotal < 700 {
				t.Fatalf("dispatched unknown released quota: reserved=%d isolated=%d", after.ReservedTotal, after.IsolatedTotal)
			}

			advanced, err := recovery.ApplyLateReceipt(ctx, agentrunapp.LateEffectReceipt{
				TaskID:             taskID,
				EffectKey:          "artifact.write/" + permit.AttemptID,
				ReceiptID:          "late-" + permit.AttemptID,
				PayloadDigest:      digest,
				SettlementRevision: 2,
				Usage:              agentrun.UsageTotals{InputTokens: 120, OutputTokens: 80},
			})
			if err != nil {
				t.Fatal(err)
			}
			if advanced.EffectStatus != agentrun.EffectCommitted || advanced.OutcomeState != "succeeded" {
				t.Fatalf("late receipt did not advance result: %+v", advanced)
			}
			if !advanced.CompletionEventPresent {
				t.Fatal("late receipt must persist the completion event")
			}
			if writes.Load() != 1 {
				t.Fatalf("late receipt rewrote artifact: writes=%d", writes.Load())
			}
			settled, err := budget.Snapshot(ctx, scope)
			if err != nil {
				t.Fatal(err)
			}
			if settled.ConsumedTotal != 200 {
				t.Fatalf("late settle consumed=%d, want 200", settled.ConsumedTotal)
			}
		})
	}

	t.Run("native_session_id_alone_is_not_resume", func(t *testing.T) {
		ctx := context.Background()
		budget, store, sessionID := executionBudgetHarness(t)
		recovery := agentrunapp.NewTaskRecovery(store.AgentRuntimeRepository())
		taskID := ulid.Make().String()
		if _, err := budget.EnsureExecutionBinding(ctx, "owner", sessionID, taskID, "task_root", taskID, "", executionBudgetPolicy(1000)); err != nil {
			t.Fatal(err)
		}
		got, err := recovery.ResumeUnknownEffect(ctx, agentrunapp.ResumeUnknownInput{
			TaskID:          taskID,
			NativeSessionID: "native-sess-only",
		})
		if err != nil {
			t.Fatal(err)
		}
		if got.OutcomeState == "succeeded" || got.ClaimedNativeResume || got.CompletionEventPresent {
			t.Fatalf("native_session_id alone looked resumed: %+v", got)
		}
		if got.OutcomeState != "outcome_unknown" {
			t.Fatalf("empty journal must stay unknown, got %q", got.OutcomeState)
		}
	})
}

func sha256HexBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
