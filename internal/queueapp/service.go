// Package queueapp implements admission and durable delivery of queued input.
// The engine uses DeliveryStore receipts to prepare messages, start a turn,
// and reconcile its outcome without losing input when a response is lost.
package queueapp

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/lunitide/lunitide/internal/domain/queueinput"
)

// Service-level errors mapped by the Bridge handlers onto M10-QI codes.
var (
	ErrPayloadInvalid  = errors.New("queue payload invalid")
	ErrSessionNotFound = errors.New("session not found")
	ErrQueueFull       = queueinput.ErrCapacity
	ErrRateLimited     = queueinput.ErrRateLimited
	ErrNotFound        = queueinput.ErrNotFound
	ErrTerminalState   = queueinput.ErrSettled
	ErrRequestReused   = queueinput.ErrRequestReused
)

// Store is the persistence surface the service needs (backed by *sqlite.Store).
type Store interface {
	SessionExists(ctx context.Context, sessionID string) (bool, error)
	EnqueueQueuedMessage(ctx context.Context, sessionID, runID, payload, mark, requestID string) (queueinput.Message, error)
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
	if n := utf8.RuneCountInString(payload); n < 1 || n > queueinput.MaxPayloadChars || len(payload) > queueinput.MaxPayloadBytes || !utf8.ValidString(payload) || strings.ContainsRune(payload, 0) {
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
	// Admission, complete-payload idempotency and quotas share the writer
	// transaction; separate reads allow concurrent requests to bypass the limits.
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
	return s.store.WithdrawQueuedMessage(ctx, sessionID, id)
}

// Consume settles every queued row as injected and returns them in seq
// order; an empty result means nothing was pending.
func (s *Service) Consume(ctx context.Context, sessionID string) ([]queueinput.Message, error) {
	if s == nil || s.store == nil {
		return nil, ErrSessionNotFound
	}
	return s.store.ConsumeQueuedMessages(ctx, sessionID)
}
