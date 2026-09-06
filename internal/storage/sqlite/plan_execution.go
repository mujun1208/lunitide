package sqlite

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/lunitide/lunitide/internal/agentorchestration"
	"github.com/lunitide/lunitide/internal/domain/agentrun"
	"github.com/lunitide/lunitide/internal/domain/planning"
	"github.com/lunitide/lunitide/internal/planningapp"
	"github.com/lunitide/lunitide/internal/sessionapp"
	"github.com/oklog/ulid/v2"
)

const planExecutionColumns = `plan_run_id,agent_run_id,session_id,spec_json,status,summary,failure,artifacts_json,version,created_at,updated_at`

func scanPlanExecution(row interface{ Scan(...any) error }) (out agentrun.PlanExecution, err error) {
	var spec, artifacts, created, updated string
	err = row.Scan(&out.PlanRunID, &out.RunID, &out.SessionID, &spec, &out.Status, &out.Summary, &out.Failure, &artifacts, &out.Version, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return out, agentrun.ErrNotFound
	}
	if err != nil {
		return
	}
	if err = json.Unmarshal([]byte(spec), &out.Spec); err != nil {
		return
	}
	if err = json.Unmarshal([]byte(artifacts), &out.Artifacts); err != nil {
		return
	}
	if out.CreatedAt, err = parseRFC(created); err != nil {
		return
	}
	out.UpdatedAt, err = parseRFC(updated)
	return
}

func (t *agentRuntimeTx) GetPlanExecution(id string) (agentrun.PlanExecution, error) {
	return scanPlanExecution(t.tx.QueryRowContext(t.ctx, `SELECT `+planExecutionColumns+` FROM plan_run_executions WHERE plan_run_id=?`, id))
}

