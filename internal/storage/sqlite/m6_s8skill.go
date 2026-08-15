// Legacy S8 skill chain persistence (migration 0053): the five skill
// tables plus the import-candidate pipeline. Version-chain rows are
// append-only in practice (a version is pinned, never edited); the
// candidate pipeline is a version-CAS state walk.
package sqlite

import (
	"database/sql"
	"errors"
	"time"

	"github.com/lunitide/lunitide/internal/domain/m6supply"
)

// ── Skill chain ─────────────────────────────────────────────────────────────

const m6SkillColumns = `id,name,publisher,status,current_version_id,created_at,updated_at`

func scanM6Skill(s interface{ Scan(...any) error }) (m6supply.Skill, error) {
	var sk m6supply.Skill
	var currentVersion sql.NullString
	var created, updated string
	if err := s.Scan(&sk.ID, &sk.Name, &sk.Publisher, &sk.Status, &currentVersion, &created, &updated); err != nil {
		return sk, err
	}
	sk.CurrentVersionID = currentVersion.String
	var err error
	if sk.CreatedAt, err = parseRFC(created); err != nil {
		return sk, err
	}
	if sk.UpdatedAt, err = parseRFC(updated); err != nil {
		return sk, err
	}
	return sk, nil
}

func (t *agentRuntimeTx) PutM6Skill(sk m6supply.Skill) error {
	var currentVersion any
	if sk.CurrentVersionID != "" {
		currentVersion = sk.CurrentVersionID
	}
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO m6_skill
		(id,name,publisher,status,current_version_id,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?)`,
		sk.ID, sk.Name, sk.Publisher, sk.Status, currentVersion, rfc(sk.CreatedAt), rfc(sk.UpdatedAt))
	return t.fail(err)
}

func (t *agentRuntimeTx) GetM6Skill(id string) (m6supply.Skill, error) {
	sk, err := scanM6Skill(t.tx.QueryRowContext(t.ctx, `SELECT `+m6SkillColumns+` FROM m6_skill WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return sk, m6supply.ErrNotFound
	}
	return sk, err
}

func (t *agentRuntimeTx) FindM6SkillByName(name string) (m6supply.Skill, error) {
	sk, err := scanM6Skill(t.tx.QueryRowContext(t.ctx, `SELECT `+m6SkillColumns+` FROM m6_skill WHERE name=?`, name))
	if errors.Is(err, sql.ErrNoRows) {
		return sk, m6supply.ErrNotFound
	}
	return sk, err
}

