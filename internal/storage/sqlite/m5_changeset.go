package sqlite

// M5 ChangeSet persistence (T-5.2.4) on the agent-runtime transaction so a
// changeset commits atomically with its items and workspace state.

import (
	"database/sql"
	"time"

	"github.com/lunitide/lunitide/internal/domain/m5workspace"
	"github.com/oklog/ulid/v2"
)

func (t *agentRuntimeTx) PutM5ChangeSet(c m5workspace.ChangeSet) error {
	var applied any
	if c.AppliedAt != nil {
		applied = rfc(*c.AppliedAt)
	}
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO m5_changeset
		(id,run_id,workspace_id,base_digest,state,source,version,created_at,applied_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?)`,
		c.ID, c.RunID, c.WorkspaceID, c.BaseDigest, string(c.State), c.Source, c.Version, rfc(c.CreatedAt), applied, rfc(c.UpdatedAt))
	return t.fail(err)
}

const m5ChangeSetColumns = `id,run_id,workspace_id,base_digest,state,source,version,created_at,applied_at,updated_at`

func scanM5ChangeSet(s interface{ Scan(...any) error }) (m5workspace.ChangeSet, error) {
	var c m5workspace.ChangeSet
	var state, created, updated string
	var applied sql.NullString
	if err := s.Scan(&c.ID, &c.RunID, &c.WorkspaceID, &c.BaseDigest, &state, &c.Source, &c.Version, &created, &applied, &updated); err != nil {
		return c, err
	}
	c.State = m5workspace.ChangeSetState(state)
	var err error
	if c.CreatedAt, err = parseRFC(created); err != nil {
		return c, err
	}
	if c.UpdatedAt, err = parseRFC(updated); err != nil {
		return c, err
	}
	if applied.Valid {
		at, err := parseRFC(applied.String)
		if err != nil {
			return c, err
		}
		c.AppliedAt = &at
	}
	return c, nil
}

func (t *agentRuntimeTx) GetM5ChangeSet(id string) (m5workspace.ChangeSet, error) {
	c, err := scanM5ChangeSet(t.tx.QueryRowContext(t.ctx, `SELECT `+m5ChangeSetColumns+` FROM m5_changeset WHERE id=?`, id))
	if err == sql.ErrNoRows {
		return c, m5workspace.ErrChangeSetNotFound
	}
	return c, err
}

// TransitionM5ChangeSetState CAS-updates the changeset state with an
// optional applied_at stamp, bumping the optimistic version.
func (t *agentRuntimeTx) TransitionM5ChangeSetState(id string, expectedVersion int64, to m5workspace.ChangeSetState, appliedAt time.Time, at time.Time) (m5workspace.ChangeSet, error) {
	var applied any
	if !appliedAt.IsZero() {
		applied = rfc(appliedAt)
	}
	res, err := t.tx.ExecContext(t.ctx, `UPDATE m5_changeset
		SET state=?, applied_at=COALESCE(?, applied_at), version=version+1, updated_at=?
		WHERE id=? AND version=?`,
		string(to), applied, rfc(at), id, expectedVersion)
	if err != nil {
		return m5workspace.ChangeSet{}, t.fail(err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return m5workspace.ChangeSet{}, m5workspace.ErrVersion
	}
	return t.GetM5ChangeSet(id)
}

func (t *agentRuntimeTx) PutM5ChangeSetItem(i m5workspace.ChangeSetItem) error {
	if i.ID == "" {
		i.ID = ulid.Make().String()
	}
	var rollback any
	if i.RollbackRef != "" {
		rollback = i.RollbackRef
	}
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO m5_changeset_item
		(id,changeset_id,path,change,patch_ref,sha256,size,rollback_ref)
		VALUES(?,?,?,?,?,?,?,?)`,
		i.ID, i.ChangeSetID, i.Path, i.Change, i.PatchRef, i.SHA256, i.Size, rollback)
	return t.fail(err)
}

const m5ChangeSetItemColumns = `id,changeset_id,path,change,patch_ref,sha256,size,rollback_ref`

func scanM5ChangeSetItem(s interface{ Scan(...any) error }) (m5workspace.ChangeSetItem, error) {
	var i m5workspace.ChangeSetItem
	var rollback sql.NullString
	if err := s.Scan(&i.ID, &i.ChangeSetID, &i.Path, &i.Change, &i.PatchRef, &i.SHA256, &i.Size, &rollback); err != nil {
		return i, err
	}
	i.RollbackRef = rollback.String
	return i, nil
}

func (t *agentRuntimeTx) ListM5ChangeSetItems(changesetID string) ([]m5workspace.ChangeSetItem, error) {
	rows, err := t.tx.QueryContext(t.ctx, `SELECT `+m5ChangeSetItemColumns+` FROM m5_changeset_item WHERE changeset_id=? ORDER BY path,id`, changesetID)
	if err != nil {
		return nil, t.fail(err)
	}
	defer rows.Close()
	var out []m5workspace.ChangeSetItem
	for rows.Next() {
		i, err := scanM5ChangeSetItem(rows)
		if err != nil {
			return nil, t.fail(err)
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

// SetM5ChangeSetItemRollback records the CAS address of the pre-apply bytes.
func (t *agentRuntimeTx) SetM5ChangeSetItemRollback(itemID, ref string) error {
	res, err := t.tx.ExecContext(t.ctx, `UPDATE m5_changeset_item SET rollback_ref=? WHERE id=?`, ref, itemID)
	if err != nil {
		return t.fail(err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return t.fail(m5workspace.ErrChangeSetNotFound)
	}
	return nil
}
