// Package queueapp implements the M10 queued-input service (wave 2):
// capacity/rate/idempotency gates around the durable 0074 queue. The
// service never touches the chat pipeline — the renderer consumes the
// queue after a stream settles and replays it as the next message.
package queueapp

import (
	"context"
	"errors"
	"time"
	"unicode/utf8"

	"github.com/lunitide/lunitide/internal/domain/queueinput"
	"github.com/lunitide/lunitide/internal/storage/sqlite"
)

// Service-level errors mapped by the Bridge handlers onto M10-QI codes.
var (
	ErrPayloadInvalid  = errors.New("queue payload invalid")
	ErrSessionNotFound = errors.New("session not found")
	ErrQueueFull       = errors.New("queue capacity reached")
	ErrRateLimited     = errors.New("queue rate limited")
	ErrNotFound        = errors.New("queued message not found")
	ErrTerminalState   = errors.New("queued message already settled")
	ErrRequestReused   = errors.New("request id already settled")
)

// Store is the persistence surface the service needs (backed by *sqlite.Store).
type Store interface {
	SessionExists(ctx context.Context, sessionID string) (bool, error)
	EnqueueQueuedMessage(ctx context.Context, sessionID, runID, payload, mark, requestID string) (queueinput.Message, error)
	GetQueuedByRequest(ctx context.Context, sessionID, requestID string) (queueinput.Message, error)
	CountQueued(ctx context.Context, sessionID string) (int, error)
	CountQueuedSince(ctx context.Context, sessionID string, since time.Time) (int, error)
	ListQueued(ctx context.Context, sessionID string) ([]queueinput.Message, error)
	WithdrawQueuedMessage(ctx context.Context, sessionID, id string) (queueinput.Message, error)
	ConsumeQueuedMessages(ctx context.Context, sessionID string) ([]queueinput.Message, error)
}

// Service wires the queue store; a nil store fails closed.
type Service struct {
	store Store
}

// New returns a Service over the given store.
func New(store Store) *Service { return &Service{store: store} }

// Enqueue validates and stores one supplement. Replaying the same
// requestId while the row is still queued is idempotent; after the row
// settled the key is burned (ErrRequestReused).
func (s *Service) Enqueue(ctx context.Context, sessionID, runID, payload, mark, requestID string) (queueinput.Message, error) {
	if s == nil || s.store == nil {
		return queueinput.Message{}, ErrSessionNotFound
	}
	if n := utf8.RuneCountInString(payload); n < 1 || n > queueinput.MaxPayloadChars {
		return queueinput.Message{}, ErrPayloadInvalid
	}
	if mark == "" {
		mark = queueinput.MarkTurnBoundary
	}
	if !queueinput.ValidMark(mark) || len(requestID) < 1 || len(requestID) > 128 {
		return queueinput.Message{}, ErrPayloadInvalid
	}
	ok, err := s.store.SessionExists(ctx, sessionID)
	if err != nil {
		return queueinput.Message{}, err
	}
	if !ok {
		return queueinput.Message{}, ErrSessionNotFound
	}
	if existing, err := s.store.GetQueuedByRequest(ctx, sessionID, requestID); err != nil {
		return queueinput.Message{}, err
	} else if existing.ID != "" {
		if existing.Status == queueinput.StatusQueued {
			return existing, nil // idempotent replay
		}
		return queueinput.Message{}, ErrRequestReused
	}
	if n, err := s.store.CountQueued(ctx, sessionID); err != nil {
		return queueinput.Message{}, err
	} else if n >= queueinput.MaxQueuedPerSession {
		return queueinput.Message{}, ErrQueueFull
	}
	if n, err := s.store.CountQueuedSince(ctx, sessionID, time.Now().UTC().Add(-time.Minute)); err != nil {
		return queueinput.Message{}, err
	} else if n >= queueinput.MaxPerMinute {
		return queueinput.Message{}, ErrRateLimited
	}
	return s.store.EnqueueQueuedMessage(ctx, sessionID, runID, payload, mark, requestID)
}

// List returns the queued rows of the session in seq order.
func (s *Service) List(ctx context.Context, sessionID string) ([]queueinput.Message, error) {
	if s == nil || s.store == nil {
		return nil, ErrSessionNotFound
	}
	return s.store.ListQueued(ctx, sessionID)
}

// Withdraw settles one queued row before it is consumed.
func (s *Service) Withdraw(ctx context.Context, sessionID, id string) (queueinput.Message, error) {
	if s == nil || s.store == nil {
		return queueinput.Message{}, ErrSessionNotFound
	}
	m, err := s.store.WithdrawQueuedMessage(ctx, sessionID, id)
	if errors.Is(err, sqlite.ErrQueuedMessageNotFound) {
		return queueinput.Message{}, ErrNotFound
	}
	if errors.Is(err, sqlite.ErrQueuedMessageSettled) {
		return queueinput.Message{}, ErrTerminalState
	}
	return m, err
}

// Consume settles every queued row as injected and returns them in seq
// order; an empty result means nothing was pending.
func (s *Service) Consume(ctx context.Context, sessionID string) ([]queueinput.Message, error) {
	if s == nil || s.store == nil {
		return nil, ErrSessionNotFound
	}
	return s.store.ConsumeQueuedMessages(ctx, sessionID)
}