// SetM6SkillCurrentVersion CAS-repoints the current-version pointer.
func (t *agentRuntimeTx) SetM6SkillCurrentVersion(id, currentVersionID string, at time.Time) error {
	res, err := t.tx.ExecContext(t.ctx, `UPDATE m6_skill SET current_version_id=?, updated_at=? WHERE id=?`,
		currentVersionID, rfc(at), id)
	if err != nil {
		return t.fail(err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return m6supply.ErrNotFound
	}
	return nil
}

// TransitionM6SkillState CAS-updates the status.
func (t *agentRuntimeTx) TransitionM6SkillState(id string, to string, at time.Time) (m6supply.Skill, error) {
	res, err := t.tx.ExecContext(t.ctx, `UPDATE m6_skill SET status=?, updated_at=? WHERE id=?`, to, rfc(at), id)
	if err != nil {
		return m6supply.Skill{}, t.fail(err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return m6supply.Skill{}, m6supply.ErrNotFound
	}
	return t.GetM6Skill(id)
}

const m6SkillVersionColumns = `id,skill_id,semver,manifest_ref,package_hash,signature_status,permissions_json,created_at`

func scanM6SkillVersion(s interface{ Scan(...any) error }) (m6supply.SkillVersion, error) {
	var v m6supply.SkillVersion
	var created string
	if err := s.Scan(&v.ID, &v.SkillID, &v.Semver, &v.ManifestRef, &v.PackageHash, &v.SignatureStatus, &v.PermissionsJSON, &created); err != nil {
		return v, err
	}
	var err error
	if v.CreatedAt, err = parseRFC(created); err != nil {
		return v, err
	}
	return v, nil
}

func (t *agentRuntimeTx) PutM6SkillVersion(v m6supply.SkillVersion) error {
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO m6_skill_version
		(id,skill_id,semver,manifest_ref,package_hash,signature_status,permissions_json,created_at)
		VALUES(?,?,?,?,?,?,?,?)`,
		v.ID, v.SkillID, v.Semver, v.ManifestRef, v.PackageHash, v.SignatureStatus, v.PermissionsJSON, rfc(v.CreatedAt))
	return t.fail(err)
}

func (t *agentRuntimeTx) GetM6SkillVersion(id string) (m6supply.SkillVersion, error) {
	v, err := scanM6SkillVersion(t.tx.QueryRowContext(t.ctx, `SELECT `+m6SkillVersionColumns+` FROM m6_skill_version WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return v, m6supply.ErrNotFound
	}
	return v, err
}

func (t *agentRuntimeTx) FindM6SkillVersion(skillID, semver string) (m6supply.SkillVersion, error) {
	v, err := scanM6SkillVersion(t.tx.QueryRowContext(t.ctx, `SELECT `+m6SkillVersionColumns+` FROM m6_skill_version WHERE skill_id=? AND semver=?`, skillID, semver))
	if errors.Is(err, sql.ErrNoRows) {
		return v, m6supply.ErrNotFound
	}
	return v, err
}

func (t *agentRuntimeTx) PutM6SkillDependency(d m6supply.SkillDependency) error {
	var lockedDigest any
	if d.LockedDigest != "" {
		lockedDigest = d.LockedDigest
	}
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO m6_skill_dependency
		(id,skill_version_id,dependency_type,name,version_constraint,locked_digest,created_at)
		VALUES(?,?,?,?,?,?,?)`,
		d.ID, d.SkillVersionID, d.DependencyType, d.Name, d.VersionConstraint, lockedDigest, rfc(d.CreatedAt))
	return t.fail(err)
}

func (t *agentRuntimeTx) ListM6SkillDependencies(skillVersionID string) ([]m6supply.SkillDependency, error) {
	rows, err := t.tx.QueryContext(t.ctx, `SELECT id,skill_version_id,dependency_type,name,version_constraint,locked_digest,created_at FROM m6_skill_dependency WHERE skill_version_id=?`, skillVersionID)
	if err != nil {
		return nil, t.fail(err)
	}
	defer rows.Close()
	var out []m6supply.SkillDependency
	for rows.Next() {
		var d m6supply.SkillDependency
		var locked sql.NullString
		var created string
		if err := rows.Scan(&d.ID, &d.SkillVersionID, &d.DependencyType, &d.Name, &d.VersionConstraint, &locked, &created); err != nil {
			return nil, t.fail(err)
		}
		d.LockedDigest = locked.String
		if d.CreatedAt, err = parseRFC(created); err != nil {
			return nil, t.fail(err)
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (t *agentRuntimeTx) PutM6SkillInstall(i m6supply.SkillInstall) error {
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO m6_skill_install
		(id,skill_version_id,workspace_id,status,installed_at,version,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?)`,
		i.ID, i.SkillVersionID, i.WorkspaceID, i.Status, rfc(i.InstalledAt), i.Version, rfc(i.CreatedAt), rfc(i.UpdatedAt))
	return t.fail(err)
}

