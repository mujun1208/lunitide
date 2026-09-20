package agentrunapp_test

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"github.com/lunitide/lunitide/internal/agentrunapp"
	"github.com/lunitide/lunitide/internal/domain/agentrun"
	"github.com/lunitide/lunitide/internal/domain/project"
	"github.com/lunitide/lunitide/internal/domain/session"
	"github.com/lunitide/lunitide/internal/projectapp"
	"github.com/lunitide/lunitide/internal/sessionapp"
	storage "github.com/lunitide/lunitide/internal/storage/sqlite"
	"github.com/oklog/ulid/v2"
)

func int64ptr(v int64) *int64 { return &v }

func executionBudgetPolicy(total int64) agentrun.ExecutionBudgetPolicy {
	return agentrun.ExecutionBudgetPolicy{
		MaxTotalTokens:   int64ptr(total),
		MaxOutputTokens:  int64ptr(total),
		MaxModelAttempts: int64ptr(16),
		MaxActiveMillis:  int64ptr(60_000),
		MaxOutputBytes:   int64ptr(1 << 20),
	}
}

func executionBudgetHarness(t *testing.T) (*agentrunapp.ExecutionBudgetService, *storage.Store, string) {
	t.Helper()
	ctx := context.Background()
	store, err := storage.OpenTemplated(ctx, filepath.Join(t.TempDir(), "execution-budget.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	p, err := projectapp.New(store, store).Create(ctx, "budget-project", "test", map[string]string{"name": "budget"}, project.Project{Name: "budget"})
	if err != nil {
		t.Fatal(err)
	}
	sess, err := sessionapp.New(store, store).Create(ctx, "budget-session", "test", map[string]string{"projectId": p.ID}, session.Session{ProjectID: p.ID, Title: "budget"})
	if err != nil {
		t.Fatal(err)
	}
	return agentrunapp.NewExecutionBudget(store.AgentRuntimeRepository()), store, sess.ID
}

func TestExecutionBudgetConcurrentParentChildReservation(t *testing.T) {
	ctx := context.Background()
	budget, _, sessionID := executionBudgetHarness(t)
	taskID := ulid.Make().String()
	policy := executionBudgetPolicy(1000)
	parent, err := budget.EnsureExecutionBinding(ctx, "owner", sessionID, taskID, "task_root", taskID, "", policy)
	if err != nil {
		t.Fatal(err)
	}
	child, err := budget.EnsureExecutionBinding(ctx, "owner", sessionID, taskID, "chat_stream", ulid.Make().String(), parent.ScopeID, policy)
	if err != nil {
		t.Fatal(err)
	}

	type outcome struct {
		permit agentrun.CallPermit
		err    error
	}
	results := make([]outcome, 3)
	var wg sync.WaitGroup
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i].permit, results[i].err = budget.AdmitCall(ctx, child, agentrun.CallEstimate{
				CallID:           ulid.Make().String(),
				AttemptID:        ulid.Make().String(),
				RequestDigest:    ulid.Make().String(),
				InputTokensUpper: 400,
				OutputTokenCap:   300,
				OutputBytesCap:   4096,
				Purpose:          "chat",
			})
		}(i)
	}
	wg.Wait()

	var admitted []agentrun.CallPermit
	for _, result := range results {
		if result.err == nil {
			admitted = append(admitted, result.permit)
		}
	}
	if len(admitted) != 1 {
		t.Fatalf("admitted %d concurrent 700-token reserves against 1000, want 1", len(admitted))
	}

	parentSnap, err := budget.Snapshot(ctx, parent)
	if err != nil {
		t.Fatal(err)
	}
	childSnap, err := budget.Snapshot(ctx, child)
	if err != nil {
		t.Fatal(err)
	}
	if parentSnap.ReservedTotal != 700 || childSnap.ReservedTotal != 700 {
		t.Fatalf("parent reserved=%d child reserved=%d, want 700/700 (same reservation, no double count)", parentSnap.ReservedTotal, childSnap.ReservedTotal)
	}

	permit := admitted[0]
	if err = budget.MarkDispatched(ctx, permit); err != nil {
		t.Fatal(err)
	}
	settle := agentrun.CallSettlement{
		ReservationID:      permit.ReservationID,
		ReceiptID:          "receipt-concurrent-1",
		PayloadDigest:      "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		SettlementRevision: 1,
		Usage:              agentrun.UsageTotals{InputTokens: 120, OutputTokens: 80},
		Integrity:          "reported",
		Dispatched:         true,
		Outcome:            "completed",
	}
	if err = budget.SettleCall(ctx, settle); err != nil {
		t.Fatal(err)
	}
	if err = budget.SettleCall(ctx, settle); err != nil {
		t.Fatal(err)
	}
	parentSnap, err = budget.Snapshot(ctx, parent)
	if err != nil {
		t.Fatal(err)
	}
	childSnap, err = budget.Snapshot(ctx, child)
	if err != nil {
		t.Fatal(err)
	}
	if parentSnap.ConsumedTotal != 200 || childSnap.ConsumedTotal != 200 {
		t.Fatalf("parent consumed=%d child consumed=%d, want 200/200 after idempotent settle", parentSnap.ConsumedTotal, childSnap.ConsumedTotal)
	}
	if parentSnap.ReservedTotal != 0 || childSnap.ReservedTotal != 0 {
		t.Fatalf("reserved after settle parent=%d child=%d, want 0", parentSnap.ReservedTotal, childSnap.ReservedTotal)
	}
}

