package ccapp

import (
	"context"
	"crypto/sha256"
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
func (s *Service) recordAudit(ctx context.Context, session, tool, risk, status, layer string, detail map[string]any, ts string) error {
	action := "cc.operation.executed"
	switch status {
	case StatusBlocked:
		action = "cc.operation.blocked"
	case StatusDenied, StatusStopped:
		action = "cc.tool.denied"
	}
	return s.writeAudit(ctx, session, tool, risk, status, layer, action, detail, ts)
}

// writeAudit persists the ledger row and the audit_events mirror on one
// transaction. Rejection callers join this error with their original denial;
// successful OS dispatch callers must not report success without this receipt.
func (s *Service) writeAudit(ctx context.Context, session, tool, risk, status, layer, action string, detail map[string]any, ts string) error {
	if detail == nil {
		detail = map[string]any{}
	}
	raw, err := json.Marshal(detail)
	if err != nil {
		return err
	}
	if len(raw) > 4096 {
		return fmt.Errorf("%w: audit detail exceeds limit", ErrCcSchema)
	}
	now, _ := time.Parse(time.RFC3339, ts)
	return s.uow.TransactCc(ctx, func(tx Tx) error {
		return writeAuditTx(tx, session, tool, risk, status, layer, action, detail, string(raw), ts, now)
	})
}

func writeAuditTx(tx Tx, session, tool, risk, status, layer, action string, detail map[string]any, raw, ts string, now time.Time) error {
	if err := tx.AppendCcAudit(AuditEntry{
		EntryID: ulid.Make().String(), SessionID: session, Tool: tool,
		Action: action, RiskLevel: risk, Status: status, Layer: layer,
		Detail: raw, CreatedAt: ts,
	}); err != nil {
		return err
	}
	meta, _ := json.Marshal(map[string]any{
		"tool": tool, "risk": risk, "status": status, "operationId": detail["operationId"], "phase": detail["phase"], "dispatched": detail["dispatched"],
	})
	return tx.PutAudit(providerapp.Audit{
		ID: ulid.Make().String(), Action: action,
		AggregateID: session, Actor: "agent-runtime",
		Metadata: meta, CreatedAt: now,
	})
}

type PendingIntent struct{ OperationID, SessionID, Tool, Risk string }
type pendingIntentReader interface {
	PendingCcIntents(int) ([]PendingIntent, error)
}

// ReconcilePendingIntents runs before readiness. A prepared operation without
// a receipt has an unknown outcome; recovery never replays native host calls.
func (s *Service) ReconcilePendingIntents(ctx context.Context) (int, error) {
	total := 0
	for {
		count := 0
		err := s.uow.TransactCc(ctx, func(tx Tx) error {
			reader, ok := tx.(pendingIntentReader)
			if !ok {
				return fmt.Errorf("%w: recovery reader unavailable", ErrCcAuditUnavailable)
			}
			pending, err := reader.PendingCcIntents(200)
			if err != nil {
				return err
			}
			if len(pending) == 0 {
				return nil
			}
			s.revokeExecution(ErrCcEmergency, true)
			now := s.clock.Now().UTC()
			ts := now.Format(time.RFC3339)
			settings, err := tx.GetCcSettings()
			if err != nil {
				return err
			}
			settings.Enabled, settings.EmergencyStopped, settings.ArmedUntil = false, true, ""
			settings.EmergencyStoppedAt, settings.UpdatedAt = ts, ts
			settings.Revision++
			if err = tx.PutCcSettings(settings); err != nil {
				return err
			}
			for _, intent := range pending {
				detail := map[string]any{"operationId": intent.OperationID, "phase": "receipt", "outcome": "unknown", "recovered": true, "summary": "引擎重启前的操作回执未确认；操作可能已经发生，请核对桌面后重新启用"}
				raw, err := json.Marshal(detail)
				if err != nil {
					return err
				}
				if err = writeAuditTx(tx, intent.SessionID, intent.Tool, intent.Risk, StatusFailed, LayerIntent, "cc.operation.executed", detail, string(raw), ts, now); err != nil {
					return err
				}
			}
			count = len(pending)
			return nil
		})
		if err != nil {
			return total, err
		}
		total += count
		if count == 0 {
			return total, nil
		}
	}
}

// The existing authorization action records intent in audit_events. It is not
// an executed cc ledger row; only terminal receipts enter that ledger.
func (s *Service) prepareAudit(ctx context.Context, session, tool, mapped, risk string, args json.RawMessage, approved bool) (string, error) {
	id := ulid.Make().String()
	meta, _ := json.Marshal(map[string]any{"operationId": id, "phase": "prepared", "outcome": "pending", "tool": tool, "mapped": mapped, "risk": risk, "approved": approved, "argsDigest": fmt.Sprintf("%x", sha256.Sum256(args))})
	err := s.uow.TransactCc(ctx, func(tx Tx) error {
		return tx.PutAudit(providerapp.Audit{ID: ulid.Make().String(), Action: "cc.operation.confirmed", AggregateID: session, Actor: "agent-runtime", Metadata: meta, CreatedAt: s.clock.Now().UTC()})
	})
	return id, err
}
