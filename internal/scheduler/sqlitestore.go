package scheduler

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

type SQLStore struct {
	db            *sql.DB
	root          string
	enforceWriter bool
	mu            sync.Mutex
}

func OpenLiveSQL(root string) (*SQLStore, error) {
	s, err := OpenSQL(root)
	if err != nil {
		return nil, err
	}
	s.enforceWriter = true
	return s, nil
}

func OpenSQL(root string) (*SQLStore, error) {
	dir := filepath.Join(root, "automation")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "scheduler.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(`PRAGMA busy_timeout=5000; CREATE TABLE IF NOT EXISTS scheduler_jobs (
		id TEXT PRIMARY KEY, enabled INTEGER NOT NULL, updated_at TEXT NOT NULL, payload TEXT NOT NULL
	); CREATE TABLE IF NOT EXISTS scheduler_runs (
		id TEXT PRIMARY KEY, job_id TEXT NOT NULL, started_at TEXT NOT NULL, state TEXT NOT NULL, payload TEXT NOT NULL
	); CREATE TABLE IF NOT EXISTS automation_cursors (
		job_id TEXT NOT NULL, cursor_key TEXT NOT NULL, value TEXT NOT NULL, updated_at TEXT NOT NULL,
		PRIMARY KEY(job_id, cursor_key)
	); CREATE TABLE IF NOT EXISTS automation_dispatches (
		id TEXT PRIMARY KEY, job_id TEXT NOT NULL, event_digest TEXT NOT NULL, created_at TEXT NOT NULL,
		UNIQUE(job_id, event_digest)
	); CREATE TABLE IF NOT EXISTS notification_outbox (
		id TEXT PRIMARY KEY, dispatch_id TEXT NOT NULL, operation_id TEXT NOT NULL DEFAULT '',
		channel TEXT NOT NULL, recipient TEXT NOT NULL, content_digest TEXT NOT NULL,
		status TEXT NOT NULL, attempts INTEGER NOT NULL DEFAULT 0, created_at TEXT NOT NULL,
		UNIQUE(dispatch_id, channel, recipient, content_digest)
	);`); err != nil {
		db.Close()
		return nil, err
	}
	return &SQLStore{db: db, root: root}, nil
}

func (s *SQLStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *SQLStore) putJobLocked(j Job) error {
	raw, err := json.Marshal(j)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT INTO scheduler_jobs(id,enabled,updated_at,payload) VALUES(?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET enabled=excluded.enabled, updated_at=excluded.updated_at, payload=excluded.payload`,
		j.ID, boolInt(j.Enabled), j.UpdatedAt.UTC().Format(time.RFC3339Nano), string(raw))
	return err
}

func (s *SQLStore) PutJob(j Job) error {
	if s != nil && s.enforceWriter && CurrentWriter(s.root) != WriterSQLite {
		return fmt.Errorf("%w: sqlite store is staging-only until cutover", ErrPersistence)
	}
	if err := ValidateJob(j); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.putJobLocked(j)
}

