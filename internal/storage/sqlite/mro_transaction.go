package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/lunitide/lunitide/internal/mroapp"
	"github.com/oklog/ulid/v2"
)

type mroTxKey struct{}
type mroTxContext struct {
	store *Store
	tx    *sql.Tx
}
type mroSQL interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func (s *Store) mroDB(ctx context.Context) mroSQL {
	if v, ok := ctx.Value(mroTxKey{}).(mroTxContext); ok && v.store == s {
		return v.tx
	}
	return s.db
}
func mroOrg(ctx context.Context) any {
	if org := mroapp.Scope(ctx); org != "" {
		return org
	}
	return nil
}
func (s *Store) TransactMRO(ctx context.Context, fn func(context.Context) error) (err error) {
	if v, ok := ctx.Value(mroTxKey{}).(mroTxContext); ok && v.store == s {
		return fn(ctx)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	// Always rollback uncommitted work, including panic and failed commit.
	defer func() { _ = tx.Rollback() }()
	if err = fn(context.WithValue(ctx, mroTxKey{}, mroTxContext{s, tx})); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) mroWrite(ctx context.Context, query string, args ...any) (sql.Result, error) {
	result, err := s.mroDB(ctx).ExecContext(ctx, query, args...)
	if err != nil {
		if strings.Contains(err.Error(), "MRO_SCOPE_MISMATCH") {
			return result, mroapp.ErrScope
		}
		return result, err
	}
	if strings.Contains(query, "DO UPDATE") {
		n, err := result.RowsAffected()
		if err != nil {
			return result, err
		}
		if n == 0 {
			return result, mroapp.ErrScope
		}
	}
	return result, nil
}

func (s *Store) ExecuteMRORequest(ctx context.Context, method, key, digest string, fn func(context.Context) (json.RawMessage, error), replay func(context.Context) error) (out json.RawMessage, err error) {
	if key == "" || len(key) > 128 || len(digest) != 64 {
		return nil, mroapp.ErrPayloadInvalid
	}
	err = s.TransactMRO(ctx, func(txCtx context.Context) error {
		var savedDigest, savedResponse string
		err := s.mroDB(txCtx).QueryRowContext(txCtx, `SELECT payload_digest,response_json FROM mro_request_receipts WHERE org_key=? AND method=? AND request_key=?`, mroapp.Scope(ctx), method, key).Scan(&savedDigest, &savedResponse)
		if err == nil {
			if savedDigest != digest {
				return mroapp.ErrConflict
			}
			if err = replay(txCtx); err != nil {
				return err
			}
			out = json.RawMessage(savedResponse)
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		out, err = fn(txCtx)
		if err != nil {
			return err
		}
		if !json.Valid(out) {
			return mroapp.ErrPayloadInvalid
		}
		now := time.Now().UTC().Format(time.RFC3339Nano)
		if _, err = s.mroDB(txCtx).ExecContext(txCtx, `INSERT INTO mro_request_receipts(org_key,method,request_key,payload_digest,response_json,created_at) VALUES(?,?,?,?,?,?)`, mroapp.Scope(ctx), method, key, digest, string(out), now); err != nil {
			return err
		}
		_, err = s.mroDB(txCtx).ExecContext(txCtx, `INSERT INTO mro_operation_audit(id,org_id,action,resource_type,resource_id,created_at) VALUES(?,?,?,?,?,?)`, ulid.Make().String(), mroOrg(ctx), method, "mro_request", key, now)
		return err
	})
	return out, err
}
func (s *Store) ListMROAudit(ctx context.Context, limit int) ([]mroapp.AuditRow, error) {
	if limit < 1 || limit > 100 {
		limit = 100
	}
	rows, err := s.mroDB(ctx).QueryContext(ctx, `SELECT id,action,resource_type,resource_id,created_at FROM mro_operation_audit WHERE org_id IS ? ORDER BY created_at DESC,id DESC LIMIT ?`, mroOrg(ctx), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []mroapp.AuditRow{}
	for rows.Next() {
		var row mroapp.AuditRow
		if err = rows.Scan(&row.ID, &row.Action, &row.ResourceType, &row.ResourceID, &row.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, row)
	}
	return items, rows.Err()
}
