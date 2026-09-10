package scheduler

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
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
		validJob("", "* * * * *"), validJob("n", "bad cron"), validJob("n", "* * * * *"),
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
//
// A run notifies near the end of its goroutine, after the executor returns,
// after the run row is appended and after the fire hooks. Waiting on any of
// those earlier signals and then reading rows is a race the test loses on a
// loaded machine, so waitFor is the synchronisation point for anything the
// notification follows.
type captureNotifier struct {
	mu   sync.Mutex
	rows []string
	sig  chan struct{}
}

func newCaptureNotifier() *captureNotifier {
	return &captureNotifier{sig: make(chan struct{}, 8)}
}

func (c *captureNotifier) Notify(title, body string) error {
	c.mu.Lock()
	c.rows = append(c.rows, title+"|"+body)
	c.mu.Unlock()
	// Non-blocking so a notifier built without a channel, or one that
	// outruns its reader, never stalls the scheduler.
	select {
	case c.sig <- struct{}{}:
	default:
	}
	return nil
}

func (c *captureNotifier) len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.rows)
}

func (c *captureNotifier) waitFor(t *testing.T, n int) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for c.len() < n {
		select {
		case <-c.sig:
		case <-deadline:
			t.Fatalf("waited for %d notifications, saw %d", n, c.len())
		}
	}
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
	notify := newCaptureNotifier()
	s := New(store, executor, notify)
	t.Cleanup(s.Close)

	// Seed nextFire so the job is due right now.
	now := time.Now().UTC().Truncate(time.Minute)
	s.mu.Lock()
	s.nextFire[job.ID] = now.Add(-time.Minute)
	s.schedules[job.ID] = job.Cron + "/" + job.UpdatedAt.Format(time.RFC3339Nano)
	s.mu.Unlock()

	s.fireDue(now)
	// Immediately firing again must not double-launch (single-flight); the
	// executor signals exactly once below.
	s.fireDue(now.Add(time.Second))

	select {
	case <-ready:
	case <-time.After(2 * time.Second):
		t.Fatal("due execution did not start")
	}
	// Wait for the async finalize (run row + notify + reschedule).
	notify.waitFor(t, 1)
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
	// A run clears itself from the running set in a deferred call, which is
	// the one step that happens after the notification waited on above.
	for deadline := time.Now().Add(2 * time.Second); len(snap.RunningJobs) != 0 && time.Now().Before(deadline); snap = s.Snapshot() {
		time.Sleep(5 * time.Millisecond)
	}
	if len(snap.RunningJobs) != 0 {
		t.Fatalf("running jobs leaked: %+v", snap.RunningJobs)
	}
}

func TestSchedulerFailureRunNotifiesFailure(t *testing.T) {
	store := newTestStore(t)
	job := validJob("failing", "*/5 * * * *")
	_ = store.PutJob(job)
	notify := newCaptureNotifier()
	s := New(store, func(context.Context, Job) Outcome {
		return Outcome{Err: errors.New("模型网关超时"), NotStarted: true}
	}, notify)
	t.Cleanup(s.Close)
	now := time.Now().UTC().Truncate(time.Minute)
	s.mu.Lock()
	s.nextFire[job.ID] = now.Add(-time.Minute)
	s.schedules[job.ID] = job.Cron + "/" + job.UpdatedAt.Format(time.RFC3339Nano)
	s.mu.Unlock()
	s.fireDue(now)
	// The run appends its row before it notifies, so this one wait covers
	// both assertions. The fire hook this used to wait on runs earlier
	// still, which is why the notification was routinely not there yet.
	notify.waitFor(t, 1)
	if notify.len() != 1 || !strings.Contains(notify.rows[0], "失败") {
		t.Fatalf("notify rows = %+v", notify.rows)
	}
	runs, _ := store.ListRuns(job.ID, 10)
	if runs[0].State != RunFailed || !strings.Contains(runs[0].Error, "超时") {
		t.Fatalf("failed run = %+v", runs[0])
	}
}

