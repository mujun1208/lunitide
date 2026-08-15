// M7 slice 1 (T-7.1.3/T-7.1.4 storage): workflow backbone tables on the
// agent-runtime single-writer transaction. agentRuntimeTx satisfies
// m7app.WorkflowTx; TransactM7 enforces the assertion at open time. All
// timestamps are TEXT RFC3339 (house style); published versions and artifact
// versions are additionally guarded by DB triggers (M7-WF-001/M7-ART-001).
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/lunitide/lunitide/internal/domain/agentrun"
	"github.com/lunitide/lunitide/internal/domain/m7flow"
	"github.com/lunitide/lunitide/internal/m7app"
)

// TransactM7 runs an m7app use case on the shared single-writer transaction.
func (r *AgentRuntimeRepository) TransactM7(ctx context.Context, fn func(m7app.WorkflowTx) error) error {
	return r.Transact(ctx, func(tx agentrun.Tx) error {
		m7tx, ok := tx.(m7app.WorkflowTx)
		if !ok {
			return errors.New("agent runtime tx does not satisfy m7app.WorkflowTx")
		}
		return fn(m7tx)
	})
}

const wfVersionColumns = `id,project_id,version,status,definition_digest,created_at,published_at`

func scanWorkflowVersion(s interface{ Scan(...any) error }) (m7flow.WorkflowVersion, error) {
	var v m7flow.WorkflowVersion
	var created string
	var published sql.NullString
	if err := s.Scan(&v.ID, &v.ProjectID, &v.Version, &v.Status, &v.DefinitionDigest, &created, &published); err != nil {
		return v, err
	}
	var err error
	if v.CreatedAt, err = parseRFC(created); err != nil {
		return v, err
	}
	if published.Valid {
		t, err := parseRFC(published.String)
		if err != nil {
			return v, err
		}
		v.PublishedAt = &t
	}
	return v, nil
}

func (t *agentRuntimeTx) GetProject(id string) (string, error) {
	var got string
	err := t.tx.QueryRowContext(t.ctx, `SELECT id FROM projects WHERE id=?`, id).Scan(&got)
	if errors.Is(err, sql.ErrNoRows) {
		return "", m7flow.ErrNotFound
	}
	return got, err
}

func (t *agentRuntimeTx) MaxWorkflowVersion(projectID string) (int64, error) {
	var max sql.NullInt64
	if err := t.tx.QueryRowContext(t.ctx, `SELECT max(version) FROM workflow_versions WHERE project_id=?`, projectID).Scan(&max); err != nil {
		return 0, t.fail(err)
	}
	return max.Int64, nil
}

func (t *agentRuntimeTx) PutWorkflowVersion(v m7flow.WorkflowVersion) error {
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO workflow_versions
		(id,project_id,version,status,definition_digest,created_at,published_at)
		VALUES(?,?,?,?,?,?,NULL)`,
		v.ID, v.ProjectID, v.Version, v.Status, v.DefinitionDigest, rfc(v.CreatedAt))
	return t.fail(err)
}

func (t *agentRuntimeTx) GetWorkflowVersion(id string) (m7flow.WorkflowVersion, error) {
	v, err := scanWorkflowVersion(t.tx.QueryRowContext(t.ctx,
		`SELECT `+wfVersionColumns+` FROM workflow_versions WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return v, m7flow.ErrNotFound
	}
	return v, t.fail(err)
}

func (t *agentRuntimeTx) FindPublishedWorkflowVersion(projectID string) (m7flow.WorkflowVersion, error) {
	v, err := scanWorkflowVersion(t.tx.QueryRowContext(t.ctx,
		`SELECT `+wfVersionColumns+` FROM workflow_versions WHERE project_id=? AND status='published' ORDER BY version DESC LIMIT 1`, projectID))
	if errors.Is(err, sql.ErrNoRows) {
		return v, m7flow.ErrNotFound
	}
	return v, t.fail(err)
}

