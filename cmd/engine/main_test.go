package main

import (
	"context"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/bootstrap"
	"github.com/lunitide/lunitide/internal/scheduler"
)

func TestSchedulerSurvivesSessionLeave(t *testing.T) {
	engineCtx, cancelEngine := context.WithCancel(context.Background())
	defer cancelEngine()
	store, err := scheduler.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fired := make(chan struct{}, 1)
	sched := scheduler.New(store, func(context.Context, scheduler.Job) scheduler.Outcome {
		select {
		case fired <- struct{}{}:
		default:
		}
		return scheduler.Outcome{Summary: "ok"}
	}, nil)
	sched.Start(engineCtx)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && !sched.Snapshot().Running {
		time.Sleep(10 * time.Millisecond)
	}
	bootstrap.ShutdownAfterSession(nil, cancelEngine)
	if !sched.Snapshot().Running {
		t.Fatal("G2: session leave must leave the cron scheduler running")
	}
	job := scheduler.Job{
		ID: "01ARZ3NDEKTSV4RRFFQ69G5FAX", Name: "关窗仍跑", Cron: "* * * * *",
		Prompt: "ping", ProviderID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", ModelID: "m",
		SessionID: "01ARZ3NDEKTSV4RRFFQ69G5FAW", Enabled: true,
	}
	if err := store.PutJob(job); err != nil {
		t.Fatal(err)
	}
	if err := sched.TriggerNow(job.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case <-fired:
	case <-time.After(2 * time.Second):
		t.Fatal("G2: scheduler must still execute after session leave")
	}
	// The executor signals `fired` from inside exec(), but the detached run
	// goroutine keeps writing run rows / rescheduling under <tmp>/automation
	// afterwards. Wait for it to fully drain before returning so t.TempDir()'s
	// RemoveAll does not race those writes (Windows: "directory is not empty").
	drain := time.Now().Add(2 * time.Second)
	for time.Now().Before(drain) && len(sched.Snapshot().RunningJobs) > 0 {
		time.Sleep(10 * time.Millisecond)
	}
	if n := len(sched.Snapshot().RunningJobs); n > 0 {
		t.Fatalf("G2: run goroutine did not drain: %d still running", n)
	}
}