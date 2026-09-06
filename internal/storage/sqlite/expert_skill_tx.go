package sqlite

import (
	"fmt"
	"time"
)

// ReplaceExpertSkillKeys participates in the catalog/version transaction,
// ensuring an invalid or failed initial equipment write leaves no expert row.
func (t *agentRuntimeTx) ReplaceExpertSkillKeys(expertID string, keys []string) error {
	if !validExpertSkillULID(expertID) {
		return fmt.Errorf("expert skill id invalid")
	}
	if len(keys) > expertSkillBindCap {
		return fmt.Errorf("expert skill capacity reached")
	}
	seen := make(map[string]bool, len(keys))
	for _, key := range keys {
		if !validExpertSkillKey(key) || seen[key] {
			return fmt.Errorf("expert skill key invalid")
		}
		seen[key] = true
	}
	if _, err := t.tx.ExecContext(t.ctx, `DELETE FROM expert_skill_bindings WHERE expert_id=?`, expertID); err != nil {
		return t.fail(err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for ordinal, key := range keys {
		if _, err := t.tx.ExecContext(t.ctx, `INSERT INTO expert_skill_bindings(expert_id,skill_key,ordinal,created_at) VALUES(?,?,?,?)`, expertID, key, ordinal, now); err != nil {
			return t.fail(err)
		}
	}
	return nil
}

func (t *agentRuntimeTx) ListExpertSkillKeys(expertID string) ([]string, error) {
	rows, err := t.tx.QueryContext(t.ctx, `SELECT skill_key FROM expert_skill_bindings WHERE expert_id=? ORDER BY ordinal,skill_key`, expertID)
	if err != nil {
		return nil, t.fail(err)
	}
	defer rows.Close()
	keys := []string{}
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, t.fail(err)
		}
		keys = append(keys, key)
	}
	return keys, t.fail(rows.Err())
}