func TestRecoverOrphanedReservationsFreesLeakedDispatch(t *testing.T) {
	ctx := context.Background()
	budget, store, sessionID := executionBudgetHarness(t)
	taskID := ulid.Make().String()
	policy := executionBudgetPolicy(1000)
	scope, err := budget.EnsureExecutionBinding(ctx, "owner", sessionID, taskID, "task_root", taskID, "", policy)
	if err != nil {
		t.Fatal(err)
	}
	permit, err := budget.AdmitCall(ctx, scope, agentrun.CallEstimate{
		CallID: ulid.Make().String(), AttemptID: ulid.Make().String(), RequestDigest: ulid.Make().String(),
		InputTokensUpper: 400, OutputTokenCap: 300, OutputBytesCap: 4096,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = budget.MarkDispatched(ctx, permit); err != nil {
		t.Fatal(err)
	}
	_, err = budget.AdmitCall(ctx, scope, agentrun.CallEstimate{
		CallID: ulid.Make().String(), AttemptID: ulid.Make().String(), RequestDigest: ulid.Make().String(),
		InputTokensUpper: 400, OutputTokenCap: 300, OutputBytesCap: 4096,
	})
	if !errors.Is(err, agentrun.ErrExecutionBudget) {
		t.Fatalf("leaked reserved+dispatched must block the next admit: %v", err)
	}
	n, err := store.RecoverOrphanedReservations(ctx)
	if err != nil || n != 1 {
		t.Fatalf("recover n=%d err=%v", n, err)
	}
	if _, err = budget.AdmitCall(ctx, scope, agentrun.CallEstimate{
		CallID: ulid.Make().String(), AttemptID: ulid.Make().String(), RequestDigest: ulid.Make().String(),
		InputTokensUpper: 400, OutputTokenCap: 300, OutputBytesCap: 4096,
	}); err != nil {
		t.Fatalf("admit after orphan recover: %v", err)
	}
}

func TestRecoverOrphanedReservationsForTaskFreesOnlyThatTask(t *testing.T) {
	ctx := context.Background()
	budget, store, sessionID := executionBudgetHarness(t)
	taskA := ulid.Make().String()
	taskB := ulid.Make().String()
	policy := executionBudgetPolicy(1000)
	scopeA, err := budget.EnsureExecutionBinding(ctx, "owner", sessionID, taskA, "task_root", taskA, "", policy)
	if err != nil {
		t.Fatal(err)
	}
	scopeB, err := budget.EnsureExecutionBinding(ctx, "owner", sessionID, taskB, "task_root", taskB, "", policy)
	if err != nil {
		t.Fatal(err)
	}
	permitA, err := budget.AdmitCall(ctx, scopeA, agentrun.CallEstimate{
		CallID: ulid.Make().String(), AttemptID: ulid.Make().String(), RequestDigest: ulid.Make().String(),
		InputTokensUpper: 400, OutputTokenCap: 300, OutputBytesCap: 4096,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = budget.MarkDispatched(ctx, permitA); err != nil {
		t.Fatal(err)
	}
	permitB, err := budget.AdmitCall(ctx, scopeB, agentrun.CallEstimate{
		CallID: ulid.Make().String(), AttemptID: ulid.Make().String(), RequestDigest: ulid.Make().String(),
		InputTokensUpper: 400, OutputTokenCap: 300, OutputBytesCap: 4096,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = budget.MarkDispatched(ctx, permitB); err != nil {
		t.Fatal(err)
	}
	n, err := store.RecoverOrphanedReservationsForTask(ctx, taskA)
	if err != nil || n != 1 {
		t.Fatalf("recover task A n=%d err=%v", n, err)
	}
	if _, err = budget.AdmitCall(ctx, scopeA, agentrun.CallEstimate{
		CallID: ulid.Make().String(), AttemptID: ulid.Make().String(), RequestDigest: ulid.Make().String(),
		InputTokensUpper: 400, OutputTokenCap: 300, OutputBytesCap: 4096,
	}); err != nil {
		t.Fatalf("task A admit after scoped recover: %v", err)
	}
	if _, err = budget.AdmitCall(ctx, scopeB, agentrun.CallEstimate{
		CallID: ulid.Make().String(), AttemptID: ulid.Make().String(), RequestDigest: ulid.Make().String(),
		InputTokensUpper: 400, OutputTokenCap: 300, OutputBytesCap: 4096,
	}); !errors.Is(err, agentrun.ErrExecutionBudget) {
		t.Fatalf("task B leak must remain: %v", err)
	}
}