func TestOutcomeUnknownDoesNotClaimFailureOrFanOutWebhook(t *testing.T) {
	store := newTestStore(t)
	job := validJob("unknown", "*/5 * * * *")
	job.WebhookURL = "https://example.com/automation-hook"
	if err := store.PutJob(job); err != nil {
		t.Fatal(err)
	}
	notify := newCaptureNotifier()
	webhook := newCaptureNotifier()
	orig := openWebhookNotifier
	openWebhookNotifier = func(raw string) (Notifier, error) {
		if raw != job.WebhookURL {
			t.Fatalf("webhook url %q", raw)
		}
		return webhook, nil
	}
	t.Cleanup(func() { openWebhookNotifier = orig })
	s := New(store, func(context.Context, Job) Outcome {
		return Outcome{Summary: "已写一半", Err: errors.New("回执丢失")}
	}, notify)
	t.Cleanup(s.Close)
	now := time.Now().UTC().Truncate(time.Minute)
	s.mu.Lock()
	s.nextFire[job.ID] = now.Add(-time.Minute)
	s.schedules[job.ID] = job.Cron + "/" + job.UpdatedAt.Format(time.RFC3339Nano)
	s.mu.Unlock()
	s.fireDue(now)
	notify.waitFor(t, 1)
	if notify.len() != 1 || !strings.Contains(notify.rows[0], "待核对") || strings.Contains(notify.rows[0], "失败") || strings.Contains(notify.rows[0], "已完成") {
		t.Fatalf("unknown outcome toast = %+v", notify.rows)
	}
	if webhook.len() != 0 {
		t.Fatalf("unknown outcome must not blind-send webhook: %+v", webhook.rows)
	}
	runs, err := store.ListRuns(job.ID, 10)
	if err != nil || len(runs) == 0 || !runs[0].OutcomeUnknown || runs[0].State != RunFailed {
		t.Fatalf("receipt: %+v %v", runs, err)
	}
}

func TestSameDueJobHundredFiresIsSingleDispatch(t *testing.T) {
	store := newTestStore(t)
	job := validJob("burst", "*/5 * * * *")
	if err := store.PutJob(job); err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	s := New(store, func(context.Context, Job) Outcome {
		if calls.Add(1) == 1 {
			close(started)
		}
		<-release
		return Outcome{Summary: "一次即可"}
	}, noopNotifier{})
	t.Cleanup(s.Close)
	now := time.Now().UTC().Truncate(time.Minute)
	s.mu.Lock()
	s.nextFire[job.ID] = now.Add(-time.Minute)
	s.schedules[job.ID] = job.Cron + "/" + job.UpdatedAt.Format(time.RFC3339Nano)
	s.mu.Unlock()
	var wg sync.WaitGroup
	for range 100 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.fireDue(now)
		}()
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("due job never started")
	}
	wg.Wait()
	close(release)
	s.wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("same due slot launched %d times", calls.Load())
	}
	runs, err := store.LatestRuns(job.ID)
	if err != nil || len(runs) != 1 {
		t.Fatalf("want one dispatch, got %+v %v", runs, err)
	}
}

func TestSleepMissCatchesCronUpOnce(t *testing.T) {
	store := newTestStore(t)
	job := validJob("sleep", "*/5 * * * *")
	if err := store.PutJob(job); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	notify := newCaptureNotifier()
	s := New(store, func(context.Context, Job) Outcome {
		calls.Add(1)
		return Outcome{Summary: "补一次"}
	}, notify)
	t.Cleanup(s.Close)
	now := time.Now().UTC().Truncate(time.Minute)
	s.mu.Lock()
	s.nextFire[job.ID] = now.Add(-8 * time.Hour)
	s.schedules[job.ID] = job.Cron + "/" + job.UpdatedAt.Format(time.RFC3339Nano)
	s.mu.Unlock()
	s.fireDue(now)
	notify.waitFor(t, 1)
	s.fireDue(now)
	s.fireDue(now.Add(time.Second))
	if calls.Load() != 1 {
		t.Fatalf("sleep miss replayed %d times", calls.Load())
	}
}

func TestPastAtReminderStillFiresAfterReplan(t *testing.T) {
	store := newTestStore(t)
	job := validJob("once", "at:2026-01-01T00:00:00Z")
	if err := store.PutJob(job); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	notify := newCaptureNotifier()
	s := New(store, func(context.Context, Job) Outcome {
		calls.Add(1)
		return Outcome{Summary: "到期提醒"}
	}, notify)
	t.Cleanup(s.Close)
	now := time.Now().UTC()
	s.replan(now)
	s.fireDue(now)
	notify.waitFor(t, 1)
	if calls.Load() != 1 {
		t.Fatalf("past at: reminder calls=%d", calls.Load())
	}
	stored, _, err := store.GetJob(job.ID)
	if err != nil || stored.Enabled {
		t.Fatalf("one-shot must disable after fire: %+v %v", stored, err)
	}
	s.replan(now.Add(time.Hour))
	s.fireDue(now.Add(time.Hour))
	if calls.Load() != 1 {
		t.Fatalf("one-shot replayed after replan: %d", calls.Load())
	}
}