// SetM6SkillInstallStatus CAS-updates an install status.
func (t *agentRuntimeTx) SetM6SkillInstallStatus(id string, expectedVersion int64, status string, at time.Time) (m6supply.SkillInstall, error) {
	res, err := t.tx.ExecContext(t.ctx, `UPDATE m6_skill_install SET status=?, version=version+1, updated_at=? WHERE id=? AND version=?`,
		status, rfc(at), id, expectedVersion)
	if err != nil {
		return m6supply.SkillInstall{}, t.fail(err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return m6supply.SkillInstall{}, m6supply.ErrVersionConflict
	}
	var out m6supply.SkillInstall
	row := t.tx.QueryRowContext(t.ctx, `SELECT id,skill_version_id,workspace_id,status,installed_at,version,created_at,updated_at FROM m6_skill_install WHERE id=?`, id)
	var installedAt, createdAt, updatedAt string
	if err := row.Scan(&out.ID, &out.SkillVersionID, &out.WorkspaceID, &out.Status, &installedAt, &out.Version, &createdAt, &updatedAt); err != nil {
		return out, err
	}
	if out.InstalledAt, err = parseRFC(installedAt); err != nil {
		return out, err
	}
	if out.CreatedAt, err = parseRFC(createdAt); err != nil {
		return out, err
	}
	if out.UpdatedAt, err = parseRFC(updatedAt); err != nil {
		return out, err
	}
	return out, nil
}

func (t *agentRuntimeTx) FindM6SkillInstall(skillVersionID, workspaceID string) (m6supply.SkillInstall, error) {
	row := t.tx.QueryRowContext(t.ctx, `SELECT id,skill_version_id,workspace_id,status,installed_at,version,created_at,updated_at FROM m6_skill_install WHERE skill_version_id=? AND workspace_id=?`, skillVersionID, workspaceID)
	var out m6supply.SkillInstall
	var installedAt, createdAt, updatedAt string
	err := row.Scan(&out.ID, &out.SkillVersionID, &out.WorkspaceID, &out.Status, &installedAt, &out.Version, &createdAt, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return out, m6supply.ErrNotFound
	}
	if err != nil {
		return out, err
	}
	if out.InstalledAt, err = parseRFC(installedAt); err != nil {
		return out, err
	}
	if out.CreatedAt, err = parseRFC(createdAt); err != nil {
		return out, err
	}
	if out.UpdatedAt, err = parseRFC(updatedAt); err != nil {
		return out, err
	}
	return out, nil
}

func (t *agentRuntimeTx) PutM6SkillTrigger(tr m6supply.SkillTrigger) error {
	var resultRef any
	if tr.ResultRef != "" {
		resultRef = tr.ResultRef
	}
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO m6_skill_trigger
		(id,session_id,skill_version_id,score,reason,status,result_ref,created_at)
		VALUES(?,?,?,?,?,?,?,?)`,
		tr.ID, tr.SessionID, tr.SkillVersionID, tr.Score, tr.Reason, tr.Status, resultRef, rfc(tr.CreatedAt))
	return t.fail(err)
}

// ── Import candidate pipeline ───────────────────────────────────────────────

const m6ImportColumns = `id,asset_type,source_url,immutable_commit,archive_hash,license,notice_ref,publisher,signature,source_attestation,scan_refs,injection_scan,evaluation_id,approval,state,version,created_at,updated_at`

func scanM6ImportCandidate(s interface{ Scan(...any) error }) (m6supply.ImportCandidate, error) {
	var c m6supply.ImportCandidate
	var noticeRef, signature, sourceAttestation, scanRefs, injectionScan, evaluationID, approval sql.NullString
	var created, updated string
	if err := s.Scan(&c.ID, &c.AssetType, &c.SourceURL, &c.ImmutableCommit, &c.ArchiveHash, &c.License,
		&noticeRef, &c.Publisher, &signature, &sourceAttestation, &scanRefs, &injectionScan,
		&evaluationID, &approval, &c.State, &c.Version, &created, &updated); err != nil {
		return c, err
	}
	c.NoticeRef = noticeRef.String
	c.Signature = signature.String
	c.SourceAttestation = sourceAttestation.String
	c.ScanRefs = scanRefs.String
	c.InjectionScan = injectionScan.String
	c.EvaluationID = evaluationID.String
	c.Approval = approval.String
	var err error
	if c.CreatedAt, err = parseRFC(created); err != nil {
		return c, err
	}
	if c.UpdatedAt, err = parseRFC(updated); err != nil {
		return c, err
	}
	return c, nil
}

func (t *agentRuntimeTx) PutM6ImportCandidate(c m6supply.ImportCandidate) error {
	var noticeRef, signature, sourceAttestation, scanRefs, injectionScan, evaluationID, approval any
	if c.NoticeRef != "" {
		noticeRef = c.NoticeRef
	}
	if c.Signature != "" {
		signature = c.Signature
	}
	if c.SourceAttestation != "" {
		sourceAttestation = c.SourceAttestation
	}
	if c.ScanRefs != "" {
		scanRefs = c.ScanRefs
	}
	if c.InjectionScan != "" {
		injectionScan = c.InjectionScan
	}
	if c.EvaluationID != "" {
		evaluationID = c.EvaluationID
	}
	if c.Approval != "" {
		approval = c.Approval
	}
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO m6_import_candidate
		(id,asset_type,source_url,immutable_commit,archive_hash,license,notice_ref,publisher,signature,source_attestation,scan_refs,injection_scan,evaluation_id,approval,state,version,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		c.ID, c.AssetType, c.SourceURL, c.ImmutableCommit, c.ArchiveHash, c.License,
		noticeRef, c.Publisher, signature, sourceAttestation, scanRefs, injectionScan,
		evaluationID, approval, c.State, c.Version, rfc(c.CreatedAt), rfc(c.UpdatedAt))
	return t.fail(err)
}

func (t *agentRuntimeTx) GetM6ImportCandidate(id string) (m6supply.ImportCandidate, error) {
	c, err := scanM6ImportCandidate(t.tx.QueryRowContext(t.ctx, `SELECT `+m6ImportColumns+` FROM m6_import_candidate WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return c, m6supply.ErrNotFound
	}
	return c, err
}

