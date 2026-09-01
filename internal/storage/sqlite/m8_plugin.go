// M8 FR-18 storage (T-8.9.x): plugin_bundles / plugin_installs /
// plugin_capability_bindings on the agent-runtime single-writer
// transaction. The install uninstall chain (binding revocation + recursive
// tombstone + audit) commits on this one transaction - any failure rolls
// the whole chain back (M8-041).
package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"github.com/lunitide/lunitide/internal/domain/agentrun"
	"github.com/lunitide/lunitide/internal/domain/m8core"
	"github.com/lunitide/lunitide/internal/m8app"
)

// TransactPlugin runs an m8app FR-18 use case on the shared single-writer
// transaction.
func (r *AgentRuntimeRepository) TransactPlugin(ctx context.Context, fn func(m8app.PluginTx) error) error {
	return r.Transact(ctx, func(tx agentrun.Tx) error {
		ptx, ok := tx.(m8app.PluginTx)
		if !ok {
			return errors.New("agent runtime tx does not satisfy m8app.PluginTx")
		}
		return fn(ptx)
	})
}

const m8pbColumns = `bundle_id,plugin_id,semver,publisher,kind,manifest_ref,entrypoint,capabilities_json,permissions_json,requires_json,package_hash,signature_status,state,created_at`

func scanPluginBundle(s interface{ Scan(...any) error }) (m8core.PluginBundle, error) {
	var b m8core.PluginBundle
	err := s.Scan(&b.BundleID, &b.PluginID, &b.Semver, &b.Publisher, &b.Kind,
		&b.ManifestRef, &b.Entrypoint, &b.CapabilitiesJSON, &b.PermissionsJSON,
		&b.RequiresJSON, &b.PackageHash, &b.SignatureStatus, &b.State, &b.CreatedAt)
	return b, err
}

// GetBundle answers one plugin bundle by id.
func (t *agentRuntimeTx) GetPluginBundle(bundleID string) (m8core.PluginBundle, error) {
	b, err := scanPluginBundle(t.tx.QueryRowContext(t.ctx,
		`SELECT `+m8pbColumns+` FROM plugin_bundles WHERE bundle_id=?`, bundleID))
	if errors.Is(err, sql.ErrNoRows) {
		return b, m8core.ErrNotFound
	}
	return b, t.fail(err)
}

// GetBundleByPluginSemver answers the append-only (plugin_id, semver) row.
func (t *agentRuntimeTx) GetPluginBundleBySemver(pluginID, semver string) (m8core.PluginBundle, error) {
	b, err := scanPluginBundle(t.tx.QueryRowContext(t.ctx,
		`SELECT `+m8pbColumns+` FROM plugin_bundles WHERE plugin_id=? AND semver=?`, pluginID, semver))
	if errors.Is(err, sql.ErrNoRows) {
		return b, m8core.ErrNotFound
	}
	return b, t.fail(err)
}

// PutBundle inserts one append-only bundle version row.
func (t *agentRuntimeTx) PutPluginBundle(b m8core.PluginBundle) error {
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO plugin_bundles
		(bundle_id,plugin_id,semver,publisher,kind,manifest_ref,entrypoint,capabilities_json,permissions_json,requires_json,package_hash,signature_status,state,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(plugin_id, semver) DO NOTHING`,
		b.BundleID, b.PluginID, b.Semver, b.Publisher, b.Kind, b.ManifestRef,
		b.Entrypoint, b.CapabilitiesJSON, b.PermissionsJSON, b.RequiresJSON,
		b.PackageHash, b.SignatureStatus, b.State, b.CreatedAt)
	return t.fail(err)
}

const m8piColumns = `install_id,bundle_id,plugin_id,subject_id,origin,state,permission_grant_digest,installed_at,updated_at`

func scanPluginInstall(s interface{ Scan(...any) error }) (m8core.PluginInstall, error) {
	var i m8core.PluginInstall
	err := s.Scan(&i.InstallID, &i.BundleID, &i.PluginID, &i.SubjectID, &i.Origin,
		&i.State, &i.PermissionGrantDigest, &i.InstalledAt, &i.UpdatedAt)
	return i, err
}

// GetInstall answers one install row by id.
func (t *agentRuntimeTx) GetInstall(installID string) (m8core.PluginInstall, error) {
	i, err := scanPluginInstall(t.tx.QueryRowContext(t.ctx,
		`SELECT `+m8piColumns+` FROM plugin_installs WHERE install_id=?`, installID))
	if errors.Is(err, sql.ErrNoRows) {
		return i, m8core.ErrNotFound
	}
	return i, t.fail(err)
}

// GetInstallBySubjectPlugin answers the UNIQUE(subject_id, plugin_id)
// install row.
func (t *agentRuntimeTx) GetInstallBySubjectPlugin(subjectID, pluginID string) (m8core.PluginInstall, bool, error) {
	i, err := scanPluginInstall(t.tx.QueryRowContext(t.ctx,
		`SELECT `+m8piColumns+` FROM plugin_installs WHERE subject_id=? AND plugin_id=?`,
		subjectID, pluginID))
	if errors.Is(err, sql.ErrNoRows) {
		return m8core.PluginInstall{}, false, nil
	}
	if err != nil {
		return m8core.PluginInstall{}, false, t.fail(err)
	}
	return i, true, nil
}

// PutInstall upserts the per-subject install row.
func (t *agentRuntimeTx) PutInstall(i m8core.PluginInstall) error {
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO plugin_installs
		(install_id,bundle_id,plugin_id,subject_id,origin,state,permission_grant_digest,installed_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?)
		ON CONFLICT(subject_id, plugin_id) DO UPDATE SET
			bundle_id=excluded.bundle_id,
			state=excluded.state,
			permission_grant_digest=excluded.permission_grant_digest,
			updated_at=excluded.updated_at`,
		i.InstallID, i.BundleID, i.PluginID, i.SubjectID, i.Origin, i.State,
		i.PermissionGrantDigest, i.InstalledAt, i.UpdatedAt)
	return t.fail(err)
}

