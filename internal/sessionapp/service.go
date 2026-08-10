package sessionapp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/lunitide/lunitide/internal/domain/session"
	"github.com/lunitide/lunitide/internal/providerapp"
	"github.com/oklog/ulid/v2"
)

var (
	ErrIdempotencyKeyRequired = errors.New("idempotency key is required")
	ErrIdempotencyConflict    = errors.New("idempotency key reused with different request")
	ErrProjectNotFound        = errors.New("project not found")
	ErrSessionCapacityReached = errors.New("session capacity reached")
)

type Tx interface {
	CreateSession(context.Context, session.Session) (session.Session, error)
	Idempotency(context.Context, string, string, time.Time) (providerapp.Record, bool, error)
	PutIdempotency(context.Context, providerapp.Record) error
	PutAudit(context.Context, providerapp.Audit) error
}
type UnitOfWork interface {
	DoSession(context.Context, func(Tx) error) error
}
type Reader interface {
	ListSessions(context.Context, session.Filter) ([]session.Session, error)
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
func (s *Service) List(ctx context.Context, f session.Filter) ([]session.Session, error) {
	if s == nil || s.read == nil {
		return nil, errors.New("session reader unavailable")
	}
	return s.read.ListSessions(ctx, f)
}
func (s *Service) Create(ctx context.Context, key, actor string, request any, value session.Session) (session.Session, error) {
	if !providerapp.ValidIdempotencyKey(key) {
		return session.Session{}, ErrIdempotencyKeyRequired
	}
	if s == nil || s.uow == nil {
		return session.Session{}, errors.New("session unit of work unavailable")
	}
	body, err := json.Marshal(request)
	if err != nil {
		return session.Session{}, err
	}
	sum := sha256.Sum256(body)
	digest := hex.EncodeToString(sum[:])
	var result session.Session
	err = s.uow.DoSession(ctx, func(tx Tx) error {
		now := s.clock.Now().UTC()
		record, found, e := tx.Idempotency(ctx, "session.create", key, now)
		if e != nil {
			return e
		}
		if found {
			if record.Digest != digest {
				return ErrIdempotencyConflict
			}
			return json.Unmarshal(record.Response, &result)
		}
		result, e = tx.CreateSession(ctx, value)
		if e != nil {
			return e
		}
		response, e := json.Marshal(result)
		if e != nil {
			return e
		}
		meta, _ := json.Marshal(map[string]any{"projectId": result.ProjectID, "version": result.Version})
		eventSum := sha256.Sum256([]byte("session-audit\x00" + digest + "\x00" + result.ID))
		var event ulid.ULID
		copy(event[:], eventSum[:16])
		if e = tx.PutAudit(ctx, providerapp.Audit{ID: event.String(), Action: "session.created", AggregateID: result.ID, Actor: actor, Metadata: meta, CreatedAt: now}); e != nil {
			return e
		}
		return tx.PutIdempotency(ctx, providerapp.Record{Operation: "session.create", Key: key, Digest: digest, Response: response, CreatedAt: now, ExpiresAt: now.Add(24 * time.Hour)})
	})
	return result, err
}
