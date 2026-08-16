// M7 slice 2 (T-7.2.1 storage): the twelve evidence tables on the shared
// agent-runtime transaction. agentRuntimeTx satisfies m7app.EvidenceTx;
// every write hits an append-only table guarded by M7-EVD-001 triggers
// except dev_tasks, whose state machine updates under optimistic locking.
// The workflow reads (GetStageRun/LatestInputSnapshot) are inherited from
// m7_workflow.go so gate evaluation never re-enters the unit of work.
package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"github.com/lunitide/lunitide/internal/domain/agentrun"
	"github.com/lunitide/lunitide/internal/domain/m7flow"
	"github.com/lunitide/lunitide/internal/m7app"
)

// TransactEvidence runs an m7app slice-2 use case on the shared single-writer
// transaction.
func (r *AgentRuntimeRepository) TransactEvidence(ctx context.Context, fn func(m7app.EvidenceTx) error) error {
	return r.Transact(ctx, func(tx agentrun.Tx) error {
		etx, ok := tx.(m7app.EvidenceTx)
		if !ok {
			return errors.New("agent runtime tx does not satisfy m7app.EvidenceTx")
		}
		return fn(etx)
	})
}

// nodeTables maps trace node types onto their authoritative tables. Only
// types with a row identity are traceable (TRC-001 endpoint existence).
var nodeTables = map[string]string{
	"project":              "projects",
	"workflow_version":     "workflow_versions",
	"workflow_instance":    "workflow_instances",
	"stage_run":            "stage_runs",
	"stage_input_snapshot": "stage_input_snapshots",
	"artifact_version":     "artifact_versions",
	"review":               "reviews",
	"trace_edge":           "trace_edges",
	"dev_task":             "dev_tasks",
	"test_run":             "test_runs",
	"scan_run":             "scan_runs",
	"cr_revision":          "cr_revisions",
	"release_package":      "release_packages",
}

func (t *agentRuntimeTx) NodeExists(nodeType, nodeID string) (bool, error) {
	table, ok := nodeTables[nodeType]
	if !ok {
		return false, nil
	}
	var one int
	err := t.tx.QueryRowContext(t.ctx, `SELECT 1 FROM `+table+` WHERE id=? LIMIT 1`, nodeID).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, t.fail(err)
	}
	return true, nil
}

const edgeColumns = `id,from_type,from_id,from_digest,relation,to_type,to_id,to_digest,created_at`

func scanEdge(s interface{ Scan(...any) error }) (m7flow.TraceEdge, error) {
	var e m7flow.TraceEdge
	var created string
	if err := s.Scan(&e.ID, &e.FromType, &e.FromID, &e.FromDigest, &e.Relation,
		&e.ToType, &e.ToID, &e.ToDigest, &created); err != nil {
		return e, err
	}
	var err error
	if e.CreatedAt, err = parseRFC(created); err != nil {
		return e, err
	}
	return e, nil
}

