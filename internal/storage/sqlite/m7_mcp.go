// M7 slice 8 storage (T-7.8.x): mcp_endpoint_settings / mcp_market_items
// on the agent-runtime single-writer transaction. Market rows are
// insert-only (the migration-0060 UPDATE trigger keeps them read-only);
// endpoint state transitions are guarded by the domain state machine.
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

// TransactMcp runs an m7app slice-8 use case on the shared single-writer
// transaction.
func (r *AgentRuntimeRepository) TransactMcp(ctx context.Context, fn func(m7app.McpTx) error) error {
	return r.Transact(ctx, func(tx agentrun.Tx) error {
		mtx, ok := tx.(m7app.McpTx)
		if !ok {
			return errors.New("agent runtime tx does not satisfy m7app.McpTx")
		}
		return fn(mtx)
	})
}

const mcpEndpointColumns = `endpoint_id,transport,command,args_json,url,origin,source_trust,enabled,state,capability_digest,pinned_digest,last_health_at,created_at`

func scanMcpEndpoint(s interface{ Scan(...any) error }) (m7flow.McpEndpointConfig, error) {
	var e m7flow.McpEndpointConfig
	var command, argsJSON, urlRef, capability, pinned, lastHealth *string
	var enabled int
	if err := s.Scan(&e.EndpointID, &e.Transport, &command, &argsJSON, &urlRef, &e.Origin,
		&e.SourceTrust, &enabled, &e.State, &capability, &pinned, &lastHealth, &e.CreatedAt); err != nil {
		return e, err
	}
	e.Enabled = enabled == 1
	if command != nil {
		e.Command = *command
	}
	if argsJSON != nil {
		e.ArgsJSON = *argsJSON
	}
	if urlRef != nil {
		e.URL = *urlRef
	}
	if capability != nil {
		e.CapabilityDigest = *capability
	}
	if pinned != nil {
		e.PinnedDigest = *pinned
	}
	if lastHealth != nil {
		e.LastHealthAt = *lastHealth
	}
	return e, nil
}

func (t *agentRuntimeTx) PutMcpEndpoint(e m7flow.McpEndpointConfig) error {
	var command, argsJSON, urlRef any
	if e.Command != "" {
		command = e.Command
	}
	if e.ArgsJSON != "" {
		argsJSON = e.ArgsJSON
	}
	if e.URL != "" {
		urlRef = e.URL
	}
	enabled := 0
	if e.Enabled {
		enabled = 1
	}
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO mcp_endpoint_settings
		(endpoint_id,transport,command,args_json,url,origin,source_trust,enabled,state,capability_digest,pinned_digest,last_health_at,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		e.EndpointID, e.Transport, command, argsJSON, urlRef, e.Origin, e.SourceTrust,
		enabled, e.State, nil, nil, nil, e.CreatedAt)
	return t.fail(err)
}

