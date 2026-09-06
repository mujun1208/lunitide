package scheduler

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
)

// These assert the required behavior. A failure is evidence of an unresolved
// defect, never a passing regression for the broken behavior.
func TestReviewValidJobsNeverCorruptReadBudget(t *testing.T) {
	store := newTestStore(t)
	for i := range MaxJobs {
		job := validJob("escaped prompt", "* * * * *")
		job.ID = ulid.Make().String()
		job.Prompt = strings.Repeat("&", maxPromptRunes)
		_, writeErr := store.PutJobVersioned(job, "")
		_, readErr := store.ListJobs()
		if readErr != nil {
			info, _ := os.Stat(store.jobs)
			t.Fatalf("valid write %d left unreadable store: write=%v read=%v bytes=%d", i+1, writeErr, readErr, info.Size())
		}
		if writeErr != nil {
			t.Fatalf("legal job %d refused before the advertised quota: %v", i+1, writeErr)
		}
	}
}

func TestReviewRunOnceDisableFailureNeverSchedulesAgain(t *testing.T) {
	for _, tc := range []struct {
		name string
		cron string
		once bool
		err  error
	}{
		{name: "at", cron: "at:2025-01-01T00:00:00Z"},
		{name: "cron once", cron: "* * * * *", once: true},
		{name: "uncertain recurring", cron: "* * * * *", err: errors.New("execution acknowledgement lost")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			testDisableFailure(t, tc.cron, tc.once, tc.err)
		})
	}
}

func testDisableFailure(t *testing.T, cron string, once bool, executionErr error) {
	t.Helper()
	store := newTestStore(t)
	input := validJob("once", cron)
	input.RunOnce = once
	job, err := store.PutJobVersioned(input, "")
	if err != nil {
		t.Fatal(err)
	}
	badPath := filepath.Join(t.TempDir(), "jobs-is-directory")
	if err := os.Mkdir(badPath, 0700); err != nil {
		t.Fatal(err)
	}
	original := store.jobs
	s := New(store, func(context.Context, Job) Outcome {
		// Start intent and TouchLastRun have committed; only final job disabling
		// now fails, while the separate run receipt still persists successfully.
		store.mu.Lock()
		store.jobs = badPath
		store.mu.Unlock()
		return Outcome{Summary: "isolated side effect completed", Err: executionErr}
	}, noopNotifier{})
	defer s.Close()
	if err := s.TriggerNow(job.ID); err != nil {
		t.Fatal(err)
	}
	s.wg.Wait()
	store.mu.Lock()
	store.jobs = original
	store.mu.Unlock()
	if s.Snapshot().LastError == "" {
		t.Fatal("fixture did not reach failed disable")
	}
	runs, err := store.ListRuns(job.ID, 10)
	if err != nil || len(runs) != 1 || runs[0].State != RunRunning {
		t.Fatalf("failed disable must retain durable unresolved intent, not a success receipt: %+v %v", runs, err)
	}
	if due := s.dueJobs(time.Now().UTC().Add(2 * time.Minute)); len(due) != 0 {
		t.Fatalf("completed one-shot became due again after failed disable: %+v", due)
	}
	reopened, err := NewStore(filepath.Dir(store.dir))
	if err != nil {
		t.Fatal(err)
	}
	if err := reopened.RecoverInterrupted(); err != nil {
		t.Fatal(err)
	}
	recovered, exists, err := reopened.GetJob(job.ID)
	if err != nil || !exists || recovered.Enabled {
		t.Fatalf("restart did not disable unresolved job: %+v %v", recovered, err)
	}
	runs, err = reopened.ListRuns(job.ID, 10)
	if err != nil || len(runs) != 2 || runs[0].State != RunFailed || !runs[0].OutcomeUnknown {
		t.Fatalf("restart must preserve an explicit uncertain result: %+v %v", runs, err)
	}
}

func TestReviewOversizeWritePreservesReadableJobs(t *testing.T) {
	store := newTestStore(t)
	job, err := store.PutJobVersioned(validJob("preserved", "* * * * *"), "")
	if err != nil {
		t.Fatal(err)
	}
	oversized := job
	oversized.Prompt = strings.Repeat("x", maxJobsFileBytes)
	if err := store.saveJobs([]Job{oversized}); err == nil {
		t.Fatal("oversize replacement accepted")
	}
	got, exists, err := store.GetJob(job.ID)
	if err != nil || !exists || JobRevision(got) != JobRevision(job) {
		t.Fatalf("failed replacement changed persisted job: %+v %v", got, err)
	}
}
