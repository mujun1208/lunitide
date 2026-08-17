// M10 wave-3 storage (T3-B): the browser multi-mode transaction surface
// over the shared single-writer agent-runtime transaction. br_settings is
// a lazily seeded singleton; br_sessions upserts drive the CDP state
// machine; br_data_usage keeps the latest snapshot per session;
// br_permissions is the ask/allow/deny approval queue.
package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/lunitide/lunitide/internal/brapp"
	"github.com/lunitide/lunitide/internal/domain/agentrun"
	"github.com/lunitide/lunitide/internal/domain/m7flow"
)

// TransactBr runs a brapp use case on the shared single-writer transaction.
func (r *AgentRuntimeRepository) TransactBr(ctx context.Context, fn func(brapp.Tx) error) error {
	return r.Transact(ctx, func(tx agentrun.Tx) error {
		btx, ok := tx.(brapp.Tx)
		if !ok {
			return errors.New("agent runtime tx does not satisfy brapp.Tx")
		}
		return fn(btx)
	})
}

// ── settings singleton ──────────────────────────────────────────────────────

const brSettingsColumns = `mode,chrome_path,edge_path,extension_port,allowlist_json,
	data_retention_days,block_private_networks,updated_at`

func scanBrSettings(s interface{ Scan(...any) error }) (brapp.Settings, error) {
	var out brapp.Settings
	var allowlist string
	var block int
	if err := s.Scan(&out.Mode, &out.ChromePath, &out.EdgePath, &out.ExtensionPort,
		&allowlist, &out.DataRetentionDays, &block, &out.UpdatedAt); err != nil {
		return out, err
	}
	out.BlockPrivateNetwork = block == 1
	out.Allowlist = []string{}
	_ = json.Unmarshal([]byte(allowlist), &out.Allowlist)
	if out.Allowlist == nil {
		out.Allowlist = []string{}
	}
	return out, nil
}

// GetBrSettings answers the singleton, seeding the default row on first read.
func (t *agentRuntimeTx) GetBrSettings() (brapp.Settings, error) {
	out, err := scanBrSettings(t.tx.QueryRowContext(t.ctx,
		`SELECT `+brSettingsColumns+` FROM br_settings WHERE id=1`))
	if errors.Is(err, sql.ErrNoRows) {
		seed := brapp.Settings{
			Mode: brapp.ModeBuiltin, ExtensionPort: 9222, Allowlist: []string{},
			DataRetentionDays: 30, BlockPrivateNetwork: true,
			UpdatedAt: time.Now().UTC().Format(time.RFC3339),
		}
		if err := t.PutBrSettings(seed); err != nil {
			return brapp.Settings{}, err
		}
		return seed, nil
	}
	return out, t.fail(err)
}

// PutBrSettings upserts the singleton row.
func (t *agentRuntimeTx) PutBrSettings(s brapp.Settings) error {
	allowlist, err := json.Marshal(s.Allowlist)
	if err != nil {
		return t.fail(err)
	}
	block := 0
	if s.BlockPrivateNetwork {
		block = 1
	}
	_, err = t.tx.ExecContext(t.ctx, `INSERT INTO br_settings
		(id,mode,chrome_path,edge_path,extension_port,allowlist_json,data_retention_days,block_private_networks,updated_at)
		VALUES(1,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET
			mode=excluded.mode, chrome_path=excluded.chrome_path, edge_path=excluded.edge_path,
			extension_port=excluded.extension_port, allowlist_json=excluded.allowlist_json,
			data_retention_days=excluded.data_retention_days,
			block_private_networks=excluded.block_private_networks, updated_at=excluded.updated_at`,
		s.Mode, s.ChromePath, s.EdgePath, s.ExtensionPort, string(allowlist),
		s.DataRetentionDays, block, s.UpdatedAt)
	return t.fail(err)
}

// ── sessions ────────────────────────────────────────────────────────────────

const brSessionColumns = `session_id,mode,state,ws_url,detail,connected_at,updated_at`

