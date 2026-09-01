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
	ErrSessionNotFound        = errors.New("session not found")
	ErrSessionVersionConflict = errors.New("session version conflict")
)

type Tx interface {
	CreateSession(context.Context, session.Session) (session.Session, error)
	UpdateSession(context.Context, string, int64, string, bool, time.Time) (session.Session, error)
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

// sessionByID is implemented by stores that can resolve one session without
// listing a whole project. Chat memory inject uses it to find the project
// that owns the current session; missing implementations skip domain recall.
type sessionByID interface {
	GetSession(context.Context, string) (session.Session, error)
}

// Deleter removes a session and all its dependent records.
type Deleter interface {
	DeleteSession(context.Context, string) error
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

func New(r Reader, u UnitOfWork) *Service { return &Service{read: r, uow: u, clock: systemClock{}} }
func (s *Service) SetDeleter(d Deleter)   { s.deleter = d }
func (s *Service) Delete(ctx context.Context, id string) error {
	if s == nil || s.deleter == nil {
		return errors.New("session deleter unavailable")
	}
	return s.deleter.DeleteSession(ctx, id)
}

type emptyDraftFinder interface {
	ListEmptyDraftSessionIDs(ctx context.Context, projectID string, titles []string, limit int) ([]string, error)
}

// ReclaimEmptyDrafts deletes leftover empty launch/companion shells so a
// personal project that looks empty in the sidebar can create a new chat.
func (s *Service) ReclaimEmptyDrafts(ctx context.Context, projectID string, need int) (int, error) {
	if s == nil || s.deleter == nil || need < 1 {
		return 0, nil
	}
	finder, ok := s.read.(emptyDraftFinder)
	if !ok {
		return 0, nil
	}
	ids, err := finder.ListEmptyDraftSessionIDs(ctx, projectID, nil, need)
	if err != nil || len(ids) == 0 {
		return 0, err
	}
	freed := 0
	for _, id := range ids {
		if delErr := s.deleter.DeleteSession(ctx, id); delErr != nil {
			return freed, delErr
		}
		freed++
	}
	return freed, nil
}
func (s *Service) List(ctx context.Context, f session.Filter) ([]session.Session, error) {
	if s == nil || s.read == nil {
		return nil, errors.New("session reader unavailable")
	}
	return s.read.ListSessions(ctx, f)
}

// Get answers one session by id when the reader implements GetSession.
func (s *Service) Get(ctx context.Context, id string) (session.Session, error) {
	if s == nil || s.read == nil {
		return session.Session{}, errors.New("session reader unavailable")
	}
	g, ok := s.read.(sessionByID)
	if !ok {
		return session.Session{}, ErrSessionNotFound
	}
	return g.GetSession(ctx, id)
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

func (s *Service) Update(ctx context.Context, key, actor string, request any, id string, version int64, title string, pinned bool) (session.Session, error) {
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
		record, found, e := tx.Idempotency(ctx, "session.update", key, now)
		if e != nil {
			return e
		}
		if found {
			if record.Digest != digest {
				return ErrIdempotencyConflict
			}
			return json.Unmarshal(record.Response, &result)
		}
		result, e = tx.UpdateSession(ctx, id, version, title, pinned, now)
		if e != nil {
			return e
		}
		response, e := json.Marshal(result)
		if e != nil {
			return e
		}
		meta, _ := json.Marshal(map[string]any{"version": result.Version, "pinned": result.Pinned})
		eventSum := sha256.Sum256([]byte("session-update-audit\x00" + digest + "\x00" + result.ID))
		var event ulid.ULID
		copy(event[:], eventSum[:16])
		if e = tx.PutAudit(ctx, providerapp.Audit{ID: event.String(), Action: "session.updated", AggregateID: result.ID, Actor: actor, Metadata: meta, CreatedAt: now}); e != nil {
			return e
		}
		return tx.PutIdempotency(ctx, providerapp.Record{Operation: "session.update", Key: key, Digest: digest, Response: response, CreatedAt: now, ExpiresAt: now.Add(24 * time.Hour)})
	})
	return result, err
}
