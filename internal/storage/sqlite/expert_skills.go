package sqlite

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
)

const expertSkillBindCap = 32

func validExpertSkillULID(v string) bool {
	u, err := ulid.ParseStrict(v)
	return err == nil && u.String() == v && v[0] <= '7'
}

func validExpertSkillKey(v string) bool {
	if len(v) < 1 || len(v) > 64 {
		return false
	}
	for _, r := range v {
		if r == ' ' || r == '\n' || r == '\t' || r == '\r' {
			return false
		}
	}
	return true
}

func (s *Store) ListExpertSkillKeys(ctx context.Context, expertID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT skill_key FROM expert_skill_bindings WHERE expert_id=? ORDER BY ordinal, skill_key`, expertID)
	if err != nil {
		return nil, fmt.Errorf("list expert skill bindings: %w", err)
	}
	defer rows.Close()
	var keys []string
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, fmt.Errorf("scan expert skill binding: %w", err)
		}
		keys = append(keys, key)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate expert skill bindings: %w", err)
	}
	if keys == nil {
		keys = []string{}
	}
	return keys, nil
}

func (s *Store) ReplaceExpertSkillKeys(ctx context.Context, expertID string, keys []string) error {
	if !validExpertSkillULID(expertID) {
		return fmt.Errorf("expert skill id invalid")
	}
	if len(keys) > expertSkillBindCap {
		return fmt.Errorf("expert skill capacity reached")
	}
	seen := map[string]bool{}
	for _, key := range keys {
		if !validExpertSkillKey(key) || seen[key] {
			return fmt.Errorf("expert skill key invalid")
		}
		seen[key] = true
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin expert skill bindings: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM expert_skill_bindings WHERE expert_id=?`, expertID); err != nil {
		return fmt.Errorf("clear expert skill bindings: %w", err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for i, key := range keys {
		if _, err := tx.ExecContext(ctx, `INSERT INTO expert_skill_bindings(expert_id, skill_key, ordinal, created_at) VALUES(?,?,?,?)`, expertID, key, i, now); err != nil {
			return fmt.Errorf("insert expert skill binding: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit expert skill bindings: %w", err)
	}
	return nil
}

func (s *Store) SeedExpertSkillsIfEmpty(ctx context.Context, expertID string, keys []string) error {
	if !validExpertSkillULID(expertID) {
		return fmt.Errorf("expert skill id invalid")
	}
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM expert_skill_bindings WHERE expert_id=?`, expertID).Scan(&n); err != nil {
		return fmt.Errorf("count expert skill bindings: %w", err)
	}
	if n > 0 {
		return nil
	}
	return s.ReplaceExpertSkillKeys(ctx, expertID, keys)
}

// MergeExpertSkillKeys appends missing keys onto an existing binding list so a
// newly shipped factory skill lands on already-installed specialists. Empty
// bindings fall through to a full replace (same as SeedExpertSkillsIfEmpty).
func (s *Store) MergeExpertSkillKeys(ctx context.Context, expertID string, keys []string) error {
	existing, err := s.ListExpertSkillKeys(ctx, expertID)
	if err != nil {
		return err
	}
	if len(existing) == 0 {
		return s.ReplaceExpertSkillKeys(ctx, expertID, keys)
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(existing)+len(keys))
	for _, key := range existing {
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, key)
	}
	added := false
	for _, key := range keys {
		key = strings.TrimSpace(key)
		if key == "" || seen[key] {
			continue
		}
		if !validExpertSkillKey(key) {
			return fmt.Errorf("expert skill key invalid")
		}
		seen[key] = true
		out = append(out, key)
		added = true
	}
	if !added {
		return nil
	}
	return s.ReplaceExpertSkillKeys(ctx, expertID, out)
}
