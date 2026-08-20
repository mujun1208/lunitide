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
	StatusCreated  Status = "created"
	StatusActive   Status = "active"
	StatusClosed   Status = "closed"
	StatusArchived Status = "archived"
)

type Type string

const (
	TypeImplementation Type = "implementation"
	TypeOperations     Type = "operations"
	TypeEnhancement    Type = "enhancement"
)

var ErrNotFound = errors.New("project not found")

type Project struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	ProjectCode string    `json:"projectCode"`
	Type        Type      `json:"type"`
	Description string    `json:"description"`
	Summary     string    `json:"summary"`
	Objective   string    `json:"objective"`
	Client      string    `json:"client"`
	ContractNo  string    `json:"contractNo"`
	Amount      float64   `json:"amount"`
	Budget      float64   `json:"budget"`
	PlanStart   string    `json:"planStart"`
	PlanEnd     string    `json:"planEnd"`
	Remark      string    `json:"remark"`
	CloseReason string    `json:"closeReason"`
	Status      Status    `json:"status"`
	OrgID       string    `json:"orgId,omitempty"`
	SpaceID     string    `json:"spaceId,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
	Version     int64     `json:"version"`
}

type Filter struct {
	Status Status
	Type   Type
	// OrgID scopes list results to the bound org plus legacy unscoped rows.
	OrgID string
}

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

func validType(t Type) bool { return t == TypeImplementation || t == TypeOperations || t == TypeEnhancement }

// validPlanDate accepts empty or a strict YYYY-MM-DD calendar date.
func validPlanDate(s string) bool {
	if s == "" {
		return true
	}
	if len(s) != 10 || s[4] != '-' || s[7] != '-' {
		return false
	}
	t, err := time.Parse("2006-01-02", s)
	return err == nil && t.Format("2006-01-02") == s
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
	if len(p.ProjectCode) < 4 || len(p.ProjectCode) > 16 || !strings.HasPrefix(p.ProjectCode, "ITM") {
		return errors.New("project code is invalid")
	}
	for _, c := range p.ProjectCode[3:] {
		if c < '0' || c > '9' {
			return errors.New("project code is invalid")
		}
	}
	if !validType(p.Type) {
		return errors.New("project type is invalid")
	}
	if p.Amount < 0 || p.Budget < 0 {
		return errors.New("project amounts must be non-negative")
	}
	if !validPlanDate(p.PlanStart) || !validPlanDate(p.PlanEnd) {
		return errors.New("project plan dates must be YYYY-MM-DD")
	}
	if p.PlanStart != "" && p.PlanEnd != "" && p.PlanEnd < p.PlanStart {
		return errors.New("project plan end must not precede plan start")
	}
	switch p.Status {
	case StatusCreated, StatusActive, StatusClosed, StatusArchived:
	default:
		return errors.New("project status is invalid")
	}
	if p.Status == StatusClosed && strings.TrimSpace(p.CloseReason) == "" {
		return errors.New("closed project requires a close reason")
	}
	if p.CreatedAt.IsZero() || p.UpdatedAt.IsZero() || p.UpdatedAt.Before(p.CreatedAt) || p.Version < 1 {
		return errors.New("project lifecycle metadata is invalid")
	}
	return nil
}

// CanEnterWorkspace reports whether the project gate allows opening the workbench.
// Only accepted (published) projects expose the workspace; created projects stay
// in the management list until publication.
func (p Project) CanEnterWorkspace() bool { return p.Status == StatusActive }

// CanEdit reports whether the frozen A-N fields may still be modified.
func (p Project) CanEdit() bool { return p.Status == StatusCreated || p.Status == StatusActive }
