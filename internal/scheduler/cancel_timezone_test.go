package scheduler

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestCancelRunOnlyStopsMatchedExecutionAndKeepsPeriodicSchedule(t *testing.T) {
	store := newTestStore(t)
	first, second := validJob("first", "0 8 * * *"), validJob("second", "0 8 * * *")
	second.ID = "01ARZ3NDEKTSV4RRFFQ69G5FAY"
	for _, job := range []Job{first, second} {
		if err := store.PutJob(job); err != nil {
			t.Fatal(err)
		}
	}
	entered := make(chan string, 3)
	s := New(store, func(ctx context.Context, j Job) Outcome {
		entered <- j.ID
		<-ctx.Done()
		return Outcome{Summary: "已完成前半部分", Err: ctx.Err()}
	}, noopNotifier{})
	defer s.Close()
	for _, job := range []Job{first, second} {
		if err := s.TriggerNow(job.ID); err != nil {
			t.Fatal(err)
		}
	}
	for range 2 {
		select {
		case <-entered:
		case <-time.After(time.Second):
			t.Fatal("executor did not start")
		}
	}
	runs, _ := store.ListRuns(first.ID, 10)
	id := runs[0].ID
	if s.CancelRun(second.ID, id) || s.CancelRun(first.ID, "stale-id") {
		t.Fatal("cancelled wrong execution")
	}
	if !s.CancelRun(first.ID, id) {
		t.Fatal("matched cancellation refused")
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		runs, _ = store.ListRuns(first.ID, 10)
		s.mu.Lock()
		_, active := s.controls[first.ID]
		s.mu.Unlock()
		if len(runs) > 0 && runs[0].Cancelled && !active {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if !runs[0].Cancelled || runs[0].Summary != "已完成前半部分" || !runs[0].OutcomeUnknown || runs[0].FinishedAt.IsZero() {
		t.Fatalf("lost cancellation receipt: %+v", runs)
	}
	stored, _, _ := store.GetJob(first.ID)
	if !stored.Enabled {
		t.Fatal("stopping one occurrence disabled the periodic job")
	}
	secondRuns, _ := store.ListRuns(second.ID, 10)
	if secondRuns[0].State != RunRunning {
		t.Fatal("unrelated task stopped")
	}
	if s.CancelRun(first.ID, id) {
		t.Fatal("finished execution cancelled again")
	}
	// Manual retry is a new occurrence; an old stop button cannot cancel it.
	if err := s.TriggerNow(first.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("manual retry not started")
	}
	if s.CancelRun(first.ID, id) {
		t.Fatal("stale stop cancelled retry")
	}
}

func TestCancelOneShotDoesNotReplayPastOccurrence(t *testing.T) {
	store := newTestStore(t)
	job := validJob("once", "at:2026-01-01T00:00:00Z")
	if err := store.PutJob(job); err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	s := New(store, func(ctx context.Context, _ Job) Outcome { close(entered); <-ctx.Done(); return Outcome{Err: ctx.Err()} }, noopNotifier{})
	defer s.Close()
	if err := s.TriggerNow(job.ID); err != nil {
		t.Fatal(err)
	}
	<-entered
	runs, _ := store.ListRuns(job.ID, 10)
	if !s.CancelRun(job.ID, runs[0].ID) {
		t.Fatal("cancel failed")
	}
	s.wg.Wait()
	if got := s.dueJobs(time.Now().Add(time.Hour)); len(got) != 0 {
		t.Fatalf("cancelled one-shot replayed: %+v", got)
	}
}

func TestJobTimezoneKeepsLegacyUTCAndUsesIANAOffset(t *testing.T) {
	now := time.Date(2026, 9, 7, 23, 0, 0, 0, time.UTC)
	job := validJob("eight", "0 8 * * *")
	legacy, err := nextJobFireTime(job, now)
	if err != nil || !legacy.Equal(time.Date(2026, 9, 8, 8, 0, 0, 0, time.UTC)) {
		t.Fatalf("legacy moved: %v %v", legacy, err)
	}
	job.Timezone = "Asia/Shanghai"
	local, err := nextJobFireTime(job, now)
	if err != nil || !local.Equal(time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("local 08:00: %v %v", local, err)
	}
	job.Cron = "at:2026-09-08T12:00:00+08:00"
	at, err := nextJobFireTime(job, now)
	if err != nil || !at.Equal(time.Date(2026, 9, 8, 4, 0, 0, 0, time.UTC)) {
		t.Fatalf("absolute one-shot shifted: %v %v", at, err)
	}
	for _, zone := range []string{"Local", "Not/A_Zone", "../UTC"} {
		job.Timezone = zone
		if err := ValidateJob(job); !errors.Is(err, ErrInvalid) {
			t.Fatalf("accepted timezone %q: %v", zone, err)
		}
	}
}

func TestJobTimezoneDSTUsesRealInstantsWithoutInventingMissingHour(t *testing.T) {
	job := validJob("DST", "30 2 * * *")
	job.Timezone = "America/New_York"
	// 02:30 does not exist on March 8, 2026; skip that day's missing time.
	got, err := nextJobFireTime(job, time.Date(2026, 3, 8, 5, 0, 0, 0, time.UTC))
	if err != nil || !got.Equal(time.Date(2026, 3, 9, 6, 30, 0, 0, time.UTC)) {
		t.Fatalf("spring: %v %v", got, err)
	}
	// Classic cron matches real minute instants. The repeated fall-back hour
	// has two distinct 01:30 instants; document and verify this policy.
	job.Cron = "30 1 * * *"
	first, err := nextJobFireTime(job, time.Date(2026, 11, 1, 4, 0, 0, 0, time.UTC))
	second, err2 := nextJobFireTime(job, first)
	if err != nil || err2 != nil || !first.Equal(time.Date(2026, 11, 1, 5, 30, 0, 0, time.UTC)) || !second.Equal(time.Date(2026, 11, 1, 6, 30, 0, 0, time.UTC)) {
		t.Fatalf("fall: %v %v %v %v", first, second, err, err2)
	}
}
