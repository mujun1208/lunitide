package sqlite

import (
	"context"
	"database/sql"

	"github.com/lunitide/lunitide/internal/skillapp"
)

// DeleteSkillVersion keeps revision, lifecycle, category cleanup and audit
// inside one transaction. A stale delete leaves every related row intact.
func (s *Store) DeleteSkillVersion(ctx context.Context, id string, expectedRev int64) error {
	return s.execWithAudit(ctx, "skill.deleted", id, "engine", map[string]any{"rev": expectedRev}, func(tx *sql.Tx) error {
		var rev int64
		var status string
		if err := tx.QueryRowContext(ctx, `SELECT rev,status FROM skills WHERE id=?`, id).Scan(&rev, &status); err == sql.ErrNoRows {
			return skillapp.ErrSkillNotFound
		} else if err != nil {
			return err
		}
		if rev != expectedRev {
			return skillapp.ErrSkillVersionConflict
		}
		if status != "draft" && status != "disabled" {
			return skillapp.ErrInvalidTransition
		}
		if err := s.DeleteSkillCategoryRow(ctx, tx, id); err != nil {
			return err
		}
		result, err := tx.ExecContext(ctx, `DELETE FROM skills WHERE id=? AND rev=? AND status IN ('draft','disabled')`, id, expectedRev)
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if n != 1 {
			return skillapp.ErrSkillVersionConflict
		}
		return nil
	})
}
