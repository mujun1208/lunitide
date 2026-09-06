package sqlite

import (
	"encoding/json"
	"time"

	"github.com/lunitide/lunitide/internal/domain/skill"
)

// The runtime row and supply-chain approval share the same database transaction.
// Candidate ID is the stable imported-skill identity, so revocation needs no
// lookup by mutable name or a new cross-table mapping.
func (t *agentRuntimeTx) PutImportedSkill(sk skill.Skill) error {
	if err := sk.Validate(); err != nil {
		return err
	}
	permissions, err := json.Marshal(sk.Permissions)
	if err != nil {
		return err
	}
	_, err = t.tx.ExecContext(t.ctx, `INSERT INTO skills(id,name,display_name,description,version,status,permissions_json,entry_point,manifest_json,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, sk.ID, sk.Name, sk.DisplayName, sk.Description, sk.Version, sk.Status, string(permissions), sk.EntryPoint, sk.ManifestJSON, formatTime(sk.CreatedAt), formatTime(sk.UpdatedAt))
	return t.fail(err)
}

func (t *agentRuntimeTx) DisableImportedSkill(candidateID string, at time.Time) error {
	_, err := t.tx.ExecContext(t.ctx, `UPDATE skills SET status='disabled',rev=rev+1,updated_at=? WHERE id=? AND status!='disabled'`, formatTime(at), candidateID)
	return t.fail(err)
}

func (t *agentRuntimeTx) ImportedSkillExists(candidateID string) (bool, error) {
	var exists bool
	err := t.tx.QueryRowContext(t.ctx, `SELECT EXISTS(SELECT 1 FROM skills WHERE id=?)`, candidateID).Scan(&exists)
	return exists, t.fail(err)
}
