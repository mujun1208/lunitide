package scheduler

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func validJob(name, cron string) Job {
	return Job{Name: name, Cron: cron, Prompt: "生成日报",
		ProviderID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", ModelID: "gpt-test",
		SessionID: "01ARZ3NDEKTSV4RRFFQ69G5FAW", Enabled: true,
		ID: "01ARZ3NDEKTSV4RRFFQ69G5FAX"}
}

func TestStoreJobCRUDAndValidation(t *testing.T) {
	s := newTestStore(t)
	if err := s.PutJob(validJob("日报", "30 8 * * *")); err != nil {
		t.Fatal(err)
	}
	jobs, err := s.ListJobs()
	if err != nil || len(jobs) != 1 || jobs[0].Name != "日报" {
		t.Fatalf("jobs = %+v %v", jobs, err)
	}
	// update in place
	updated := validJob("日报v2", "0 9 * * *")
	if err := s.PutJob(updated); err != nil {
		t.Fatal(err)
	}
	jobs, _ = s.ListJobs()
	if len(jobs) != 1 || jobs[0].Name != "日报v2" {
		t.Fatalf("update not in place: %+v", jobs)
	}
	// invalid fields refused
	for _, bad := range []Job{
		validJob("", "* * * * *"), validJob("n", "bad cron"), validJob("n", "* * * * *", ),
	} {
		bad.Prompt = ""
		if err := s.PutJob(bad); err == nil {
			t.Fatal("invalid job accepted")
		}
	}
	if err := s.DeleteJob(updated.ID); err != nil {
		t.Fatal(err)
	}
	jobs, _ = s.ListJobs()
	if len(jobs) != 0 {
		t.Fatalf("delete failed: %+v", jobs)
	}
}

func TestStoreRunAppendListAndTrim(t *testing.T) {
	s := newTestStore(t)
	job := validJob("j", "* * * * *")
	_ = s.PutJob(job)
	for i := 0; i < MaxRunsPerJob+5; i++ {
		if err := s.AppendRun(Run{ID: string(rune('a'+i%26)) + time.Now().Format("150405.000000000"), JobID: job.ID, State: RunSucceeded, Summary: strings.Repeat("x", 600), StartedAt: time.Now()}); err != nil {
			t.Fatal(err)
		}
	}
	runs, err := s.ListRuns(job.ID, 500)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != MaxRunsPerJob {
		t.Fatalf("trim kept %d, want %d", len(runs), MaxRunsPerJob)
	}
	// long summary was bounded with the ellipsis marker
	if len([]rune(runs[0].Summary)) > maxSummaryRunes+1 {
		t.Fatalf("summary unbounded: %d", len([]rune(runs[0].Summary)))
	}
	// other-job isolation
	other := validJob("o", "* * * * *")
	other.ID = "01ARZ3NDEKTSV4RRFFQ69G5FAZ"
	_ = s.AppendRun(Run{ID: "other1", JobID: other.ID, State: RunRunning, StartedAt: time.Now()})
	all, _ := s.ListRuns("", 500)
	found := 0
	for _, r := range all {
		if r.JobID == other.ID {
			found++
		}
	}
	if found != 1 {
		t.Fatalf("other job runs = %d", found)
	}
}

// captureNotifier records notifications for assertions.
type captureNotifier struct {
	mu   sync.Mutex
	rows []string
}

func (c *captureNotifier) Notify(title, body string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.rows = append(c.rows, title+"|"+body)
	return nil
}

func (c *captureNotifier) len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.rows)
}

