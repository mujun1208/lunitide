package officeapp

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	domain "github.com/lunitide/lunitide/internal/domain/officestudio"
	"github.com/lunitide/lunitide/internal/org"
	"github.com/oklog/ulid/v2"
)

func TestOfficeLifecycleSuccessFailureAndPanicRetainCheckpoints(t *testing.T) {
	s, store, task := studioServiceFixture(t)
	ctx := context.Background()
	task.Checkpoint = json.RawMessage(`{"completedSteps":["old-step"]}`)
	var err error
	task, err = store.UpdateOfficeTask(ctx, task, task.Revision)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, status string
		run          func(context.Context) error
	}{{"check", "succeeded", func(c context.Context) error {
		during, e := store.GetOfficeTask(c, task.ID)
		if e != nil {
			return e
		}
		if during.Status != "validating" || during.RunID == "" {
			t.Errorf("active task: %+v", during)
		}
		return nil
	}}, {"failure", "failed", func(context.Context) error { return errors.New("invalid source") }}, {"panic", "failed", func(context.Context) error { panic("isolated operation") }}} {
		t.Run(test.name, func(t *testing.T) {
			err := s.Execute(ctx, task.ID, "validating", test.run)
			if test.status == "succeeded" && err != nil {
				t.Fatal(err)
			}
			if test.status != "succeeded" && err == nil {
				t.Fatal("failed operation lost error")
			}
			after, e := store.GetOfficeTask(ctx, task.ID)
			if e != nil || after.Status != test.status {
				t.Fatalf("terminal %+v %v", after, e)
			}
			var checkpoint map[string]json.RawMessage
			if e = json.Unmarshal(after.Checkpoint, &checkpoint); e != nil || string(checkpoint["completedSteps"]) != `["old-step"]` {
				t.Fatalf("checkpoint lost: %s %v", after.Checkpoint, e)
			}
			receipts, e := store.ListOfficeStepReceipts(ctx, task.ID, after.RunID)
			if e != nil || len(receipts) != 2 || receipts[1].State != test.status {
				t.Fatalf("receipts: %+v %v", receipts, e)
			}
		})
	}
}

func TestOfficeLifecycleCancelKeepsTerminalAfterRequestEnds(t *testing.T) {
	s, store, task := studioServiceFixture(t)
	ctx := context.Background()
	started, ended := make(chan struct{}), make(chan error, 1)
	go func() {
		ended <- s.Execute(ctx, task.ID, "validate", func(c context.Context) error { close(started); <-c.Done(); return c.Err() })
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("operation did not start")
	}
	if err := s.Cancel(ctx, task.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-ended:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancel result: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancellation did not settle")
	}
	after, err := store.GetOfficeTask(ctx, task.ID)
	if err != nil || after.Status != "cancelled" {
		t.Fatalf("cancelled task: %+v %v", after, err)
	}
	if err = s.Execute(ctx, task.ID, "running", func(context.Context) error { return nil }); err != nil {
		t.Fatalf("new operation after cancel: %v", err)
	}
	after, err = store.GetOfficeTask(ctx, task.ID)
	if err != nil || after.Status != "succeeded" {
		t.Fatalf("new task run: %+v %v", after, err)
	}
}

func TestOfficeLifecycleRecoveryDoesNotInterruptOwnedRun(t *testing.T) {
	s, store, task := studioServiceFixture(t)
	ctx := context.Background()
	task.Status = "running"
	task.RunID = ulid.Make().String()
	var err error
	task, err = store.UpdateOfficeTask(ctx, task, task.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.RecoverOnce(ctx); err != nil {
		t.Fatal(err)
	}
	after, err := store.GetOfficeTask(ctx, task.ID)
	if err != nil || after.Status != "interrupted" {
		t.Fatalf("orphan recovery: %+v %v", after, err)
	}
	started, release, ended := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		ended <- s.Execute(ctx, task.ID, "running", func(context.Context) error { close(started); <-release; return nil })
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("new run not started")
	}
	if err = s.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	after, err = store.GetOfficeTask(ctx, task.ID)
	if err != nil || after.Status != "running" {
		t.Fatalf("owned run recovered as orphan: %+v %v", after, err)
	}
	close(release)
	if err = <-ended; err != nil {
		t.Fatal(err)
	}
}