func (t *agentRuntimeTx) ListPlanExecutions() ([]agentrun.PlanExecution, error) {
	rows, err := t.tx.QueryContext(t.ctx, `SELECT `+planExecutionColumns+` FROM plan_run_executions WHERE status IN ('running','cancel_requested') ORDER BY created_at,plan_run_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []agentrun.PlanExecution
	for rows.Next() {
		v, e := scanPlanExecution(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (t *agentRuntimeTx) LoadPlanExecutionSpec(id string) (out agentrun.PlanExecutionSpec, err error) {
	err = t.tx.QueryRowContext(t.ctx, `SELECT p.project_id,r.plan_id,r.node_id,r.role,r.todo_title,r.todo_description FROM agent_plan_runs r JOIN plans p ON p.id=r.plan_id JOIN plan_nodes n ON n.id=r.node_id AND n.plan_id=p.id WHERE r.id=?`, id).Scan(&out.ProjectID, &out.PlanID, &out.NodeID, &out.Role, &out.Title, &out.Description)
	if errors.Is(err, sql.ErrNoRows) {
		err = agentrun.ErrNotFound
	}
	return
}

func (t *agentRuntimeTx) CheckPlanExecutionReady(id string, at time.Time) error {
	var planID, nodeID, planStatus, nodeStatus, projectStatus, risk, runStatus string
	var parent sql.NullString
	err := t.tx.QueryRowContext(t.ctx, `SELECT p.id,n.id,p.status,n.status,j.status,n.risk_level,n.parent_node_id,r.status FROM agent_plan_runs r JOIN plans p ON p.id=r.plan_id JOIN plan_nodes n ON n.id=r.node_id AND n.plan_id=p.id JOIN projects j ON j.id=p.project_id WHERE r.id=?`, id).Scan(&planID, &nodeID, &planStatus, &nodeStatus, &projectStatus, &risk, &parent, &runStatus)
	if errors.Is(err, sql.ErrNoRows) {
		return agentrun.ErrNotFound
	}
	if err != nil {
		return err
	}
	if planStatus != "active" {
		return planningapp.ErrPlanNotActive
	}
	if projectStatus == "closed" || projectStatus == "archived" || projectStatus == "suspended" {
		return planningapp.ErrInvalidTransition
	}
	if nodeStatus != "pending" && nodeStatus != "ready" && nodeStatus != "running" {
		return planningapp.ErrNodeNotReady
	}
	if runStatus != "queued" && runStatus != "running" && runStatus != "joining" {
		return agentorchestration.ErrInvalidTransition
	}
	if parent.Valid {
		var valid int
		if err = t.tx.QueryRowContext(t.ctx, `SELECT count(*) FROM plan_nodes WHERE id=? AND plan_id=? AND status='completed'`, parent.String, planID).Scan(&valid); err != nil {
			return err
		}
		if valid != 1 {
			return planningapp.ErrDependencyNotMet
		}
	}
	if risk == "high" || risk == "critical" {
		_, _, digest, err := t.planExecutionReviewData(id)
		if err != nil {
			return err
		}
		var approved int
		if err = t.tx.QueryRowContext(t.ctx, `SELECT count(*) FROM governance_reviews WHERE plan_id=? AND node_id=? AND action_type='plan.run.start' AND input_digest=? AND policy_version=1 AND risk_level=? AND status='approved' AND julianday(expires_at)>julianday(?)`, planID, nodeID, digest, risk, rfc(at)).Scan(&approved); err != nil {
			return err
		}
		if approved == 0 {
			return planningapp.ErrReviewRequired
		}
	}
	return nil
}

func (t *agentRuntimeTx) planExecutionReviewData(id string) (agentrun.PlanExecutionSpec, string, string, error) {
	spec, err := t.LoadPlanExecutionSpec(id)
	if err != nil {
		return spec, "", "", err
	}
	var risk string
	if err = t.tx.QueryRowContext(t.ctx, `SELECT risk_level FROM plan_nodes WHERE id=? AND plan_id=?`, spec.NodeID, spec.PlanID).Scan(&risk); err != nil {
		return spec, risk, "", err
	}
	body, err := json.Marshal(struct {
		TaskID string
		Spec   agentrun.PlanExecutionSpec
		Risk   string
		Policy string
	}{id, spec, risk, "plan-executor-v1/session-workspace-and-allowlisted-command"})
	if err != nil {
		return spec, risk, "", err
	}
	sum := sha256.Sum256(body)
	return spec, risk, hex.EncodeToString(sum[:]), nil
}

func (t *agentRuntimeTx) PreparePlanExecutionReview(id string, at time.Time) (bool, error) {
	spec, risk, digest, err := t.planExecutionReviewData(id)
	if err != nil {
		return false, err
	}
	if risk != "high" && risk != "critical" {
		return true, nil
	}
	var status string
	err = t.tx.QueryRowContext(t.ctx, `SELECT status FROM governance_reviews WHERE plan_id=? AND node_id=? AND action_type='plan.run.start' AND input_digest=? AND policy_version=1 AND risk_level=? AND status IN ('pending','approved') AND julianday(expires_at)>julianday(?) ORDER BY created_at DESC LIMIT 1`, spec.PlanID, spec.NodeID, digest, risk, rfc(at)).Scan(&status)
	if err == nil {
		return status == "approved", nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	reviewID := ulid.Make().String()
	_, err = t.tx.ExecContext(t.ctx, `INSERT INTO governance_reviews(id,plan_id,node_id,action_type,action_digest,input_digest,state_digest,policy_version,risk_level,status,reviewer_note,expires_at,created_at) VALUES(?,?,?,'plan.run.start',?,?,?,1,?,'pending',?,?,?)`, reviewID, spec.PlanID, spec.NodeID, digest, digest, digest, risk, "计划任务："+spec.Title, rfc(at.Add(30*time.Minute)), rfc(at))
	if err != nil {
		return false, t.fail(err)
	}
	s := &Store{idEntropy: rand.Reader}
	err = s.appendAuditTx(t.ctx, t.tx, "review.created", reviewID, "engine", map[string]any{"actionType": "plan.run.start", "planRunId": id, "inputDigest": digest, "riskLevel": risk})
	return false, err
}

func (t *agentRuntimeTx) CreatePlanExecutionSession(id, projectID, title string, at time.Time) error {
	var count int
	if err := t.tx.QueryRowContext(t.ctx, `SELECT count(*) FROM sessions WHERE project_id=?`, projectID).Scan(&count); err != nil {
		return err
	}
	if count >= 100 {
		return sessionapp.ErrSessionCapacityReached
	}
	if len([]rune(title)) > 200 {
		title = string([]rune(title)[:200])
	}
	if _, err := t.tx.ExecContext(t.ctx, `INSERT INTO sessions(id,project_id,title,status,created_at,updated_at,version) VALUES(?,?,?,'active',?,?,1)`, id, projectID, title, rfc(at), rfc(at)); err != nil {
		return t.fail(err)
	}
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO message_session_state(session_id,last_sequence,message_count,text_bytes) VALUES(?,0,0,0)`, id)
	return t.fail(err)
}

