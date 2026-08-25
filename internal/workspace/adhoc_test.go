package workspace_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/agentrunapp"
	"github.com/lunitide/lunitide/internal/domain/agentrun"
	"github.com/lunitide/lunitide/internal/domain/m5workspace"
	"github.com/lunitide/lunitide/internal/domain/project"
	"github.com/lunitide/lunitide/internal/domain/session"
	"github.com/lunitide/lunitide/internal/projectapp"
	"github.com/lunitide/lunitide/internal/sessionapp"
	"github.com/lunitide/lunitide/internal/workspace"
	storage "github.com/lunitide/lunitide/internal/storage/sqlite"
)

type fakeClock struct{ now time.Time }

func (c *fakeClock) Now() time.Time { return c.now }

var adhocRuns int

// newRunID creates a real agent_run row so the workspace foreign key holds.
func newRunID(t *testing.T, store *storage.Store) string {
	t.Helper()
	ctx := context.Background()
	adhocRuns++
	key := fmt.Sprintf("m5-adhoc-%d", adhocRuns)
	p, err := projectapp.New(store, store).Create(ctx, key+"-p", "test", map[string]string{"name": "adhoc"}, project.Project{Name: "adhoc"})
	if err != nil {
		t.Fatal(err)
	}
	sess, err := sessionapp.New(store, store).Create(ctx, key+"-s", "test", map[string]string{"projectId": p.ID}, session.Session{ProjectID: p.ID, Title: "adhoc"})
	if err != nil {
		t.Fatal(err)
	}
	run, err := agentrunapp.New(store.AgentRuntimeRepository()).Start(ctx, key+"-r", "test", map[string]string{"sessionId": sess.ID}, sess.ID, agentrun.Budget{MaxModelTurns: 4, MaxToolCalls: 4, MaxTokens: 100, MaxCostMicros: 100, MaxWallClockSeconds: 30, MaxOutputBytes: 1024, MaxRetries: 1, MaxNoProgress: 1, HardCeiling: true})
	if err != nil {
		t.Fatal(err)
	}
	return run.ID
}

