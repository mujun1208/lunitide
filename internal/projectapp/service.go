// Package projectapp coordinates project queries and atomic idempotent creation.
package projectapp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/lunitide/lunitide/internal/domain/project"
	"github.com/lunitide/lunitide/internal/providerapp"
	"github.com/oklog/ulid/v2"
)

var (
	ErrIdempotencyKeyRequired = errors.New("idempotency key is required")
	ErrIdempotencyConflict    = errors.New("idempotency key reused with different request")
	ErrProjectCapacityReached = errors.New("project capacity reached")
)

type Tx interface {
	CreateProject(context.Context, project.Project) (project.Project, error)
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
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Status    project.Status `json:"status"`
	CreatedAt time.Time      `json:"createdAt"`
	UpdatedAt time.Time      `json:"updatedAt"`
	Version   int64          `json:"version"`
}

func projectReplayDTOFrom(p project.Project) projectReplayDTO {
	return projectReplayDTO{ID: p.ID, Name: p.Name, Status: p.Status, CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt, Version: p.Version}
}
