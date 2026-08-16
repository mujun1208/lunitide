// M7 slice 6 storage (T-7.6.x): subagent_runs / subagent_observations on
// the agent-runtime single-writer transaction. Observations are appended
// inside the same tx that transitions the run; the migration-0058 triggers
// keep them WORM (M7-EVD-001).
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/lunitide/lunitide/internal/domain/agentrun"
	"github.com/lunitide/lunitide/internal/domain/m7flow"
	"github.com/lunitide/lunitide/internal/m7app"
)

// TransactSubagent runs an m7app slice-6 use case on the shared single-writer
// transaction.
func (r *AgentRuntimeRepository) TransactSubagent(ctx context.Context, fn func(m7app.SubagentTx) error) error {
	return r.Transact(ctx, func(tx agentrun.Tx) error {
		stx, ok := tx.(m7app.SubagentTx)
		if !ok {
			return errors.New("agent runtime tx does not satisfy m7app.SubagentTx")
		}
		return fn(stx)
	})
}

const sagRunColumns = `id,root_run_id,stage_run_id,purpose,capability_digest,policy_version,persona_digest,status,budget_tokens,spent_tokens,deadline_ms,idempotency_key,created_at,completed_at`

func scanSubagentRun(s interface{ Scan(...any) error }) (m7flow.SubagentRun, error) {
	var r m7flow.SubagentRun
	var stage, persona, completed *string
	if err := s.Scan(&r.ID, &r.RootRunID, &stage, &r.Purpose, &r.CapabilityDigest,
		&r.PolicyVersion, &persona, &r.Status, &r.BudgetTokens, &r.SpentTokens,
		&r.DeadlineMS, &r.IdempotencyKey, &r.CreatedAt, &completed); err != nil {
		return r, err
	}
	if stage != nil {
		r.StageRunID = *stage
	}
	if persona != nil {
		r.PersonaDigest = *persona
	}
	if completed != nil {
		r.CompletedAt = completed
	}
	return r, nil
}

func (t *agentRuntimeTx) PutSubagentRun(r m7flow.SubagentRun) error {
	var stage, persona any
	if r.StageRunID != "" {
		stage = r.StageRunID
	}
	if r.PersonaDigest != "" {
		persona = r.PersonaDigest
	}
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO subagent_runs
		(id,root_run_id,stage_run_id,purpose,capability_digest,policy_version,persona_digest,status,budget_tokens,spent_tokens,deadline_ms,idempotency_key,created_at,completed_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		r.ID, r.RootRunID, stage, r.Purpose, r.CapabilityDigest, r.PolicyVersion,
		persona, r.Status, r.BudgetTokens, r.SpentTokens, r.DeadlineMS, r.IdempotencyKey,
		r.CreatedAt, nil)
	return t.fail(err)
}

func (t *agentRuntimeTx) GetSubagentRun(id string) (m7flow.SubagentRun, error) {
	r, err := scanSubagentRun(t.tx.QueryRowContext(t.ctx,
		`SELECT `+sagRunColumns+` FROM subagent_runs WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return r, m7flow.ErrNotFound
	}
	return r, t.fail(err)
}

func (t *agentRuntimeTx) FindSubagentByIdempotency(rootRunID, key string) (m7flow.SubagentRun, error) {
	r, err := scanSubagentRun(t.tx.QueryRowContext(t.ctx,
		`SELECT `+sagRunColumns+` FROM subagent_runs WHERE root_run_id=? AND idempotency_key=?`,
		rootRunID, key))
	if errors.Is(err, sql.ErrNoRows) {
		return r, m7flow.ErrNotFound
	}
	return r, t.fail(err)
}

func (t *agentRuntimeTx) CountActiveSubagents(rootRunID string) (int, error) {
	var n int
	err := t.tx.QueryRowContext(t.ctx,
		`SELECT COUNT(*) FROM subagent_runs WHERE root_run_id=? AND status IN ('queued','running','orphaned')`,
		rootRunID).Scan(&n)
	return n, t.fail(err)
}

func (t *agentRuntimeTx) UpdateSubagentStatus(id, from, to string, spentTokens int64, completedAt *time.Time) error {
	if !m7flow.SagTransitionAllowed(from, to) {
		return m7app.ErrIllegalTransition
	}
	var completed any
	if completedAt != nil {
		completed = completedAt.UTC().Format(time.RFC3339)
	}
	res, err := t.tx.ExecContext(t.ctx,
		`UPDATE subagent_runs SET status=?, spent_tokens=?, completed_at=? WHERE id=? AND status=?`,
		to, spentTokens, completed, id, from)
	if err != nil {
		return t.fail(err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return m7app.ErrIllegalTransition
	}
	return nil
}

func (t *agentRuntimeTx) CancelActiveByRoot(rootRunID string, now time.Time) (int, error) {
	res, err := t.tx.ExecContext(t.ctx,
		`UPDATE subagent_runs SET status='cancelled', completed_at=? WHERE root_run_id=? AND status IN ('queued','running','orphaned')`,
		now.UTC().Format(time.RFC3339), rootRunID)
	if err != nil {
		return 0, t.fail(err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

func (t *agentRuntimeTx) ListSubagentsByRoot(rootRunID string, afterID string, limit int) ([]m7flow.SubagentRun, error) {
	q := `SELECT ` + sagRunColumns + ` FROM subagent_runs WHERE root_run_id=?`
	args := []any{rootRunID}
	if afterID != "" {
		q += ` AND id > ?`
		args = append(args, afterID)
	}
	q += ` ORDER BY id LIMIT ?`
	args = append(args, limit)
	rows, err := t.tx.QueryContext(t.ctx, q, args...)
	if err != nil {
		return nil, t.fail(err)
	}
	defer rows.Close()
	var out []m7flow.SubagentRun
	for rows.Next() {
		r, err := scanSubagentRun(rows)
		if err != nil {
			return nil, t.fail(err)
		}
		out = append(out, r)
	}
	return out, t.fail(rows.Err())
}

func (t *agentRuntimeTx) PutSubagentObservation(o m7flow.SubagentObservation) error {
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO subagent_observations
		(id,subagent_run_id,seq,evidence_id,summary,digest,created_at) VALUES(?,?,?,?,?,?,?)`,
		o.ID, o.SubagentRunID, o.Seq, o.EvidenceID, o.Summary, o.Digest, o.CreatedAt)
	return t.fail(err)
}

func (t *agentRuntimeTx) ListSubagentObservations(subagentRunID string) ([]m7flow.SubagentObservation, error) {
	rows, err := t.tx.QueryContext(t.ctx,
		`SELECT id,subagent_run_id,seq,evidence_id,summary,digest,created_at
		 FROM subagent_observations WHERE subagent_run_id=? ORDER BY seq`, subagentRunID)
	if err != nil {
		return nil, t.fail(err)
	}
	defer rows.Close()
	var out []m7flow.SubagentObservation
	for rows.Next() {
		var o m7flow.SubagentObservation
		if err := rows.Scan(&o.ID, &o.SubagentRunID, &o.Seq, &o.EvidenceID, &o.Summary, &o.Digest, &o.CreatedAt); err != nil {
			return nil, t.fail(err)
		}
		out = append(out, o)
	}
	return out, t.fail(rows.Err())
}