func TestSchedulerFiresDueJobSingleFlightNotifiesAndPersists(t *testing.T) {
	store := newTestStore(t)
	job := validJob("日报", "*/5 * * * *") // every 5th minute
	if err := store.PutJob(job); err != nil {
		t.Fatal(err)
	}
	var calls sync.Map
	ready := make(chan struct{}, 4)
	executor := func(_ context.Context, j Job) Outcome {
		n := 0
		if v, ok := calls.Load(j.ID); ok {
			n = v.(int)
		}
		calls.Store(j.ID, n+1)
		ready <- struct{}{}
		return Outcome{Summary: "日报完成\n明细略", TotalTokens: 42}
	}
	notify := &captureNotifier{}
	s := New(store, executor, notify)

	// Seed nextFire so the job is due right now.
	now := time.Now().UTC().Truncate(time.Minute)
	s.mu.Lock()
	s.nextFire[job.ID] = now.Add(-time.Minute)
	s.mu.Unlock()

	s.fireDue(now)
	// Immediately firing again must not double-launch (single-flight); the
	// executor signals exactly once below.
	s.fireDue(now.Add(time.Second))

	<-ready
	// Wait for the async finalize (run row + notify + reschedule).
	deadline := time.Now().Add(2 * time.Second)
	for notify.len() < 1 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got, ok := calls.Load(job.ID); !ok || got.(int) != 1 {
		t.Fatalf("executor calls = %v, want exactly 1", got)
	}
	if notify.len() != 1 || !strings.Contains(notify.rows[0], "已完成") {
		t.Fatalf("notify rows = %+v", notify.rows)
	}
	runs, err := store.ListRuns(job.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	// running row + finished row
	if len(runs) != 2 || runs[0].State != RunSucceeded || runs[0].TotalTokens != 42 {
		t.Fatalf("runs = %+v", runs)
	}
	if runs[1].StartedAt.IsZero() || runs[1].State != RunRunning {
		t.Fatalf("running row missing: %+v", runs[1])
	}
	// rescheduled into the future
	snap := s.Snapshot()
	next := snap.NextFire[job.ID]
	if next == "" {
		t.Fatal("nextFire missing after run")
	}
	parsed, err := time.Parse(time.RFC3339, next)
	if err != nil || !parsed.After(now) {
		t.Fatalf("nextFire = %s (%v)", next, err)
	}
	if len(snap.RunningJobs) != 0 {
		t.Fatalf("running jobs leaked: %+v", snap.RunningJobs)
	}
}

func TestSchedulerFailureRunNotifiesFailure(t *testing.T) {
	store := newTestStore(t)
	job := validJob("failing", "*/5 * * * *")
	_ = store.PutJob(job)
	notify := &captureNotifier{}
	done := make(chan struct{})
	s := New(store, func(context.Context, Job) Outcome {
		return Outcome{Err: errors.New("模型网关超时")}
	}, notify)
	s.fireHooks = append(s.fireHooks, func(string, string, Outcome) { close(done) })
	now := time.Now().UTC().Truncate(time.Minute)
	s.mu.Lock()
	s.nextFire[job.ID] = now.Add(-time.Minute)
	s.mu.Unlock()
	s.fireDue(now)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("run did not finish")
	}
	if notify.len() != 1 || !strings.Contains(notify.rows[0], "失败") {
		t.Fatalf("notify rows = %+v", notify.rows)
	}
	runs, _ := store.ListRuns(job.ID, 10)
	if runs[0].State != RunFailed || !strings.Contains(runs[0].Error, "超时") {
		t.Fatalf("failed run = %+v", runs[0])
	}
}

func TestTriggerNowRejectsUnknownAndConcurrent(t *testing.T) {
	store := newTestStore(t)
	job := validJob("manual", "*/5 * * * *")
	_ = store.PutJob(job)
	block := make(chan struct{})
	started := make(chan struct{})
	s := New(store, func(context.Context, Job) Outcome {
		close(started)
		<-block
		return Outcome{}
	}, &captureNotifier{})
	if err := s.TriggerNow("01ARZ3NDEKTSV4RRFFQ69G5FAQQ"); err == nil {
		t.Fatal("unknown job triggered")
	}
	if err := s.TriggerNow(job.ID); err != nil {
		t.Fatal(err)
	}
	<-started
	if err := s.TriggerNow(job.ID); err == nil || !strings.Contains(err.Error(), "already running") {
		t.Fatalf("concurrent trigger = %v", err)
	}
	close(block)
}

func TestDisabledJobNeverFires(t *testing.T) {
	store := newTestStore(t)
	job := validJob("off", "*/5 * * * *")
	job.Enabled = false
	_ = store.PutJob(job)
	called := 0
	s := New(store, func(context.Context, Job) Outcome { called++; return Outcome{} }, &captureNotifier{})
	now := time.Now().UTC().Truncate(time.Minute)
	s.mu.Lock()
	s.nextFire[job.ID] = now.Add(-time.Minute)
	s.mu.Unlock()
	s.fireDue(now)
	if called != 0 {
		t.Fatal("disabled job fired")
	}
}

func TestPsQuoteNeutralizesInjection(t *testing.T) {
	got := psQuote("a'; Remove-Item C:\\ -Recurse; 'b")
	// 2 embedded quotes doubled to 4, plus the 2 wrapping quotes = 6.
	if strings.Count(got, "'") != 6 || !strings.Contains(got, "''") {
		t.Fatalf("psQuote = %q", got)
	}
}
