// T-6.4.4 application service: the M6 transactional outbox. Producers
// append events in the SAME single-writer transaction as the state change
// they describe (merge transitions, final-gate verdicts); the publisher
// drains unpublished rows and delivers them at-least-once — consumers
// dedupe by event id, because a crash between delivery and the published
// mark replays the same batch.
//
// Retention: published rows may be pruned after 30 days; unpublished rows
// are never pruned (zero loss over the retention window).
package m6app

import (
	"context"
	"errors"
	"time"

	"github.com/lunitide/lunitide/internal/domain/m6supply"
	"github.com/oklog/ulid/v2"
)

// ErrOutboxUnavailable: the service is not wired.
var ErrOutboxUnavailable = errors.New("m6app: outbox service unavailable")

// OutboxRetention is the published-row retention window (design: 30 days).
const OutboxRetention = 30 * 24 * time.Hour

// Publisher is the delivery sink: it receives one batch and reports
// per-event success. Events it does NOT report delivered stay unpublished
// and are retried on the next drain.
type Publisher interface {
	Publish(ctx context.Context, events []m6supply.OutboxEvent) ([]string, error)
}

// OutboxService implements the append side (inside caller transactions)
// and the drain side (outside transactions, at-least-once).
type OutboxService struct {
	uow   UnitOfWork
	clock Clock
}

func NewOutboxService(uow UnitOfWork) *OutboxService {
	return &OutboxService{uow: uow, clock: systemClock{}}
}

// SetClock substitutes the wall clock (tests).
func (s *OutboxService) SetClock(c Clock) { s.clock = c }

// AppendTx records one event inside the caller's open transaction — the
// "same transaction as the state change" half of the pattern. The event
// id is allocated here so consumers can dedupe replays by id.
func (s *OutboxService) AppendTx(tx Tx, aggregateType, aggregateID, eventType, payload string) error {
	if tx == nil {
		return ErrOutboxUnavailable
	}
	return tx.AppendM6Outbox(m6supply.OutboxEvent{
		ID:            ulid.Make().String(),
		AggregateType: aggregateType,
		AggregateID:   aggregateID,
		EventType:     eventType,
		Payload:       payload,
		CreatedAt:     s.clock.Now().UTC(),
	})
}

// DrainResult reports one drain pass.
type DrainResult struct {
	Fetched   int
	Delivered int
	Remaining int
}

// Drain fetches up to `limit` unpublished events, hands them to the
// publisher and marks exactly the reported-delivered ids as published in
// one transaction. Anything else (publish error, partial delivery, crash)
// leaves rows unpublished — the next drain re-delivers them, which is why
// consumers must be idempotent by event id.
func (s *OutboxService) Drain(ctx context.Context, pub Publisher, limit int) (DrainResult, error) {
	if s == nil || s.uow == nil {
		return DrainResult{}, ErrOutboxUnavailable
	}
	if pub == nil {
		return DrainResult{}, ErrOutboxUnavailable
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	var out DrainResult
	events, err := s.listUnpublished(ctx, limit)
	if err != nil {
		return out, err
	}
	out.Fetched = len(events)
	if len(events) == 0 {
		return out, nil
	}
	delivered, err := pub.Publish(ctx, events)
	if err != nil {
		return out, err
	}
	if len(delivered) == 0 {
		return out, nil
	}
	now := s.clock.Now().UTC()
	if err := s.uow.TransactM6(ctx, func(tx Tx) error {
		return tx.MarkM6OutboxPublished(delivered, now)
	}); err != nil {
		return out, err
	}
	out.Delivered = len(delivered)
	remaining, err := s.countUnpublished(ctx)
	if err != nil {
		return out, err
	}
	out.Remaining = int(remaining)
	return out, nil
}

func (s *OutboxService) listUnpublished(ctx context.Context, limit int) ([]m6supply.OutboxEvent, error) {
	var events []m6supply.OutboxEvent
	err := s.uow.TransactM6(ctx, func(tx Tx) error {
		var ierr error
		events, ierr = tx.ListUnpublishedM6Outbox(limit)
		return ierr
	})
	return events, err
}

func (s *OutboxService) countUnpublished(ctx context.Context) (int64, error) {
	var n int64
	err := s.uow.TransactM6(ctx, func(tx Tx) error {
		var ierr error
		n, ierr = tx.CountUnpublishedM6Outbox()
		return ierr
	})
	return n, err
}

// PrunePublished drops published rows older than the retention window.
// Unpublished rows are never touched.
func (s *OutboxService) PrunePublished(ctx context.Context) (int64, error) {
	if s == nil || s.uow == nil {
		return 0, ErrOutboxUnavailable
	}
	cutoff := s.clock.Now().UTC().Add(-OutboxRetention)
	var n int64
	err := s.uow.TransactM6(ctx, func(tx Tx) error {
		var ierr error
		n, ierr = tx.PruneM6Outbox(cutoff)
		return ierr
	})
	return n, err
}