func adhocHarness(t *testing.T) (*workspace.AdHocService, *fakeClock, *storage.Store) {
	t.Helper()
	ctx := context.Background()
	store, err := storage.OpenTemplated(ctx, filepath.Join(t.TempDir(), "m5-adhoc.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	svc := workspace.NewAdHocService(store.AgentRuntimeRepository())
	clock := &fakeClock{now: time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)}
	svc.SetClock(clock)
	return svc, clock, store
}

func TestAdHocCreateRootUnique(t *testing.T) {
	svc, _, store := adhocHarness(t)
	ctx := context.Background()
	run1 := newRunID(t, store)
	w, err := svc.Create(ctx, run1, `C:\ws\alpha`, "Alpha")
	if err != nil {
		t.Fatal(err)
	}
	if w.State != m5workspace.StateActive || w.Version != 1 || w.UsedBytes != 0 {
		t.Fatalf("initial state wrong: %+v", w)
	}
	if w.QuotaSoft != m5workspace.DefaultQuotaSoft || w.QuotaHard != m5workspace.DefaultQuotaHard {
		t.Fatalf("frozen 2/4 GiB quotas expected, got soft=%d hard=%d", w.QuotaSoft, w.QuotaHard)
	}
	run2 := newRunID(t, store)
	if _, err := svc.Create(ctx, run2, `C:\ws\alpha`, "Duplicate"); !errors.Is(err, m5workspace.ErrRootInUse) {
		t.Fatalf("duplicate root must fail with ErrRootInUse, got %v", err)
	}
	run3 := newRunID(t, store)
	if _, err := svc.Create(ctx, run3, `C:\ws\beta`, "Beta"); err != nil {
		t.Fatalf("distinct root must succeed: %v", err)
	}
	if _, err := svc.Create(ctx, "", `C:\ws\gamma`, "Gamma"); !errors.Is(err, workspace.ErrInvalidInput) {
		t.Fatalf("empty run id must fail invalid, got %v", err)
	}
}

func TestAdHocChargeHardQuotaFlipsReadonly(t *testing.T) {
	svc, _, store := adhocHarness(t)
	ctx := context.Background()
	w, err := svc.Create(ctx, newRunID(t, store), `C:\ws\quota`, "Quota")
	if err != nil {
		t.Fatal(err)
	}
	// Normal charge inside quota stays active.
	w, err = svc.Charge(ctx, w.ID, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if w.UsedBytes != 1024 || w.State != m5workspace.StateActive {
		t.Fatalf("charge should keep active, got %+v", w)
	}
	if w.NearQuota() {
		t.Fatal("small charge must not be near quota")
	}
	// Charge up to exactly soft: derived near_quota flips on.
	w, err = svc.Charge(ctx, w.ID, m5workspace.DefaultQuotaSoft-1024)
	if err != nil {
		t.Fatal(err)
	}
	if !w.NearQuota() {
		t.Fatal("used >= soft must report near quota")
	}
	// One byte beyond hard: rejected with WS-001 and state readonly_full.
	_, err = svc.Charge(ctx, w.ID, m5workspace.DefaultQuotaHard-w.UsedBytes+1)
	if !errors.Is(err, workspace.ErrQuotaExceeded) {
		t.Fatalf("hard quota breach must answer WS-001, got %v", err)
	}
	got, err := svc.Charge(ctx, w.ID, 1)
	if !errors.Is(err, workspace.ErrQuotaExceeded) || got.State != m5workspace.StateReadonlyFull {
		t.Fatalf("readonly_full must reject further writes, got err=%v state=%s", err, got.State)
	}
}

func TestAdHocTickExpiryStateMachine(t *testing.T) {
	svc, clock, store := adhocHarness(t)
	ctx := context.Background()
	w, err := svc.Create(ctx, newRunID(t, store), `C:\ws\tick`, "Tick")
	if err != nil {
		t.Fatal(err)
	}
	// Before the grace window: nothing changes.
	clock.now = w.LeaseExpiry.Add(-m5workspace.GracePeriod - time.Hour)
	if err := svc.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	// Inside the last 24h: active -> expiring.
	clock.now = w.LeaseExpiry.Add(-time.Hour)
	if err := svc.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	// Past the lease: expiring -> cleaning.
	clock.now = w.LeaseExpiry.Add(time.Hour)
	if err := svc.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	// Cleaning rows are untouched by further ticks; retain from cleaning is legal.
	kept, err := svc.Retain(ctx, w.ID)
	if err != nil {
		t.Fatalf("retain from cleaning expected to pass, got %v", err)
	}
	if kept.State != m5workspace.StateRetained || !kept.State.Terminal() {
		t.Fatalf("retain from cleaning expected, got %s", kept.State)
	}
}

func TestAdHocCompleteCleanup(t *testing.T) {
	svc, clock, store := adhocHarness(t)
	ctx := context.Background()
	w, err := svc.Create(ctx, newRunID(t, store), `C:\ws\clean`, "Clean")
	if err != nil {
		t.Fatal(err)
	}
	clock.now = w.LeaseExpiry.Add(time.Hour)
	// Tick advances one layer per pass: active -> expiring, then cleaning.
	if err := svc.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if err := svc.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	// Cleanup fails first: cleaning -> cleaning_failed.
	failed, err := svc.CompleteCleanup(ctx, w.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if failed.State != m5workspace.StateCleaningFailed {
		t.Fatalf("failed cleanup expected cleaning_failed, got %s", failed.State)
	}
	// Direct cleaning_failed -> deleted is illegal in the frozen machine.
	if _, err := svc.CompleteCleanup(ctx, w.ID, true); !errors.Is(err, m5workspace.ErrTransition) {
		t.Fatalf("cleaning_failed -> deleted must be refused, got %v", err)
	}
}

func TestAdHocRetainFromExpiring(t *testing.T) {
	svc, clock, store := adhocHarness(t)
	ctx := context.Background()
	w, err := svc.Create(ctx, newRunID(t, store), `C:\ws\keep`, "Keep")
	if err != nil {
		t.Fatal(err)
	}
	clock.now = w.LeaseExpiry.Add(-time.Hour)
	if err := svc.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	kept, err := svc.Retain(ctx, w.ID)
	if err != nil {
		t.Fatal(err)
	}
	if kept.State != m5workspace.StateRetained {
		t.Fatalf("expiring workspace retained expected, got %s", kept.State)
	}
	// Terminal rows never appear in Tick's live set again.
	clock.now = kept.UpdatedAt.Add(30 * 24 * time.Hour)
	if err := svc.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Retain(ctx, w.ID); !errors.Is(err, m5workspace.ErrTransition) {
		t.Fatalf("retained is terminal, re-retain must be refused, got %v", err)
	}
}

func TestWorkspaceStateMachineMatrix(t *testing.T) {
	cases := []struct {
		from, to m5workspace.State
		ok       bool
	}{
		{m5workspace.StateActive, m5workspace.StateReadonlyFull, true},
		{m5workspace.StateActive, m5workspace.StateExpiring, true},
		{m5workspace.StateActive, m5workspace.StateDeleted, false},
		{m5workspace.StateReadonlyFull, m5workspace.StateActive, true},
		{m5workspace.StateReadonlyFull, m5workspace.StateExpiring, true},
		{m5workspace.StateExpiring, m5workspace.StateCleaning, true},
		{m5workspace.StateExpiring, m5workspace.StateRetained, true},
		{m5workspace.StateExpiring, m5workspace.StateActive, false},
		{m5workspace.StateCleaning, m5workspace.StateDeleted, true},
		{m5workspace.StateCleaning, m5workspace.StateRetained, true},
		{m5workspace.StateCleaning, m5workspace.StateCleaningFailed, true},
		{m5workspace.StateCleaningFailed, m5workspace.StateCleaning, true},
		{m5workspace.StateCleaningFailed, m5workspace.StateRetained, true},
		{m5workspace.StateRetained, m5workspace.StateActive, false},
		{m5workspace.StateDeleted, m5workspace.StateActive, false},
	}
	for _, c := range cases {
		w := m5workspace.Workspace{State: c.from}
		if got := w.CanTransitionTo(c.to); got != c.ok {
			t.Fatalf("%s -> %s: want %v got %v", c.from, c.to, c.ok, got)
		}
	}
}
