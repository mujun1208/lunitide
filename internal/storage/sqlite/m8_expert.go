// M8 FR-19 storage (T-8.10.x): expert_catalog / expert_versions /
// project_phase_expert_mounting on the agent-runtime single-writer
// transaction. The expert_versions WORM chain is trigger-guarded
// (UPDATE/DELETE -> M8-043) so this layer only ever INSERTs version rows;
// the mounting cap is trigger-backed (trg_mount_limit -> M8-044).
package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"github.com/lunitide/lunitide/internal/domain/agentrun"
	"github.com/lunitide/lunitide/internal/domain/m8core"
	"github.com/lunitide/lunitide/internal/m8app"
)

// TransactExpert runs an m8app FR-19 use case on the shared single-writer
// transaction.
func (r *AgentRuntimeRepository) TransactExpert(ctx context.Context, fn func(m8app.ExpertTx) error) error {
	return r.Transact(ctx, func(tx agentrun.Tx) error {
		etx, ok := tx.(m8app.ExpertTx)
		if !ok {
			return errors.New("agent runtime tx does not satisfy m8app.ExpertTx")
		}
		return fn(etx)
	})
}

const m8ecColumns = `expert_id,subject_id,name,division,source,origin_bundle_id,current_version_id,state,created_at,updated_at`

func scanExpert(s interface{ Scan(...any) error }) (m8core.ExpertCatalog, error) {
	var e m8core.ExpertCatalog
	var origin *string
	err := s.Scan(&e.ExpertID, &e.SubjectID, &e.Name, &e.Division, &e.Source,
		&origin, &e.CurrentVersionID, &e.State, &e.CreatedAt, &e.UpdatedAt)
	if origin != nil {
		e.OriginBundleID = *origin
	}
	return e, err
}

// GetExpert answers one catalog row by id.
func (t *agentRuntimeTx) GetExpert(expertID string) (m8core.ExpertCatalog, error) {
	e, err := scanExpert(t.tx.QueryRowContext(t.ctx,
		`SELECT `+m8ecColumns+` FROM expert_catalog WHERE expert_id=?`, expertID))
	if errors.Is(err, sql.ErrNoRows) {
		return e, m8core.ErrNotFound
	}
	return e, t.fail(err)
}

// GetExpertByName answers the UNIQUE(subject_id, name) row.
func (t *agentRuntimeTx) GetExpertByName(subjectID, name string) (m8core.ExpertCatalog, bool, error) {
	e, err := scanExpert(t.tx.QueryRowContext(t.ctx,
		`SELECT `+m8ecColumns+` FROM expert_catalog WHERE subject_id=? AND name=?`, subjectID, name))
	if errors.Is(err, sql.ErrNoRows) {
		return m8core.ExpertCatalog{}, false, nil
	}
	if err != nil {
		return m8core.ExpertCatalog{}, false, t.fail(err)
	}
	return e, true, nil
}

// PutExpert upserts one catalog row.
func (t *agentRuntimeTx) PutExpert(e m8core.ExpertCatalog) error {
	var origin any
	if e.OriginBundleID != "" {
		origin = e.OriginBundleID
	}
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO expert_catalog
		(expert_id,subject_id,name,division,source,origin_bundle_id,current_version_id,state,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(expert_id) DO UPDATE SET
			current_version_id=excluded.current_version_id,
			state=excluded.state,
			updated_at=excluded.updated_at`,
		e.ExpertID, e.SubjectID, e.Name, e.Division, e.Source, origin,
		e.CurrentVersionID, e.State, e.CreatedAt, e.UpdatedAt)
	return t.fail(err)
}

const m8evColumns = `version_id,expert_id,semver,persona_ref,six_section_digest,change_note,created_at`

func scanExpertVersion(s interface{ Scan(...any) error }) (m8core.ExpertVersion, error) {
	var v m8core.ExpertVersion
	err := s.Scan(&v.VersionID, &v.ExpertID, &v.Semver, &v.PersonaRef,
		&v.SixSectionDigest, &v.ChangeNote, &v.CreatedAt)
	return v, err
}

// GetVersion answers one append-only version row.
func (t *agentRuntimeTx) GetVersion(versionID string) (m8core.ExpertVersion, error) {
	v, err := scanExpertVersion(t.tx.QueryRowContext(t.ctx,
		`SELECT `+m8evColumns+` FROM expert_versions WHERE version_id=?`, versionID))
	if errors.Is(err, sql.ErrNoRows) {
		return v, m8core.ErrNotFound
	}
	return v, t.fail(err)
}

// GetVersionBySemver answers the UNIQUE(expert_id, semver) row.
func (t *agentRuntimeTx) GetVersionBySemver(expertID, semver string) (m8core.ExpertVersion, bool, error) {
	v, err := scanExpertVersion(t.tx.QueryRowContext(t.ctx,
		`SELECT `+m8evColumns+` FROM expert_versions WHERE expert_id=? AND semver=?`, expertID, semver))
	if errors.Is(err, sql.ErrNoRows) {
		return m8core.ExpertVersion{}, false, nil
	}
	if err != nil {
		return m8core.ExpertVersion{}, false, t.fail(err)
	}
	return v, true, nil
}

// PutVersion inserts one append-only version row (the WORM triggers block
// any UPDATE/DELETE with M8-043).
func (t *agentRuntimeTx) PutVersion(v m8core.ExpertVersion) error {
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO expert_versions
		(version_id,expert_id,semver,persona_ref,six_section_digest,change_note,created_at)
		VALUES(?,?,?,?,?,?,?)`,
		v.VersionID, v.ExpertID, v.Semver, v.PersonaRef, v.SixSectionDigest,
		v.ChangeNote, v.CreatedAt)
	return t.fail(err)
}