func (t *agentRuntimeTx) PutPlanExecution(ex agentrun.PlanExecution, expected int64) error {
	spec, err := json.Marshal(ex.Spec)
	if err != nil {
		return err
	}
	artifacts, err := json.Marshal(ex.Artifacts)
	if err != nil {
		return err
	}
	if ex.Artifacts == nil {
		artifacts = []byte("[]")
	}
	if expected == 0 {
		_, err = t.tx.ExecContext(t.ctx, `INSERT INTO plan_run_executions(`+planExecutionColumns+`) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, ex.PlanRunID, ex.RunID, ex.SessionID, string(spec), ex.Status, ex.Summary, ex.Failure, string(artifacts), ex.Version, rfc(ex.CreatedAt), rfc(ex.UpdatedAt))
	} else {
		var result sql.Result
		result, err = t.tx.ExecContext(t.ctx, `UPDATE plan_run_executions SET agent_run_id=?,session_id=?,spec_json=?,status=?,summary=?,failure=?,artifacts_json=?,version=?,created_at=?,updated_at=? WHERE plan_run_id=? AND version=?`, ex.RunID, ex.SessionID, string(spec), ex.Status, ex.Summary, ex.Failure, string(artifacts), ex.Version, rfc(ex.CreatedAt), rfc(ex.UpdatedAt), ex.PlanRunID, expected)
		if err == nil {
			var n int64
			n, err = result.RowsAffected()
			if err == nil && n != 1 {
				err = agentrun.ErrVersionConflict
			}
		}
	}
	if err != nil {
		return t.fail(err)
	}
	var from string
	if err = t.tx.QueryRowContext(t.ctx, `SELECT status FROM agent_plan_runs WHERE id=?`, ex.PlanRunID).Scan(&from); err != nil {
		return err
	}
	status := ex.Status
	if status == "interrupted" || status == "outcome_unknown" {
		status = "failed"
	}
	var terminal any
	if ex.Terminal() {
		terminal = rfc(ex.UpdatedAt)
	}
	if _, err = t.tx.ExecContext(t.ctx, `UPDATE agent_plan_runs SET status=?,failure=?,terminal_at=?,updated_at=?,version=version+1,todo_metadata_json=json_set(CASE WHEN json_type(todo_metadata_json)='object' THEN todo_metadata_json ELSE '{}' END,'$.agentRunId',?,'$.executionStatus',?) WHERE id=?`, status, ex.Failure, terminal, rfc(ex.UpdatedAt), ex.RunID, ex.Status, ex.PlanRunID); err != nil {
		return t.fail(err)
	}
	if _, err = t.tx.ExecContext(t.ctx, `INSERT INTO agent_plan_run_events(run_id,type,from_status,to_status,detail,created_at) VALUES(?,'status_changed',?,?,?,?)`, ex.PlanRunID, from, status, ex.Failure, rfc(ex.UpdatedAt)); err != nil {
		return t.fail(err)
	}
	store := &Store{idEntropy: rand.Reader}
	if ex.Status == "running" {
		return store.transitionNodeTx(t.ctx, t.tx, ex.Spec.NodeID, planning.NodeStatusRunning)
	}
	if !ex.Terminal() {
		return nil
	}
	var total, active, failed, cancelled int
	if err = t.tx.QueryRowContext(t.ctx, `SELECT count(*),COALESCE(sum(status NOT IN ('succeeded','failed','cancelled','timed_out')),0),COALESCE(sum(status IN ('failed','timed_out')),0),COALESCE(sum(status='cancelled'),0) FROM agent_plan_runs WHERE plan_id=? AND node_id=?`, ex.Spec.PlanID, ex.Spec.NodeID).Scan(&total, &active, &failed, &cancelled); err != nil {
		return err
	}
	if total == 0 || active != 0 {
		return nil
	}
	target := planning.NodeStatusCompleted
	if failed > 0 {
		target = planning.NodeStatusFailed
	} else if cancelled > 0 {
		target = planning.NodeStatusCancelled
	}
	return store.transitionNodeTx(t.ctx, t.tx, ex.Spec.NodeID, target)
}

// ReopenPlanExecution is only used by the explicit, versioned retry claim.
// Existing runtime records stay terminal and their evidence is never rewritten.
func (t *agentRuntimeTx) ReopenPlanExecution(ex agentrun.PlanExecution) error {
	var planStatus, nodeStatus, projectStatus string
	if err := t.tx.QueryRowContext(t.ctx, `SELECT p.status,n.status,j.status FROM plans p JOIN plan_nodes n ON n.plan_id=p.id JOIN projects j ON j.id=p.project_id WHERE p.id=? AND n.id=?`, ex.Spec.PlanID, ex.Spec.NodeID).Scan(&planStatus, &nodeStatus, &projectStatus); err != nil {
		return err
	}
	if projectStatus == "closed" || projectStatus == "archived" || planStatus == "paused" || planStatus == "completed" || nodeStatus == "completed" {
		return planningapp.ErrInvalidTransition
	}
	if nodeStatus != "failed" && nodeStatus != "cancelled" && nodeStatus != "running" {
		return planningapp.ErrNodeNotReady
	}
	if _, err := t.tx.ExecContext(t.ctx, `UPDATE plans SET status='active',version=version+1,updated_at=? WHERE id=?`, rfc(ex.UpdatedAt), ex.Spec.PlanID); err != nil {
		return t.fail(err)
	}
	if _, err := t.tx.ExecContext(t.ctx, `UPDATE plan_nodes SET status='pending',updated_at=? WHERE id=?`, rfc(ex.UpdatedAt), ex.Spec.NodeID); err != nil {
		return t.fail(err)
	}
	_, err := t.tx.ExecContext(t.ctx, `UPDATE agent_plan_runs SET status='queued',terminal_at=NULL WHERE id=?`, ex.PlanRunID)
	return t.fail(err)
}

var _ agentrun.PlanExecutionTx = (*agentRuntimeTx)(nil)