func (t *agentRuntimeTx) GetMcpEndpoint(id string) (m7flow.McpEndpointConfig, error) {
	e, err := scanMcpEndpoint(t.tx.QueryRowContext(t.ctx,
		`SELECT `+mcpEndpointColumns+` FROM mcp_endpoint_settings WHERE endpoint_id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return e, m7flow.ErrNotFound
	}
	return e, t.fail(err)
}

func (t *agentRuntimeTx) FindMcpEndpointByFingerprint(transport, command, urlRef, argsJSON string) (m7flow.McpEndpointConfig, error) {
	e, err := scanMcpEndpoint(t.tx.QueryRowContext(t.ctx,
		`SELECT `+mcpEndpointColumns+` FROM mcp_endpoint_settings
		 WHERE transport=? AND IFNULL(command,'')=? AND IFNULL(url,'')=? AND IFNULL(args_json,'')=?`,
		transport, command, urlRef, argsJSON))
	if errors.Is(err, sql.ErrNoRows) {
		return e, m7flow.ErrNotFound
	}
	return e, t.fail(err)
}

func (t *agentRuntimeTx) ListMcpEndpoints(transport string) ([]m7flow.McpEndpointConfig, error) {
	q := `SELECT ` + mcpEndpointColumns + ` FROM mcp_endpoint_settings`
	var args []any
	if transport != "" {
		q += ` WHERE transport=?`
		args = append(args, transport)
	}
	q += ` ORDER BY created_at, endpoint_id`
	rows, err := t.tx.QueryContext(t.ctx, q, args...)
	if err != nil {
		return nil, t.fail(err)
	}
	defer rows.Close()
	var out []m7flow.McpEndpointConfig
	for rows.Next() {
		e, err := scanMcpEndpoint(rows)
		if err != nil {
			return nil, t.fail(err)
		}
		out = append(out, e)
	}
	return out, t.fail(rows.Err())
}

func (t *agentRuntimeTx) CountMcpEndpoints() (int, error) {
	var n int
	err := t.tx.QueryRowContext(t.ctx, `SELECT COUNT(*) FROM mcp_endpoint_settings`).Scan(&n)
	return n, t.fail(err)
}

func (t *agentRuntimeTx) UpdateMcpEndpointState(id, from, to string, capabilityDigest *string, checkedAt time.Time) error {
	if !m7flow.McpTransitionAllowed(from, to) {
		return m7app.ErrIllegalTransition
	}
	var capability any
	if capabilityDigest != nil {
		capability = *capabilityDigest
	}
	res, err := t.tx.ExecContext(t.ctx, `UPDATE mcp_endpoint_settings
		SET state=?, capability_digest=COALESCE(?,capability_digest), last_health_at=?
		WHERE endpoint_id=? AND state=?`,
		to, capability, checkedAt.UTC().Format(time.RFC3339), id, from)
	if err != nil {
		return t.fail(err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return m7app.ErrIllegalTransition
	}
	// pin is recorded when transitioning into ready
	if to == m7flow.McpStateReady {
		_, err = t.tx.ExecContext(t.ctx, `UPDATE mcp_endpoint_settings
			SET pinned_digest=capability_digest WHERE endpoint_id=? AND (pinned_digest IS NULL OR pinned_digest='')`, id)
		return t.fail(err)
	}
	return nil
}

func (t *agentRuntimeTx) SetMcpEndpointEnabled(id string, enabled bool) error {
	v := 0
	if enabled {
		v = 1
	}
	_, err := t.tx.ExecContext(t.ctx,
		`UPDATE mcp_endpoint_settings SET enabled=? WHERE endpoint_id=?`, v, id)
	return t.fail(err)
}

// ── market cache ───────────────────────────────────────────────────────────

const mcpMarketColumns = `id,name,publisher,description,transport_hint,install_config_json,catalog_digest,signature,fetched_at`

func scanMcpMarketItem(s interface{ Scan(...any) error }) (m7flow.McpMarketItem, error) {
	var it m7flow.McpMarketItem
	err := s.Scan(&it.ID, &it.Name, &it.Publisher, &it.Description, &it.TransportHint,
		&it.InstallConfigJSON, &it.CatalogDigest, &it.Signature, &it.FetchedAt)
	return it, err
}

func (t *agentRuntimeTx) PutMcpMarketItem(it m7flow.McpMarketItem) error {
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO mcp_market_items
		(id,name,publisher,description,transport_hint,install_config_json,catalog_digest,signature,fetched_at)
		VALUES(?,?,?,?,?,?,?,?,?)`,
		it.ID, it.Name, it.Publisher, it.Description, it.TransportHint,
		it.InstallConfigJSON, it.CatalogDigest, it.Signature, it.FetchedAt)
	return t.fail(err)
}

func (t *agentRuntimeTx) GetMcpMarketItem(id string) (m7flow.McpMarketItem, error) {
	it, err := scanMcpMarketItem(t.tx.QueryRowContext(t.ctx,
		`SELECT `+mcpMarketColumns+` FROM mcp_market_items WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return it, m7flow.ErrNotFound
	}
	return it, t.fail(err)
}

func (t *agentRuntimeTx) FindMcpMarketItemByName(name string) (m7flow.McpMarketItem, error) {
	it, err := scanMcpMarketItem(t.tx.QueryRowContext(t.ctx,
		`SELECT `+mcpMarketColumns+` FROM mcp_market_items WHERE name=?`, name))
	if errors.Is(err, sql.ErrNoRows) {
		return it, m7flow.ErrNotFound
	}
	return it, t.fail(err)
}

func (t *agentRuntimeTx) ListMcpMarketItems(query string, afterID string, limit int) ([]m7flow.McpMarketItem, error) {
	q := `SELECT ` + mcpMarketColumns + ` FROM mcp_market_items WHERE 1=1`
	var args []any
	if query != "" {
		q += ` AND (name LIKE ? OR publisher LIKE ? OR description LIKE ?)`
		pattern := "%" + query + "%"
		args = append(args, pattern, pattern, pattern)
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