func scanBrSession(s interface{ Scan(...any) error }) (brapp.Session, error) {
	var out brapp.Session
	var wsURL, detail, connected sql.NullString
	err := s.Scan(&out.SessionID, &out.Mode, &out.State, &wsURL, &detail, &connected, &out.UpdatedAt)
	out.WsURL = wsURL.String
	out.Detail = detail.String
	out.ConnectedAt = connected.String
	return out, err
}

// GetBrSession answers one tracked session.
func (t *agentRuntimeTx) GetBrSession(id string) (brapp.Session, error) {
	out, err := scanBrSession(t.tx.QueryRowContext(t.ctx,
		`SELECT `+brSessionColumns+` FROM br_sessions WHERE session_id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return out, m7flow.ErrNotFound
	}
	return out, t.fail(err)
}

// PutBrSession upserts one session row (state-machine write).
func (t *agentRuntimeTx) PutBrSession(s brapp.Session) error {
	var connected any
	if s.ConnectedAt != "" {
		connected = s.ConnectedAt
	}
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO br_sessions
		(session_id,mode,state,ws_url,detail,connected_at,updated_at)
		VALUES(?,?,?,?,?,?,?)
		ON CONFLICT(session_id) DO UPDATE SET
			mode=excluded.mode, state=excluded.state, ws_url=excluded.ws_url,
			detail=excluded.detail, connected_at=excluded.connected_at,
			updated_at=excluded.updated_at`,
		s.SessionID, s.Mode, s.State, s.WsURL, s.Detail, connected, s.UpdatedAt)
	return t.fail(err)
}

// ListBrSessions answers all sessions newest-first.
func (t *agentRuntimeTx) ListBrSessions() ([]brapp.Session, error) {
	rows, err := t.tx.QueryContext(t.ctx,
		`SELECT `+brSessionColumns+` FROM br_sessions ORDER BY updated_at DESC, session_id`)
	if err != nil {
		return nil, t.fail(err)
	}
	defer rows.Close()
	var out []brapp.Session
	for rows.Next() {
		s, err := scanBrSession(rows)
		if err != nil {
			return nil, t.fail(err)
		}
		out = append(out, s)
	}
	return out, t.fail(rows.Err())
}

// ── data usage ──────────────────────────────────────────────────────────────

func scanBrUsage(s interface{ Scan(...any) error }) (brapp.DataUsage, error) {
	var out brapp.DataUsage
	err := s.Scan(&out.SessionID, &out.ProfileBytes, &out.CacheBytes,
		&out.CookiesBytes, &out.ComputedAt, &out.UpdatedAt)
	return out, err
}

// PutBrDataUsage upserts the latest snapshot for one session.
func (t *agentRuntimeTx) PutBrDataUsage(u brapp.DataUsage) error {
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO br_data_usage
		(session_id,profile_bytes,cache_bytes,cookies_bytes,computed_at,updated_at)
		VALUES(?,?,?,?,?,?)
		ON CONFLICT(session_id) DO UPDATE SET
			profile_bytes=excluded.profile_bytes, cache_bytes=excluded.cache_bytes,
			cookies_bytes=excluded.cookies_bytes, computed_at=excluded.computed_at,
			updated_at=excluded.updated_at`,
		u.SessionID, u.ProfileBytes, u.CacheBytes, u.CookiesBytes, u.ComputedAt, u.UpdatedAt)
	return t.fail(err)
}

// ListBrDataUsage answers all persisted snapshots.
func (t *agentRuntimeTx) ListBrDataUsage() ([]brapp.DataUsage, error) {
	rows, err := t.tx.QueryContext(t.ctx,
		`SELECT session_id,profile_bytes,cache_bytes,cookies_bytes,computed_at,updated_at
		 FROM br_data_usage ORDER BY session_id`)
	if err != nil {
		return nil, t.fail(err)
	}
	defer rows.Close()
	var out []brapp.DataUsage
	for rows.Next() {
		u, err := scanBrUsage(rows)
		if err != nil {
			return nil, t.fail(err)
		}
		out = append(out, u)
	}
	return out, t.fail(rows.Err())
}

// DeleteBrDataUsage drops one snapshot (post-clear).
func (t *agentRuntimeTx) DeleteBrDataUsage(sessionID string) error {
	_, err := t.tx.ExecContext(t.ctx, `DELETE FROM br_data_usage WHERE session_id=?`, sessionID)
	return t.fail(err)
}

// ── permissions ─────────────────────────────────────────────────────────────

const brPermissionColumns = `permission_id,origin,permission,policy,state,session_id,decided_at,created_at`

func scanBrPermission(s interface{ Scan(...any) error }) (brapp.Permission, error) {
	var out brapp.Permission
	var session, decided sql.NullString
	err := s.Scan(&out.PermissionID, &out.Origin, &out.Permission, &out.Policy,
		&out.State, &session, &decided, &out.CreatedAt)
	out.SessionID = session.String
	out.DecidedAt = decided.String
	return out, err
}

// PutBrPermission inserts one approval-queue row.
func (t *agentRuntimeTx) PutBrPermission(p brapp.Permission) error {
	var decided any
	if p.DecidedAt != "" {
		decided = p.DecidedAt
	}
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO br_permissions
		(permission_id,origin,permission,policy,state,session_id,decided_at,created_at)
		VALUES(?,?,?,?,?,?,?,?)`,
		p.PermissionID, p.Origin, p.Permission, p.Policy, p.State, p.SessionID, decided, p.CreatedAt)
	return t.fail(err)
}

