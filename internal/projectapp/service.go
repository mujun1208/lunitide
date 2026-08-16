// Package projectapp coordinates project queries and atomic idempotent creation.
package projectapp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/lunitide/lunitide/internal/domain/project"
	"github.com/lunitide/lunitide/internal/providerapp"
	"github.com/oklog/ulid/v2"
)

var (
	ErrIdempotencyKeyRequired = errors.New("idempotency key is required")
	ErrIdempotencyConflict    = errors.New("idempotency key reused with different request")
	ErrProjectCapacityReached = errors.New("project capacity reached")
	ErrProjectVersionConflict = errors.New("project version conflict")
	ErrInvalidTransition      = errors.New("project lifecycle transition is invalid")
)

type Tx interface {
	CreateProject(context.Context, project.Project) (project.Project, error)
	GetProject(context.Context, string) (project.Project, error)
	UpdateProject(context.Context, string, int64, func(*project.Project) error) (project.Project, error)
	Idempotency(context.Context, string, string, time.Time) (providerapp.Record, bool, error)
	PutIdempotency(context.Context, providerapp.Record) error
	PutAudit(context.Context, providerapp.Audit) error
}
type UnitOfWork interface {
	DoProject(context.Context, func(Tx) error) error
}
type Reader interface {
	ListProjects(context.Context, project.Filter) ([]project.Project, error)
}
// Deleter removes a project and all its dependent records.
type Deleter interface {
	DeleteProject(context.Context, string) error
}
type Clock interface{ Now() time.Time }
type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now().UTC() }

type Service struct {
	read    Reader
	uow     UnitOfWork
	deleter Deleter
	clock   Clock
}

func New(read Reader, uow UnitOfWork) *Service {
	return &Service{read: read, uow: uow, clock: systemClock{}}
}
func NewWithClock(read Reader, uow UnitOfWork, clock Clock) *Service {
	return &Service{read: read, uow: uow, clock: clock}
}
func (s *Service) SetDeleter(d Deleter) { s.deleter = d }
func (s *Service) Delete(ctx context.Context, id string) error {
	if s == nil || s.deleter == nil {
		return errors.New("project deleter unavailable")
	}
	return s.deleter.DeleteProject(ctx, id)
}
func (s *Service) List(ctx context.Context, f project.Filter) ([]project.Project, error) {
	if s == nil || s.read == nil {
		return nil, errors.New("project reader is unavailable")
	}
	return s.read.ListProjects(ctx, f)
}

func (s *Service) Create(ctx context.Context, key, actor string, request any, p project.Project) (project.Project, error) {
	if !providerapp.ValidIdempotencyKey(key) {
		return project.Project{}, ErrIdempotencyKeyRequired
	}
	if s == nil || s.uow == nil || s.clock == nil {
		return project.Project{}, errors.New("project unit of work is unavailable")
	}
	body, err := json.Marshal(request)
	if err != nil {
		return project.Project{}, err
	}
	sum := sha256.Sum256(body)
	digest := hex.EncodeToString(sum[:])
	var result project.Project
	err = s.uow.DoProject(ctx, func(tx Tx) error {
		now := s.clock.Now().UTC()
		record, found, err := tx.Idempotency(ctx, "project.create", key, now)
		if err != nil {
			return err
		}
		if found {
			if record.Digest != digest {
				return ErrIdempotencyConflict
			}
			return json.Unmarshal(record.Response, &result)
		}
		result, err = tx.CreateProject(ctx, p)
		if err != nil {
			return err
		}
		// Keep the durable replay representation deliberately narrower than the
		// domain object so future internal fields cannot become public by accident.
		response, err := json.Marshal(projectReplayDTOFrom(result))
		if err != nil {
			return err
		}
		meta, _ := json.Marshal(map[string]any{"version": result.Version})
		eventSum := sha256.Sum256([]byte("project-audit\x00" + digest + "\x00" + result.ID))
		var eventULID ulid.ULID
		copy(eventULID[:], eventSum[:16])
		eventID := eventULID.String()
		if err = tx.PutAudit(ctx, providerapp.Audit{ID: eventID, Action: "project.created", AggregateID: result.ID, Actor: actor, Metadata: meta, CreatedAt: now}); err != nil {
			return err
		}
		return tx.PutIdempotency(ctx, providerapp.Record{Operation: "project.create", Key: key, Digest: digest, Response: response, CreatedAt: now, ExpiresAt: now.Add(24 * time.Hour)})
	})
	return result, err
}

