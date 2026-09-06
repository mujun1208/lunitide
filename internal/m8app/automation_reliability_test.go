package m8app_test

import (
	"context"
	"errors"
	"testing"

	"github.com/lunitide/lunitide/internal/domain/m8core"
	"github.com/lunitide/lunitide/internal/m8app"
	"github.com/oklog/ulid/v2"
)

func TestAutomationBlockedReceiptCommitsAndReplayBindsAllInput(t *testing.T) {
	store := openSliceStore(t)
	repo := store.AgentRuntimeRepository()
	svc := m8app.NewAutomationService(repo)
	ctx := context.Background()
	bundle := ulid.Make().String()
	if err := svc.RegisterBundle(ctx, m8app.RegisterBundleInput{BundleID: bundle, Version: 1, Checksum: sha64("a"), Permissions: m8core.BundlePermissions{Allow: []string{"fs.read"}, BudgetCeiling: 100}}); err != nil {
		t.Fatal(err)
	}
	in := dispatchInput(bundle, []string{"fs.write"}, 10, "blocked-stable")
	first, err := svc.Dispatch(ctx, in)
	if !errors.Is(err, m8app.ErrBundlePermissionDenied) || first.RunID == "" || first.ExecutionStarted {
		t.Fatalf("refusal %+v %v", first, err)
	}
	if err := repo.TransactAutomation(ctx, func(tx m8app.AutomationTx) error {
		run, found, err := tx.GetRunByIdempotencyKey(in.RequestID)
		if err != nil {
			return err
		}
		if !found || run.ID != first.RunID || run.State != m8core.RunQuarantined || run.CheckpointJSON == "" {
			t.Fatal("refusal was rolled back instead of committed")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	replayed, err := svc.Dispatch(ctx, in)
	if !errors.Is(err, m8app.ErrBundlePermissionDenied) || replayed.RunID != first.RunID {
		t.Fatalf("refusal replay %+v %v", replayed, err)
	}
	for _, mutate := range []func(*m8app.DispatchInput){
		func(i *m8app.DispatchInput) { i.BundleID = ulid.Make().String() },
		func(i *m8app.DispatchInput) { i.BundleVersion++ },
		func(i *m8app.DispatchInput) { i.Actor = "another actor" },
		func(i *m8app.DispatchInput) { i.Trigger = []byte(`{"type":"manual","actions":["fs.read"]}`) },
		func(i *m8app.DispatchInput) { i.Budget = []byte(`{"maxTokens":11}`) },
	} {
		changed := in
		mutate(&changed)
		if _, err := svc.Dispatch(ctx, changed); !errors.Is(err, m8app.ErrAutomationIdempotencyConflict) {
			t.Fatalf("changed input reused key: %v", err)
		}
	}
	canonical := in
	canonical.Trigger = []byte(`{ "actions": ["fs.write"], "type": "manual" }`)
	if replay, err := svc.Dispatch(ctx, canonical); !errors.Is(err, m8app.ErrBundlePermissionDenied) || replay.RunID != first.RunID {
		t.Fatal("format-only JSON change broke replay")
	}
}