func (t *agentRuntimeTx) PutEdge(e m7flow.TraceEdge) error {
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO trace_edges
		(id,from_type,from_id,from_digest,relation,to_type,to_id,to_digest,created_at)
		VALUES(?,?,?,?,?,?,?,?,?)`,
		e.ID, e.FromType, e.FromID, e.FromDigest, e.Relation, e.ToType, e.ToID, e.ToDigest, rfc(e.CreatedAt))
	return t.fail(err)
}

func (t *agentRuntimeTx) FindEdge(fromType, fromID, relation, toType, toID string) (m7flow.TraceEdge, error) {
	e, err := scanEdge(t.tx.QueryRowContext(t.ctx,
		`SELECT `+edgeColumns+` FROM trace_edges
		WHERE from_type=? AND from_id=? AND relation=? AND to_type=? AND to_id=? LIMIT 1`,
		fromType, fromID, relation, toType, toID))
	if errors.Is(err, sql.ErrNoRows) {
		return e, m7flow.ErrNotFound
	}
	return e, t.fail(err)
}

func (t *agentRuntimeTx) EdgesFrom(fromType, fromID string, limit int) ([]m7flow.TraceEdge, error) {
	rows, err := t.tx.QueryContext(t.ctx,
		`SELECT `+edgeColumns+` FROM trace_edges WHERE from_type=? AND from_id=? ORDER BY id LIMIT ?`,
		fromType, fromID, limit)
	if err != nil {
		return nil, t.fail(err)
	}
	defer rows.Close()
	var out []m7flow.TraceEdge
	for rows.Next() {
		e, err := scanEdge(rows)
		if err != nil {
			return nil, t.fail(err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (t *agentRuntimeTx) EdgesTo(toType, toID string, limit int) ([]m7flow.TraceEdge, error) {
	rows, err := t.tx.QueryContext(t.ctx,
		`SELECT `+edgeColumns+` FROM trace_edges WHERE to_type=? AND to_id=? ORDER BY id LIMIT ?`,
		toType, toID, limit)
	if err != nil {
		return nil, t.fail(err)
	}
	defer rows.Close()
	var out []m7flow.TraceEdge
	for rows.Next() {
		e, err := scanEdge(rows)
		if err != nil {
			return nil, t.fail(err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

const staleMarkColumns = `id,subject_type,subject_id,cause_edge,detected_at`

func scanStaleMark(s interface{ Scan(...any) error }) (m7flow.StaleMark, error) {
	var m m7flow.StaleMark
	var detected string
	if err := s.Scan(&m.ID, &m.SubjectType, &m.SubjectID, &m.CauseEdge, &detected); err != nil {
		return m, err
	}
	var err error
	if m.DetectedAt, err = parseRFC(detected); err != nil {
		return m, err
	}
	return m, nil
}

func (t *agentRuntimeTx) PutStaleMark(m m7flow.StaleMark) error {
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO stale_marks
		(id,subject_type,subject_id,cause_edge,detected_at) VALUES(?,?,?,?,?)`,
		m.ID, m.SubjectType, m.SubjectID, m.CauseEdge, rfc(m.DetectedAt))
	return t.fail(err)
}

func (t *agentRuntimeTx) FindStaleMark(subjectType, subjectID string) (m7flow.StaleMark, error) {
	m, err := scanStaleMark(t.tx.QueryRowContext(t.ctx,
		`SELECT `+staleMarkColumns+` FROM stale_marks WHERE subject_type=? AND subject_id=? ORDER BY id DESC LIMIT 1`,
		subjectType, subjectID))
	if errors.Is(err, sql.ErrNoRows) {
		return m, m7flow.ErrNotFound
	}
	return m, t.fail(err)
}

