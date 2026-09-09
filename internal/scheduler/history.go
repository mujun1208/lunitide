package scheduler

// LatestRuns returns one authoritative, newest-first row per run from the
// bounded journal. Access filtering belongs to the caller and must happen
// before its display limit; the journal itself remains unchanged.
func (s *Store) LatestRuns(jobID string) ([]Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	runs, err := s.loadRunsLocked()
	if err != nil {
		return nil, err
	}
	out := make([]Run, 0, len(runs))
	seen := make(map[string]bool, len(runs))
	for i := len(runs) - 1; i >= 0; i-- {
		r := runs[i]
		if seen[r.ID] {
			continue
		}
		seen[r.ID] = true
		if jobID == "" || r.JobID == jobID {
			out = append(out, r)
		}
	}
	return out, nil
}
