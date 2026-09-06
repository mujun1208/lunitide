// M10 wave-4 storage (T4-2): the computer-control transaction surface over
// the shared single-writer agent-runtime transaction. cc_security_config is
// a lazily seeded singleton; cc_audit_log is the append-only ledger
// (UPDATE/DELETE aborted by triggers, mirrored into audit_events).
package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/lunitide/lunitide/internal/ccapp"
	"github.com/lunitide/lunitide/internal/domain/agentrun"
)

// TransactCc runs a ccapp use case on the shared single-writer transaction.
func (r *AgentRuntimeRepository) TransactCc(ctx context.Context, fn func(ccapp.Tx) error) error {
	return r.Transact(ctx, func(tx agentrun.Tx) error {
		ctx, ok := tx.(ccapp.Tx)
		if !ok {
			return errors.New("agent runtime tx does not satisfy ccapp.Tx")
		}
		return fn(ctx)
	})
}

// ── settings singleton ──────────────────────────────────────────────────────

const ccSettingsColumns = `enabled,security_level,allow_critical,process_blocklist_json,
	max_actions_per_minute,confirm_timeout_seconds,emergency_stopped,emergency_stopped_at,armed_until,updated_at,revision`

// getCcSettings decodes one singleton row (scan + flag decode).
func getCcSettings(s interface{ Scan(...any) error }) (ccapp.Settings, error) {
	var out ccapp.Settings
	var blocklist string
	var enabled, allowCritical, emergency int
	var stoppedAt, armedUntil sql.NullString
	if err := s.Scan(&enabled, &out.SecurityLevel, &allowCritical, &blocklist,
		&out.MaxActionsPerMinute, &out.ConfirmTimeoutSecond, &emergency,
		&stoppedAt, &armedUntil, &out.UpdatedAt, &out.Revision); err != nil {
		return out, err
	}
	out.Enabled = enabled == 1
	out.AllowCritical = allowCritical == 1
	out.EmergencyStopped = emergency == 1
	out.EmergencyStoppedAt = stoppedAt.String
	out.ArmedUntil = armedUntil.String
	out.ProcessBlocklist = []string{}
	_ = json.Unmarshal([]byte(blocklist), &out.ProcessBlocklist)
	if out.ProcessBlocklist == nil {
		out.ProcessBlocklist = []string{}
	}
	return out, nil
}

// GetCcSettings answers the singleton, seeding the default row on first read.
func (t *agentRuntimeTx) GetCcSettings() (ccapp.Settings, error) {
	out, err := getCcSettings(t.tx.QueryRowContext(t.ctx,
		`SELECT `+ccSettingsColumns+` FROM cc_security_config WHERE id=1`))
	if errors.Is(err, sql.ErrNoRows) {
		seed := ccapp.Settings{
			Revision:             1,
			SecurityLevel:        ccapp.LevelStandard,
			AllowCritical:        false,
			ProcessBlocklist:     append([]string(nil), ccapp.DefaultProcessBlocklist...),
			MaxActionsPerMinute:  ccapp.CcDefaultMaxActionsPerMinute,
			ConfirmTimeoutSecond: 60,
			UpdatedAt:            time.Now().UTC().Format(time.RFC3339),
		}
		if err := t.PutCcSettings(seed); err != nil {
			return ccapp.Settings{}, err
		}
		return seed, nil
	}
	return out, t.fail(err)
}

