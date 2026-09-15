package sqlite

import (
	"database/sql"

	"github.com/lunitide/lunitide/internal/agentrunapp"
	"github.com/lunitide/lunitide/internal/domain/agentrun"
)

func (t *agentRuntimeTx) PutExecutionStepOutcome(row agentrunapp.ExecutionStepOutcomeRow) error {
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO execution_step_outcomes(task_id,goal_revision,step_id,attempt_id,state,outcome_json,revision,created_at)
		VALUES(?,?,?,?,?,?,?,?)
		ON CONFLICT(task_id, goal_revision, step_id, attempt_id) DO UPDATE SET
			state=excluded.state,
			outcome_json=excluded.outcome_json,
			revision=excluded.revision`,
		row.TaskID, row.GoalRevision, row.StepID, row.AttemptID, row.State, row.OutcomeJSON, row.Revision, rfc(row.CreatedAt))
	return t.fail(err)
}

func (t *agentRuntimeTx) GetExecutionStepOutcome(taskID string, goalRevision int64, stepID, attemptID string) (agentrunapp.ExecutionStepOutcomeRow, error) {
	var row agentrunapp.ExecutionStepOutcomeRow
	var created string
	err := t.tx.QueryRowContext(t.ctx, `SELECT task_id,goal_revision,step_id,attempt_id,state,outcome_json,revision,created_at
		FROM execution_step_outcomes WHERE task_id=? AND goal_revision=? AND step_id=? AND attempt_id=?`,
		taskID, goalRevision, stepID, attemptID).
		Scan(&row.TaskID, &row.GoalRevision, &row.StepID, &row.AttemptID, &row.State, &row.OutcomeJSON, &row.Revision, &created)
	if err == sql.ErrNoRows {
		return row, agentrun.ErrNotFound
	}
	if err != nil {
		return row, t.fail(err)
	}
	if row.CreatedAt, err = parseRFC(created); err != nil {
		return row, t.fail(err)
	}
	return row, nil
}

func (t *agentRuntimeTx) ListExecutionStepOutcomes(taskID string) ([]agentrunapp.ExecutionStepOutcomeRow, error) {
	rows, err := t.tx.QueryContext(t.ctx, `SELECT task_id,goal_revision,step_id,attempt_id,state,outcome_json,revision,created_at
		FROM execution_step_outcomes WHERE task_id=? ORDER BY created_at, step_id, attempt_id`, taskID)
	if err != nil {
		return nil, t.fail(err)
	}
	defer rows.Close()
	var out []agentrunapp.ExecutionStepOutcomeRow
	for rows.Next() {
		var row agentrunapp.ExecutionStepOutcomeRow
		var created string
		if err = rows.Scan(&row.TaskID, &row.GoalRevision, &row.StepID, &row.AttemptID, &row.State, &row.OutcomeJSON, &row.Revision, &created); err != nil {
			return nil, t.fail(err)
		}
		if row.CreatedAt, err = parseRFC(created); err != nil {
			return nil, t.fail(err)
		}
		out = append(out, row)
	}
	return out, t.fail(rows.Err())
}
