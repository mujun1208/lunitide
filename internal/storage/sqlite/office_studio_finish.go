package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/lunitide/lunitide/internal/domain/officestudio"
	"github.com/oklog/ulid/v2"
)

var _ officestudio.OperationFinisher = (*Store)(nil)

func (s *Store) FinishOfficeTask(ctx context.Context, t officestudio.Task, expected int64, r officestudio.StepReceipt) (officestudio.Task, error) {
	r.Result = officeJSON(r.Result)
	if r.TaskID != t.ID || r.RunID != t.RunID || r.State != t.Status || !officestudio.ValidDigest(r.InputDigest) || !officeKey(r.StepKey) || !officeKey(r.IdempotencyKey) || !officestudio.ValidJSON(r.Result, 1<<20) {
		return t, officestudio.ErrInvalid
	}
	switch r.State {
	case "succeeded", "failed", "cancelled":
	default:
		return t, officestudio.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return t, err
	}
	defer tx.Rollback()
	if err = officeAuthorize(ctx, tx, "office-task", t.ID); err != nil {
		return t, err
	}
	old, err := scanOfficeTask(tx.QueryRowContext(ctx, `SELECT `+officeTaskColumns+` FROM office_tasks WHERE id=?`, t.ID))
	if err != nil {
		return t, err
	}
	if old.RunID != r.RunID || old.Revision != expected {
		return t, officestudio.ErrConflict
	}
	if !officestudio.ValidTransition(old.Status, t.Status) {
		return t, officestudio.ErrInvalid
	}
	var input, state, result string
	err = tx.QueryRowContext(ctx, `SELECT input_digest,state,result_json FROM office_step_receipts WHERE task_id=? AND run_id=? AND step_key=? AND idempotency_key=? AND state IN ('succeeded','failed','cancelled')`, r.TaskID, r.RunID, r.StepKey, r.IdempotencyKey).Scan(&input, &state, &result)
	if err == nil {
		if state != r.State || input != r.InputDigest || result != string(r.Result) {
			return t, officestudio.ErrConflict
		}
		if old.Status == t.Status {
			return old, nil
		}
		return t, officestudio.ErrConflict
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return t, err
	}
	status, checkpoint := t.Status, t.Checkpoint
	t = old
	t.Status = status
	t.Checkpoint = officeJSON(checkpoint)
	t.UpdatedAt = time.Now().UTC()
	t.Revision = expected + 1
	if err = t.Validate(); err != nil {
		return t, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE office_tasks SET status=?,checkpoint_json=?,revision=?,updated_at=? WHERE id=? AND revision=?`, t.Status, string(t.Checkpoint), t.Revision, officeRFC(t.UpdatedAt), t.ID, expected)
	if err != nil {
		return t, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO office_step_receipts(id,task_id,run_id,step_key,idempotency_key,input_digest,state,result_json,created_at) VALUES(?,?,?,?,?,?,?,?,?)`, ulid.Make().String(), r.TaskID, r.RunID, r.StepKey, r.IdempotencyKey, r.InputDigest, r.State, string(r.Result), officeRFC(t.UpdatedAt))
	if err != nil {
		return t, err
	}
	if err = appendOfficeEvent(ctx, tx, t.ID, "", "task.updated", map[string]any{"from": old.Status, "to": t.Status, "revision": t.Revision}, t.UpdatedAt); err != nil {
		return t, err
	}
	if err = appendOfficeEvent(ctx, tx, t.ID, "", "step."+r.State, map[string]any{"stepKey": r.StepKey, "runId": r.RunID}, t.UpdatedAt); err != nil {
		return t, err
	}
	return t, tx.Commit()
}