// PutCcSettings upserts the singleton row.
func (t *agentRuntimeTx) PutCcSettings(v ccapp.Settings) error {
	blocklist, err := json.Marshal(v.ProcessBlocklist)
	if err != nil {
		return t.fail(err)
	}
	enabled, allowCritical, emergency := 0, 0, 0
	if v.Enabled {
		enabled = 1
	}
	if v.AllowCritical {
		allowCritical = 1
	}
	if v.EmergencyStopped {
		emergency = 1
	}
	var stoppedAt, armedUntil any
	if v.EmergencyStoppedAt != "" {
		stoppedAt = v.EmergencyStoppedAt
	}
	if v.ArmedUntil != "" {
		armedUntil = v.ArmedUntil
	}
	result, err := t.tx.ExecContext(t.ctx, `INSERT INTO cc_security_config
		(id,enabled,security_level,allow_critical,process_blocklist_json,
		 max_actions_per_minute,confirm_timeout_seconds,emergency_stopped,emergency_stopped_at,armed_until,updated_at,revision)
		VALUES(1,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET
			enabled=excluded.enabled, security_level=excluded.security_level,
			allow_critical=excluded.allow_critical, process_blocklist_json=excluded.process_blocklist_json,
			max_actions_per_minute=excluded.max_actions_per_minute,
			confirm_timeout_seconds=excluded.confirm_timeout_seconds,
			emergency_stopped=excluded.emergency_stopped,
			emergency_stopped_at=excluded.emergency_stopped_at,
			armed_until=excluded.armed_until, updated_at=excluded.updated_at, revision=excluded.revision
		WHERE cc_security_config.revision=excluded.revision-1`,
		enabled, v.SecurityLevel, allowCritical, string(blocklist),
		v.MaxActionsPerMinute, v.ConfirmTimeoutSecond, emergency, stoppedAt, armedUntil, v.UpdatedAt, v.Revision)
	if err != nil {
		return t.fail(err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return t.fail(err)
	}
	if n != 1 {
		return t.fail(ccapp.ErrCcConflict)
	}
	return nil
}

// ── append-only audit ledger ────────────────────────────────────────────────

// AppendCcAudit inserts one ledger row. UPDATE/DELETE are aborted by the
// schema triggers with M10-CC-003.
func (t *agentRuntimeTx) AppendCcAudit(e ccapp.AuditEntry) error {
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO cc_audit_log
		(entry_id,session_id,tool,action,risk_level,status,layer,detail_json,created_at)
		VALUES(?,?,?,?,?,?,?,?,?)`,
		e.EntryID, e.SessionID, e.Tool, e.Action, e.RiskLevel, e.Status,
		e.Layer, e.Detail, e.CreatedAt)
	return t.fail(err)
}

func (t *agentRuntimeTx) PendingCcIntents(limit int) ([]ccapp.PendingIntent, error) {
	if limit < 1 || limit > 200 {
		limit = 200
	}
	rows, err := t.tx.QueryContext(t.ctx, `SELECT json_extract(a.metadata_json,'$.operationId'),a.aggregate_id,json_extract(a.metadata_json,'$.tool'),json_extract(a.metadata_json,'$.risk') FROM audit_events a WHERE a.action='cc.operation.confirmed' AND json_valid(a.metadata_json) AND json_extract(a.metadata_json,'$.phase')='prepared' AND json_extract(a.metadata_json,'$.operationId') IS NOT NULL AND NOT EXISTS (SELECT 1 FROM cc_audit_log l WHERE json_valid(l.detail_json) AND json_extract(l.detail_json,'$.operationId')=json_extract(a.metadata_json,'$.operationId') AND json_extract(l.detail_json,'$.phase')='receipt') ORDER BY a.created_at,a.id LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ccapp.PendingIntent
	for rows.Next() {
		var v ccapp.PendingIntent
		if err = rows.Scan(&v.OperationID, &v.SessionID, &v.Tool, &v.Risk); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// ListCcAudit answers the newest entries with optional status/session
// filters, bounded by the caller's limit.
func (t *agentRuntimeTx) ListCcAudit(limit int, status, sessionID string) ([]ccapp.AuditEntry, error) {
	q := strings.Builder{}
	q.WriteString(`SELECT entry_id,session_id,tool,action,risk_level,status,layer,detail_json,created_at
		FROM cc_audit_log`)
	var args []any
	var conds []string
	if status != "" {
		conds = append(conds, `status=?`)
		args = append(args, status)
	}
	if sessionID != "" {
		conds = append(conds, `session_id=?`)
		args = append(args, sessionID)
	}
	if len(conds) > 0 {
		q.WriteString(` WHERE ` + strings.Join(conds, ` AND `))
	}
	q.WriteString(` ORDER BY created_at DESC, entry_id DESC LIMIT ?`)
	args = append(args, limit)
	rows, err := t.tx.QueryContext(t.ctx, q.String(), args...)
	if err != nil {
		return nil, t.fail(err)
	}
	defer rows.Close()
	var out []ccapp.AuditEntry
	for rows.Next() {
		var e ccapp.AuditEntry
		if err := rows.Scan(&e.EntryID, &e.SessionID, &e.Tool, &e.Action,
			&e.RiskLevel, &e.Status, &e.Layer, &e.Detail, &e.CreatedAt); err != nil {
			return nil, t.fail(err)
		}
		out = append(out, e)
	}
	return out, t.fail(rows.Err())
}
