// Package stage models a single phase instance in the nine-stage business chain.
package stage

import (
	"context"
	"errors"
	"time"

	"github.com/lunitide/lunitide/internal/domain/project"
	"github.com/oklog/ulid/v2"
)

type Status string

const (
	StatusNotStarted    Status = "not_started"
	StatusInProgress    Status = "in_progress"
	StatusWaitingReview Status = "waiting_review"
	StatusApproved      Status = "approved"
	StatusCompleted     Status = "completed"
	StatusRejected      Status = "rejected"
	StatusStale         Status = "stale"
	StatusPaused        Status = "paused"
	StatusBlocked       Status = "blocked"
	StatusCancelled     Status = "cancelled"
)

var ErrPhaseConflict = errors.New("stage phase already exists for project")

type Stage struct {
	ID        string    `json:"id"`
	ProjectID string    `json:"projectId"`
	Phase     int       `json:"phase"`
	Title     string    `json:"title"`
	Status    Status    `json:"status"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
	Version   int64     `json:"version"`
}

type Filter struct{ ProjectID string }

type Repository interface {
	List(context.Context, Filter) ([]Stage, error)
}

func NormalizeTitle(raw string) (string, error) { return project.NormalizeName(raw) }

func canonical(id string) bool {
	u, err := ulid.ParseStrict(id)
	return err == nil && u.String() == id && id[0] <= '7'
}

func (s Stage) Validate() error {
	if !canonical(s.ID) || !canonical(s.ProjectID) {
		return errors.New("stage IDs must be uppercase canonical ULIDs")
	}
	if s.Phase < 1 || s.Phase > 9 {
		return errors.New("stage phase must be between 1 and 9")
	}
	title, err := NormalizeTitle(s.Title)
	if err != nil || title != s.Title {
		return errors.New("stage title is not normalized")
	}
	switch s.Status {
	case StatusNotStarted, StatusInProgress, StatusWaitingReview, StatusApproved, StatusCompleted,
		StatusRejected, StatusStale, StatusPaused, StatusBlocked, StatusCancelled:
	default:
		return errors.New("stage status is invalid")
	}
	if s.CreatedAt.IsZero() || s.UpdatedAt.Before(s.CreatedAt) || s.Version < 1 {
		return errors.New("stage lifecycle metadata is invalid")
	}
	return nil
}
