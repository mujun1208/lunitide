// Package scheduler drives Lunitide resident automation (P2-3): cron
// scheduled jobs fire headless executions through the injected executor (the
// app layer reuses the single chat kernel - never a second execution core),
// persist an append-only run log, emit Windows toast notifications on
// completion/failure, and expose a heartbeat the UI can poll. Storage stays
// file-based (settings-plane precedent): jobs.json atomically replaced,
// runs.jsonl append-only, both under the data root.
package scheduler

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"
	"unicode/utf16"

	"github.com/oklog/ulid/v2"

	"github.com/lunitide/lunitide/internal/cronexpr"
)

// Run states (frozen wire enum).
const (
	RunRunning   = "running"
	RunSucceeded = "succeeded"
	RunFailed    = "failed"
)

// Field limits mirroring the bridge validation layer.
const (
	MaxJobs          = 100
	MaxRunsPerJob    = 200
	maxNameRunes     = 64
	maxPromptRunes   = 8000
	maxSummaryRunes  = 500
	defaultTickEvery = 15 * time.Second
)

var ErrInvalid = errors.New("scheduler: invalid job")
var ErrBusy = errors.New("scheduler: job already running")
var ErrPersistence = errors.New("scheduler: durable execution record unavailable")
var ErrClosed = errors.New("scheduler: closed")

// Job is one cron scheduled automation bound to an existing session.
type Job struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	Cron          string    `json:"cron"`
	Prompt        string    `json:"prompt"`
	ProviderID    string    `json:"providerId"`
	ModelID       string    `json:"modelId"`
	SessionID     string    `json:"sessionId"`
	ExecutionMode string    `json:"executionMode"`
	SessionMode   string    `json:"sessionMode,omitempty"` // bound | isolated
	RunOnce       bool      `json:"runOnce,omitempty"`
	Enabled       bool      `json:"enabled"`
	WebhookURL    string    `json:"webhookUrl,omitempty"` // P3-1 optional IM fan-out (https IM custom-bot URL)
	LastRunAt     time.Time `json:"lastRunAt,omitempty"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

// Run is one execution record (append-only).
type Run struct {
	ID             string    `json:"id"`
	JobID          string    `json:"jobId"`
	JobName        string    `json:"jobName"`
	SessionID      string    `json:"sessionId,omitempty"`
	State          string    `json:"state"`
	Trigger        string    `json:"trigger"` // "cron" | "manual"
	Summary        string    `json:"summary,omitempty"`
	TotalTokens    int64     `json:"totalTokens"`
	Error          string    `json:"error,omitempty"`
	StartedAt      time.Time `json:"startedAt"`
	FinishedAt     time.Time `json:"finishedAt,omitempty"`
	OutcomeUnknown bool      `json:"outcomeUnknown,omitempty"`
}

// Outcome is what the headless executor answers for one fired job.
type Outcome struct {
	Summary     string
	TotalTokens int64
	Err         error
}

// Executor performs one headless run. The app layer implements it on top of
// the chat pipeline; the scheduler owns only timing, persistence, and
// notification.
type Executor func(ctx context.Context, job Job) Outcome

// Notifier surfaces run results to the user. Implementations must be safe
// for concurrent use and must never block the scheduler loop.
type Notifier interface {
	Notify(title, body string) error
}

// noopNotifier is the fallback on non-Windows hosts and in tests.
type noopNotifier struct{}

func (noopNotifier) Notify(string, string) error { return nil }

// NewPlatformNotifier answers a Windows toast notifier, or the noop one on
// other platforms (single-user desktop product ships on Windows).
func NewPlatformNotifier() Notifier {
	if runtime.GOOS != "windows" {
		return noopNotifier{}
	}
	return &toastNotifier{}
}

// toastNotifier raises a Windows toast via PowerShell + the built-in
// Windows.UI.Notifications WinRT API. The script travels as an
// -EncodedCommand blob (UTF-16LE base64) so titles/bodies cannot inject
// shell metacharacters.
type toastNotifier struct{}

func (t *toastNotifier) Notify(title, body string) error {
	if title == "" || len(title) > 120 || len(body) > 500 {
		return fmt.Errorf("scheduler: notify payload invalid")
	}
	script := fmt.Sprintf(`[Windows.UI.Notifications.ToastNotificationManager, Windows.UI.Notifications, ContentType = WindowsRuntime] | Out-Null
