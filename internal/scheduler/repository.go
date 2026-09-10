package scheduler

import "time"

// Repository is the persistence surface for cron/at jobs.
// JSON remains the default sole writer until Cutover switches the marker.
// Do not dual-write.
type Repository interface {
	PutJob(Job) error
	PutJobVersioned(Job, string) (Job, error)
	DeleteJob(id string) error
	ListJobs() ([]Job, error)
	GetJob(id string) (Job, bool, error)
	TouchLastRun(id string, at time.Time) error
	AppendRun(Run) error
	BindRunSession(runID, sessionID string) error
	ListRuns(jobID string, limit int) ([]Run, error)
	LatestRuns(jobID string) ([]Run, error)
	DisableIfUnchanged(observed Job) error
	RecoverInterrupted() error
}

var _ Repository = (*Store)(nil)
