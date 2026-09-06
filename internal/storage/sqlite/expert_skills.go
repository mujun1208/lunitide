package sqlite

import (
	"context"
	"errors"
	"fmt"
	"github.com/lunitide/lunitide/internal/domain/m8core"
	"github.com/lunitide/lunitide/internal/m8app"
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
	return s.changeExpertSkillKeys(ctx, expertID, keys, "replace")
}
func (s *Store) SeedExpertSkillsIfEmpty(ctx context.Context, expertID string, keys []string) error {
	return s.changeExpertSkillKeys(ctx, expertID, keys, "seed")
}
func (s *Store) MergeExpertSkillKeys(ctx context.Context, expertID string, keys []string) error {
	return s.changeExpertSkillKeys(ctx, expertID, keys, "merge")
}
func (s *Store) changeExpertSkillKeys(ctx context.Context, id string, keys []string, mode string) error {
	return s.AgentRuntimeRepository().TransactExpert(ctx, func(tx m8app.ExpertTx) error {
		current, err := tx.ListExpertSkillKeys(id)
		if err != nil {
			return err
		}
		if mode == "seed" && len(current) > 0 {
			return nil
		}
		if mode == "merge" {
			out := append([]string{}, current...)
			seen := map[string]bool{}
			for _, key := range current {
				seen[key] = true
			}
			for _, key := range keys {
				key = strings.TrimSpace(key)
				if key != "" && !seen[key] {
					seen[key] = true
					out = append(out, key)
				}
			}
			keys = out
		}
		_, err = tx.GetExpert(id)
		// Legacy bindings may be seeded before their catalog record exists. A
		// later expert Create snapshots its own committed bindings.
		if errors.Is(err, m8core.ErrNotFound) {
			return tx.ReplaceExpertSkillKeys(id, keys)
		}
		if err != nil {
			return err
		}
		_, err = m8app.UpdateExpertEquipmentTx(tx, id, "", keys, time.Now().UTC().Format(time.RFC3339Nano))
		return err
	})
}
