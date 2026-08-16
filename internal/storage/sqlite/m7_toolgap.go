// M7 slice 7 storage (T-7.7.x): tool_manifest_v2 / db_connections /
// tool_call_quota plus idempotent tool results on the agent-runtime
// single-writer transaction. Quota rows are upserted under the same guard
// values the service froze (M7-TOOL-006 family).
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

// TransactToolgap runs an m7app slice-7 use case on the shared single-writer
// transaction.
func (r *AgentRuntimeRepository) TransactToolgap(ctx context.Context, fn func(m7app.ToolgapTx) error) error {
	return r.Transact(ctx, func(tx agentrun.Tx) error {
		ttx, ok := tx.(m7app.ToolgapTx)
		if !ok {
			return errors.New("agent runtime tx does not satisfy m7app.ToolgapTx")
		}
		return fn(ttx)
	})
}

// ── manifest ────────────────────────────────────────────────────────────────

func scanToolManifest(s interface{ Scan(...any) error }) (m7flow.ToolManifestEntry, error) {
	var e m7flow.ToolManifestEntry
	var enabled int
	if err := s.Scan(&e.ToolName, &e.DescriptorVersion, &e.ManifestJSON, &e.ManifestDigest,
		&e.IOSemantics, &e.TimeoutMS, &enabled, &e.ImportedAt); err != nil {
		return e, err
	}
	e.Enabled = enabled == 1
	return e, nil
}

func (t *agentRuntimeTx) GetToolManifest(toolName string) (m7flow.ToolManifestEntry, error) {
	e, err := scanToolManifest(t.tx.QueryRowContext(t.ctx,
		`SELECT tool_name,descriptor_version,manifest_json,manifest_digest,io_semantics,timeout_ms,enabled,imported_at
		 FROM tool_manifest_v2 WHERE tool_name=?`, toolName))
	if errors.Is(err, sql.ErrNoRows) {
		return e, m7flow.ErrNotFound
	}
	return e, t.fail(err)
}

func (t *agentRuntimeTx) PutToolManifest(e m7flow.ToolManifestEntry) error {
	enabled := 0
	if e.Enabled {
		enabled = 1
	}
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO tool_manifest_v2
		(tool_name,descriptor_version,manifest_json,manifest_digest,io_semantics,timeout_ms,enabled,imported_at)
		VALUES(?,?,?,?,?,?,?,?)`,
		e.ToolName, e.DescriptorVersion, e.ManifestJSON, e.ManifestDigest,
		e.IOSemantics, e.TimeoutMS, enabled, e.ImportedAt)
	return t.fail(err)
}

func (t *agentRuntimeTx) CountToolManifest() (int, error) {
	var n int
	err := t.tx.QueryRowContext(t.ctx, `SELECT COUNT(*) FROM tool_manifest_v2`).Scan(&n)
	return n, t.fail(err)
}

// ── db connections ─────────────────────────────────────────────────────────

func scanDBConnection(s interface{ Scan(...any) error }) (m7flow.DBConnection, error) {
	var c m7flow.DBConnection
	var verified *string
	if err := s.Scan(&c.ID, &c.Name, &c.Kind, &c.DSNSecretRef, &verified, &c.CreatedAt, &c.CreatedBy); err != nil {
		return c, err
	}
	c.ReadOnlyVerifiedAt = verified
	return c, nil
}

func (t *agentRuntimeTx) GetDBConnection(id string) (m7flow.DBConnection, error) {
	c, err := scanDBConnection(t.tx.QueryRowContext(t.ctx,
		`SELECT id,name,kind,dsn_secret_ref,readonly_verified_at,created_at,created_by
		 FROM db_connections WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return c, m7flow.ErrNotFound
	}
	return c, t.fail(err)
}

func (t *agentRuntimeTx) PutDBConnection(c m7flow.DBConnection) error {
	var verified any
	if c.ReadOnlyVerifiedAt != nil {
		verified = *c.ReadOnlyVerifiedAt
	}
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO db_connections
		(id,name,kind,dsn_secret_ref,readonly_verified_at,created_at,created_by) VALUES(?,?,?,?,?,?,?)`,
		c.ID, c.Name, c.Kind, c.DSNSecretRef, verified, c.CreatedAt, c.CreatedBy)
	return t.fail(err)
}

// ── quota ──────────────────────────────────────────────────────────────────

// BeginToolCall increments in_flight/calls_total and refuses over-quota
// calls (concurrency / call count / byte budget - never amplified).
func (t *agentRuntimeTx) BeginToolCall(runID, toolName string) error {
	res, err := t.tx.ExecContext(t.ctx, `INSERT INTO tool_call_quota
		(run_id,tool_name,in_flight,calls_total,bytes_total,updated_at) VALUES(?,?,1,1,0,?)
		ON CONFLICT(run_id,tool_name) DO UPDATE SET
		 in_flight=CASE WHEN in_flight >= ? THEN -1 ELSE in_flight+1 END,
		 calls_total=calls_total+1, updated_at=excluded.updated_at
		 WHERE in_flight >= 0 AND calls_total < ?`,
		runID, toolName, time.Now().UTC().Format(time.RFC3339), m7app.ToolMaxConcurrent, m7app.ToolMaxCallsPerRun)
	if err != nil {
		return t.fail(err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errors.New("quota exceeded")
	}
	return nil
}

// EndToolCall decrements in_flight and accumulates bytes.
func (t *agentRuntimeTx) EndToolCall(runID, toolName string, bytes int64) error {
	_, err := t.tx.ExecContext(t.ctx, `UPDATE tool_call_quota
		SET in_flight=MAX(in_flight-1,0), bytes_total=bytes_total+?, updated_at=?
		WHERE run_id=? AND tool_name=?`,
		bytes, time.Now().UTC().Format(time.RFC3339), runID, toolName)
	return t.fail(err)
}

// ── tool results (P2 idempotency) ──────────────────────────────────────────

func (t *agentRuntimeTx) PutToolResult(r m7flow.ToolResult) error {
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO tool_results
		(run_id,tool_name,idempotency_key,result_json,result_digest,created_at) VALUES(?,?,?,?,?,?)`,
		r.RunID, r.ToolName, r.IdempotencyKey, r.ResultJSON, r.ResultDigest, r.CreatedAt)
	return t.fail(err)
}

func (t *agentRuntimeTx) GetToolResult(runID, idempotencyKey string) (m7flow.ToolResult, error) {
	var r m7flow.ToolResult
	err := t.tx.QueryRowContext(t.ctx,
		`SELECT run_id,tool_name,idempotency_key,result_json,result_digest,created_at
		 FROM tool_results WHERE run_id=? AND idempotency_key=?`, runID, idempotencyKey).
		Scan(&r.RunID, &r.ToolName, &r.IdempotencyKey, &r.ResultJSON, &r.ResultDigest, &r.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return r, m7flow.ErrNotFound
	}
	return r, t.fail(err)
}