// ListVersions answers the whole version chain oldest-first.
func (t *agentRuntimeTx) ListVersions(expertID string) ([]m8core.ExpertVersion, error) {
	rows, err := t.tx.QueryContext(t.ctx,
		`SELECT `+m8evColumns+` FROM expert_versions WHERE expert_id=? ORDER BY created_at, version_id`, expertID)
	if err != nil {
		return nil, t.fail(err)
	}
	defer rows.Close()
	var out []m8core.ExpertVersion
	for rows.Next() {
		v, err := scanExpertVersion(rows)
		if err != nil {
			return nil, t.fail(err)
		}
		out = append(out, v)
	}
	return out, t.fail(rows.Err())
}

// ListExperts answers the catalog projection joined with the current
// version, version count and mounted-phase count, with optional
// division/source/state filters; projectId narrows to experts holding a
// mounting row in that project.
func (t *agentRuntimeTx) ListExperts(filter m8app.ExpertFilter) ([]m8app.ExpertListItem, error) {
	q := `SELECT e.expert_id, e.name, e.division, e.source, v.semver, e.state,
			(SELECT COUNT(*) FROM expert_versions ev WHERE ev.expert_id = e.expert_id) AS version_count,
			(SELECT COUNT(*) FROM project_phase_expert_mounting m
			 WHERE m.expert_id = e.expert_id AND m.state = 'mounted') AS mounted_phase_count,
			e.origin_bundle_id
		FROM expert_catalog e JOIN expert_versions v ON v.version_id = e.current_version_id
		WHERE 1=1`
	var args []any
	if filter.Division != "" {
		q += ` AND e.division = ?`
		args = append(args, filter.Division)
	}
	if filter.Source != "" {
		q += ` AND e.source = ?`
		args = append(args, filter.Source)
	}
	if filter.State != "" {
		q += ` AND e.state = ?`
		args = append(args, filter.State)
	}
	if filter.ProjectID != "" {
		q += ` AND EXISTS (SELECT 1 FROM project_phase_expert_mounting pm
			WHERE pm.expert_id = e.expert_id AND pm.project_id = ?)`
		args = append(args, filter.ProjectID)
	}
	q += ` ORDER BY e.name`
	rows, err := t.tx.QueryContext(t.ctx, q, args...)
	if err != nil {
		return nil, t.fail(err)
	}
	defer rows.Close()
	out := []m8app.ExpertListItem{}
	for rows.Next() {
		var it m8app.ExpertListItem
		var origin *string
		if err := rows.Scan(&it.ExpertID, &it.Name, &it.Division, &it.Source,
			&it.Semver, &it.State, &it.VersionCount, &it.MountedPhaseCount, &origin); err != nil {
			return nil, t.fail(err)
		}
		if origin != nil {
			it.OriginBundleID = *origin
		}
		out = append(out, it)
	}
	return out, t.fail(rows.Err())
}

const m8pemColumns = `mounting_id,project_id,phase_key,expert_id,version_id,state,mounted_at,updated_at`

func scanMounting(s interface{ Scan(...any) error }) (m8core.ExpertMounting, error) {
	var m m8core.ExpertMounting
	err := s.Scan(&m.MountingID, &m.ProjectID, &m.PhaseKey, &m.ExpertID,
		&m.VersionID, &m.State, &m.MountedAt, &m.UpdatedAt)
	return m, err
}

