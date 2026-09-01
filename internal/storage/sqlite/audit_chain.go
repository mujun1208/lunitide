package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"

	"github.com/lunitide/lunitide/internal/audit"
)

// auditExecutor is the read+write surface an audit append needs: it reads the
// current chain tail and writes the sealed row on the same handle, so the seq
// assignment and prev-hash linkage stay consistent. Both *sql.Tx (execWithAudit,
// the agent-runtime tx) and *sql.Conn (the UoW adapter) satisfy it, and SQLite's
// single-writer lock — held by the domain write that always precedes the audit
// write in these paths — serialises the tail read against concurrent appends.
type auditExecutor interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// auditMetaDigest folds the metadata JSON into the hashed document. The chain
// hash covers a digest of the metadata rather than the raw bytes, but any edit
// to metadata_json changes the digest and therefore breaks verification.
func auditMetaDigest(metaJSON string) string {
	sum := sha256.Sum256([]byte(metaJSON))
	return hex.EncodeToString(sum[:])
}

// appendAuditChained is the single write entry point for audit_events. It reads
// the chain tail, seals the new row onto it via the shared M7 hash-chain kernel
// (seq+1, prev_hash = tail.event_hash, event_hash over the canonical document)
// and inserts it. Rows written before migration 0112 have NULL chain columns
// and are ignored here; the chain begins at the first row this appends.
func appendAuditChained(ctx context.Context, q auditExecutor, id, action, aggregateID, actor, metaJSON, createdAt string) error {
	var seq sql.NullInt64
	var eventHash sql.NullString
	err := q.QueryRowContext(ctx,
		`SELECT seq, event_hash FROM audit_events WHERE seq IS NOT NULL ORDER BY seq DESC LIMIT 1`).
		Scan(&seq, &eventHash)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	var prev *audit.Event
	if seq.Valid {
		prev = &audit.Event{Seq: seq.Int64, EventHash: eventHash.String}
	}
	sealed := audit.Link(prev, audit.Event{
		ID:          id,
		Action:      action,
		ResourceID:  aggregateID,
		Actor:       actor,
		AfterDigest: auditMetaDigest(metaJSON),
		CreatedAt:   createdAt,
	})
	_, err = q.ExecContext(ctx,
		`INSERT INTO audit_events(id,action,aggregate_id,actor,metadata_json,created_at,seq,prev_hash,event_hash)
		 VALUES(?,?,?,?,?,?,?,?,?)`,
		id, action, aggregateID, actor, metaJSON, createdAt, sealed.Seq, sealed.PrevHash, sealed.EventHash)
	return err
}

// VerifyAuditChain re-derives the audit_events hash chain and reports the first
// break (deletion, reorder, insertion or field edit) as audit.ErrChainBroken.
// Only sealed rows (seq NOT NULL, i.e. written from migration 0112 onward) form
// the chain; pre-chain rows are excluded. An empty chain is intact.
func (s *Store) VerifyAuditChain(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx,
		`SELECT seq,id,action,aggregate_id,actor,metadata_json,created_at,prev_hash,event_hash
		   FROM audit_events WHERE seq IS NOT NULL ORDER BY seq ASC`)
	if err != nil {
		return err
	}
	defer rows.Close()
	var events []audit.Event
	for rows.Next() {
		var e audit.Event
		var meta string
		if err := rows.Scan(&e.Seq, &e.ID, &e.Action, &e.ResourceID, &e.Actor,
			&meta, &e.CreatedAt, &e.PrevHash, &e.EventHash); err != nil {
			return err
		}
		e.AfterDigest = auditMetaDigest(meta)
		events = append(events, e)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return audit.VerifyChain(events)
}
