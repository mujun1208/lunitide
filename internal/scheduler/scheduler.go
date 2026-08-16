// Package scheduler drives Lunitide resident automation (P2-3): cron
// scheduled jobs fire headless executions through the injected executor (the
// app layer reuses the single chat kernel - never a second execution core),
// persist an append-only run log, emit Windows toast notifications on
// completion/failure, and expose a heartbeat the UI can poll. Storage stays
// file-based (settings-plane precedent): jobs.json atomically replaced,
// runs.jsonl append-only, both under the data root.
package scheduler

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

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
	Enabled       bool      `json:"enabled"`
	WebhookURL    string    `json:"webhookUrl,omitempty"` // P3-1 optional IM fan-out (https IM custom-bot URL)
	LastRunAt     time.Time `json:"lastRunAt,omitempty"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

// Run is one execution record (append-only).
type Run struct {
	ID          string    `json:"id"`
	JobID       string    `json:"jobId"`
	JobName     string    `json:"jobName"`
	State       string    `json:"state"`
	Trigger     string    `json:"trigger"` // "cron" | "manual"
	Summary     string    `json:"summary,omitempty"`
	TotalTokens int64     `json:"totalTokens"`
	Error       string    `json:"error,omitempty"`
	StartedAt   time.Time `json:"startedAt"`
	FinishedAt  time.Time `json:"finishedAt,omitempty"`
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
	encoded := base64.StdEncoding.EncodeToString([]byte(script))
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-EncodedCommand", encoded)
	return cmd.Start()
}

// psQuote wraps s in single quotes doubling embedded ones.
func psQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// Store persists jobs and runs.
type Store struct {
	dir  string
	mu   sync.Mutex
	jobs string
	runs string
}

// NewStore opens (or lazily creates) the store under <root>/automation.
func NewStore(root string) (*Store, error) {
	if !filepath.IsAbs(root) {
		return nil, ErrInvalid
	}
	dir := filepath.Join(root, "automation")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	return &Store{dir: dir, jobs: filepath.Join(dir, "jobs.json"), runs: filepath.Join(dir, "runs.jsonl")}, nil
}

// ValidateJob enforces the frozen field contract.
func ValidateJob(j Job) error {
	if j.Name == "" || len([]rune(j.Name)) > maxNameRunes || strings.ContainsRune(j.Name, 0) {
		return fmt.Errorf("%w: name", ErrInvalid)
	}
	if _, err := cronexpr.Parse(j.Cron); err != nil {
		return fmt.Errorf("%w: cron", ErrInvalid)
	}
	if j.Prompt == "" || len([]rune(j.Prompt)) > maxPromptRunes || strings.ContainsRune(j.Prompt, 0) {
		return fmt.Errorf("%w: prompt", ErrInvalid)
	}
	if len(j.ProviderID) != 26 || j.ModelID == "" || len(j.ModelID) > 128 || len(j.SessionID) != 26 {
		return fmt.Errorf("%w: provider/model/session", ErrInvalid)
	}
	if err := ValidateWebhookURL(j.WebhookURL); err != nil {
		return fmt.Errorf("%w: webhook", ErrInvalid)
	}
	return nil
}

// PutJob inserts or updates (matching ID) one job atomically.
func (s *Store) PutJob(j Job) error {
	if err := ValidateJob(j); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	jobs, err := s.loadJobs()
	if err != nil {
		return err
	}
	for i := range jobs {
		if jobs[i].ID == j.ID {
			jobs[i] = j
			return s.saveJobs(jobs)
		}
	}
	if len(jobs) >= MaxJobs {
		return fmt.Errorf("%w: job quota", ErrInvalid)
	}
	jobs = append(jobs, j)
	return s.saveJobs(jobs)
}

// DeleteJob removes one job; its run history stays.
func (s *Store) DeleteJob(id string) error {
	if len(id) != 26 {
		return fmt.Errorf("%w: id", ErrInvalid)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	jobs, err := s.loadJobs()
	if err != nil {
		return err
	}
	out := jobs[:0]
	for _, j := range jobs {
		if j.ID != id {
			out = append(out, j)
		}
	}
	return s.saveJobs(out)
}

// ListJobs answers all jobs.
func (s *Store) ListJobs() ([]Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadJobs()
}

// GetJob answers one job by id.
func (s *Store) GetJob(id string) (Job, bool, error) {
	jobs, err := s.ListJobs()
	if err != nil {
		return Job{}, false, err
	}
	for _, j := range jobs {
		if j.ID == id {
			return j, true, nil
		}
	}
	return Job{}, false, nil
}

// TouchLastRun persists the new last-run stamp for one job.
func (s *Store) TouchLastRun(id string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	jobs, err := s.loadJobs()
	if err != nil {
		return err
	}
	for i := range jobs {
		if jobs[i].ID == id {
			jobs[i].LastRunAt = at.UTC()
			return s.saveJobs(jobs)
		}
	}
	return fmt.Errorf("%w: job missing", ErrInvalid)
}

// AppendRun appends one run record (bounded per job).
func (s *Store) AppendRun(r Run) error {
	if len(r.JobID) != 26 || (r.State != RunRunning && r.State != RunSucceeded && r.State != RunFailed) {
		return fmt.Errorf("%w: run", ErrInvalid)
	}
	if len([]rune(r.Summary)) > maxSummaryRunes {
		r.Summary = string([]rune(r.Summary)[:maxSummaryRunes]) + "…"
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := os.OpenFile(s.runs, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(f)
	err = enc.Encode(r)
	// Close before trim: Windows refuses to rename onto a file still held
	// open by this process.
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return s.trimRunsLocked(r.JobID)
}

// trimRuns keeps only the newest MaxRunsPerJob rows of the job (rewrite via
// temp + rename; runs of other jobs are preserved verbatim).
func (s *Store) trimRunsLocked(jobID string) error {
	runs, err := s.loadRunsLocked()
	if err != nil {
		return err
	}
	var kept []Run
	counts := map[string]int{}
	for i := len(runs) - 1; i >= 0; i-- {
		r := runs[i]
		if r.JobID != jobID {
			kept = append([]Run{r}, kept...)
			continue
		}
		if counts[jobID] < MaxRunsPerJob {
			kept = append([]Run{r}, kept...)
			counts[jobID]++
		}
	}
	if len(kept) == len(runs) {
		return nil
	}
	return s.saveRuns(kept)
}

// ListRuns answers the newest-first runs (all jobs when jobID is empty).
func (s *Store) ListRuns(jobID string, limit int) ([]Run, error) {
	if limit < 1 || limit > 500 {
		limit = 50
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	runs, err := s.loadRunsLocked()
	if err != nil {
		return nil, err
	}
	out := make([]Run, 0, limit)
	for i := len(runs) - 1; i >= 0 && len(out) < limit; i-- {
		if jobID == "" || runs[i].JobID == jobID {
			out = append(out, runs[i])
		}
	}
	return out, nil
}

func (s *Store) loadJobs() ([]Job, error) {
	b, err := os.ReadFile(s.jobs)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var jobs []Job
	if err := json.Unmarshal(b, &jobs); err != nil {
		return nil, err
	}
	return jobs, nil
}

func (s *Store) saveJobs(jobs []Job) error {
	b, err := json.MarshalIndent(jobs, "", " ")
	if err != nil {
		return err
	}
	tmp := s.jobs + ".tmp"
	if err := os.WriteFile(tmp, b, 0600); err != nil {
		return err
	}
	_ = os.Remove(s.jobs)
	return os.Rename(tmp, s.jobs)
}

func (s *Store) loadRunsLocked() ([]Run, error) {
	f, err := os.Open(s.runs)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var runs []Run
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for sc.Scan() {
		var r Run
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			return nil, err
		}
		runs = append(runs, r)
	}
	return runs, sc.Err()
}

func (s *Store) saveRuns(runs []Run) error {
	f, err := os.Create(s.runs + ".tmp")
	if err != nil {
		return err
	}
	w := bufio.NewWriter(f)
	for _, r := range runs {
		b, err := json.Marshal(r)
		if err != nil {
			f.Close()
			return err
		}
		w.Write(b)
		w.WriteByte('\n')
	}
	if err := w.Flush(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	_ = os.Remove(s.runs)
	return os.Rename(s.runs+".tmp", s.runs)
}

// Status is the scheduler heartbeat snapshot the UI polls.
type Status struct {
	Running       bool              `json:"running"`
	StartedAt     time.Time         `json:"startedAt,omitempty"`
	LastHeartbeat time.Time         `json:"lastHeartbeat"`
	NextFire      map[string]string `json:"nextFire"` // jobID -> RFC3339
	RunningJobs   []string          `json:"runningJobs"`
}

// Scheduler owns timing, single-flight firing, persistence, and notify.
type Scheduler struct {
	store  *Store
	exec   Executor
	notify Notifier

	mu        sync.Mutex
	nextFire  map[string]time.Time
	running   map[string]bool
	status    Status
	exprs     map[string]*cronexpr.Expression
	exprMu    sync.Mutex
	fireHooks []func(jobID, trigger string, outcome Outcome) // tests
}

// New wires the scheduler; a nil notifier falls back to the platform one.
func New(store *Store, exec Executor, notifier Notifier) *Scheduler {
	if notifier == nil {
		notifier = NewPlatformNotifier()
	}
	return &Scheduler{store: store, exec: exec, notify: notifier,
		nextFire: map[string]time.Time{}, running: map[string]bool{},
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
	go func() {
		s.mu.Lock()
		s.status.Running = true
		s.status.StartedAt = time.Now().UTC()
		s.mu.Unlock()
		s.replan(time.Now().UTC())
		tick := time.NewTicker(defaultTickEvery)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				s.mu.Lock()
				s.status.Running = false
				s.mu.Unlock()
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
		e := s.exprFor(j.Cron)
		if e == nil {
			continue
		}
		next[j.ID] = e.Next(now)
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
		s.launch(j, "cron", now)
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
		if !j.Enabled || s.running[j.ID] {
			continue
		}
		t, ok := s.nextFire[j.ID]
		if !ok {
			// Job created after start: seed from now.
			if e := s.exprFor(j.Cron); e != nil {
				t = e.Next(now)
				s.nextFire[j.ID] = t
				s.publishNextFireLocked()
			}
			continue
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
	if err != nil || !ok {
		return fmt.Errorf("scheduler: job not found")
	}
	s.mu.Lock()
	if s.running[j.ID] {
		s.mu.Unlock()
		return fmt.Errorf("scheduler: job already running")
	}
	s.mu.Unlock()
	s.launch(j, "manual", time.Now().UTC())
	return nil
}

// launch marks running, persists the run row, executes detached, then
// finalizes + notifies + replans.
func (s *Scheduler) launch(j Job, trigger string, now time.Time) {
	s.mu.Lock()
	if s.running[j.ID] {
		s.mu.Unlock()
		return
	}
	s.running[j.ID] = true
	s.status.RunningJobs = append(s.status.RunningJobs, j.ID)
	s.mu.Unlock()

	run := Run{ID: ulid.Make().String(), JobID: j.ID, JobName: j.Name,
		State: RunRunning, Trigger: trigger, StartedAt: now}
	_ = s.store.AppendRun(run)
	_ = s.store.TouchLastRun(j.ID, now)
	go func() {
		defer func() {
			s.mu.Lock()
			delete(s.running, j.ID)
			for i, id := range s.status.RunningJobs {
				if id == j.ID {
					s.status.RunningJobs = append(s.status.RunningJobs[:i], s.status.RunningJobs[i+1:]...)
					break
				}
			}
			s.mu.Unlock()
		}()
		out := s.exec(context.Background(), j)
		finished := time.Now().UTC()
		run.FinishedAt, run.TotalTokens, run.Summary = finished, out.TotalTokens, out.Summary
		if out.Err != nil {
			run.State = RunFailed
			run.Error = out.Err.Error()
		} else {
			run.State = RunSucceeded
		}
		_ = s.store.AppendRun(run)
		for _, h := range s.fireHooks {
			h(j.ID, trigger, out)
		}
		if run.State == RunSucceeded {
			_ = s.notify.Notify("Lunitide", fmt.Sprintf("自动化任务「%s」已完成：%s", j.Name, firstLine(run.Summary)))
		} else {
			_ = s.notify.Notify("Lunitide", fmt.Sprintf("自动化任务「%s」失败：%s", j.Name, firstLine(run.Error)))
		}
		// P3-1 IM fan-out: the job's webhook (if any) gets the same result
		// as a second, independent channel. Failures are swallowed (the
		// toast above already surfaced the outcome to the local user).
		if j.WebhookURL != "" {
			if wn, err := NewWebhookNotifier(j.WebhookURL); err == nil {
				title := "Lunitide 自动化"
				body := fmt.Sprintf("任务「%s」已完成：%s", j.Name, firstLine(run.Summary))
				if run.State != RunSucceeded {
					body = fmt.Sprintf("任务「%s」失败：%s", j.Name, firstLine(run.Error))
				}
				_ = wn.Notify(title, body)
			}
		}
		// Reschedule from the finish instant.
		s.mu.Lock()
		if e := s.exprFor(j.Cron); e != nil {
			s.nextFire[j.ID] = e.Next(time.Now().UTC())
			s.publishNextFireLocked()
		}
		s.mu.Unlock()
	}()
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
