package scheduler

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/lunitide/lunitide/internal/workspace"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Store persists jobs and runs.
type Store struct {
	dir  string
	mu   sync.Mutex
	jobs string
	runs string
}

// JSON escaping can expand one prompt character to six bytes. This budget
// accommodates all 100 legal jobs and is enforced before atomic replacement.
const maxJobsFileBytes = 8 << 20

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
	if len(j.ID) != 26 || len(j.Cron) > 256 || len(j.ExecutionMode) > 64 || len(j.SessionMode) > 32 {
		return fmt.Errorf("%w: id/schedule/execution mode", ErrInvalid)
	}
	if j.Name == "" || len([]rune(j.Name)) > maxNameRunes || strings.ContainsRune(j.Name, 0) {
		return fmt.Errorf("%w: name", ErrInvalid)
	}
	if _, err := nextFireTime(j.Cron, time.Now().UTC()); err != nil {
		return fmt.Errorf("%w: cron", ErrInvalid)
	}
	if _, err := normalizeSessionMode(j.SessionMode); err != nil {
		return err
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
	if len(r.JobID) != 26 || r.ID == "" || (r.State != RunRunning && r.State != RunSucceeded && r.State != RunFailed) {
		return fmt.Errorf("%w: run", ErrInvalid)
	}
	if len([]rune(r.Summary)) > maxSummaryRunes {
		r.Summary = string([]rune(r.Summary)[:maxSummaryRunes]) + "…"
	}
	if len([]rune(r.Error)) > maxSummaryRunes {
		r.Error = string([]rune(r.Error)[:maxSummaryRunes]) + "…"
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	runs, err := s.loadRunsLocked()
	if err != nil {
		return err
	}
	runs = append(runs, r)
	counts := map[string]int{}
	kept := make([]Run, 0, min(len(runs), MaxJobs*MaxRunsPerJob))
	for i := len(runs) - 1; i >= 0; i-- {
		row := runs[i]
		if counts[row.JobID] < MaxRunsPerJob {
			kept = append(kept, row)
			counts[row.JobID]++
		}
	}
	// Bound history even if thousands of deleted job IDs are represented.
	if len(kept) > MaxJobs*MaxRunsPerJob {
		kept = kept[:MaxJobs*MaxRunsPerJob]
	}
	for i, j := 0, len(kept)-1; i < j; i, j = i+1, j-1 {
		kept[i], kept[j] = kept[j], kept[i]
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
	b, err := readStoreFile(s.jobs, maxJobsFileBytes)
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
	seen := map[string]bool{}
	for _, job := range jobs {
		if seen[job.ID] {
			return nil, ErrInvalid
		}
		seen[job.ID] = true
		if err := ValidateJob(job); err != nil {
			return nil, err
		}
	}
	if len(jobs) > MaxJobs {
		return nil, fmt.Errorf("%w: job quota", ErrInvalid)
	}
	return jobs, nil
}

func (s *Store) saveJobs(jobs []Job) error {
	b, err := json.MarshalIndent(jobs, "", " ")
	if err != nil {
		return err
	}
	if len(b) > maxJobsFileBytes {
		return errors.New("automation store exceeds size budget")
	}
	root, err := workspace.NewSecureRoot(s.dir)
	if err != nil {
		return err
	}
	return root.WriteAtomic(filepath.Base(s.jobs), b, 0600)
}
func readStoreFile(path string, limit int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, errors.New("automation store exceeds size budget")
	}
	return b, nil
}

func (s *Store) loadRunsLocked() ([]Run, error) {
	raw, err := readStoreFile(s.runs, 32<<20)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var runs []Run
	sc := bufio.NewScanner(bytes.NewReader(raw))
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
	var out bytes.Buffer
	enc := json.NewEncoder(&out)
	for _, r := range runs {
		if err := enc.Encode(r); err != nil {
			return err
		}
	}
	if out.Len() > 32<<20 {
		return errors.New("automation history exceeds size budget")
	}
	root, err := workspace.NewSecureRoot(s.dir)
	if err != nil {
		return err
	}
	return root.WriteAtomic(filepath.Base(s.runs), out.Bytes(), 0600)
}
