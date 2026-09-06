package sqlite

import (
	"context"
	"fmt"
	"time"
)

// Stored times are UTC RFC3339Nano, whose optional fraction is not directly
// lexicographically sortable (whole-second Z sorts after .1Z). Pad to nine
// digits instead of julianday, which loses sub-millisecond TTL precision.
func memoryExpiryOrderSQL(column string) string {
	return "substr(replace(" + column + ",'Z','') || CASE WHEN instr(" + column + ",'.')=0 THEN '.' ELSE '' END || '000000000',1,29)"
}
func expiryCutoff(now time.Time) string { return now.UTC().Format("2006-01-02T15:04:05.000000000") }
func (s *Store) PurgeExpiredMemories(ctx context.Context, projectID string, now time.Time, limit int) (int, error) {
	if s == nil || s.db == nil {
		return 0, fmt.Errorf("memory storage unavailable")
	}
	if limit < 1 || limit > 256 {
		return 0, fmt.Errorf("memory purge batch limit invalid")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `DELETE FROM memories WHERE id IN (SELECT id FROM memories WHERE project_id=? AND expires_at IS NOT NULL AND `+memoryExpiryOrderSQL("expires_at")+`<=? ORDER BY id LIMIT ?)`, projectID, expiryCutoff(now), limit)
	if err != nil {
		return 0, mapWriteError(err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	if n == 0 {
		return 0, nil
	}
	if err = s.appendAuditTx(ctx, tx, "memory.deleted", projectID, "engine", map[string]any{"reason": "expired", "count": n, "cutoff": formatTime(now)}); err != nil {
		return 0, err
	}
	if err = tx.Commit(); err != nil {
		return 0, err
	}
	return int(n), nil
}