func (t *agentRuntimeTx) FindM6ImportCandidate(sourceURL, immutableCommit string) (m6supply.ImportCandidate, error) {
	c, err := scanM6ImportCandidate(t.tx.QueryRowContext(t.ctx, `SELECT `+m6ImportColumns+` FROM m6_import_candidate WHERE source_url=? AND immutable_commit=?`, sourceURL, immutableCommit))
	if errors.Is(err, sql.ErrNoRows) {
		return c, m6supply.ErrNotFound
	}
	return c, err
}

// TransitionM6ImportCandidate CAS-updates the pipeline state. The payload
// columns (scan_refs, evaluation_id, approval, ...) ride the same UPDATE
// as the state; empty strings leave the stored column untouched.
func (t *agentRuntimeTx) TransitionM6ImportCandidate(id string, expectedVersion int64, to string, evidence m6supply.ImportEvidence, at time.Time) (m6supply.ImportCandidate, error) {
	var noticeRef, signature, sourceAttestation, scanRefs, injectionScan, evaluationID, approval any
	if evidence.NoticeRef != "" {
		noticeRef = evidence.NoticeRef
	}
	if evidence.Signature != "" {
		signature = evidence.Signature
	}
	if evidence.SourceAttestation != "" {
		sourceAttestation = evidence.SourceAttestation
	}
	if evidence.ScanRefs != "" {
		scanRefs = evidence.ScanRefs
	}
	if evidence.InjectionScan != "" {
		injectionScan = evidence.InjectionScan
	}
	if evidence.EvaluationID != "" {
		evaluationID = evidence.EvaluationID
	}
	if evidence.Approval != "" {
		approval = evidence.Approval
	}
	res, err := t.tx.ExecContext(t.ctx, `UPDATE m6_import_candidate SET
		state=?, version=version+1, updated_at=?,
		notice_ref=COALESCE(?, notice_ref),
		signature=COALESCE(?, signature),
		source_attestation=COALESCE(?, source_attestation),
		scan_refs=COALESCE(?, scan_refs),
		injection_scan=COALESCE(?, injection_scan),
		evaluation_id=COALESCE(?, evaluation_id),
		approval=COALESCE(?, approval)
		WHERE id=? AND version=?`,
		to, rfc(at), noticeRef, signature, sourceAttestation, scanRefs, injectionScan, evaluationID, approval, id, expectedVersion)
	if err != nil {
		return m6supply.ImportCandidate{}, t.fail(err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return m6supply.ImportCandidate{}, m6supply.ErrVersionConflict
	}
	return t.GetM6ImportCandidate(id)
}
