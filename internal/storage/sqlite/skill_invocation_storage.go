package sqlite

import (
	"context"
	"database/sql"
	"time"

	"github.com/lunitide/lunitide/internal/skillapp"
)

// InsertSkillInvocation persists a frozen invocation proposal. The authoritative
// state lives here; skillapp keeps only a best-effort LRU cache in front of it.
// Plain exec (no audit row): an invocation is a short-lived, TTL-bounded
// proposal, not a lifecycle mutation, so it stays off the audit action allowlist.
func (s *Store) InsertSkillInvocation(ctx context.Context, inv skillapp.Invocation) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO skill_invocations(id, skill_id, skill_version, session_id, input,
		 input_digest, manifest_digest, risk, mode, requires_approval, consumed,
		 expires_at, created_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		inv.ID, inv.SkillID, inv.SkillVersion, inv.SessionID, inv.Input,
		inv.InputDigest, inv.ManifestDigest, inv.Risk, inv.Mode,
		boolToInt(inv.RequiresApproval), boolToInt(inv.Consumed),
		formatTime(inv.ExpiresAt), formatTime(time.Now().UTC()))
	return mapWriteError(err)
}

// GetSkillInvocation returns one invocation by id, or (nil, nil) when absent.
func (s *Store) GetSkillInvocation(ctx context.Context, id string) (*skillapp.Invocation, error) {
	var inv skillapp.Invocation
	var requiresApproval, consumed int
	var expires string
	err := s.db.QueryRowContext(ctx,
		`SELECT id, skill_id, skill_version, session_id, input, input_digest,
		 manifest_digest, risk, mode, requires_approval, consumed, expires_at
		 FROM skill_invocations WHERE id=?`, id).Scan(
		&inv.ID, &inv.SkillID, &inv.SkillVersion, &inv.SessionID, &inv.Input,
		&inv.InputDigest, &inv.ManifestDigest, &inv.Risk, &inv.Mode,
		&requiresApproval, &consumed, &expires)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	inv.RequiresApproval = requiresApproval != 0
	inv.Consumed = consumed != 0
	inv.ExpiresAt, err = time.Parse(time.RFC3339Nano, expires)
	if err != nil {
		return nil, err
	}
	return &inv, nil
}

// MarkSkillInvocationConsumed atomically flips consumed 0->1. It reports whether
// this caller won the race (RowsAffected==1). A false with no error means the
// row was already consumed (or gone); the caller distinguishes via GetSkillInvocation.
func (s *Store) MarkSkillInvocationConsumed(ctx context.Context, id string) (bool, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE skill_invocations SET consumed=1 WHERE id=? AND consumed=0`, id)
	if err != nil {
		return false, mapWriteError(err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n == 1, nil
}

// DeleteExpiredSkillInvocations removes invocations whose TTL elapsed at or
// before now, returning the number of rows purged.
func (s *Store) DeleteExpiredSkillInvocations(ctx context.Context, now time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM skill_invocations WHERE expires_at <= ?`, formatTime(now.UTC()))
	if err != nil {
		return 0, mapWriteError(err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	return n, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}