// T-6.5.x audit bridge (slice 5B): adapts the stdioworker runtime audit
// sink onto the m6 UnitOfWork so every stdio.worker.* lifecycle event of
// the controlled implementation lands in audit_events (migration 0050).
//
// Each event commits in its own short transaction: the events fire from
// the monitor goroutine and Recover() at host startup, outside any task
// transaction. The bridge refuses actions outside the 0050 catalog — the
// schema CHECK would reject them anyway; failing loudly in development
// beats a runtime CHECK abort mid-recovery.
package m6app

import (
	"context"
	"fmt"

	"github.com/lunitide/lunitide/internal/providerapp"
	"github.com/lunitide/lunitide/internal/stdioworker"
	"github.com/oklog/ulid/v2"
)

// stdioAuditActions is the closed action catalog of migration 0050.
var stdioAuditActions = map[string]bool{
	stdioworker.AuditLaunched:  true,
	stdioworker.AuditCompleted: true,
	stdioworker.AuditRevoked:   true,
	stdioworker.AuditExpired:   true,
	stdioworker.AuditRecovered: true,
}

// StdioAuditSink implements stdioworker.AuditSink against the m6 store.
type StdioAuditSink struct {
	uow   UnitOfWork
	clock Clock
}

// NewStdioAuditSink wires the worker runtime to the audit tables.
func NewStdioAuditSink(uow UnitOfWork) *StdioAuditSink {
	return &StdioAuditSink{uow: uow, clock: systemClock{}}
}

// SetClock substitutes the wall clock (tests).
func (s *StdioAuditSink) SetClock(c Clock) { s.clock = c }

// Emit persists one stdio.worker.* event.
func (s *StdioAuditSink) Emit(action, aggregateID, actor string, metadata []byte) error {
	if !stdioAuditActions[action] {
		return fmt.Errorf("m6app: stdio audit action %q not in the 0050 catalog", action)
	}
	if len(metadata) < 2 {
		metadata = []byte(`{}`)
	}
	audit := providerapp.Audit{
		ID: ulid.Make().String(), Action: action, AggregateID: aggregateID,
		Actor: actor, Metadata: metadata, CreatedAt: s.clock.Now(),
	}
	return s.uow.TransactM6(context.Background(), func(tx Tx) error {
		return tx.PutAudit(audit)
	})
}
