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
	StatusActive   Status = "active" // legacy alias → chartered
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

type TreeStatus string

const (
	TreeNone    TreeStatus = "none"
	TreePending TreeStatus = "pending"
	TreeReady   TreeStatus = "ready"
	TreePartial TreeStatus = "partial"
	TreeFailed  TreeStatus = "failed"
)

type Executor string

const (
	ExecutorLunitide Executor = "lunitide"
	ExecutorCursor   Executor = "cursor"
	ExecutorCodex    Executor = "codex"
)

type DBStatus string

const (
	DBNone    DBStatus = "none"
	DBPending DBStatus = "pending"
	DBReady   DBStatus = "ready"
	DBFailed  DBStatus = "failed"
)

type Project struct {
	ID                string     `json:"id"`
	Name              string     `json:"name"`
	ProjectCode       string     `json:"projectCode"`
	Type              Type       `json:"type"`
	Description       string     `json:"description"`
	Summary           string     `json:"summary"`
	Objective         string     `json:"objective"`
	Client            string     `json:"client"`
	ContractNo        string     `json:"contractNo"`
	Amount            float64    `json:"amount"`
	Budget            float64    `json:"budget"`
	PlanStart         string     `json:"planStart"`
	PlanEnd           string     `json:"planEnd"`
	Remark            string     `json:"remark"`
	CloseReason       string     `json:"closeReason"`
	StatusBeforeClose Status     `json:"statusBeforeClose"`
	ReopenReason      string     `json:"reopenReason"`
	Status            Status     `json:"status"`
	OrgID             string     `json:"orgId,omitempty"`
	SpaceID           string     `json:"spaceId,omitempty"`
	RootPath          string     `json:"rootPath,omitempty"`
	TreeStatus        TreeStatus `json:"treeStatus,omitempty"`
	TreeDigest        string     `json:"treeDigest,omitempty"`
	TreeGeneratedAt   string     `json:"treeGeneratedAt,omitempty"`
	DefaultExecutor      Executor   `json:"defaultExecutor,omitempty"`
	RulesDigest          string     `json:"rulesDigest,omitempty"`
	RulesMaterializedAt  string     `json:"rulesMaterializedAt,omitempty"`
	DBStatus             DBStatus   `json:"dbStatus,omitempty"`
	DBPath               string     `json:"dbPath,omitempty"`
	DBDigest             string     `json:"dbDigest,omitempty"`
	DBVerifiedAt         string     `json:"dbVerifiedAt,omitempty"`
	CreatedAt         time.Time  `json:"createdAt"`
	UpdatedAt         time.Time  `json:"updatedAt"`
	Version           int64      `json:"version"`
}

type Filter struct {
	Status Status
	Type   Type
	OrgID  string
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

func validType(t Type) bool {
	return t == TypeImplementation || t == TypeOperations || t == TypeEnhancement
}

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

func (p *Project) validateBase() error {
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
	if p.Status == StatusClosed && strings.TrimSpace(p.CloseReason) == "" {
		return errors.New("closed project requires a close reason")
	}
	if p.CreatedAt.IsZero() || p.UpdatedAt.IsZero() || p.UpdatedAt.Before(p.CreatedAt) || p.Version < 1 {
		return errors.New("project lifecycle metadata is invalid")
	}
	if len(p.RootPath) > 1024 {
		return errors.New("project root path is invalid")
	}
	if p.TreeStatus != "" && !validTreeStatus(p.TreeStatus) {
		return errors.New("project tree status is invalid")
	}
	if p.DefaultExecutor != "" && !validExecutor(p.DefaultExecutor) {
		return errors.New("project executor is invalid")
	}
	if p.TreeDigest != "" && !validTreeDigest(p.TreeDigest) {
		return errors.New("project tree digest is invalid")
	}
	if p.DBStatus != "" && !validDBStatus(p.DBStatus) {
		return errors.New("project database status is invalid")
	}
	if p.RulesDigest != "" && !validTreeDigest(p.RulesDigest) {
		return errors.New("project rules digest is invalid")
	}
	if p.DBDigest != "" && !validTreeDigest(p.DBDigest) {
		return errors.New("project database digest is invalid")
	}
	return nil
}

func validDBStatus(s DBStatus) bool {
	switch s {
	case DBNone, DBPending, DBReady, DBFailed:
		return true
	default:
		return false
	}
}

func validTreeStatus(s TreeStatus) bool {
	switch s {
	case TreeNone, TreePending, TreeReady, TreePartial, TreeFailed:
		return true
	default:
		return false
	}
}

func validExecutor(e Executor) bool {
	switch e {
	case ExecutorLunitide, ExecutorCursor, ExecutorCodex:
		return true
	default:
		return false
	}
}

func NormalizeExecutor(e Executor) Executor {
	if e == "" {
		return ExecutorLunitide
	}
	return e
}

func NormalizeTreeStatus(s TreeStatus) TreeStatus {
	if s == "" {
		return TreeNone
	}
	return s
}

func validTreeDigest(d string) bool {
	if len(d) != 64 {
		return false
	}
	for _, c := range d {
		if c < '0' || c > '9' && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// ValidateCreateBusinessFields enforces required business fields on project.create.
func ValidateCreateBusinessFields(p Project) error {
	if !validType(p.Type) {
		return errors.New("project type is required")
	}
	if strings.TrimSpace(p.Description) == "" {
		return errors.New("project description is required")
	}
	if strings.TrimSpace(p.Client) == "" {
		return errors.New("project client is required")
	}
	if !validPlanDate(p.PlanStart) || p.PlanStart == "" {
		return errors.New("project plan start date is required")
	}
	if !validPlanDate(p.PlanEnd) || p.PlanEnd == "" {
		return errors.New("project plan end date is required")
	}
	if p.PlanEnd < p.PlanStart {
		return errors.New("project plan end must not precede plan start")
	}
	if strings.TrimSpace(p.RootPath) == "" {
		return errors.New("project root path is required")
	}
	if len(p.RootPath) > 1024 {
		return errors.New("project root path is invalid")
	}
	return nil
}

// CanEdit reports whether the project form may be opened for mutation.
func (p Project) CanEdit() bool { return p.CanEditMutableFields() || p.CanEditIdentity() }
