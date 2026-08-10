// Package stageapp coordinates stage queries and atomic idempotent creation.
package stageapp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/lunitide/lunitide/internal/domain/stage"
	"github.com/lunitide/lunitide/internal/providerapp"
	"github.com/oklog/ulid/v2"
)

var (
	ErrIdempotencyKeyRequired = errors.New("idempotency key is required")
	ErrIdempotencyConflict    = errors.New("idempotency key reused with different request")
	ErrProjectNotFound        = errors.New("project not found")
	ErrStagePhaseConflict     = errors.New("stage phase already exists for project")
)

type Tx interface {
	CreateStage(context.Context, stage.Stage) (stage.Stage, error)
	Idempotency(context.Context, string, string, time.Time) (providerapp.Record, bool, error)
	PutIdempotency(context.Context, providerapp.Record) error
	PutAudit(context.Context, providerapp.Audit) error
}
type UnitOfWork interface {
	DoStage(context.Context, func(Tx) error) error
}
type Reader interface {
	ListStages(context.Context, stage.Filter) ([]stage.Stage, error)
}
type Clock interface{ Now() time.Time }
type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now().UTC() }

type Service struct {
	read  Reader
	uow   UnitOfWork
	clock Clock
}

func New(r Reader, u UnitOfWork) *Service { return &Service{r, u, systemClock{}} }

func (s *Service) List(ctx context.Context, f stage.Filter) ([]stage.Stage, error) {
	if s == nil || s.read == nil {
		return nil, errors.New("stage reader unavailable")
	}
	return s.read.ListStages(ctx, f)
}

func (s *Service) Create(ctx context.Context, key, actor string, request any, value stage.Stage) (stage.Stage, error) {
	if !providerapp.ValidIdempotencyKey(key) {
		return stage.Stage{}, ErrIdempotencyKeyRequired
	}
	if s == nil || s.uow == nil {
		return stage.Stage{}, errors.New("stage unit of work unavailable")
	}
	body, err := json.Marshal(request)
	if err != nil {
		return stage.Stage{}, err
	}
	sum := sha256.Sum256(body)
	digest := hex.EncodeToString(sum[:])
	var result stage.Stage
	err = s.uow.DoStage(ctx, func(tx Tx) error {
		now := s.clock.Now().UTC()
		record, found, e := tx.Idempotency(ctx, "stage.create", key, now)
		if e != nil {
			return e
		}
		if found {
			if record.Digest != digest {
				return ErrIdempotencyConflict
			}
			return json.Unmarshal(record.Response, &result)
		}
		result, e = tx.CreateStage(ctx, value)
		if e != nil {
			return e
		}
		response, e := json.Marshal(result)
		if e != nil {
			return e
		}
		meta, _ := json.Marshal(map[string]any{"projectId": result.ProjectID, "phase": result.Phase, "version": result.Version})
		eventSum := sha256.Sum256([]byte("stage-audit\x00" + digest + "\x00" + result.ID))
		var event ulid.ULID
		copy(event[:], eventSum[:16])
		if e = tx.PutAudit(ctx, providerapp.Audit{ID: event.String(), Action: "stage.created", AggregateID: result.ID, Actor: actor, Metadata: meta, CreatedAt: now}); e != nil {
			return e
		}
		return tx.PutIdempotency(ctx, providerapp.Record{Operation: "stage.create", Key: key, Digest: digest, Response: response, CreatedAt: now, ExpiresAt: now.Add(24 * time.Hour)})
	})
	return result, err
}
