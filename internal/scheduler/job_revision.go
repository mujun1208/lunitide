package scheduler

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"
)

var ErrJobConflict = errors.New("automation job changed; reload before saving")

func JobRevision(job Job) string {
	job.LastRunAt = time.Time{}
	raw, _ := json.Marshal(job)
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}

func sameJobConfiguration(a, b Job) bool {
	a.CreatedAt, b.CreatedAt = time.Time{}, time.Time{}
	a.UpdatedAt, b.UpdatedAt = time.Time{}, time.Time{}
	return JobRevision(a) == JobRevision(b)
}

// PutJobVersioned implements creation replay and full-document edit CAS under
// the same lock used for execution timestamps and durable atomic replacement.
func (s *Store) PutJobVersioned(job Job, expected string) (Job, error) {
	if err := ValidateJob(job); err != nil {
		return Job{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	jobs, err := s.loadJobs()
	if err != nil {
		return Job{}, err
	}
	for i, old := range jobs {
		if old.ID != job.ID {
			continue
		}
		if expected == "" && sameJobConfiguration(old, job) {
			return old, nil
		}
		if expected == "" || JobRevision(old) != expected {
			return Job{}, ErrJobConflict
		}
		job.CreatedAt = old.CreatedAt
		job.LastRunAt = old.LastRunAt
		job.UpdatedAt = nextJobTimestamp(old.UpdatedAt)
		jobs[i] = job
		return job, s.saveJobs(jobs)
	}
	if expected != "" {
		return Job{}, ErrJobConflict
	}
	if len(jobs) >= MaxJobs {
		return Job{}, ErrInvalid
	}
	job.CreatedAt = time.Now().UTC()
	job.UpdatedAt = job.CreatedAt
	return job, s.saveJobs(append(jobs, job))
}
