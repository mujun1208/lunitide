// Prompt bundle persistence (migration 0084).
package sqlite

import (
	"database/sql"
	"errors"
	"time"

	"github.com/lunitide/lunitide/internal/domain/m6supply"
)

const m6PromptBundleColumns = `id,name,publisher,status,current_version_id,created_at,updated_at`

func scanM6PromptBundle(s interface{ Scan(...any) error }) (m6supply.PromptBundle, error) {
	var pb m6supply.PromptBundle
	var currentVersion sql.NullString
	var created, updated string
	if err := s.Scan(&pb.ID, &pb.Name, &pb.Publisher, &pb.Status, &currentVersion, &created, &updated); err != nil {
		return pb, err
	}
	pb.CurrentVersionID = currentVersion.String
	var err error
	if pb.CreatedAt, err = parseRFC(created); err != nil {
		return pb, err
	}
	if pb.UpdatedAt, err = parseRFC(updated); err != nil {
		return pb, err
	}
	return pb, nil
}

func (t *agentRuntimeTx) PutM6PromptBundle(pb m6supply.PromptBundle) error {
	var currentVersion any
	if pb.CurrentVersionID != "" {
		currentVersion = pb.CurrentVersionID
	}
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO m6_prompt_bundle
		(id,name,publisher,status,current_version_id,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?)`,
		pb.ID, pb.Name, pb.Publisher, pb.Status, currentVersion, rfc(pb.CreatedAt), rfc(pb.UpdatedAt))
	return t.fail(err)
}

func (t *agentRuntimeTx) FindM6PromptBundleByName(name string) (m6supply.PromptBundle, error) {
	pb, err := scanM6PromptBundle(t.tx.QueryRowContext(t.ctx, `SELECT `+m6PromptBundleColumns+` FROM m6_prompt_bundle WHERE name=?`, name))
	if errors.Is(err, sql.ErrNoRows) {
		return pb, m6supply.ErrNotFound
	}
	return pb, err
}

func (t *agentRuntimeTx) SetM6PromptBundleCurrentVersion(id, currentVersionID string, at time.Time) error {
	res, err := t.tx.ExecContext(t.ctx, `UPDATE m6_prompt_bundle SET current_version_id=?, updated_at=? WHERE id=?`,
		currentVersionID, rfc(at), id)
	if err != nil {
		return t.fail(err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return m6supply.ErrNotFound
	}
	return nil
}

const m6PromptBundleVersionColumns = `id,bundle_id,semver,manifest_ref,template_ref,package_hash,compiled_digest,compiled_body,signature_status,created_at`

func scanM6PromptBundleVersion(s interface{ Scan(...any) error }) (m6supply.PromptBundleVersion, error) {
	var v m6supply.PromptBundleVersion
	var created string
	if err := s.Scan(&v.ID, &v.BundleID, &v.Semver, &v.ManifestRef, &v.TemplateRef, &v.PackageHash, &v.CompiledDigest, &v.CompiledBody, &v.SignatureStatus, &created); err != nil {
		return v, err
	}
	var err error
	if v.CreatedAt, err = parseRFC(created); err != nil {
		return v, err
	}
	return v, nil
}

func (t *agentRuntimeTx) PutM6PromptBundleVersion(v m6supply.PromptBundleVersion) error {
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO m6_prompt_bundle_version
		(id,bundle_id,semver,manifest_ref,template_ref,package_hash,compiled_digest,compiled_body,signature_status,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?)`,
		v.ID, v.BundleID, v.Semver, v.ManifestRef, v.TemplateRef, v.PackageHash, v.CompiledDigest, v.CompiledBody, v.SignatureStatus, rfc(v.CreatedAt))
	return t.fail(err)
}

func (t *agentRuntimeTx) FindM6PromptBundleVersion(bundleID, semver string) (m6supply.PromptBundleVersion, error) {
	v, err := scanM6PromptBundleVersion(t.tx.QueryRowContext(t.ctx, `SELECT `+m6PromptBundleVersionColumns+` FROM m6_prompt_bundle_version WHERE bundle_id=? AND semver=?`, bundleID, semver))
	if errors.Is(err, sql.ErrNoRows) {
		return v, m6supply.ErrNotFound
	}
	return v, err
}

func (t *agentRuntimeTx) GetM6PromptBundleVersion(id string) (m6supply.PromptBundleVersion, error) {
	v, err := scanM6PromptBundleVersion(t.tx.QueryRowContext(t.ctx, `SELECT `+m6PromptBundleVersionColumns+` FROM m6_prompt_bundle_version WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return v, m6supply.ErrNotFound
	}
	return v, err
}
