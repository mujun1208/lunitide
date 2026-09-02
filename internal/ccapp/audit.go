package ccapp

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/lunitide/lunitide/internal/providerapp"
	"github.com/oklog/ulid/v2"
)

// GetAuditLog answers the newest entries (bounded by CcMaxAuditEntries)
// with optional status/session filters.
func (s *Service) GetAuditLog(ctx context.Context, limit int, status, sessionID string) ([]AuditEntry, error) {
	if limit <= 0 || limit > CcMaxAuditEntries {
		limit = 50
	}
	if status != "" && status != StatusExecuted && status != StatusBlocked &&
		status != StatusDenied && status != StatusFailed && status != StatusStopped {
		return nil, fmt.Errorf("%w: status filter", ErrCcSchema)
	}
	var out []AuditEntry
	err := s.uow.TransactCc(ctx, func(tx Tx) error {
		var e error
		out, e = tx.ListCcAudit(limit, status, sessionID)
		return e
	})
	if err != nil {
		return nil, err
	}
	if out == nil {
		out = []AuditEntry{}
	}
	return out, nil
}

// recordAudit writes one ledger row plus the mirror audit_events action
// derived from the ledger status.
func (s *Service) recordAudit(ctx context.Context, session, tool, risk, status, layer string, detail map[string]any, ts string) {
	action := "cc.operation.executed"
	switch status {
	case StatusBlocked:
		action = "cc.operation.blocked"
	case StatusDenied, StatusStopped:
		action = "cc.tool.denied"
	}
	s.writeAudit(ctx, session, tool, risk, status, layer, action, detail, ts)
}

// writeAudit persists the ledger row and the audit_events mirror on one
// transaction. Audit writes are best-effort after a rejection: a ledger
// failure never masks the original error.
func (s *Service) writeAudit(ctx context.Context, session, tool, risk, status, layer, action string, detail map[string]any, ts string) {
	if detail == nil {
		detail = map[string]any{}
	}
	raw, err := json.Marshal(detail)
	if err != nil || len(raw) < 2 {
		raw = []byte("{}")
	}
	if len(raw) > 4096 {
		raw = raw[:4096]
	}
	now, _ := time.Parse(time.RFC3339, ts)
	_ = s.uow.TransactCc(ctx, func(tx Tx) error {
		if err := tx.AppendCcAudit(AuditEntry{
			EntryID: ulid.Make().String(), SessionID: session, Tool: tool,
			Action: action, RiskLevel: risk, Status: status, Layer: layer,
			Detail: string(raw), CreatedAt: ts,
		}); err != nil {
			return err
		}
		meta, _ := json.Marshal(map[string]any{
			"tool": tool, "risk": risk, "status": status,
		})
		return tx.PutAudit(providerapp.Audit{
			ID: ulid.Make().String(), Action: action,
			AggregateID: session, Actor: "agent-runtime",
			Metadata: meta, CreatedAt: now,
		})
	})
}
