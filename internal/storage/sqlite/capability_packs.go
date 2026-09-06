package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/lunitide/lunitide/internal/audit"
	"github.com/oklog/ulid/v2"

	"github.com/lunitide/lunitide/internal/capabilitypack"
	"github.com/lunitide/lunitide/internal/domain/agentrun"
)

func (r *AgentRuntimeRepository) TransactPack(ctx context.Context, fn func(capabilitypack.Tx) error) error {
	return r.Transact(ctx, func(tx agentrun.Tx) error {
		pack, ok := tx.(capabilitypack.Tx)
		if !ok {
			return capabilitypack.ErrUnavailable
		}
		return fn(pack)
	})
}

const packColumns = `pack_id,manifest_json,manifest_digest,state,desired,version,last_error,created_at,updated_at`

func scanPack(row interface{ Scan(...any) error }) (capabilitypack.Record, error) {
	var out capabilitypack.Record
	var id, manifest string
	err := row.Scan(&id, &manifest, &out.Digest, &out.State, &out.Desired, &out.Version, &out.Error, &out.CreatedAt, &out.UpdatedAt)
	if err != nil {
		return out, err
	}
	if err = json.Unmarshal([]byte(manifest), &out.Spec); err != nil {
		return out, err
	}
	if id != out.Spec.ID {
		return out, capabilitypack.ErrConflict
	}
	return out, nil
}
func (t *agentRuntimeTx) GetPack(id string) (capabilitypack.Record, error) {
	out, err := scanPack(t.tx.QueryRowContext(t.ctx, `SELECT `+packColumns+` FROM capability_pack_operations WHERE pack_id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return out, capabilitypack.ErrNotFound
	}
	return out, t.fail(err)
}
func (t *agentRuntimeTx) ListPacks() ([]capabilitypack.Record, error) {
	rows, err := t.tx.QueryContext(t.ctx, `SELECT `+packColumns+` FROM capability_pack_operations ORDER BY pack_id`)
	if err != nil {
		return nil, t.fail(err)
	}
	defer rows.Close()
	out := []capabilitypack.Record{}
	for rows.Next() {
		record, err := scanPack(rows)
		if err != nil {
			return nil, t.fail(err)
		}
		out = append(out, record)
	}
	return out, t.fail(rows.Err())
}
func (t *agentRuntimeTx) SavePack(record capabilitypack.Record, expected int64) error {
	manifest, err := json.Marshal(record.Spec)
	if err != nil {
		return err
	}
	var result sql.Result
	if expected == 0 {
		result, err = t.tx.ExecContext(t.ctx, `INSERT INTO capability_pack_operations(`+packColumns+`) VALUES(?,?,?,?,?,?,?,?,?) ON CONFLICT(pack_id) DO NOTHING`, record.Spec.ID, string(manifest), record.Digest, record.State, record.Desired, record.Version, record.Error, record.CreatedAt, record.UpdatedAt)
	} else {
		result, err = t.tx.ExecContext(t.ctx, `UPDATE capability_pack_operations SET state=?,desired=?,version=?,last_error=?,updated_at=? WHERE pack_id=? AND version=? AND manifest_digest=?`, record.State, record.Desired, record.Version, record.Error, record.UpdatedAt, record.Spec.ID, expected, record.Digest)
	}
	if err != nil {
		return t.fail(err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return t.fail(err)
	}
	if n != 1 {
		return capabilitypack.ErrConflict
	}
	action := "plugin.pack.install"
	if record.Desired == "uninstalled" {
		action = "plugin.pack.uninstall"
	}
	_, err = t.AppendAuditEvent(audit.Event{ID: ulid.Make().String(), Action: action, ResourceType: "capability_pack", ResourceID: record.Spec.ID, Actor: "local-user", AfterDigest: record.Digest, CreatedAt: record.UpdatedAt})
	return err
}
func (t *agentRuntimeTx) PlanResource(resource capabilitypack.Resource) (capabilitypack.Resource, error) {
	var existing capabilitypack.Resource
	var managed int
	err := t.tx.QueryRowContext(t.ctx, `SELECT kind,resource_key,target_id,managed FROM capability_pack_resources WHERE kind=? AND resource_key=?`, resource.Kind, resource.Key).Scan(&existing.Kind, &existing.Key, &existing.TargetID, &managed)
	if err == nil {
		existing.Managed = managed == 1
		return existing, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return resource, t.fail(err)
	}
	// Baseline is read on the same writer transaction as the ownership row;
	// a manual enable cannot slip between inspection and ownership creation.
	resource.Managed = false
	switch resource.Kind {
	case "gate":
		var state string
		err = t.tx.QueryRowContext(t.ctx, `SELECT state FROM plugin_installs WHERE install_id=?`, resource.TargetID).Scan(&state)
		if err != nil {
			return resource, t.fail(err)
		}
		resource.Managed = state != "enabled"
	case "mcp":
		var enabled int
		err = t.tx.QueryRowContext(t.ctx, `SELECT enabled FROM mcp_endpoint_settings WHERE endpoint_id=?`, resource.TargetID).Scan(&enabled)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return resource, t.fail(err)
		}
		resource.Managed = errors.Is(err, sql.ErrNoRows) || enabled == 0
	case "skill": // Imported/installed skills are intentionally retained.
	default:
		return resource, capabilitypack.ErrConflict
	}
	managed = 0
	if resource.Managed {
		managed = 1
	}
	_, err = t.tx.ExecContext(t.ctx, `INSERT INTO capability_pack_resources(kind,resource_key,target_id,managed) VALUES(?,?,?,?)`, resource.Kind, resource.Key, resource.TargetID, managed)
	return resource, t.fail(err)
}
func (t *agentRuntimeTx) PutReference(id string, c capabilitypack.Component) error {
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO capability_pack_references(pack_id,kind,resource_key,ordinal,state) VALUES(?,?,?,?,?) ON CONFLICT(pack_id,kind,resource_key) DO UPDATE SET state='planned',ordinal=excluded.ordinal`, id, c.Kind, c.Key, c.Ordinal, c.State)
	return t.fail(err)
}
func (t *agentRuntimeTx) References(id string) ([]capabilitypack.Component, error) {
	rows, err := t.tx.QueryContext(t.ctx, `SELECT r.kind,r.resource_key,r.target_id,r.managed,f.ordinal,f.state FROM capability_pack_references f JOIN capability_pack_resources r ON r.kind=f.kind AND r.resource_key=f.resource_key WHERE f.pack_id=? ORDER BY f.ordinal`, id)
	if err != nil {
		return nil, t.fail(err)
	}
	defer rows.Close()
	out := []capabilitypack.Component{}
	for rows.Next() {
		var c capabilitypack.Component
		var managed int
		if err = rows.Scan(&c.Kind, &c.Key, &c.TargetID, &managed, &c.Ordinal, &c.State); err != nil {
			return nil, t.fail(err)
		}
		c.Managed = managed == 1
		out = append(out, c)
	}
	return out, t.fail(rows.Err())
}
func (t *agentRuntimeTx) SetReferenceState(id, kind, key, state string) error {
	_, err := t.tx.ExecContext(t.ctx, `UPDATE capability_pack_references SET state=? WHERE pack_id=? AND kind=? AND resource_key=?`, state, id, kind, key)
	return t.fail(err)
}
func (t *agentRuntimeTx) SetResourceTarget(kind, key, target string) error {
	if target == "" {
		return capabilitypack.ErrConflict
	}
	_, err := t.tx.ExecContext(t.ctx, `UPDATE capability_pack_resources SET managed=CASE WHEN target_id=? THEN managed ELSE 0 END,target_id=? WHERE kind=? AND resource_key=?`, target, target, kind, key)
	return t.fail(err)
}
func (t *agentRuntimeTx) MayRelease(kind, key, packID string) (bool, error) {
	var managed, others int
	err := t.tx.QueryRowContext(t.ctx, `SELECT managed,(SELECT count(*) FROM capability_pack_references f WHERE f.kind=r.kind AND f.resource_key=r.resource_key AND f.pack_id<>? AND f.state<>'released') FROM capability_pack_resources r WHERE kind=? AND resource_key=?`, packID, kind, key).Scan(&managed, &others)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return managed == 1 && others == 0, t.fail(err)
}
func (t *agentRuntimeTx) ClaimPackResourceIndependent(kind, target string) error {
	_, err := t.tx.ExecContext(t.ctx, `UPDATE capability_pack_resources SET managed=0 WHERE kind=? AND target_id=?`, kind, target)
	return t.fail(err)
}