func (t *agentRuntimeTx) PublishWorkflowVersion(id string, at time.Time) error {
	res, err := t.tx.ExecContext(t.ctx,
		`UPDATE workflow_versions SET status='published', published_at=? WHERE id=? AND status='draft'`, rfc(at), id)
	if err != nil {
		return t.fail(err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil // already published — idempotent
	}
	return nil
}

func (t *agentRuntimeTx) PutStageDefinitions(versionID string, defs []m7flow.StageDefinition) error {
	for _, d := range defs {
		if d.ID == "" {
			d.ID = ulid.Make().String()
		}
		_, err := t.tx.ExecContext(t.ctx, `INSERT INTO stage_definitions
			(id,workflow_version_id,stage_key,ordinal,name,dependency_keys,gate_policy)
			VALUES(?,?,?,?,?,?,?)`,
			d.ID, versionID, d.StageKey, d.Ordinal, d.Name, d.DependencyKeys, d.GatePolicy)
		if err != nil {
			return t.fail(err)
		}
	}
	return nil
}

const stageDefColumns = `id,workflow_version_id,stage_key,ordinal,name,dependency_keys,gate_policy`

func scanStageDef(s interface{ Scan(...any) error }) (m7flow.StageDefinition, error) {
	var d m7flow.StageDefinition
	err := s.Scan(&d.ID, &d.WorkflowVersion, &d.StageKey, &d.Ordinal, &d.Name, &d.DependencyKeys, &d.GatePolicy)
	return d, err
}

func (t *agentRuntimeTx) ListStageDefinitions(versionID string) ([]m7flow.StageDefinition, error) {
	rows, err := t.tx.QueryContext(t.ctx,
		`SELECT `+stageDefColumns+` FROM stage_definitions WHERE workflow_version_id=? ORDER BY ordinal`, versionID)
	if err != nil {
		return nil, t.fail(err)
	}
	defer rows.Close()
	var out []m7flow.StageDefinition
	for rows.Next() {
		d, err := scanStageDef(rows)
		if err != nil {
			return nil, t.fail(err)
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

const wfInstanceColumns = `id,project_id,workflow_version_id,state,created_at,completed_at`

func scanWorkflowInstance(s interface{ Scan(...any) error }) (m7flow.WorkflowInstance, error) {
	var v m7flow.WorkflowInstance
	var created string
	var completed sql.NullString
	if err := s.Scan(&v.ID, &v.ProjectID, &v.WorkflowVersionID, &v.State, &created, &completed); err != nil {
		return v, err
	}
	var err error
	if v.CreatedAt, err = parseRFC(created); err != nil {
		return v, err
	}
	if completed.Valid {
		tm, err := parseRFC(completed.String)
		if err != nil {
			return v, err
		}
		v.CompletedAt = &tm
	}
	return v, nil
}

func (t *agentRuntimeTx) PutWorkflowInstance(v m7flow.WorkflowInstance) error {
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO workflow_instances
		(id,project_id,workflow_version_id,state,created_at,completed_at)
		VALUES(?,?,?,?,?,NULL)`,
		v.ID, v.ProjectID, v.WorkflowVersionID, v.State, rfc(v.CreatedAt))
	return t.fail(err)
}

func (t *agentRuntimeTx) GetWorkflowInstance(id string) (m7flow.WorkflowInstance, error) {
	v, err := scanWorkflowInstance(t.tx.QueryRowContext(t.ctx,
		`SELECT `+wfInstanceColumns+` FROM workflow_instances WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return v, m7flow.ErrNotFound
	}
	return v, t.fail(err)
}

func (t *agentRuntimeTx) FindRunningInstance(projectID string) (m7flow.WorkflowInstance, error) {
	v, err := scanWorkflowInstance(t.tx.QueryRowContext(t.ctx,
		`SELECT `+wfInstanceColumns+` FROM workflow_instances WHERE project_id=? AND state='running' ORDER BY created_at DESC LIMIT 1`, projectID))
	if errors.Is(err, sql.ErrNoRows) {
		return v, m7flow.ErrNotFound
	}
	return v, t.fail(err)
}

const stageRunColumns = `id,project_workflow_instance_id,stage_definition_id,attempt_no,state,lock_version,started_at,completed_at,created_at`

func scanStageRun(s interface{ Scan(...any) error }) (m7flow.StageRun, error) {
	var r m7flow.StageRun
	var started, completed sql.NullString
	var created string
	if err := s.Scan(&r.ID, &r.InstanceID, &r.StageDefinitionID, &r.AttemptNo, &r.State, &r.LockVersion, &started, &completed, &created); err != nil {
		return r, err
	}
	var err error
	if r.CreatedAt, err = parseRFC(created); err != nil {
		return r, err
	}
	if started.Valid {
		tm, err := parseRFC(started.String)
		if err != nil {
			return r, err
		}
		r.StartedAt = &tm
	}
	if completed.Valid {
		tm, err := parseRFC(completed.String)
		if err != nil {
			return r, err
		}
		r.CompletedAt = &tm
	}
	return r, nil
}

func (t *agentRuntimeTx) PutStageRun(r m7flow.StageRun) error {
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO stage_runs
		(id,project_workflow_instance_id,stage_definition_id,attempt_no,state,lock_version,started_at,completed_at,created_at)
		VALUES(?,?,?,?,?,?,?,?,?)`,
		r.ID, r.InstanceID, r.StageDefinitionID, r.AttemptNo, r.State, r.LockVersion,
		nullTime(r.StartedAt), nullTime(r.CompletedAt), rfc(r.CreatedAt))
	return t.fail(err)
}

func (t *agentRuntimeTx) GetStageRun(id string) (m7flow.StageRun, error) {
	r, err := scanStageRun(t.tx.QueryRowContext(t.ctx,
		`SELECT `+stageRunColumns+` FROM stage_runs WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return r, m7flow.ErrNotFound
	}
	return r, t.fail(err)
}

func (t *agentRuntimeTx) FindActiveStageRun(instanceID, stageDefID string) (m7flow.StageRun, error) {
	r, err := scanStageRun(t.tx.QueryRowContext(t.ctx,
		`SELECT `+stageRunColumns+` FROM stage_runs
		WHERE project_workflow_instance_id=? AND stage_definition_id=? AND state NOT IN ('completed','cancelled')
		ORDER BY attempt_no DESC LIMIT 1`, instanceID, stageDefID))
	if errors.Is(err, sql.ErrNoRows) {
		return r, m7flow.ErrNotFound
	}
	return r, t.fail(err)
}

func (t *agentRuntimeTx) MaxStageAttempt(instanceID, stageDefID string) (int64, error) {
	var max sql.NullInt64
	if err := t.tx.QueryRowContext(t.ctx,
		`SELECT max(attempt_no) FROM stage_runs WHERE project_workflow_instance_id=? AND stage_definition_id=?`,
		instanceID, stageDefID).Scan(&max); err != nil {
		return 0, t.fail(err)
	}
	return max.Int64, nil
}

func (t *agentRuntimeTx) LatestStageRunState(instanceID, stageDefID string) (string, error) {
	var state string
	err := t.tx.QueryRowContext(t.ctx,
		`SELECT state FROM stage_runs WHERE project_workflow_instance_id=? AND stage_definition_id=?
		ORDER BY attempt_no DESC LIMIT 1`, instanceID, stageDefID).Scan(&state)
	if errors.Is(err, sql.ErrNoRows) {
		return "", m7flow.ErrNotFound
	}
	return state, t.fail(err)
}

func (t *agentRuntimeTx) UpdateStageRunState(id string, expectedVersion int64, to string, at time.Time, completed bool) (m7flow.StageRun, error) {
	var completedAt any
	if completed {
		completedAt = rfc(at)
	}
	res, err := t.tx.ExecContext(t.ctx,
		`UPDATE stage_runs SET state=?, lock_version=lock_version+1, completed_at=COALESCE(?, completed_at),
		 started_at=COALESCE(started_at, CASE WHEN ?='running' THEN ? ELSE started_at END)
		 WHERE id=? AND lock_version=?`,
		to, completedAt, to, rfc(at), id, expectedVersion)
	if err != nil {
		return m7flow.StageRun{}, t.fail(err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return m7flow.StageRun{}, m7flow.ErrVersionConflict
	}
	return t.GetStageRun(id)
}

func nullTime(tp *time.Time) any {
	if tp == nil {
		return nil
	}
	return rfc(*tp)
}

const snapshotColumns = `id,stage_run_id,inputs_json,digest,captured_at`

func scanInputSnapshot(s interface{ Scan(...any) error }) (m7flow.InputSnapshot, error) {
	var v m7flow.InputSnapshot
	var captured string
	if err := s.Scan(&v.ID, &v.StageRunID, &v.InputsJSON, &v.Digest, &captured); err != nil {
		return v, err
	}
	var err error
	if v.CapturedAt, err = parseRFC(captured); err != nil {
		return v, err
	}
	return v, nil
}

func (t *agentRuntimeTx) PutInputSnapshot(v m7flow.InputSnapshot) error {
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO stage_input_snapshots
		(id,stage_run_id,inputs_json,digest,captured_at) VALUES(?,?,?,?,?)`,
		v.ID, v.StageRunID, v.InputsJSON, v.Digest, rfc(v.CapturedAt))
	return t.fail(err)
}

func (t *agentRuntimeTx) LatestInputSnapshot(stageRunID string) (m7flow.InputSnapshot, error) {
	v, err := scanInputSnapshot(t.tx.QueryRowContext(t.ctx,
		`SELECT `+snapshotColumns+` FROM stage_input_snapshots WHERE stage_run_id=? ORDER BY captured_at DESC, id DESC LIMIT 1`, stageRunID))
	if errors.Is(err, sql.ErrNoRows) {
		return v, m7flow.ErrNotFound
	}
	return v, t.fail(err)
}

const artifactColumns = `id,artifact_id,version_no,kind,scope_type,scope_id,content_ref,sha256,size,media_type,state,created_by,created_at`

func scanArtifactVersion(s interface{ Scan(...any) error }) (m7flow.ArtifactVersion, error) {
	var v m7flow.ArtifactVersion
	var created string
	if err := s.Scan(&v.ID, &v.ArtifactID, &v.VersionNo, &v.Kind, &v.ScopeType, &v.ScopeID,
		&v.ContentRef, &v.SHA256, &v.Size, &v.MediaType, &v.State, &v.CreatedBy, &created); err != nil {
		return v, err
	}
	var err error
	if v.CreatedAt, err = parseRFC(created); err != nil {
		return v, err
	}
	return v, nil
}

func (t *agentRuntimeTx) PutArtifactVersion(v m7flow.ArtifactVersion) error {
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO artifact_versions
		(id,artifact_id,version_no,kind,scope_type,scope_id,content_ref,sha256,size,media_type,state,created_by,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		v.ID, v.ArtifactID, v.VersionNo, v.Kind, v.ScopeType, v.ScopeID,
		v.ContentRef, v.SHA256, v.Size, v.MediaType, v.State, v.CreatedBy, rfc(v.CreatedAt))
	return t.fail(err)
}

func (t *agentRuntimeTx) FindArtifactVersion(artifactID string, versionNo int64) (m7flow.ArtifactVersion, error) {
	v, err := scanArtifactVersion(t.tx.QueryRowContext(t.ctx,
		`SELECT `+artifactColumns+` FROM artifact_versions WHERE artifact_id=? AND version_no=?`, artifactID, versionNo))
	if errors.Is(err, sql.ErrNoRows) {
		return v, m7flow.ErrNotFound
	}
	return v, t.fail(err)
}

func (t *agentRuntimeTx) MaxArtifactVersion(artifactID string) (int64, error) {
	var max sql.NullInt64
	if err := t.tx.QueryRowContext(t.ctx,
		`SELECT max(version_no) FROM artifact_versions WHERE artifact_id=?`, artifactID).Scan(&max); err != nil {
		return 0, t.fail(err)
	}
	return max.Int64, nil
}

var _ m7app.WorkflowTx = (*agentRuntimeTx)(nil)
var _ m7app.WorkflowUnitOfWork = (*AgentRuntimeRepository)(nil)