type projectReplayDTO struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	ProjectCode string         `json:"projectCode"`
	Type        project.Type   `json:"type"`
	Description string         `json:"description"`
	Summary     string         `json:"summary"`
	Objective   string         `json:"objective"`
	Client      string         `json:"client"`
	ContractNo  string         `json:"contractNo"`
	Amount      float64        `json:"amount"`
	Budget      float64        `json:"budget"`
	PlanStart   string         `json:"planStart"`
	PlanEnd     string         `json:"planEnd"`
	Remark      string         `json:"remark"`
	CloseReason string         `json:"closeReason"`
	Status      project.Status `json:"status"`
	CreatedAt   time.Time      `json:"createdAt"`
	UpdatedAt   time.Time      `json:"updatedAt"`
	Version     int64          `json:"version"`
}

func projectReplayDTOFrom(p project.Project) projectReplayDTO {
	return projectReplayDTO{ID: p.ID, Name: p.Name, ProjectCode: p.ProjectCode, Type: p.Type, Description: p.Description, Summary: p.Summary, Objective: p.Objective, Client: p.Client, ContractNo: p.ContractNo, Amount: p.Amount, Budget: p.Budget, PlanStart: p.PlanStart, PlanEnd: p.PlanEnd, Remark: p.Remark, CloseReason: p.CloseReason, Status: p.Status, CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt, Version: p.Version}
}

// Mutate applies an optimistic-locking lifecycle mutation (update / publish /
// close / reopen) inside one project unit of work with audit trail.
func (s *Service) Mutate(ctx context.Context, key, actor, action string, id string, version int64, mutate func(*project.Project) error) (project.Project, error) {
	if !providerapp.ValidIdempotencyKey(key) {
		return project.Project{}, ErrIdempotencyKeyRequired
	}
	if s == nil || s.uow == nil || s.clock == nil {
		return project.Project{}, errors.New("project unit of work is unavailable")
	}
	var result project.Project
	err := s.uow.DoProject(ctx, func(tx Tx) error {
		now := s.clock.Now().UTC()
		record, found, err := tx.Idempotency(ctx, action, key, now)
		if err != nil {
			return err
		}
		if found {
			if record.Digest != digestOf(action, id, version) {
				return ErrIdempotencyConflict
			}
			return json.Unmarshal(record.Response, &result)
		}
		result, err = tx.UpdateProject(ctx, id, version, mutate)
		if err != nil {
			return err
		}
		response, err := json.Marshal(projectReplayDTOFrom(result))
		if err != nil {
			return err
		}
		meta, _ := json.Marshal(map[string]any{"version": result.Version, "status": result.Status})
		eventSum := sha256.Sum256([]byte("project-audit\x00" + action + "\x00" + result.ID))
		var eventULID ulid.ULID
		copy(eventULID[:], eventSum[:16])
		if err = tx.PutAudit(ctx, providerapp.Audit{ID: eventULID.String(), Action: action, AggregateID: result.ID, Actor: actor, Metadata: meta, CreatedAt: now}); err != nil {
			return err
		}
		return tx.PutIdempotency(ctx, providerapp.Record{Operation: action, Key: key, Digest: digestOf(action, id, version), Response: response, CreatedAt: now, ExpiresAt: now.Add(24 * time.Hour)})
	})
	return result, err
}

func digestOf(action, id string, version int64) string {
	sum := sha256.Sum256([]byte(action + "\x00" + id + "\x00" + strconv.FormatInt(version, 10)))
	return hex.EncodeToString(sum[:])
}
