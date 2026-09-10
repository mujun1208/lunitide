package scheduler

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestSchedulerRefusesExecutionWithoutDurableIntent(t *testing.T) {
	store := newTestStore(t)
	job := validJob("intent", "* * * * *")
	if err := store.PutJob(job); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(store.runs, 0700); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	s := New(store, func(context.Context, Job) Outcome { calls.Add(1); return Outcome{} }, noopNotifier{})
	defer s.Close()
	if err := s.TriggerNow(job.ID); !errors.Is(err, ErrPersistence) {
		t.Fatalf("intent failure: %v", err)
	}
	if calls.Load() != 0 || len(s.Snapshot().RunningJobs) != 0 {
		t.Fatal("unrecorded execution escaped")
	}
}

func TestSchedulerFailurePreservesActualSessionAndPartialOutput(t *testing.T) {
	for _, notStarted := range []bool{false, true} {
		t.Run(fmt.Sprint(notStarted), func(t *testing.T) {
			store := newTestStore(t)
			job := validJob("partial", "* * * * *")
			if err := store.PutJob(job); err != nil {
				t.Fatal(err)
			}
			const actualSession = "01ARZ3NDEKTSV4RRFFQ69G5FAA"
			s := New(store, func(context.Context, Job) Outcome {
				return Outcome{SessionID: actualSession, Summary: "已完成前半部分", Err: context.DeadlineExceeded, NotStarted: notStarted}
			}, noopNotifier{})
			defer s.Close()
			if err := s.TriggerNow(job.ID); err != nil {
				t.Fatal(err)
			}
			s.wg.Wait()
			runs, err := store.ListRuns(job.ID, 10)
			if err != nil || len(runs) != 2 {
				t.Fatalf("receipt: %v %+v", err, runs)
			}
			got := runs[0]
			if got.State != RunFailed || got.OutcomeUnknown == notStarted || got.SessionID != actualSession || got.Summary != "已完成前半部分" || got.FinishedAt.IsZero() {
				t.Fatalf("failure erased output or misreported state: %+v", got)
			}
			if len(s.Snapshot().RunningJobs) != 0 {
				t.Fatal("failed task still marked running")
			}
			stored, _, err := store.GetJob(job.ID)
			if err != nil || stored.Enabled {
				t.Fatal("failed automation silently scheduled to repeat")
			}
		})
	}
}

func TestSchedulerConcurrentTriggerAndCloseOwnsExecution(t *testing.T) {
	store := newTestStore(t)
	job := validJob("close", "* * * * *")
	if err := store.PutJob(job); err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	var calls atomic.Int32
	s := New(store, func(ctx context.Context, _ Job) Outcome {
		calls.Add(1)
		close(entered)
		<-ctx.Done()
		return Outcome{Err: ctx.Err()}
	}, noopNotifier{})
	var wg sync.WaitGroup
	results := make(chan error, 10)
	for range 10 {
		wg.Add(1)
		go func() { defer wg.Done(); results <- s.TriggerNow(job.ID) }()
	}
	wg.Wait()
	close(results)
	accepted := 0
	for err := range results {
		if err == nil {
			accepted++
		} else if !errors.Is(err, ErrBusy) {
			t.Fatal(err)
		}
	}
	if accepted != 1 {
		t.Fatalf("accepted %d", accepted)
	}
	<-entered
	s.Close()
	if calls.Load() != 1 || len(s.Snapshot().RunningJobs) != 0 {
		t.Fatal("duplicate or surviving executor")
	}
	runs, err := store.ListRuns(job.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 2 || runs[0].State != RunFailed || !runs[0].OutcomeUnknown {
		t.Fatalf("shutdown receipt: %+v", runs)
	}
	if !errors.Is(s.TriggerNow(job.ID), ErrClosed) {
		t.Fatal("closed scheduler accepted work")
	}
}

func TestSchedulerPanicBecomesReceiptWithoutLeakingPanicValue(t *testing.T) {
	store := newTestStore(t)
	job := validJob("panic", "* * * * *")
	if err := store.PutJob(job); err != nil {
		t.Fatal(err)
	}
	s := New(store, func(context.Context, Job) Outcome { panic("private exception data") }, noopNotifier{})
	if err := s.TriggerNow(job.ID); err != nil {
		t.Fatal(err)
	}
	s.Close()
	runs, err := store.ListRuns(job.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 2 || runs[0].State != RunFailed || runs[0].Error == "private exception data" {
		t.Fatalf("panic: %+v", runs)
	}
}

func TestSchedulerRestartReconcilesIntentAndDisablesUncertainJob(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	job := validJob("orphan", "at:2026-01-01T00:00:00Z")
	if err = store.PutJob(job); err != nil {
		t.Fatal(err)
	}
	if err = store.AppendRun(Run{ID: "01ARZ3NDEKTSV4RRFFQ69G5FAZ", JobID: job.ID, State: RunRunning, StartedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err = reopened.RecoverInterrupted(); err != nil {
			t.Fatal(err)
		}
	}
	recovered, ok, err := reopened.GetJob(job.ID)
	if err != nil || !ok || recovered.Enabled {
		t.Fatalf("job: %+v %v", recovered, err)
	}
	runs, err := reopened.ListRuns(job.ID, 10)
	if err != nil || len(runs) != 2 || !runs[0].OutcomeUnknown || runs[0].ID != runs[1].ID {
		t.Fatalf("recovery: %+v %v", runs, err)
	}
	var calls atomic.Int32
	s := New(reopened, func(context.Context, Job) Outcome { calls.Add(1); return Outcome{} }, noopNotifier{})
	defer s.Close()
	s.fireDue(time.Now().UTC())
	if calls.Load() != 0 {
		t.Fatal("uncertain operation replayed on startup")
	}
}

func TestBindRunSessionUpdatesRunningIntentWithoutRelaunch(t *testing.T) {
	store := newTestStore(t)
	job := validJob("bind", "* * * * *")
	if err := store.PutJob(job); err != nil {
		t.Fatal(err)
	}
	runID := "01ARZ3NDEKTSV4RRFFQ69G5FAZ"
	if err := store.AppendRun(Run{ID: runID, JobID: job.ID, JobName: job.Name, SessionID: job.SessionID, State: RunRunning, StartedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	isolated := "01ARZ3NDEKTSV4RRFFQ69G5FAA"
	if err := store.BindRunSession(runID, isolated); err != nil {
		t.Fatal(err)
	}
	latest, err := store.LatestRuns(job.ID)
	if err != nil || len(latest) != 1 || latest[0].State != RunRunning || latest[0].SessionID != isolated || latest[0].ID != runID {
		t.Fatalf("bound run %+v %v", latest, err)
	}
}
