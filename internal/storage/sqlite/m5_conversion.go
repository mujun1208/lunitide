package sqlite

// M5 T-5.5.1 m5_workspace_conversion persistence on the agent-runtime
// transaction so conversion phase transitions commit atomically with run
// events. The domain types live in internal/workspace (convert.go).

import (
	"database/sql"
	"time"

	"github.com/lunitide/lunitide/internal/workspace"
)

const m5ConversionColumns = `id,run_id,source_workspace_id,target_project_id,preview_digest,scope_json,phase,publish_journal,committed,committed_at,audit_event_id,created_at`

func scanM5Conversion(s interface{ Scan(...any) error }) (workspace.Conversion, error) {
	var c workspace.Conversion
	var phase, created string
	var committedAt sql.NullString
	var committed int
	if err := s.Scan(&c.ID, &c.RunID, &c.SourceWorkspaceID, &c.TargetProjectID, &c.PreviewDigest,
		&c.ScopeJSON, &phase, &c.PublishJournal, &committed, &committedAt, &c.AuditEventID, &created); err != nil {
		return c, err
	}
	c.Phase = workspace.ConversionPhase(phase)
	c.Committed = committed == 1
	var err error
	if c.CreatedAt, err = parseRFC(created); err != nil {
		return c, err
	}
	if committedAt.Valid {
		at, err := parseRFC(committedAt.String)
		if err != nil {
			return c, err
		}
		c.CommittedAt = &at
	}
	return c, nil
}

func (t *agentRuntimeTx) PutM5Conversion(c workspace.Conversion) error {
	var committedAt any
	if c.CommittedAt != nil {
		committedAt = rfc(*c.CommittedAt)
	}
	committed := 0
	if c.Committed {
		committed = 1
	}
	if c.PublishJournal == "" {
		c.PublishJournal = "{}"
	}
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO m5_workspace_conversion
		(id,run_id,source_workspace_id,target_project_id,preview_digest,scope_json,phase,publish_journal,committed,committed_at,audit_event_id,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		c.ID, c.RunID, c.SourceWorkspaceID, c.TargetProjectID, c.PreviewDigest, c.ScopeJSON,
		string(c.Phase), c.PublishJournal, committed, committedAt, c.AuditEventID, rfc(c.CreatedAt))
	return t.fail(err)
}

func (t *agentRuntimeTx) GetM5Conversion(id string) (workspace.Conversion, error) {
	c, err := scanM5Conversion(t.tx.QueryRowContext(t.ctx, `SELECT `+m5ConversionColumns+` FROM m5_workspace_conversion WHERE id=?`, id))
	if err == sql.ErrNoRows {
		return c, workspace.ErrConvertNotFound
	}
	return c, err
}

// GetM5ConversionBySource resolves the newest in-flight (non-terminal)
// conversion of a workspace, if any.
func (t *agentRuntimeTx) GetM5ConversionBySource(workspaceID string) (workspace.Conversion, error) {
	c, err := scanM5Conversion(t.tx.QueryRowContext(t.ctx, `SELECT `+m5ConversionColumns+`
		FROM m5_workspace_conversion WHERE source_workspace_id=? AND phase NOT IN ('committed','failed','abandoned')
		ORDER BY created_at DESC, id DESC LIMIT 1`, workspaceID))
	if err == sql.ErrNoRows {
		return c, workspace.ErrConvertNotFound
	}
	return c, err
}

// UpdateM5ConversionPhase CAS-advances the phase (WHERE id=? AND phase=?).
// Zero rows means the phase already moved on: workspace.ErrConversionPhase.
// The table carries no updated_at column; at is accepted for call symmetry
// and used by callers for audit trails.
func (t *agentRuntimeTx) UpdateM5ConversionPhase(id string, from, to workspace.ConversionPhase, at time.Time) error {
	res, err := t.tx.ExecContext(t.ctx, `UPDATE m5_workspace_conversion SET phase=? WHERE id=? AND phase=?`,
		string(to), id, string(from))
	if err != nil {
		return t.fail(err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return t.fail(workspace.ErrConversionPhase)
	}
	return nil
}

// UpdateM5ConversionJournal overwrites the publish crash-recovery journal.
// No phase CAS: the journal is recovery data, not conversion state, and is
// only written while the conversion row stays in its current phase.
func (t *agentRuntimeTx) UpdateM5ConversionJournal(id, journal string) error {
	_, err := t.tx.ExecContext(t.ctx,
		`UPDATE m5_workspace_conversion SET publish_journal=? WHERE id=?`, journal, id)
	return t.fail(err)
}

// MarkM5ConversionCommitted finalises a conversion: phase=committed,
// committed=1 and committed_at land in one statement (the table CHECK
// binds all three together), CAS-guarded on phase='publishing' and
// clearing the crash-recovery journal in the same write.
func (t *agentRuntimeTx) MarkM5ConversionCommitted(id string, at time.Time) error {
	res, err := t.tx.ExecContext(t.ctx, `UPDATE m5_workspace_conversion
		SET phase='committed', committed=1, committed_at=?, publish_journal='{}'
		WHERE id=? AND phase='publishing'`, rfc(at), id)
	if err != nil {
		return t.fail(err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return t.fail(workspace.ErrConversionPhase)
	}
	return nil
}
