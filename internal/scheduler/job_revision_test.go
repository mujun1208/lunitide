package scheduler

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func TestAutomationJobCreationReplayAndConcurrentEdit(t *testing.T) {
	store := newTestStore(t)
	job, err := store.PutJobVersioned(validJob("stable", "*/5 * * * *"), "")
	if err != nil {
		t.Fatal(err)
	}
	replay, err := store.PutJobVersioned(job, "")
	if err != nil || JobRevision(replay) != JobRevision(job) {
		t.Fatalf("creation replay: %v", err)
	}
	if err := store.TouchLastRun(job.ID, time.Now()); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, name := range []string{"editor one", "editor two"} {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			candidate := job
			candidate.Name = name
			_, err := store.PutJobVersioned(candidate, JobRevision(job))
			results <- err
		}(name)
	}
	wg.Wait()
	close(results)
	success, conflict := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, ErrJobConflict) {
			conflict++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("concurrent writes %d/%d", success, conflict)
	}
	saved, _, _ := store.GetJob(job.ID)
	if saved.LastRunAt.IsZero() {
		t.Fatal("edit lost independent execution timestamp")
	}
	if _, err := store.PutJobVersioned(job, ""); !errors.Is(err, ErrJobConflict) {
		t.Fatal("changed payload reused creation key")
	}
}

func TestSchedulerReplansEditedScheduleWithoutFiringStaleTime(t *testing.T) {
	store := newTestStore(t)
	job, err := store.PutJobVersioned(validJob("reschedule", "*/5 * * * *"), "")
	if err != nil {
		t.Fatal(err)
	}
	s := New(store, nil, noopNotifier{})
	defer s.Close()
	now := time.Now().UTC()
	s.replan(now)
	s.nextFire[job.ID] = now.Add(-time.Minute)
	job.Cron = "at:" + now.Add(time.Hour).Format(time.RFC3339)
	if _, err := store.PutJobVersioned(job, JobRevision(func() Job { j, _, _ := store.GetJob(job.ID); return j }())); err != nil {
		t.Fatal(err)
	}
	if due := s.dueJobs(now); len(due) != 0 {
		t.Fatal("old scheduled time executed changed job")
	}
}