func TestNilNotifierIsQuiet(t *testing.T) {
	s := New(newTestStore(t), nil, nil)
	t.Cleanup(s.Close)
	if _, ok := s.notify.(noopNotifier); !ok {
		t.Fatalf("nil notifier must stay quiet, got %T", s.notify)
	}
}

func TestTypedTriggerIsNotFiredByCronTick(t *testing.T) {
	store := newTestStore(t)
	job := validJob("watch-files", "* * * * *")
	job.TriggerKind = TriggerFileSetChanged
	job.TriggerSpec = t.TempDir()
	if err := store.PutJob(job); err != nil {
		t.Fatal(err)
	}
	fired := 0
	s := New(store, func(context.Context, Job) Outcome {
		fired++
		return Outcome{Summary: "should not run"}
	}, nil)
	t.Cleanup(s.Close)
	now := time.Now().UTC()
	s.mu.Lock()
	s.nextFire[job.ID] = now.Add(-time.Minute)
	s.schedules[job.ID] = scheduleKey(job)
	s.mu.Unlock()
	s.fireDue(now)
	time.Sleep(50 * time.Millisecond)
	if fired != 0 {
		t.Fatalf("typed trigger must not ride the cron tick, fired=%d", fired)
	}
}

func TestTriggerNowRejectsUnknownAndConcurrent(t *testing.T) {
	store := newTestStore(t)
	job := validJob("manual", "*/5 * * * *")
	_ = store.PutJob(job)
	block := make(chan struct{})
	started := make(chan struct{})
	s := New(store, func(ctx context.Context, _ Job) Outcome {
		close(started)
		select {
		case <-block:
		case <-ctx.Done():
		}
		return Outcome{}
	}, &captureNotifier{})
	t.Cleanup(s.Close)
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
	t.Cleanup(s.Close)
	now := time.Now().UTC().Truncate(time.Minute)
	s.mu.Lock()
	s.nextFire[job.ID] = now.Add(-time.Minute)
	s.schedules[job.ID] = job.Cron + "/" + job.UpdatedAt.Format(time.RFC3339Nano)
	s.mu.Unlock()
	s.fireDue(now)
	if called != 0 {
		t.Fatal("disabled job fired")
	}
}

func TestRunContextDispatchKeyMatchesRun(t *testing.T) {
	store := newTestStore(t)
	job := validJob("ctx", "*/5 * * * *")
	_ = store.PutJob(job)
	got := make(chan RunContext, 1)
	s := New(store, nil, noopNotifier{})
	s.SetContextualExecutor(func(_ context.Context, j Job, rc RunContext) Outcome {
		if j.ID != job.ID {
			t.Errorf("job id %q", j.ID)
		}
		got <- rc
		return Outcome{}
	})
	t.Cleanup(s.Close)
	now := time.Now().UTC().Truncate(time.Minute)
	s.mu.Lock()
	s.nextFire[job.ID] = now.Add(-time.Minute)
	s.schedules[job.ID] = job.Cron + "/" + job.UpdatedAt.Format(time.RFC3339Nano)
	s.mu.Unlock()
	if err := s.TriggerNow(job.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case rc := <-got:
		if rc.RunID == "" || rc.DispatchKey != job.ID+":"+rc.RunID || rc.SessionID != job.SessionID || rc.Recover {
			t.Fatalf("%+v", rc)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("contextual executor was not invoked")
	}
}

func TestPsQuoteNeutralizesInjection(t *testing.T) {
	got := psQuote("a'; Remove-Item C:\\ -Recurse; 'b")
	// 2 embedded quotes doubled to 4, plus the 2 wrapping quotes = 6.
	if strings.Count(got, "'") != 6 || !strings.Contains(got, "''") {
		t.Fatalf("psQuote = %q", got)
	}
}

func TestUserMessageDropsEnglish(t *testing.T) {
	if got := UserMessage("context deadline exceeded"); !strings.Contains(got, "超时") || strings.Contains(got, "deadline") {
		t.Fatalf("deadline: %q", got)
	}
	if got := UserMessage("sql: database is locked"); strings.Contains(got, "sql:") || !strings.Contains(got, "未完成") {
		t.Fatalf("unknown english: %q", got)
	}
	if got := UserMessage("自动化对话执行失败"); got != "自动化对话执行失败" {
		t.Fatalf("keep chinese: %q", got)
	}
}
