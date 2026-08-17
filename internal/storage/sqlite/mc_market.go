// M10 wave-3 storage (T3-A): the MCP-market transaction surface. Confirm
// tokens are single-use rows keyed by token hash; endpoint usage rows are
// UPSERT-aggregated and never deleted (statistics survive uninstalls).
// Market listing adds the transport-hint filter the mc.* catalog browse
// needs on top of the insert-only 0060 cache.
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/lunitide/lunitide/internal/domain/agentrun"
	"github.com/lunitide/lunitide/internal/domain/m7flow"
	"github.com/lunitide/lunitide/internal/mcapp"
)

// TransactMc runs an mcapp use case on the shared single-writer transaction.
func (r *AgentRuntimeRepository) TransactMc(ctx context.Context, fn func(mcapp.Tx) error) error {
	return r.Transact(ctx, func(tx agentrun.Tx) error {
		mtx, ok := tx.(mcapp.Tx)
		if !ok {
			return errors.New("agent runtime tx does not satisfy mcapp.Tx")
		}
		return fn(mtx)
	})
}

// ListMcMarket extends the insert-only catalog cache read with a
// transport-hint filter (mc.market.list).
func (t *agentRuntimeTx) ListMcMarket(query, transportHint, afterID string, limit int) ([]m7flow.McpMarketItem, error) {
	q := `SELECT ` + mcpMarketColumns + ` FROM mcp_market_items WHERE 1=1`
	var args []any
	if query != "" {
		q += ` AND (name LIKE ? OR publisher LIKE ? OR description LIKE ?)`
		pattern := "%" + query + "%"
		args = append(args, pattern, pattern, pattern)
	}
	if transportHint != "" {
		q += ` AND transport_hint=?`
		args = append(args, transportHint)
	}
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
	var out []m7flow.McpMarketItem
	for rows.Next() {
		it, err := scanMcpMarketItem(rows)
		if err != nil {
			return nil, t.fail(err)
		}
		out = append(out, it)
	}
	return out, t.fail(rows.Err())
}

// ── confirm tokens ──────────────────────────────────────────────────────────

func (t *agentRuntimeTx) PutConfirmToken(tok mcapp.ConfirmTokenRow) error {
	var consumed any
	if tok.ConsumedAt != "" {
		consumed = tok.ConsumedAt
	}
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO mc_confirm_tokens
		(token_hash,method,target,digest,issued_at,expires_at,consumed_at)
		VALUES(?,?,?,?,?,?,?)`,
		tok.TokenHash, tok.Method, tok.Target, tok.Digest, tok.IssuedAt, tok.ExpiresAt, consumed)
	return t.fail(err)
}

func (t *agentRuntimeTx) GetConfirmToken(tokenHash string) (mcapp.ConfirmTokenRow, error) {
	var tok mcapp.ConfirmTokenRow
	var consumed sql.NullString
	err := t.tx.QueryRowContext(t.ctx,
		`SELECT token_hash,method,target,digest,issued_at,expires_at,consumed_at
		 FROM mc_confirm_tokens WHERE token_hash=?`, tokenHash).
		Scan(&tok.TokenHash, &tok.Method, &tok.Target, &tok.Digest, &tok.IssuedAt, &tok.ExpiresAt, &consumed)
	if errors.Is(err, sql.ErrNoRows) {
		return tok, m7flow.ErrNotFound
	}
	if err == nil {
		tok.ConsumedAt = consumed.String
	}
	return tok, t.fail(err)
}

// ConsumeConfirmToken marks one unconsumed, unexpired token as used; the
// conditional UPDATE is the single-use gate (exactly one row must match).
func (t *agentRuntimeTx) ConsumeConfirmToken(tokenHash, consumedAt string) error {
	res, err := t.tx.ExecContext(t.ctx,
		`UPDATE mc_confirm_tokens SET consumed_at=?
		 WHERE token_hash=? AND consumed_at IS NULL AND expires_at > ?`,
		consumedAt, tokenHash, consumedAt)
	if err != nil {
		return t.fail(err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return mcapp.ErrMcConfirm
	}
	return nil
}

// ── endpoint target update (mc.connector.update) ────────────────────────────

// UpdateMcpEndpointTarget swaps the https url / stdio args and clears the
// capability + pinned digests so the next probe re-pins (drift discipline).
func (t *agentRuntimeTx) UpdateMcpEndpointTarget(id, urlRef, argsJSON string) error {
	var urlVal, argsVal any
	if urlRef != "" {
		urlVal = urlRef
	}
	if argsJSON != "" {
		argsVal = argsJSON
	}
	res, err := t.tx.ExecContext(t.ctx, `UPDATE mcp_endpoint_settings
		SET url=COALESCE(?,url), args_json=COALESCE(?,args_json),
		    capability_digest=NULL, pinned_digest=NULL
		WHERE endpoint_id=? AND state <> 'revoked'`,
		urlVal, argsVal, id)
	if err != nil {
		return t.fail(err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return m7flow.ErrNotFound
	}
	return nil
}

// ── usage statistics ────────────────────────────────────────────────────────

func (t *agentRuntimeTx) UpsertEndpointUsage(endpointID string, delta mcapp.UsageDelta, at time.Time) error {
	ts := at.UTC().Format(time.RFC3339)
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO mc_endpoint_usage
		(endpoint_id,installs,updates,uninstalls,last_used_at,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?)
		ON CONFLICT(endpoint_id) DO UPDATE SET
			installs=installs+excluded.installs,
			updates=updates+excluded.updates,
			uninstalls=uninstalls+excluded.uninstalls,
			last_used_at=excluded.last_used_at,
			updated_at=excluded.updated_at`,
		endpointID, delta.Installs, delta.Updates, delta.Uninstalls, ts, ts, ts)
	return t.fail(err)
}

const mcUsageJoin = `SELECT u.endpoint_id,u.installs,u.updates,u.uninstalls,u.last_used_at,
	e.transport,e.state,e.origin,e.enabled
	FROM mc_endpoint_usage u JOIN mcp_endpoint_settings e ON e.endpoint_id=u.endpoint_id`

func scanMcUsage(s interface{ Scan(...any) error }) (mcapp.EndpointUsage, error) {
	var u mcapp.EndpointUsage
	var lastUsed sql.NullString
	var enabled int
	err := s.Scan(&u.EndpointID, &u.Installs, &u.Updates, &u.Uninstalls, &lastUsed,
		&u.Transport, &u.State, &u.Origin, &enabled)
	u.LastUsedAt = lastUsed.String
	u.Enabled = enabled == 1
	return u, err
}

func (t *agentRuntimeTx) GetEndpointUsage(endpointID string) (mcapp.EndpointUsage, error) {
	u, err := scanMcUsage(t.tx.QueryRowContext(t.ctx, mcUsageJoin+` WHERE u.endpoint_id=?`, endpointID))
	if errors.Is(err, sql.ErrNoRows) {
		return u, m7flow.ErrNotFound
	}
	return u, t.fail(err)
}

func (t *agentRuntimeTx) ListEndpointUsage() ([]mcapp.EndpointUsage, error) {
	rows, err := t.tx.QueryContext(t.ctx, mcUsageJoin+` ORDER BY u.created_at, u.endpoint_id`)
	if err != nil {
		return nil, t.fail(err)
	}
	defer rows.Close()
	var out []mcapp.EndpointUsage
	for rows.Next() {
		u, err := scanMcUsage(rows)
		if err != nil {
			return nil, t.fail(err)
		}
		out = append(out, u)
	}
	return out, t.fail(rows.Err())
}
