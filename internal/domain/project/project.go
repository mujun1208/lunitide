package project

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
)

type Status string

const (
	StatusActive   Status = "active"
	StatusArchived Status = "archived"
)

var ErrNotFound = errors.New("project not found")

type Project struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Status    Status    `json:"status"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
	Version   int64     `json:"version"`
}

type Filter struct{ Status Status }

type Repository interface {
	Create(context.Context, Project) (Project, error)
	List(context.Context, Filter) ([]Project, error)
}

func NormalizeName(raw string) (string, error) {
	if len([]rune(raw)) > 200 {
		return "", errors.New("project name must contain 1 to 200 characters")
	}
	name := strings.Join(strings.Fields(raw), " ")
	if name == "" || len([]rune(name)) > 200 {
		return "", errors.New("project name must contain 1 to 200 characters")
	}
	return name, nil
}

func (p Project) Validate() error {
	id, err := ulid.ParseStrict(p.ID)
	if err != nil || id.String() != p.ID || p.ID[0] > '7' {
		return errors.New("project ID must be an uppercase canonical ULID")
	}
	name, err := NormalizeName(p.Name)
	if err != nil || name != p.Name {
		return errors.New("project name is not normalized")
	}
	if p.Status != StatusActive && p.Status != StatusArchived {
		return errors.New("project status is invalid")
	}
	if p.CreatedAt.IsZero() || p.UpdatedAt.IsZero() || p.UpdatedAt.Before(p.CreatedAt) || p.Version < 1 {
		return errors.New("project lifecycle metadata is invalid")
	}
	return nil
}
