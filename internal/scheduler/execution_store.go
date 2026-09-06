package scheduler

import "time"

// DisableIfUnchanged never overwrites a job edited while its old run was active.
func (s *Store) DisableIfUnchanged(observed Job) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	jobs, err := s.loadJobs()
	if err != nil {
		return err
	}
	for i := range jobs {
		if jobs[i].ID == observed.ID && jobs[i].UpdatedAt.Equal(observed.UpdatedAt) {
			jobs[i].Enabled = false
			jobs[i].UpdatedAt = nextJobTimestamp(jobs[i].UpdatedAt)
			return s.saveJobs(jobs)
		}
	}
	return nil
}

// RecoverInterrupted runs under the exclusive application instance lock before
// starting the scheduler. It never repeats an uncertain external side effect.
func (s *Store) RecoverInterrupted() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	runs, err := s.loadRunsLocked()
	if err != nil {
		return err
	}
	latest := map[string]Run{}
	for _, run := range runs {
		latest[run.ID] = run
	}
	interrupted := map[string]bool{}
	for _, run := range latest {
		if run.State != RunRunning {
			continue
		}
		interrupted[run.JobID] = true
		run.State = RunFailed
		run.OutcomeUnknown = true
		run.FinishedAt = time.Now().UTC()
		run.Error = "上次执行被中断，结果不确定；已停用任务，请核对后重新启用"
		runs = append(runs, run)
	}
	if len(interrupted) == 0 {
		return nil
	}
	jobs, err := s.loadJobs()
	if err != nil {
		return err
	}
	for i := range jobs {
		if interrupted[jobs[i].ID] {
			jobs[i].Enabled = false
			jobs[i].UpdatedAt = nextJobTimestamp(jobs[i].UpdatedAt)
		}
	}
	// Disable first: if the receipt replacement fails, the next startup retries
	// reconciliation while the executable intent stays durably disabled.
	if err = s.saveJobs(jobs); err != nil {
		return err
	}
	return s.saveRuns(runs)
}

func nextJobTimestamp(previous time.Time) time.Time {
	now := time.Now().UTC()
	if !now.After(previous) {
		return previous.Add(time.Nanosecond)
	}
	return now
}