func TestOfficeLifecycleRejectsForeignCancelAndDuplicateWriter(t *testing.T) {
	s, store, task := studioServiceFixture(t)
	ctx := context.Background()
	started, release, ended := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		ended <- s.Execute(ctx, task.ID, "running", func(context.Context) error { close(started); <-release; return nil })
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("run not started")
	}
	if err := s.Execute(ctx, task.ID, "running", func(context.Context) error { t.Error("duplicate writer executed"); return nil }); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("concurrent writer: %v", err)
	}
	foreign := domain.WithScope(ctx, ulid.Make().String())
	if err := s.Cancel(foreign, task.ID); !errors.Is(err, org.ErrCrossOrgAccess) {
		t.Fatalf("foreign cancel: %v", err)
	}
	if err := s.Recover(foreign); err != nil {
		t.Fatal(err)
	}
	after, err := store.GetOfficeTask(ctx, task.ID)
	if err != nil || after.Status != "running" {
		t.Fatalf("foreign recovery touched task: %+v %v", after, err)
	}
	close(release)
	if err = <-ended; err != nil {
		t.Fatal(err)
	}
}

type recoveryFailOnce struct {
	domain.Store
	loader interface {
		ListOfficeActiveTasks(context.Context, int) ([]domain.Task, error)
	}
	calls atomic.Int32
}

func (r *recoveryFailOnce) ListOfficeActiveTasks(ctx context.Context, limit int) ([]domain.Task, error) {
	if r.calls.Add(1) == 1 {
		return nil, context.DeadlineExceeded
	}
	return r.loader.ListOfficeActiveTasks(ctx, limit)
}

func TestOfficeLifecycleRecoveryOnceRetriesFailureThenCoalesces(t *testing.T) {
	s, store, _ := studioServiceFixture(t)
	ctx := context.Background()
	r := &recoveryFailOnce{Store: store, loader: store}
	s.Store = r
	if err := s.RecoverOnce(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected first error: %v", err)
	}
	for i := 0; i < 4; i++ {
		if err := s.RecoverOnce(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if r.calls.Load() != 2 {
		t.Fatalf("recovery rescanned completed scope %d times", r.calls.Load())
	}
}

type cancelAdvanceStore struct {
	domain.Store
	newRunID string
	swapped  bool
}

func (s *cancelAdvanceStore) UpdateOfficeTask(ctx context.Context, t domain.Task, revision int64) (domain.Task, error) {
	if t.Status == "cancelling" && !s.swapped {
		s.swapped = true
		current, err := s.GetOfficeTask(ctx, t.ID)
		if err != nil {
			return t, err
		}
		for _, status := range []string{"succeeded", "queued", "running"} {
			current.Status = status
			if status == "queued" {
				current.RunID = s.newRunID
			}
			current, err = s.Store.UpdateOfficeTask(ctx, current, current.Revision)
			if err != nil {
				return t, err
			}
		}
		return t, domain.ErrConflict
	}
	return s.Store.UpdateOfficeTask(ctx, t, revision)
}

func TestOfficeLifecycleLateCancelCannotCancelReplacementRun(t *testing.T) {
	s, store, task := studioServiceFixture(t)
	ctx := context.Background()
	task.Status = "running"
	task.RunID = ulid.Make().String()
	var err error
	task, err = store.UpdateOfficeTask(ctx, task, task.Revision)
	if err != nil {
		t.Fatal(err)
	}
	replacement := ulid.Make().String()
	var cancelled atomic.Bool
	s.Store = &cancelAdvanceStore{Store: store, newRunID: replacement}
	s.runs[task.ID] = officeOperation{id: replacement, cancel: func() { cancelled.Store(true) }}
	if err = s.Cancel(ctx, task.ID); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("late cancellation: %v", err)
	}
	if cancelled.Load() {
		t.Fatal("old cancellation reached replacement worker")
	}
	actual, err := store.GetOfficeTask(ctx, task.ID)
	if err != nil || actual.RunID != replacement || actual.Status != "running" {
		t.Fatalf("replacement state: %+v %v", actual, err)
	}
}