const m8pcbColumns = `binding_id,install_id,target_type,target_id,capability_digest,state,created_at,revoked_at`

func scanPluginBinding(s interface{ Scan(...any) error }) (m8core.PluginCapabilityBinding, error) {
	var b m8core.PluginCapabilityBinding
	var revoked *string
	err := s.Scan(&b.BindingID, &b.InstallID, &b.TargetType, &b.TargetID,
		&b.CapabilityDigest, &b.State, &b.CreatedAt, &revoked)
	if revoked != nil {
		b.RevokedAt = *revoked
	}
	return b, err
}

// PutBinding inserts one binding row.
func (t *agentRuntimeTx) PutBinding(b m8core.PluginCapabilityBinding) error {
	var revoked any
	if b.RevokedAt != "" {
		revoked = b.RevokedAt
	}
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO plugin_capability_bindings
		(binding_id,install_id,target_type,target_id,capability_digest,state,created_at,revoked_at)
		VALUES(?,?,?,?,?,?,?,?)`,
		b.BindingID, b.InstallID, b.TargetType, b.TargetID, b.CapabilityDigest,
		b.State, b.CreatedAt, revoked)
	return t.fail(err)
}

// ListBindings answers every binding of one install (active and revoked).
func (t *agentRuntimeTx) ListBindings(installID string) ([]m8core.PluginCapabilityBinding, error) {
	rows, err := t.tx.QueryContext(t.ctx,
		`SELECT `+m8pcbColumns+` FROM plugin_capability_bindings WHERE install_id=?`, installID)
	if err != nil {
		return nil, t.fail(err)
	}
	defer rows.Close()
	var out []m8core.PluginCapabilityBinding
	for rows.Next() {
		b, err := scanPluginBinding(rows)
		if err != nil {
			return nil, t.fail(err)
		}
		out = append(out, b)
	}
	return out, t.fail(rows.Err())
}

// RevokeBindings flips every active binding of the install to revoked and
// answers the revoked count.
func (t *agentRuntimeTx) RevokeBindings(installID, now string) (int, error) {
	res, err := t.tx.ExecContext(t.ctx,
		`UPDATE plugin_capability_bindings SET state='revoked', revoked_at=? WHERE install_id=? AND state='active'`,
		now, installID)
	if err != nil {
		return 0, t.fail(err)
	}
	n, err := res.RowsAffected()
	return int(n), t.fail(err)
}

// ListInstalls answers the plugin.list projection joined with bundles,
// with optional kind/state filters.
func (t *agentRuntimeTx) ListInstalls(kind, state string) ([]m8app.PluginListItem, error) {
	q := `SELECT i.install_id, b.plugin_id, b.semver, b.publisher, b.kind,
			i.origin, i.state,
			(SELECT COUNT(*) FROM plugin_capability_bindings c
			 WHERE c.install_id = i.install_id AND c.state = 'active') AS binding_count,
			i.installed_at
		FROM plugin_installs i JOIN plugin_bundles b ON b.bundle_id = i.bundle_id
		WHERE 1=1`
	var args []any
	if kind != "" {
		q += ` AND b.kind = ?`
		args = append(args, kind)
	}
	if state != "" {
		q += ` AND i.state = ?`
		args = append(args, state)
	}
	q += ` ORDER BY i.installed_at`
	rows, err := t.tx.QueryContext(t.ctx, q, args...)
	if err != nil {
		return nil, t.fail(err)
	}
	defer rows.Close()
	out := []m8app.PluginListItem{}
	for rows.Next() {
		var it m8app.PluginListItem
		if err := rows.Scan(&it.InstallID, &it.PluginID, &it.Semver, &it.Publisher,
			&it.Kind, &it.Origin, &it.State, &it.BindingCount, &it.InstalledAt); err != nil {
			return nil, t.fail(err)
		}
		out = append(out, it)
	}
	return out, t.fail(rows.Err())
}