$template = [Windows.UI.Notifications.ToastNotificationManager]::GetTemplateContent([Windows.UI.Notifications.ToastTemplateType]::ToastText02)
$texts = $template.GetElementsByTagName('text')
$texts.Item(0).AppendChild($template.CreateTextNode('Lunitide')) | Out-Null
$texts.Item(1).AppendChild($template.CreateTextNode(%s)) | Out-Null
$toast = [Windows.UI.Notifications.ToastNotification]::new($template)
[Windows.UI.Notifications.ToastNotificationManager]::CreateToastNotifier('Lunitide').Show($toast)`, psQuote(body))
	units := utf16.Encode([]rune(script))
	raw := make([]byte, len(units)*2)
	for i, v := range units {
		binary.LittleEndian.PutUint16(raw[i*2:], v)
	}
	encoded := base64.StdEncoding.EncodeToString(raw)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-EncodedCommand", encoded)
	hideNotificationWindow(cmd)
	return cmd.Run()
}

// psQuote wraps s in single quotes doubling embedded ones.
func psQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// Status is the scheduler heartbeat snapshot the UI polls.
type Status struct {
	Running       bool              `json:"running"`
	StartedAt     time.Time         `json:"startedAt,omitempty"`
	LastHeartbeat time.Time         `json:"lastHeartbeat"`
	NextFire      map[string]string `json:"nextFire"` // jobID -> RFC3339
	RunningJobs   []string          `json:"runningJobs"`
	LastError     string            `json:"lastError,omitempty"`
}

// Scheduler owns timing, single-flight firing, persistence, and notify.
type Scheduler struct {
	schedules map[string]string
	store     *Store
	exec      Executor
	notify    Notifier

	mu         sync.Mutex
	ctx        context.Context
	cancel     context.CancelFunc
	closed     bool
	started    bool
	wg         sync.WaitGroup
	notifyGate chan struct{}
	nextFire   map[string]time.Time
	running    map[string]bool
	blocked    map[string]time.Time
	status     Status
	exprs      map[string]*cronexpr.Expression
	exprMu     sync.Mutex
	fireHooks  []func(jobID, trigger string, outcome Outcome) // tests
}

// New wires the scheduler; a nil notifier falls back to the platform one.
func New(store *Store, exec Executor, notifier Notifier) *Scheduler {
	if notifier == nil {
		notifier = NewPlatformNotifier()
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Scheduler{store: store, exec: exec, notify: notifier, ctx: ctx, cancel: cancel, notifyGate: make(chan struct{}, 1),
		schedules: map[string]string{}, nextFire: map[string]time.Time{}, running: map[string]bool{}, blocked: map[string]time.Time{},
		status: Status{NextFire: map[string]string{}, RunningJobs: []string{}}}
}

// SetExecutor swaps the executor after construction (the app layer builds
// the scheduler before the engine that the executor closes over). It must
// run before Start.
func (s *Scheduler) SetExecutor(exec Executor) { s.exec = exec }

// Store exposes the job/run store for bridge handlers.
func (s *Scheduler) Store() *Store { return s.store }

// Start launches the resident loop until ctx is cancelled. Missing nextFire
// entries are seeded from now (a restart never replays missed runs - the
// desktop product prefers quiet catch-up over burst execution).
func (s *Scheduler) Start(ctx context.Context) {
	s.mu.Lock()
	if s.closed || s.started {
		s.mu.Unlock()
		return
	}
	s.started = true
	s.status.Running = true
	s.status.StartedAt = time.Now().UTC()
	s.status.LastHeartbeat = s.status.StartedAt
	s.wg.Add(1)
	s.mu.Unlock()
	go func() {
		defer s.wg.Done()
		stop := context.AfterFunc(ctx, s.cancel)
		defer stop()
		s.replan(time.Now().UTC())
		tick := time.NewTicker(defaultTickEvery)
		defer tick.Stop()
		defer func() { s.mu.Lock(); s.status.Running = false; s.mu.Unlock() }()
		for {
			select {
			case <-s.ctx.Done():
				return
			case now := <-tick.C:
				s.mu.Lock()
				s.status.LastHeartbeat = now.UTC()
				s.mu.Unlock()
				s.fireDue(now.UTC())
			}
		}
	}()
}

// Close cancels the loop and all owned executions before their database closes.
func (s *Scheduler) Close() {
	s.mu.Lock()
	s.closed = true
	s.cancel()
	s.mu.Unlock()
	s.wg.Wait()
}
func (s *Scheduler) recordFailure(message string) {
	s.mu.Lock()
	s.status.LastError = message
	s.mu.Unlock()
}

// replan recomputes next-fire for every enabled job.
func (s *Scheduler) replan(now time.Time) {
	jobs, err := s.store.ListJobs()
	if err != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	next := map[string]time.Time{}
	for _, j := range jobs {
		if !j.Enabled {
			continue
		}
		t, err := nextFireTime(j.Cron, now)
		if err != nil {
			continue
		}
		next[j.ID] = t
		s.schedules[j.ID] = j.Cron + "/" + j.UpdatedAt.Format(time.RFC3339Nano)
	}
	s.nextFire = next
	s.publishNextFireLocked()
}

func (s *Scheduler) exprFor(cron string) *cronexpr.Expression {
	s.exprMu.Lock()
	defer s.exprMu.Unlock()
	if s.exprs == nil {
		s.exprs = map[string]*cronexpr.Expression{}
	}
	if e, ok := s.exprs[cron]; ok {
		return e
	}
	e, err := cronexpr.Parse(cron)
	if err != nil {
		return nil
	}
	if len(s.exprs) >= MaxJobs {
		s.exprs = map[string]*cronexpr.Expression{}
	}
	s.exprs[cron] = e
	return e
}

func (s *Scheduler) publishNextFireLocked() {
	out := map[string]string{}
	for id, t := range s.nextFire {
		if !t.IsZero() {
			out[id] = t.UTC().Format(time.RFC3339)
		}
	}
	s.status.NextFire = out
}

// fireDue triggers every due job serially; executions run detached with the
// single-flight guard so one slow run never blocks the tick loop and the
// same job never overlaps itself.
func (s *Scheduler) fireDue(now time.Time) {
	due := s.dueJobs(now)
	for _, j := range due {
		_ = s.launch(j, "cron", now)
	}
}

func (s *Scheduler) dueJobs(now time.Time) []Job {
	jobs, err := s.store.ListJobs()
	if err != nil {
		return nil
	}
	var due []Job
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, j := range jobs {
		blockedAt, blocked := s.blocked[j.ID]
		if !j.Enabled || s.running[j.ID] || (blocked && blockedAt.Equal(j.UpdatedAt)) {
			continue
		}
		t, ok := s.nextFire[j.ID]
		if !ok || s.schedules[j.ID] != j.Cron+"/"+j.UpdatedAt.Format(time.RFC3339Nano) {
			s.schedules[j.ID] = j.Cron + "/" + j.UpdatedAt.Format(time.RFC3339Nano)
			stamp, err := nextFireTime(j.Cron, now)
			if err != nil {
				continue
			}
			t = stamp
			s.nextFire[j.ID] = t
			s.publishNextFireLocked()
			if t.After(now) {
				continue
			}
		}
		if !t.IsZero() && !t.After(now) {
			due = append(due, j)
		}
	}
	return due
}

// TriggerNow fires one job immediately (manual "run now" path); it answers
// an error when the job is unknown or already running.
func (s *Scheduler) TriggerNow(jobID string) error {
	j, ok, err := s.store.GetJob(jobID)
	if err != nil {
		return errors.Join(ErrPersistence, err)
	}
	if !ok {
		return fmt.Errorf("scheduler: job not found")
	}
	return s.launch(j, "manual", time.Now().UTC())
}

// launch marks running, persists the run row, executes detached, then
// finalizes + notifies + replans.
func (s *Scheduler) launch(j Job, trigger string, now time.Time) error {
	s.mu.Lock()
	if s.closed || s.ctx.Err() != nil {
		s.mu.Unlock()
		return ErrClosed
	}
	if s.running[j.ID] {
		s.mu.Unlock()
		return ErrBusy
	}
	if s.exec == nil {
		s.mu.Unlock()
		return errors.New("scheduler: executor unavailable")
	}
	s.running[j.ID] = true
	s.status.RunningJobs = append(s.status.RunningJobs, j.ID)
	s.wg.Add(1)
	s.mu.Unlock()
	release := func() {
		s.mu.Lock()
		delete(s.running, j.ID)
		for i, id := range s.status.RunningJobs {
			if id == j.ID {
				s.status.RunningJobs = append(s.status.RunningJobs[:i], s.status.RunningJobs[i+1:]...)
				break
			}
		}
		s.mu.Unlock()
		s.wg.Done()
	}
	run := Run{ID: ulid.Make().String(), JobID: j.ID, JobName: j.Name, SessionID: j.SessionID, State: RunRunning, Trigger: trigger, StartedAt: now}
	if err := s.store.AppendRun(run); err != nil {
		release()
		s.recordFailure("启动记录写入失败，任务未执行")
		return errors.Join(ErrPersistence, err)
	}
	if err := s.store.TouchLastRun(j.ID, now); err != nil {
		run.State = RunFailed
		run.Error = "任务启动时间保存失败，未开始执行"
		run.FinishedAt = time.Now().UTC()
		_ = s.store.AppendRun(run)
		release()
		s.recordFailure(run.Error)
		return errors.Join(ErrPersistence, err)
	}
	go func() {
		defer release()
		ctx, cancel := context.WithTimeout(s.ctx, 20*time.Minute)
		defer cancel()
		out := func() (out Outcome) {
			defer func() {
				if recover() != nil {
					out = Outcome{Err: errors.New("自动化执行发生内部异常，请核对可能产生的结果")}
				}
			}()
			return s.exec(ctx, j)
		}()
		if ctx.Err() != nil && out.Err == nil {
			out.Err = ctx.Err()
		}
		finished := time.Now().UTC()
		run.FinishedAt, run.TotalTokens, run.Summary = finished, out.TotalTokens, out.Summary
		if out.Err != nil {
			run.State = RunFailed
			run.Error = out.Err.Error()
			run.OutcomeUnknown = true
		} else {
			run.State = RunSucceeded
		}
		mustDisable := run.OutcomeUnknown || j.RunOnce || IsAtSchedule(j.Cron)
		if mustDisable {
			// Persist the stop before finalizing the receipt. If this fails, the
			// durable running intent survives for startup reconciliation; writing
			// success first would allow the same one-shot to run after restart.
			if err := s.store.DisableIfUnchanged(j); err != nil {
				s.recordFailure("任务停用状态保存失败，执行结果待核对；已阻止该版本继续自动运行")
				s.blockAutomaticRun(j)
				return
			}
		}
		if err := s.store.AppendRun(run); err != nil {
			s.recordFailure("执行回执保存失败，结果待核对；本实例已阻止该任务继续自动运行")
			// Keep a durable running intent for startup recovery. Do not report success.
			_ = s.store.DisableIfUnchanged(j)
			s.blockAutomaticRun(j)
			return
		}
		if mustDisable {
			s.blockAutomaticRun(j)
		} else {
			s.mu.Lock()
			if expr := s.exprFor(j.Cron); expr != nil {
				s.nextFire[j.ID] = expr.Next(finished)
				s.publishNextFireLocked()
			}
			s.mu.Unlock()
		}
		for _, h := range s.fireHooks {
			h(j.ID, trigger, out)
		}
		s.notifyOutcome(j, run)
	}()
	return nil
}

func (s *Scheduler) blockAutomaticRun(j Job) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.blocked[j.ID] = j.UpdatedAt
	delete(s.nextFire, j.ID)
	s.publishNextFireLocked()
}

// A broken notifier can occupy only one slot and never hold a task or shutdown.
func (s *Scheduler) notifyOutcome(j Job, run Run) {
	select {
	case s.notifyGate <- struct{}{}:
	default:
		return
	}
	done := make(chan struct{})
	go func() {
		defer func() { _ = recover(); <-s.notifyGate; close(done) }()
		body := fmt.Sprintf("自动化任务「%s」已完成：%s", j.Name, firstLine(run.Summary))
		if run.State != RunSucceeded {
			body = fmt.Sprintf("自动化任务「%s」失败：%s", j.Name, firstLine(run.Error))
		}
		_ = s.notify.Notify("Lunitide", body)
		if j.WebhookURL != "" {
			if notifier, err := NewWebhookNotifier(j.WebhookURL); err == nil {
				_ = notifier.Notify("Lunitide 自动化", body)
			}
		}
	}()
	select {
	case <-done:
	case <-s.ctx.Done():
	case <-time.After(100 * time.Millisecond):
	}
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len([]rune(s)) > 80 {
		s = string([]rune(s)[:80]) + "…"
	}
	return s
}

// Snapshot answers the heartbeat status.
func (s *Scheduler) Snapshot() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := s.status
	cp.NextFire = map[string]string{}
	for k, v := range s.status.NextFire {
		cp.NextFire[k] = v
	}
	cp.RunningJobs = append([]string{}, s.status.RunningJobs...)
	return cp
}