// GetMounting answers the UNIQUE(project_id, phase_key, expert_id) row.
func (t *agentRuntimeTx) GetMounting(projectID, phaseKey, expertID string) (m8core.ExpertMounting, bool, error) {
	m, err := scanMounting(t.tx.QueryRowContext(t.ctx,
		`SELECT `+m8pemColumns+` FROM project_phase_expert_mounting
		WHERE project_id=? AND phase_key=? AND expert_id=?`, projectID, phaseKey, expertID))
	if errors.Is(err, sql.ErrNoRows) {
		return m8core.ExpertMounting{}, false, nil
	}
	if err != nil {
		return m8core.ExpertMounting{}, false, t.fail(err)
	}
	return m, true, nil
}

// PutMounting inserts or updates one mounting row (state, pinned version).
func (t *agentRuntimeTx) PutMounting(m m8core.ExpertMounting) error {
	// Existing rows take the UPDATE path: the M8-044 BEFORE INSERT trigger
	// counts the mounting row itself, so an upsert re-insert would refuse a
	// legal updateVersion/unmount-remount on a full phase.
	res, err := t.tx.ExecContext(t.ctx, `UPDATE project_phase_expert_mounting SET
		version_id=?, state=?, updated_at=? WHERE mounting_id=?`,
		m.VersionID, m.State, m.UpdatedAt, m.MountingID)
	if err != nil {
		return t.fail(err)
	}
	if n, _ := res.RowsAffected(); n > 0 {
		return nil
	}
	_, err = t.tx.ExecContext(t.ctx, `INSERT INTO project_phase_expert_mounting
		(mounting_id,project_id,phase_key,expert_id,version_id,state,mounted_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?)`,
		m.MountingID, m.ProjectID, m.PhaseKey, m.ExpertID, m.VersionID,
		m.State, m.MountedAt, m.UpdatedAt)
	return t.fail(err)
}

// ListMountingsByExpert answers every mounting row of one expert.
func (t *agentRuntimeTx) ListMountingsByExpert(expertID string) ([]m8core.ExpertMounting, error) {
	rows, err := t.tx.QueryContext(t.ctx,
		`SELECT `+m8pemColumns+` FROM project_phase_expert_mounting WHERE expert_id=?`, expertID)
	if err != nil {
		return nil, t.fail(err)
	}
	defer rows.Close()
	var out []m8core.ExpertMounting
	for rows.Next() {
		m, err := scanMounting(rows)
		if err != nil {
			return nil, t.fail(err)
		}
		out = append(out, m)
	}
	return out, t.fail(rows.Err())
}

// ListMountingsByProjectPhase answers the mounting projections of one
// (project, phase) joined with the pinned version semver and the expert
// catalog state.
func (t *agentRuntimeTx) ListMountingsByProjectPhase(projectID, phaseKey string) ([]m8app.ExpertMountingView, error) {
	rows, err := t.tx.QueryContext(t.ctx,
		`SELECT m.mounting_id, m.expert_id, m.version_id, v.semver, m.state, e.state
		FROM project_phase_expert_mounting m
		JOIN expert_versions v ON v.version_id = m.version_id
		JOIN expert_catalog e ON e.expert_id = m.expert_id
		WHERE m.project_id=? AND m.phase_key=?`, projectID, phaseKey)
	if err != nil {
		return nil, t.fail(err)
	}
	defer rows.Close()
	out := []m8app.ExpertMountingView{}
	for rows.Next() {
		var v m8app.ExpertMountingView
		if err := rows.Scan(&v.MountingID, &v.ExpertID, &v.VersionID,
			&v.Semver, &v.State, &v.ExpertState); err != nil {
			return nil, t.fail(err)
		}
		out = append(out, v)
	}
	return out, t.fail(rows.Err())
}

// CountMountedInPhase answers the mounted expert count of one (project,
// phase) - the M8-044 app pre-check (the trigger backs the insert path).
func (t *agentRuntimeTx) CountMountedInPhase(projectID, phaseKey string) (int, error) {
	var n int
	err := t.tx.QueryRowContext(t.ctx,
		`SELECT COUNT(*) FROM project_phase_expert_mounting
		WHERE project_id=? AND phase_key=? AND state='mounted'`, projectID, phaseKey).Scan(&n)
	return n, t.fail(err)
}