// GetBrPermission answers one row.
func (t *agentRuntimeTx) GetBrPermission(id string) (brapp.Permission, error) {
	out, err := scanBrPermission(t.tx.QueryRowContext(t.ctx,
		`SELECT `+brPermissionColumns+` FROM br_permissions WHERE permission_id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return out, m7flow.ErrNotFound
	}
	return out, t.fail(err)
}

// FindBrPermission answers the latest row for one origin+permission pair.
func (t *agentRuntimeTx) FindBrPermission(origin, permission string) (brapp.Permission, error) {
	out, err := scanBrPermission(t.tx.QueryRowContext(t.ctx,
		`SELECT `+brPermissionColumns+` FROM br_permissions
		 WHERE origin=? AND permission=? ORDER BY created_at DESC, permission_id DESC LIMIT 1`,
		origin, permission))
	if errors.Is(err, sql.ErrNoRows) {
		return out, m7flow.ErrNotFound
	}
	return out, t.fail(err)
}

// ListBrPermissions answers the queue with an optional state filter.
func (t *agentRuntimeTx) ListBrPermissions(state string) ([]brapp.Permission, error) {
	q := `SELECT ` + brPermissionColumns + ` FROM br_permissions`
	var args []any
	if state != "" {
		q += ` WHERE state=?`
		args = append(args, state)
	}
	q += ` ORDER BY created_at DESC, permission_id DESC`
	rows, err := t.tx.QueryContext(t.ctx, q, args...)
	if err != nil {
		return nil, t.fail(err)
	}
	defer rows.Close()
	var out []brapp.Permission
	for rows.Next() {
		p, err := scanBrPermission(rows)
		if err != nil {
			return nil, t.fail(err)
		}
		out = append(out, p)
	}
	return out, t.fail(rows.Err())
}

// UpdateBrPermissionState is the conditional pending→decided gate.
func (t *agentRuntimeTx) UpdateBrPermissionState(id, from, to, decidedAt string) error {
	var decided any
	if decidedAt != "" {
		decided = decidedAt
	}
	res, err := t.tx.ExecContext(t.ctx,
		`UPDATE br_permissions SET state=?, decided_at=? WHERE permission_id=? AND state=?`,
		to, decided, id, from)
	if err != nil {
		return t.fail(err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return m7flow.ErrNotFound
	}
	return nil
}

// ApplyBrPermissionPolicy swaps the policy (and resolves a pending row
// when the new policy is not ask).
func (t *agentRuntimeTx) ApplyBrPermissionPolicy(id, policy, state, decidedAt string) error {
	var decided any
	if decidedAt != "" {
		decided = decidedAt
	}
	res, err := t.tx.ExecContext(t.ctx,
		`UPDATE br_permissions SET policy=?, state=?, decided_at=? WHERE permission_id=?`,
		policy, state, decided, id)
	if err != nil {
		return t.fail(err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return m7flow.ErrNotFound
	}
	return nil
}