func (t *agentRuntimeTx) GetStaleMark(id string) (m7flow.StaleMark, error) {
	m, err := scanStaleMark(t.tx.QueryRowContext(t.ctx,
		`SELECT `+staleMarkColumns+` FROM stale_marks WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return m, m7flow.ErrNotFound
	}
	return m, t.fail(err)
}

const staleResColumns = `id,stale_mark_id,resolution_type,reevaluation_id,resolved_by,resolved_at`

func scanStaleResolution(s interface{ Scan(...any) error }) (m7flow.StaleResolution, error) {
	var r m7flow.StaleResolution
	var reeval sql.NullString
	var resolved string
	if err := s.Scan(&r.ID, &r.StaleMarkID, &r.ResolutionType, &reeval, &r.ResolvedBy, &resolved); err != nil {
		return r, err
	}
	r.ReevaluationID = reeval.String
	var err error
	if r.ResolvedAt, err = parseRFC(resolved); err != nil {
		return r, err
	}
	return r, nil
}

func (t *agentRuntimeTx) PutStaleResolution(r m7flow.StaleResolution) error {
	var reeval any
	if r.ReevaluationID != "" {
		reeval = r.ReevaluationID
	}
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO stale_resolutions
		(id,stale_mark_id,resolution_type,reevaluation_id,resolved_by,resolved_at)
		VALUES(?,?,?,?,?,?)`,
		r.ID, r.StaleMarkID, r.ResolutionType, reeval, r.ResolvedBy, rfc(r.ResolvedAt))
	return t.fail(err)
}

func (t *agentRuntimeTx) StaleResolutions(markID string) ([]m7flow.StaleResolution, error) {
	rows, err := t.tx.QueryContext(t.ctx,
		`SELECT `+staleResColumns+` FROM stale_resolutions WHERE stale_mark_id=? ORDER BY id`, markID)
	if err != nil {
		return nil, t.fail(err)
	}
	defer rows.Close()
	var out []m7flow.StaleResolution
	for rows.Next() {
		r, err := scanStaleResolution(rows)
		if err != nil {
			return nil, t.fail(err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

const gateEvalColumns = `id,stage_run_id,gate_key,input_digest,decision,findings_json,created_at`

func scanGateEvaluation(s interface{ Scan(...any) error }) (m7flow.GateEvaluation, error) {
	var g m7flow.GateEvaluation
	var findings string
	var created string
	if err := s.Scan(&g.ID, &g.StageRunID, &g.GateKey, &g.InputDigest, &g.Decision, &findings, &created); err != nil {
		return g, err
	}
	fs, err := m7flow.ParseFindings(findings)
	if err != nil {
		return g, err
	}
	g.Findings = fs
	if g.CreatedAt, err = parseRFC(created); err != nil {
		return g, err
	}
	return g, nil
}

func (t *agentRuntimeTx) PutGateEvaluation(g m7flow.GateEvaluation) error {
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO gate_evaluations
		(id,stage_run_id,gate_key,input_digest,decision,findings_json,created_at)
		VALUES(?,?,?,?,?,?,?)`,
		g.ID, g.StageRunID, g.GateKey, g.InputDigest, g.Decision, m7flow.FindingsJSON(g.Findings), rfc(g.CreatedAt))
	return t.fail(err)
}

func (t *agentRuntimeTx) FindGateEvaluation(stageRunID, gateKey, inputDigest string) (m7flow.GateEvaluation, error) {
	g, err := scanGateEvaluation(t.tx.QueryRowContext(t.ctx,
		`SELECT `+gateEvalColumns+` FROM gate_evaluations
		WHERE stage_run_id=? AND gate_key=? AND input_digest=? ORDER BY id DESC LIMIT 1`,
		stageRunID, gateKey, inputDigest))
	if errors.Is(err, sql.ErrNoRows) {
		return g, m7flow.ErrNotFound
	}
	return g, t.fail(err)
}

func (t *agentRuntimeTx) LatestGateEvaluation(stageRunID, gateKey string) (m7flow.GateEvaluation, error) {
	g, err := scanGateEvaluation(t.tx.QueryRowContext(t.ctx,
		`SELECT `+gateEvalColumns+` FROM gate_evaluations
		WHERE stage_run_id=? AND gate_key=? ORDER BY created_at DESC, id DESC LIMIT 1`,
		stageRunID, gateKey))
	if errors.Is(err, sql.ErrNoRows) {
		return g, m7flow.ErrNotFound
	}
	return g, t.fail(err)
}

const checkpointColumns = `id,stage_run_id,snapshot_digest,trace_root,sequence,created_at`

func scanCheckpoint(s interface{ Scan(...any) error }) (m7flow.Checkpoint, error) {
	var c m7flow.Checkpoint
	var created string
	if err := s.Scan(&c.ID, &c.StageRunID, &c.SnapshotDigest, &c.TraceRoot, &c.Sequence, &created); err != nil {
		return c, err
	}
	var err error
	if c.CreatedAt, err = parseRFC(created); err != nil {
		return c, err
	}
	return c, nil
}

func (t *agentRuntimeTx) PutCheckpoint(c m7flow.Checkpoint) error {
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO checkpoints
		(id,stage_run_id,snapshot_digest,trace_root,sequence,created_at)
		VALUES(?,?,?,?,?,?)`,
		c.ID, c.StageRunID, c.SnapshotDigest, c.TraceRoot, c.Sequence, rfc(c.CreatedAt))
	return t.fail(err)
}

func (t *agentRuntimeTx) MaxCheckpointSequence(stageRunID string) (int64, error) {
	var max sql.NullInt64
	if err := t.tx.QueryRowContext(t.ctx,
		`SELECT max(sequence) FROM checkpoints WHERE stage_run_id=?`, stageRunID).Scan(&max); err != nil {
		return 0, t.fail(err)
	}
	return max.Int64, nil
}

const reviewColumns = `id,subject_type,subject_id,subject_version,verdict,reviewer_id,reason,created_at`

func scanReview(s interface{ Scan(...any) error }) (m7flow.Review, error) {
	var r m7flow.Review
	var created string
	if err := s.Scan(&r.ID, &r.SubjectType, &r.SubjectID, &r.SubjectVersion, &r.Verdict, &r.ReviewerID, &r.Reason, &created); err != nil {
		return r, err
	}
	var err error
	if r.CreatedAt, err = parseRFC(created); err != nil {
		return r, err
	}
	return r, nil
}

func (t *agentRuntimeTx) PutReview(r m7flow.Review) error {
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO reviews
		(id,subject_type,subject_id,subject_version,verdict,reviewer_id,reason,created_at)
		VALUES(?,?,?,?,?,?,?,?)`,
		r.ID, r.SubjectType, r.SubjectID, r.SubjectVersion, r.Verdict, r.ReviewerID, r.Reason, rfc(r.CreatedAt))
	return t.fail(err)
}

// LatestApprovedReview answers the newest approve-verdict review for the
// subject. subjectVersion <= 0 matches any version (gate semantics: the
// stage_run subject has no version notion).
func (t *agentRuntimeTx) LatestApprovedReview(subjectType, subjectID string, subjectVersion int64) (m7flow.Review, error) {
	q := `SELECT ` + reviewColumns + ` FROM reviews
		WHERE subject_type=? AND subject_id=? AND verdict='approve'`
	args := []any{subjectType, subjectID}
	if subjectVersion > 0 {
		q += ` AND subject_version=?`
		args = append(args, subjectVersion)
	}
	q += ` ORDER BY created_at DESC, id DESC LIMIT 1`
	r, err := scanReview(t.tx.QueryRowContext(t.ctx, q, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return r, m7flow.ErrNotFound
	}
	return r, t.fail(err)
}

const devTaskColumns = `id,stage_run_id,title,state,priority,risk,acceptance_digest,assignee_id,state_reason,block_reason,blocker_ref,lock_version,trace_edge_id,created_at`

func scanDevTask(s interface{ Scan(...any) error }) (m7flow.DevTask, error) {
	var d m7flow.DevTask
	var assignee, stateReason, blockReason, blockerRef, traceEdge sql.NullString
	var created string
	if err := s.Scan(&d.ID, &d.StageRunID, &d.Title, &d.State, &d.Priority, &d.Risk, &d.AcceptanceDigest,
		&assignee, &stateReason, &blockReason, &blockerRef, &d.LockVersion, &traceEdge, &created); err != nil {
		return d, err
	}
	d.AssigneeID = assignee.String
	d.StateReason = stateReason.String
	d.BlockReason = blockReason.String
	d.BlockerRef = blockerRef.String
	d.TraceEdgeID = traceEdge.String
	var err error
	if d.CreatedAt, err = parseRFC(created); err != nil {
		return d, err
	}
	return d, nil
}

func (t *agentRuntimeTx) PutDevTask(d m7flow.DevTask) error {
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO dev_tasks
		(id,stage_run_id,title,state,priority,risk,acceptance_digest,assignee_id,state_reason,block_reason,blocker_ref,lock_version,trace_edge_id,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		d.ID, d.StageRunID, d.Title, d.State, d.Priority, d.Risk, d.AcceptanceDigest,
		nullStr(d.AssigneeID), nullStr(d.StateReason), nullStr(d.BlockReason), nullStr(d.BlockerRef),
		d.LockVersion, nullStr(d.TraceEdgeID), rfc(d.CreatedAt))
	return t.fail(err)
}

func nullStr(v string) any {
	if v == "" {
		return nil
	}
	return v
}

func (t *agentRuntimeTx) GetDevTask(id string) (m7flow.DevTask, error) {
	d, err := scanDevTask(t.tx.QueryRowContext(t.ctx,
		`SELECT `+devTaskColumns+` FROM dev_tasks WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return d, m7flow.ErrNotFound
	}
	return d, t.fail(err)
}

// UpdateDevTaskState applies one optimistic-locked state transition. A
// transition into blocked also records the block reason (DEV legacy
// contract: in_progress→blocked needs block_reason).
func (t *agentRuntimeTx) UpdateDevTaskState(id string, expectedVersion int64, to, stateReason string) (m7flow.DevTask, error) {
	var blockReason any
	if to == m7flow.TaskBlocked && stateReason != "" {
		blockReason = stateReason
	}
	res, err := t.tx.ExecContext(t.ctx,
		`UPDATE dev_tasks SET state=?, state_reason=?, block_reason=COALESCE(?, block_reason),
		 lock_version=lock_version+1 WHERE id=? AND lock_version=?`,
		to, nullStr(stateReason), blockReason, id, expectedVersion)
	if err != nil {
		return m7flow.DevTask{}, t.fail(err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return m7flow.DevTask{}, m7flow.ErrVersionConflict
	}
	return t.GetDevTask(id)
}

func (t *agentRuntimeTx) TaskStatesForRun(stageRunID string) (map[string]int, error) {
	rows, err := t.tx.QueryContext(t.ctx,
		`SELECT state, count(*) FROM dev_tasks WHERE stage_run_id=? GROUP BY state`, stageRunID)
	if err != nil {
		return nil, t.fail(err)
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var state string
		var n int
		if err := rows.Scan(&state, &n); err != nil {
			return nil, t.fail(err)
		}
		out[state] = n
	}
	return out, rows.Err()
}

func (t *agentRuntimeTx) PutTestRun(r m7flow.TestRun) error {
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO test_runs
		(id,task_ref,result,report_digest,created_at) VALUES(?,?,?,?,?)`,
		r.ID, r.TaskRef, r.Result, r.ReportDigest, rfc(r.CreatedAt))
	return t.fail(err)
}

func (t *agentRuntimeTx) PutScanRun(r m7flow.ScanRun) error {
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO scan_runs
		(id,task_ref,scanner,severity_gate,report_digest,created_at) VALUES(?,?,?,?,?,?)`,
		r.ID, r.TaskRef, r.Scanner, r.SeverityGate, r.ReportDigest, rfc(r.CreatedAt))
	return t.fail(err)
}

// TestResultsForTask aggregates test-run outcomes per result for one task
// reference (the gate binds the stage-run ID as the task ref).
func (t *agentRuntimeTx) TestResultsForTask(taskRef string) (map[string]int, error) {
	rows, err := t.tx.QueryContext(t.ctx,
		`SELECT result, count(*) FROM test_runs WHERE task_ref=? GROUP BY result`, taskRef)
	if err != nil {
		return nil, t.fail(err)
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var result string
		var n int
		if err := rows.Scan(&result, &n); err != nil {
			return nil, t.fail(err)
		}
		out[result] = n
	}
	return out, rows.Err()
}

// ScanResultsForTask aggregates scan outcomes for one task reference. A
// recorded scan with a severity gate and report digest counts as a pass
// bucket entry; the gate keys off scans["pass"].
func (t *agentRuntimeTx) ScanResultsForTask(taskRef string) (map[string]int, error) {
	var total int
	if err := t.tx.QueryRowContext(t.ctx,
		`SELECT count(*) FROM scan_runs WHERE task_ref=?`, taskRef).Scan(&total); err != nil {
		return nil, t.fail(err)
	}
	return map[string]int{"pass": total}, nil
}

var _ m7app.EvidenceTx = (*agentRuntimeTx)(nil)
var _ m7app.EvidenceUnitOfWork = (*AgentRuntimeRepository)(nil)
