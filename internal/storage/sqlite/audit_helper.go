package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"
)

// There is deliberately no best-effort appendAudit here. One used to exist,
// outside any transaction and discarding its own error, so a business write
// could commit while its audit row silently vanished. Nothing called it; it is
// gone rather than left as the obvious thing for the next audit write to reach
// for. Audit rows land on the caller's transaction — appendAuditTx below, the
// UoW, or execWithAudit — or they do not land at all.

// appendAuditTx writes the audit row on the caller's transaction.
func (s *Store) appendAuditTx(ctx context.Context, tx *sql.Tx, action, aggregateID, actor string, metadata map[string]any) error {
	var metaJSON string
	if metadata != nil {
		b, err := json.Marshal(metadata)
		if err != nil {
			metaJSON = "{}"
		} else {
			metaJSON = string(b)
		}
	} else {
		metaJSON = "{}"
	}
	id, err := s.newULID(time.Now().UTC())
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx,
		`INSERT INTO audit_events(id,action,aggregate_id,actor,metadata_json,created_at) VALUES(?,?,?,?,?,?)`,
		id, action, aggregateID, actor, metaJSON, formatTime(time.Now().UTC()))
	return err
}

// execWithAudit runs one business write and its audit row in a single
// transaction: either both commit or neither does.
func (s *Store) execWithAudit(ctx context.Context, action, aggregateID, actor string, metadata map[string]any, write func(tx *sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := write(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := s.appendAuditTx(ctx, tx, action, aggregateID, actor, metadata); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}
