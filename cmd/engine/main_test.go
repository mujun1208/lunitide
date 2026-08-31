package main

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/compactionapp"
	"github.com/lunitide/lunitide/internal/ipc"
	"github.com/lunitide/lunitide/internal/scheduler"
)

func TestAuthenticatedSessionEndKeepsEngine(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	shutdownAfterSession(nil, cancel)
	select {
	case <-ctx.Done():
		t.Fatal("clean session end must not cancel the engine")
	default:
	}
}

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
	shutdownAfterSession(nil, cancelEngine)
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
}

func TestACKWriteFailureTriggersShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	shutdownAfterSession(fmt.Errorf("write failed: %w", ipc.ErrHandshakeACK), cancel)
	select {
	case <-ctx.Done():
	default:
		t.Fatal("ACK write failure did not trigger Engine shutdown")
	}
}

func TestCompactionRecoveryErrorFailsClosed(t *testing.T) {
	internal := fmt.Errorf("database path C:/private/lunitide.db")
	if err := compactionRecoveryError(nil, internal); !errors.Is(err, internal) {
		t.Fatalf("top-level recovery error not propagated: %v", err)
	}
	results := []compactionapp.RecoveryResult{{CheckpointID: "checkpoint-1", Err: internal}}
	if err := compactionRecoveryError(results, nil); !errors.Is(err, internal) {
		t.Fatalf("per-checkpoint recovery error not propagated: %v", err)
	}
	if err := compactionRecoveryError([]compactionapp.RecoveryResult{{CheckpointID: "checkpoint-1"}}, nil); err != nil {
		t.Fatalf("successful recovery rejected: %v", err)
	}
}