func (s *SQLStore) PutJobVersioned(job Job, expected string) (Job, error) {
	if s != nil && s.enforceWriter && CurrentWriter(s.root) != WriterSQLite {
		return Job{}, fmt.Errorf("%w: sqlite store is staging-only until cutover", ErrPersistence)
	}
	if err := ValidateJob(job); err != nil {
		return Job{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	old, ok, err := s.getJobLocked(job.ID)
	if err != nil {
		return Job{}, err
	}
	if ok {
		if expected == "" && sameJobConfiguration(old, job) {
			return old, nil
		}
		if expected == "" || JobRevision(old) != expected {
			return Job{}, ErrJobConflict
		}
		job.CreatedAt = old.CreatedAt
		job.LastRunAt = old.LastRunAt
		job.UpdatedAt = nextJobTimestamp(old.UpdatedAt)
		return job, s.putJobLocked(job)
	}
	if expected != "" {
		return Job{}, ErrJobConflict
	}
	jobs, err := s.listJobsLocked()
	if err != nil {
		return Job{}, err
	}
	if len(jobs) >= MaxJobs {
		return Job{}, ErrInvalid
	}
	job.CreatedAt = time.Now().UTC()
	job.UpdatedAt = job.CreatedAt
	return job, s.putJobLocked(job)
}

func (s *SQLStore) DeleteJob(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(`DELETE FROM scheduler_jobs WHERE id=?`, id)
	return err
}

func (s *SQLStore) ListJobs() ([]Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.listJobsLocked()
}

func (s *SQLStore) listJobsLocked() ([]Job, error) {
	rows, err := s.db.Query(`SELECT payload FROM scheduler_jobs`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Job
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var j Job
		if err := json.Unmarshal([]byte(raw), &j); err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

func (s *SQLStore) GetJob(id string) (Job, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.getJobLocked(id)
}

func (s *SQLStore) getJobLocked(id string) (Job, bool, error) {
	var raw string
	err := s.db.QueryRow(`SELECT payload FROM scheduler_jobs WHERE id=?`, id).Scan(&raw)
	if err == sql.ErrNoRows {
		return Job{}, false, nil
	}
	if err != nil {
		return Job{}, false, err
	}
	var j Job
	if err := json.Unmarshal([]byte(raw), &j); err != nil {
		return Job{}, false, err
	}
	return j, true, nil
}

func (s *SQLStore) TouchLastRun(id string, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, ok, err := s.getJobLocked(id)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("%w: job missing", ErrInvalid)
	}
	j.LastRunAt = at.UTC()
	return s.putJobLocked(j)
}

func (s *SQLStore) AppendRun(r Run) error {
	if len(r.JobID) != 26 || r.ID == "" {
		return fmt.Errorf("%w: run", ErrInvalid)
	}
	raw, err := json.Marshal(r)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err = s.db.Exec(`INSERT INTO scheduler_runs(id,job_id,started_at,state,payload) VALUES(?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET job_id=excluded.job_id, started_at=excluded.started_at, state=excluded.state, payload=excluded.payload`,
		r.ID, r.JobID, r.StartedAt.UTC().Format(time.RFC3339Nano), r.State, string(raw))
	return err
}

func (s *SQLStore) ListRuns(jobID string, limit int) ([]Run, error) {
	if limit < 1 || limit > 500 {
		limit = 50
	}
	runs, err := s.LatestRuns(jobID)
	if err != nil {
		return nil, err
	}
	if len(runs) > limit {
		runs = runs[:limit]
	}
	return runs, nil
}

func (s *SQLStore) LatestRuns(jobID string) ([]Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(`SELECT payload FROM scheduler_runs ORDER BY started_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Run
	seen := map[string]bool{}
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var r Run
		if err := json.Unmarshal([]byte(raw), &r); err != nil {
			return nil, err
		}
		if seen[r.ID] {
			continue
		}
		seen[r.ID] = true
		if jobID == "" || r.JobID == jobID {
			out = append(out, r)
		}
	}
	return out, rows.Err()
}

func (s *SQLStore) BindRunSession(runID, sessionID string) error {
	if runID == "" || sessionID == "" {
		return ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var raw string
	if err := s.db.QueryRow(`SELECT payload FROM scheduler_runs WHERE id=?`, runID).Scan(&raw); err != nil {
		return err
	}
	var r Run
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		return err
	}
	if r.State != RunRunning {
		return ErrInvalid
	}
	r.SessionID = sessionID
	return s.appendRunLocked(r)
}

func (s *SQLStore) DisableIfUnchanged(observed Job) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, ok, err := s.getJobLocked(observed.ID)
	if err != nil || !ok {
		return err
	}
	if !j.UpdatedAt.Equal(observed.UpdatedAt) {
		return nil
	}
	j.Enabled = false
	j.UpdatedAt = nextJobTimestamp(j.UpdatedAt)
	return s.putJobLocked(j)
}

func (s *SQLStore) RecoverInterrupted() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	runs, err := s.latestRunsLocked("")
	if err != nil {
		return err
	}
	interrupted := map[string]bool{}
	for _, run := range runs {
		if run.State != RunRunning {
			continue
		}
		interrupted[run.JobID] = true
		run.State = RunFailed
		run.OutcomeUnknown = true
		run.FinishedAt = time.Now().UTC()
		run.Error = "上次执行被中断，结果不确定；已停用任务，请核对后重新启用"
		if err := s.appendRunLocked(run); err != nil {
			return err
		}
	}
	if len(interrupted) == 0 {
		return nil
	}
	jobs, err := s.listJobsLocked()
	if err != nil {
		return err
	}
	for _, j := range jobs {
		if !interrupted[j.ID] {
			continue
		}
		j.Enabled = false
		j.UpdatedAt = nextJobTimestamp(j.UpdatedAt)
		if err := s.putJobLocked(j); err != nil {
			return err
		}
	}
	return nil
}

func (s *SQLStore) latestRunsLocked(jobID string) ([]Run, error) {
	rows, err := s.db.Query(`SELECT payload FROM scheduler_runs ORDER BY started_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Run
	seen := map[string]bool{}
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var r Run
		if err := json.Unmarshal([]byte(raw), &r); err != nil {
			return nil, err
		}
		if seen[r.ID] {
			continue
		}
		seen[r.ID] = true
		if jobID == "" || r.JobID == jobID {
			out = append(out, r)
		}
	}
	return out, rows.Err()
}

func (s *SQLStore) appendRunLocked(r Run) error {
	raw, err := json.Marshal(r)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT INTO scheduler_runs(id,job_id,started_at,state,payload) VALUES(?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET job_id=excluded.job_id, started_at=excluded.started_at, state=excluded.state, payload=excluded.payload`,
		r.ID, r.JobID, r.StartedAt.UTC().Format(time.RFC3339Nano), r.State, string(raw))
	return err
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

var _ Repository = (*SQLStore)(nil)
