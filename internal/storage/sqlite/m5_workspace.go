package sqlite

import (
	"database/sql"
	"time"

	"github.com/lunitide/lunitide/internal/domain/m5workspace"
)

// M5 AdHocWorkspace persistence on the same agent-runtime transaction so
// workspace state changes commit atomically with run events (T-5.1.4).

func (t *agentRuntimeTx) PutM5Workspace(w m5workspace.Workspace) error {
	if err := w.Validate(); err != nil {
		return err
	}
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO m5_adhoc_workspace
		(id,run_id,root_canonical,display_path,grant_json,lease_expiry,base_digest,quota_soft,quota_hard,used_bytes,state,version,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		w.ID, w.RunID, w.RootCanonical, w.DisplayPath, w.GrantJSON, rfc(w.LeaseExpiry), w.BaseDigest,
		w.QuotaSoft, w.QuotaHard, w.UsedBytes, string(w.State), w.Version, rfc(w.CreatedAt), rfc(w.UpdatedAt))
	return t.fail(err)
}

const m5WorkspaceColumns = `id,run_id,root_canonical,display_path,grant_json,lease_expiry,base_digest,quota_soft,quota_hard,used_bytes,state,version,created_at,updated_at`

func scanM5Workspace(s interface{ Scan(...any) error }) (m5workspace.Workspace, error) {
	var w m5workspace.Workspace
	var state, lease, created, updated string
	if err := s.Scan(&w.ID, &w.RunID, &w.RootCanonical, &w.DisplayPath, &w.GrantJSON, &lease, &w.BaseDigest,
		&w.QuotaSoft, &w.QuotaHard, &w.UsedBytes, &state, &w.Version, &created, &updated); err != nil {
		return w, err
	}
	w.State = m5workspace.State(state)
	var err error
	if w.LeaseExpiry, err = parseRFC(lease); err != nil {
		return w, err
	}
	if w.CreatedAt, err = parseRFC(created); err != nil {
		return w, err
	}
	if w.UpdatedAt, err = parseRFC(updated); err != nil {
		return w, err
	}
	return w, nil
}

func (t *agentRuntimeTx) GetM5Workspace(id string) (m5workspace.Workspace, error) {
	w, err := scanM5Workspace(t.tx.QueryRowContext(t.ctx, `SELECT `+m5WorkspaceColumns+` FROM m5_adhoc_workspace WHERE id=?`, id))
	if err == sql.ErrNoRows {
		return w, m5workspace.ErrNotFound
	}
	return w, err
}

// GetM5WorkspaceByRoot resolves the live workspace owning a canonical root
// (partial unique index excludes deleted rows).
func (t *agentRuntimeTx) GetM5WorkspaceByRoot(root string) (m5workspace.Workspace, error) {
	w, err := scanM5Workspace(t.tx.QueryRowContext(t.ctx, `SELECT `+m5WorkspaceColumns+` FROM m5_adhoc_workspace WHERE root_canonical=? AND state != 'deleted'`, root))
	if err == sql.ErrNoRows {
		return w, m5workspace.ErrNotFound
	}
	return w, err
}

func (t *agentRuntimeTx) ListM5Workspaces() ([]m5workspace.Workspace, error) {
	rows, err := t.tx.QueryContext(t.ctx, `SELECT `+m5WorkspaceColumns+` FROM m5_adhoc_workspace WHERE state NOT IN ('retained','deleted') ORDER BY created_at,id`)
	if err != nil {
		return nil, t.fail(err)
	}
	defer rows.Close()
	var out []m5workspace.Workspace
	for rows.Next() {
		w, err := scanM5Workspace(rows)
		if err != nil {
			return nil, t.fail(err)
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// TransitionM5Workspace CAS-updates state (and optionally usage), bumping the
// optimistic version. usedBytes < 0 keeps the current value.
func (t *agentRuntimeTx) TransitionM5Workspace(id string, expectedVersion int64, to m5workspace.State, usedBytes int64, lease time.Time, at time.Time) (m5workspace.Workspace, error) {
	res, err := t.tx.ExecContext(t.ctx, `UPDATE m5_adhoc_workspace
		SET state=?, used_bytes=CASE WHEN ? >= 0 THEN ? ELSE used_bytes END,
		    lease_expiry=CASE WHEN ? != '0001-01-01T00:00:00Z' THEN ? ELSE lease_expiry END,
		    version=version+1, updated_at=?
		WHERE id=? AND version=?`,
		string(to), usedBytes, usedBytes, rfc(lease), rfc(lease), rfc(at), id, expectedVersion)
	if err != nil {
		return m5workspace.Workspace{}, t.fail(err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return m5workspace.Workspace{}, m5workspace.ErrVersion
	}
	return t.GetM5Workspace(id)
}
