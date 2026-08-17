// M10 skill-category storage (migration 0073): sk_category_map rows on the
// Store connection alongside the M6 skills tables. Only manual assignments
// upsert; computed manifest/keyword mappings seed with INSERT OR IGNORE so a
// later manual choice always wins (priority manual > manifest > keyword).
package sqlite

import (
	"context"
	"database/sql"
	"time"

	"github.com/lunitide/lunitide/internal/domain/skill"
)

// UpsertSkillCategoryManual stores one manual category assignment. The row is
// created or overwritten with match_source='manual'; the mapping id is kept
// stable across overwrites.
func (s *Store) UpsertSkillCategoryManual(ctx context.Context, skillID string, category skill.Category) (skill.CategoryMap, error) {
	now := time.Now().UTC()
	id, err := s.newULID(now)
	if err != nil {
		return skill.CategoryMap{}, err
	}
	err = s.execWithAudit(ctx, "skill.category_set", skillID, "engine",
		map[string]any{"category": string(category), "matchSource": "manual"},
		func(tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx,
				`INSERT INTO sk_category_map(id, skill_id, category, match_source, updated_at)
				 VALUES(?,?,?,?,?)
				 ON CONFLICT(skill_id) DO UPDATE SET category=excluded.category,
				 match_source='manual', updated_at=excluded.updated_at`,
				id, skillID, string(category), string(skill.CategorySourceManual), formatTime(now))
			return err
		})
	if err != nil {
		return skill.CategoryMap{}, mapWriteError(err)
	}
	return s.GetSkillCategoryRow(ctx, skillID)
}

// SeedSkillCategory persists a computed mapping (manifest/keyword) without
// overriding an existing row: INSERT OR IGNORE keeps any prior decision.
func (s *Store) SeedSkillCategory(ctx context.Context, skillID string, category skill.Category, source skill.CategorySource) error {
	now := time.Now().UTC()
	id, err := s.newULID(now)
	if err != nil {
		return err
	}
	err = s.execWithAudit(ctx, "skill.category_seeded", skillID, "engine",
		map[string]any{"category": string(category), "matchSource": string(source)},
		func(tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx,
				`INSERT OR IGNORE INTO sk_category_map(id, skill_id, category, match_source, updated_at)
				 VALUES(?,?,?,?,?)`,
				id, skillID, string(category), string(source), formatTime(now))
			return err
		})
	return mapWriteError(err)
}

// GetSkillCategoryRow returns the stored mapping for one skill or nil.
func (s *Store) GetSkillCategoryRow(ctx context.Context, skillID string) (skill.CategoryMap, error) {
	var m skill.CategoryMap
	var source string
	err := s.db.QueryRowContext(ctx,
		`SELECT id, skill_id, category, match_source, updated_at
		 FROM sk_category_map WHERE skill_id=?`, skillID).
		Scan(&m.ID, &m.SkillID, &m.Category, &source, &m.UpdatedAt)
	if err == sql.ErrNoRows {
		return skill.CategoryMap{}, nil
	}
	if err != nil {
		return skill.CategoryMap{}, err
	}
	m.Source = skill.CategorySource(source)
	return m, nil
}

// ListSkillCategories returns every stored mapping, newest update first.
func (s *Store) ListSkillCategories(ctx context.Context) ([]skill.CategoryMap, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, skill_id, category, match_source, updated_at
		 FROM sk_category_map ORDER BY updated_at DESC, id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []skill.CategoryMap
	for rows.Next() {
		var m skill.CategoryMap
		var source string
		if err := rows.Scan(&m.ID, &m.SkillID, &m.Category, &source, &m.UpdatedAt); err != nil {
			return nil, err
		}
		m.Source = skill.CategorySource(source)
		result = append(result, m)
	}
	return result, rows.Err()
}

// DeleteSkillCategoryRow removes the mapping for one skill. Called on the
// same transaction as the skill DELETE so no orphan mappings survive.
func (s *Store) DeleteSkillCategoryRow(ctx context.Context, tx *sql.Tx, skillID string) error {
	_, err := tx.ExecContext(ctx, `DELETE FROM sk_category_map WHERE skill_id=?`, skillID)
	return err
}
