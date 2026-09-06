package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/lunitide/lunitide/internal/datasourceapp"
	"github.com/lunitide/lunitide/internal/providerapp"
	"github.com/oklog/ulid/v2"
)

const datasourceWriteColumns = `id,request_key,connection_id,connection_name,sql_text,digest,target_digest,state,created_at,expires_at,result_json`

// RecoverDatasourceWrites runs before accepting requests after process startup.
// External SQL and SQLite cannot share a transaction; never replay its intent.
func (s *Store) RecoverDatasourceWrites(ctx context.Context) error {
	for {
		rows, err := s.db.QueryContext(ctx, `SELECT id FROM datasource_write_operations WHERE state='executing' LIMIT 100`)
		if err != nil {
			return err
		}
		var ids []string
		for rows.Next() {
			var id string
			if err = rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			ids = append(ids, id)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		if len(ids) == 0 {
			return nil
		}
		for _, id := range ids {
			if _, err = s.TransitionDatasourceWrite(ctx, id, "executing", "unknown", nil); err != nil {
				return err
			}
		}
	}
}

func scanDatasourceWrite(s interface{ Scan(...any) error }) (datasourceapp.WriteOperation, error) {
	var op datasourceapp.WriteOperation
	var result sql.NullString
	err := s.Scan(&op.ID, &op.RequestKey, &op.ConnectionID, &op.ConnectionName, &op.SQL, &op.Digest, &op.TargetDigest, &op.State, &op.CreatedAt, &op.ExpiresAt, &result)
	if err == sql.ErrNoRows {
		return op, datasourceapp.ErrNotFound
	}
	if err != nil {
		return op, err
	}
	if result.Valid {
		err = json.Unmarshal([]byte(result.String), &op.Result)
	}
	return op, err
}

func (s *Store) PrepareDatasourceWrite(ctx context.Context, op datasourceapp.WriteOperation) (datasourceapp.WriteOperation, error) {
	var out datasourceapp.WriteOperation
	err := s.do(ctx, func(t *txAdapter) error {
		saved, err := scanDatasourceWrite(t.q.QueryRowContext(ctx, `SELECT `+datasourceWriteColumns+` FROM datasource_write_operations WHERE request_key=?`, op.RequestKey))
		if err == nil {
			if saved.Digest != op.Digest {
				return datasourceapp.ErrWriteConflict
			}
			out = saved
			return nil
		}
		if err != datasourceapp.ErrNotFound {
			return err
		}
		_, err = t.q.ExecContext(ctx, `INSERT INTO datasource_write_operations (`+datasourceWriteColumns+`) VALUES(?,?,?,?,?,?,?,?,?,?,NULL)`, op.ID, op.RequestKey, op.ConnectionID, op.ConnectionName, op.SQL, op.Digest, op.TargetDigest, op.State, op.CreatedAt, op.ExpiresAt)
		if err != nil {
			return err
		}
		if err = t.datasourceWriteAudit(ctx, op.ID, op.ConnectionID, op.Digest, "prepared"); err != nil {
			return err
		}
		out = op
		return nil
	})
	return out, err
}

func (s *Store) GetDatasourceWrite(ctx context.Context, id string) (datasourceapp.WriteOperation, error) {
	return scanDatasourceWrite(s.db.QueryRowContext(ctx, `SELECT `+datasourceWriteColumns+` FROM datasource_write_operations WHERE id=?`, id))
}
func (s *Store) ListDatasourceWrites(ctx context.Context, id string) ([]datasourceapp.WriteOperation, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+datasourceWriteColumns+` FROM datasource_write_operations WHERE connection_id=? ORDER BY id DESC LIMIT 100`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []datasourceapp.WriteOperation{}
	for rows.Next() {
		op, err := scanDatasourceWrite(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, op)
	}
	return out, rows.Err()
}
func (t *txAdapter) datasourceWriteAudit(ctx context.Context, id, connection, digest, state string) error {
	meta, _ := json.Marshal(map[string]string{"connectionId": connection, "digest": digest, "state": state})
	return t.PutAudit(ctx, providerapp.Audit{ID: ulid.Make().String(), Action: "datasource.write." + state, AggregateID: id, Actor: "local-user", Metadata: meta, CreatedAt: time.Now().UTC()})
}
func (s *Store) TransitionDatasourceWrite(ctx context.Context, id, from, to string, result *datasourceapp.QueryResult) (bool, error) {
	if !(from == "prepared" && to == "executing" || from == "executing" && (to == "completed" || to == "unknown")) {
		return false, datasourceapp.ErrWriteConflict
	}
	var changed bool
	err := s.do(ctx, func(t *txAdapter) error {
		var raw any
		if result != nil {
			b, err := json.Marshal(result)
			if err != nil {
				return err
			}
			raw = string(b)
		}
		res, err := t.q.ExecContext(ctx, `UPDATE datasource_write_operations SET state=?,result_json=? WHERE id=? AND state=?`, to, raw, id, from)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n == 0 {
			return nil
		}
		op, err := scanDatasourceWrite(t.q.QueryRowContext(ctx, `SELECT `+datasourceWriteColumns+` FROM datasource_write_operations WHERE id=?`, id))
		if err != nil {
			return err
		}
		if err = t.datasourceWriteAudit(ctx, id, op.ConnectionID, op.Digest, to); err != nil {
			return err
		}
		changed = true
		return nil
	})
	return changed, err
}
