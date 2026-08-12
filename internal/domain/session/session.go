package session

import (
	"context"
	"errors"
	"time"

	"github.com/lunitide/lunitide/internal/domain/project"
	"github.com/oklog/ulid/v2"
)

type Status string

const StatusActive Status = "active"

type Session struct {
	ID        string    `json:"id"`
	ProjectID string    `json:"projectId"`
	Title     string    `json:"title"`
	Pinned    bool      `json:"pinned"`
	Status    Status    `json:"status"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
	Version   int64     `json:"version"`
}

type Filter struct{ ProjectID string }
type Repository interface {
	List(context.Context, Filter) ([]Session, error)
}

func NormalizeTitle(raw string) (string, error) { return project.NormalizeName(raw) }

func canonical(id string) bool {
	u, err := ulid.ParseStrict(id)
	return err == nil && u.String() == id && id[0] <= '7'
}

func (s Session) Validate() error {
	if !canonical(s.ID) || !canonical(s.ProjectID) {
		return errors.New("session IDs must be uppercase canonical ULIDs")
	}
	title, err := NormalizeTitle(s.Title)
	if err != nil || title != s.Title {
		return errors.New("session title is not normalized")
	}
	if s.Status != StatusActive {
		return errors.New("session status is invalid")
	}
	if s.CreatedAt.IsZero() || s.UpdatedAt.Before(s.CreatedAt) || s.Version < 1 {
		return errors.New("session lifecycle metadata is invalid")
	}
	return nil
}
