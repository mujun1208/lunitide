package agentrunapp_test

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
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

func runtimeHarness(t *testing.T, budget agentrun.Budget) (*agentrunapp.Service, *storage.Store, agentrun.AgentRun) {
	t.Helper()
	ctx := context.Background()
	store, err := storage.OpenTemplated(ctx, filepath.Join(t.TempDir(), "runtime.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	p, err := projectapp.New(store, store).Create(ctx, "kernel-project", "test", map[string]string{"name": "kernel"}, project.Project{Name: "kernel"})
	if err != nil {
		t.Fatal(err)
	}
	sess, err := sessionapp.New(store, store).Create(ctx, "kernel-session", "test", map[string]string{"projectId": p.ID}, session.Session{ProjectID: p.ID, Title: "kernel"})
	if err != nil {
		t.Fatal(err)
	}
	svc := agentrunapp.New(store.AgentRuntimeRepository())
	// After store.Close so Drain runs first: a launched command writes its
	// result from a goroutine that outlives CommandStart, and closing the
	// store underneath that transaction leaves the database file open —
	// which on Windows makes t.TempDir's removal fail.
	t.Cleanup(svc.DrainCommands)
	run, err := svc.Start(ctx, "kernel-start", "test", map[string]string{"sessionId": sess.ID}, sess.ID, budget)
	if err != nil {
		t.Fatal(err)
	}
	return svc, store, run
}

func testBudget() agentrun.Budget {
	return agentrun.Budget{MaxModelTurns: 8, MaxToolCalls: 8, MaxTokens: 1000, MaxCostMicros: 1000, MaxWallClockSeconds: 60, MaxOutputBytes: 4096, MaxRetries: 2, MaxNoProgress: 2, HardCeiling: true}
}

func TestKernelGoldenTrace(t *testing.T) {
	svc, store, run := runtimeHarness(t, testBudget())
	ctx := context.Background()
	if _, err := svc.Advance(ctx, run.ID, agentrunapp.AdvanceInput{Kind: agentrunapp.AdvanceToolCall, ToolName: "fs.read", Args: json.RawMessage(`{"path":"README.md"}`), Usage: agentrun.Usage{ToolCalls: 1}}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Advance(ctx, run.ID, agentrunapp.AdvanceInput{Kind: agentrunapp.AdvanceObservation, ObservationKind: "fs.read.result", Observation: []byte("ok")}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Advance(ctx, run.ID, agentrunapp.AdvanceInput{Kind: agentrunapp.AdvanceComplete}); err != nil {
		t.Fatal(err)
	}
	var got []string
	err := store.AgentRuntimeRepository().Transact(ctx, func(tx agentrun.Tx) error {
		events, err := tx.ListEvents(run.ID)
		if err != nil {
			return err
		}
		for _, event := range events {
			got = append(got, event.EventType)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"AgentRunStartCompleted", "ToolCallProposed", "ObservationCaptured", "AgentRunTerminal"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("golden trace mismatch\n got: %v\nwant: %v", got, want)
	}
}

func TestConcurrentHardBudgetHasZeroEffect(t *testing.T) {
	budget := testBudget()
	budget.MaxToolCalls = 1
	svc, store, run := runtimeHarness(t, budget)
	ctx := context.Background()
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)
	for i := 0; i < 2; i++ {
		go func() {
			defer wg.Done()
			<-start
			_, _ = svc.Advance(ctx, run.ID, agentrunapp.AdvanceInput{Kind: agentrunapp.AdvanceToolCall, ToolName: "fs.read", Args: json.RawMessage(`{}`), Usage: agentrun.Usage{ToolCalls: 1}})
		}()
	}
	close(start)
	wg.Wait()
	got, err := svc.Get(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != agentrun.RunFailed {
		t.Fatalf("status=%s want failed", got.Status)
	}
	var calls int
	err = store.AgentRuntimeRepository().Transact(ctx, func(tx agentrun.Tx) error {
		turns, e := tx.ListTurns(run.ID)
		if e != nil {
			return e
		}
		steps, e := tx.ListSteps(turns[0].ID)
		if e != nil {
			return e
		}
		cs, e := tx.ListToolCalls(steps[0].ID)
		calls = len(cs)
		return e
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || got.Used.ToolCalls != 1 {
		t.Fatalf("calls=%d used=%d; over-budget attempt produced an effect", calls, got.Used.ToolCalls)
	}
}

func TestBudgetResumeRequiresExpandedCoveringEnvelope(t *testing.T) {
	budget := testBudget()
	budget.HardCeiling = false
	budget.MaxToolCalls = 1
	svc, _, run := runtimeHarness(t, budget)
	ctx := context.Background()
	if _, err := svc.Advance(ctx, run.ID, agentrunapp.AdvanceInput{Kind: agentrunapp.AdvanceToolCall, ToolName: "fs.read", Args: json.RawMessage(`{}`), Usage: agentrun.Usage{ToolCalls: 2}}); err != nil {
		t.Fatal(err)
	}
	paused, _ := svc.Get(ctx, run.ID)
	if paused.Status != agentrun.RunPausedBudget {
		t.Fatalf("status=%s", paused.Status)
	}
	if _, err := svc.ResumeWithBudget(ctx, "resume-small", "test", map[string]int{"n": 1}, run.ID, paused.Version, budget); !errors.Is(err, agentrun.ErrBudgetExceeded) {
		t.Fatalf("non-expanded resume err=%v", err)
	}
	expanded := budget
	expanded.MaxToolCalls = 3
	resumed, err := svc.ResumeWithBudget(ctx, "resume-expanded", "test", map[string]int{"n": 2}, run.ID, paused.Version, expanded)
	if err != nil || resumed.Status != agentrun.RunRunning {
		t.Fatalf("resume=%+v err=%v", resumed, err)
	}
}

func TestKernelTerminalRaceFirstWins(t *testing.T) {
	svc, _, run := runtimeHarness(t, testBudget())
	ctx := context.Background()
	start := make(chan struct{})
	errs := make(chan error, 2)
	for _, kind := range []agentrunapp.AdvanceKind{agentrunapp.AdvanceComplete, agentrunapp.AdvanceFail} {
		go func(k agentrunapp.AdvanceKind) {
			<-start
			_, err := svc.Advance(ctx, run.ID, agentrunapp.AdvanceInput{Kind: k})
			errs <- err
		}(kind)
	}
	close(start)
	a, b := <-errs, <-errs
	if (a == nil) == (b == nil) {
		t.Fatalf("race errors=(%v,%v), want exactly one winner", a, b)
	}
	loser := a
	if loser == nil {
		loser = b
	}
	if !errors.Is(loser, agentrun.ErrTerminal) {
		t.Fatalf("loser err=%v", loser)
	}
}

func TestRecoveryConvergesAndIsIdempotent(t *testing.T) {
	svc, store, run := runtimeHarness(t, testBudget())
	ctx := context.Background()
	err := store.AgentRuntimeRepository().Transact(ctx, func(tx agentrun.Tx) error {
		turns, e := tx.ListTurns(run.ID)
		if e != nil {
			return e
		}
		steps, e := tx.ListSteps(turns[0].ID)
		if e != nil {
			return e
		}
		call := agentrun.ToolCall{ID: ulid.Make().String(), StepID: steps[0].ID, ToolName: "fs.read", ArgsDigest: strings.Repeat("a", 64), Status: agentrun.CallRunning, CreatedAt: run.CreatedAt, UpdatedAt: run.CreatedAt}
		if e = tx.PutToolCall(call); e != nil {
			return e
		}
		return tx.PutEffect(agentrun.EffectJournal{ID: ulid.Make().String(), RunID: run.ID, EffectKey: "test/recovery", RequestDigest: strings.Repeat("b", 64), Status: agentrun.EffectPrepared, CreatedAt: run.CreatedAt, UpdatedAt: run.CreatedAt})
	})
	if err != nil {
		t.Fatal(err)
	}
	first, err := svc.RunRecoveryScanner(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if first.Runs != 1 || first.Steps != 1 || first.ToolCalls != 1 || first.Effects != 1 {
		t.Fatalf("first=%+v", first)
	}
	got, _ := svc.Get(ctx, run.ID)
	if got.Status != agentrun.RunOutcomeUnknown {
		t.Fatalf("status=%s", got.Status)
	}
	second, err := svc.RunRecoveryScanner(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if second != (agentrunapp.RecoveryResult{}) {
		t.Fatalf("second=%+v", second)
	}
}

func TestRecoveryPreservesSpecializedPreparedRun(t *testing.T) {
	svc, store, run := runtimeHarness(t, testBudget())
	ctx := context.Background()
	err := store.AgentRuntimeRepository().Transact(ctx, func(tx agentrun.Tx) error {
		return tx.PutEffect(agentrun.EffectJournal{ID: ulid.Make().String(), RunID: run.ID, EffectKey: "changeset.revert/test", RequestDigest: strings.Repeat("c", 64), Status: agentrun.EffectPrepared, CreatedAt: run.CreatedAt, UpdatedAt: run.CreatedAt})
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := svc.RunRecoveryScanner(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got != (agentrunapp.RecoveryResult{}) {
		t.Fatalf("recovery=%+v", got)
	}
	preserved, err := svc.Get(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if preserved.Status != agentrun.RunRunning {
		t.Fatalf("status=%s want running", preserved.Status)
	}
	err = store.AgentRuntimeRepository().Transact(ctx, func(tx agentrun.Tx) error {
		effect, e := tx.GetEffectByKey("changeset.revert/test")
		if e != nil {
			return e
		}
		if effect.Status != agentrun.EffectPrepared {
			t.Fatalf("effect=%s want prepared", effect.Status)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestCommandReconcileAcceptsEffectAlreadyOutcomeUnknown(t *testing.T) {
	svc, store, run := runtimeHarness(t, testBudget())
	ctx := context.Background()
	jobID := ulid.Make().String()
	err := store.AgentRuntimeRepository().Transact(ctx, func(tx agentrun.Tx) error {
		if e := tx.PutCommandJob(agentrun.CommandJob{ID: jobID, RunID: run.ID, CommandSpecDigest: strings.Repeat("d", 64), Status: agentrun.JobRunning, CreatedAt: run.CreatedAt, UpdatedAt: run.CreatedAt}); e != nil {
			return e
		}
		return tx.PutEffect(agentrun.EffectJournal{ID: ulid.Make().String(), RunID: run.ID, EffectKey: "command.start/" + jobID, RequestDigest: strings.Repeat("d", 64), Status: agentrun.EffectOutcomeUnknown, CreatedAt: run.CreatedAt, UpdatedAt: run.CreatedAt})
	})
	if err != nil {
		t.Fatal(err)
	}
	count, err := svc.ReconcileCommandJobs(ctx)
	if err != nil || count != 1 {
		t.Fatalf("first reconcile count=%d err=%v", count, err)
	}
	count, err = svc.ReconcileCommandJobs(ctx)
	if err != nil || count != 0 {
		t.Fatalf("second reconcile count=%d err=%v", count, err)
	}
}
