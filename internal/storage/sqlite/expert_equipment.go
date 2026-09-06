package sqlite

import (
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/lunitide/lunitide/internal/m8app"
)

func (t *agentRuntimeTx) PutExpertEquipmentSnapshot(version string, keys []string) error {
	if keys == nil {
		keys = []string{}
	}
	raw, err := json.Marshal(keys)
	if err != nil {
		return err
	}
	var old string
	err = t.tx.QueryRowContext(t.ctx, `SELECT keys_json FROM expert_version_equipment WHERE version_id=?`, version).Scan(&old)
	if err == nil {
		if old != string(raw) {
			return m8app.ErrExpertVersionConflict
		}
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return t.fail(err)
	}
	_, err = t.tx.ExecContext(t.ctx, `INSERT INTO expert_version_equipment(version_id,keys_json,known) VALUES(?,?,1)`, version, string(raw))
	return t.fail(err)
}
func (t *agentRuntimeTx) GetExpertEquipmentSnapshot(version string) ([]string, bool, error) {
	var raw string
	var known bool
	err := t.tx.QueryRowContext(t.ctx, `SELECT keys_json,known FROM expert_version_equipment WHERE version_id=?`, version).Scan(&raw, &known)
	if err != nil {
		return nil, false, t.fail(err)
	}
	keys := []string{}
	if err = json.Unmarshal([]byte(raw), &keys); err != nil {
		return nil, false, err
	}
	return keys, known, nil
}
func (t *agentRuntimeTx) PersonaReferenced(ref string) (bool, error) {
	var count int
	err := t.tx.QueryRowContext(t.ctx, `SELECT EXISTS(SELECT 1 FROM expert_versions WHERE persona_ref=?)`, ref).Scan(&count)
	return count != 0, t.fail(err)
}
