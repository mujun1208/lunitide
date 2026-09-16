package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/lunitide/lunitide/internal/agentrunapp"
	"github.com/lunitide/lunitide/internal/domain/agentrun"
	"github.com/lunitide/lunitide/internal/modelfit"
	"github.com/oklog/ulid/v2"
)

func (r *AgentRuntimeRepository) TransactExecutionBudget(ctx context.Context, fn func(agentrunapp.ExecutionBudgetTx) error) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Write first so SQLite takes the reserved lock before either Admit reads
	// the same remaining balance.
	if _, err = tx.ExecContext(ctx, `UPDATE execution_task_contracts SET revision=revision WHERE 0`); err != nil {
		return err
	}
	atx := &agentRuntimeTx{ctx: ctx, tx: tx}
	if err = fn(atx); err != nil {
		return err
	}
	if atx.failed != nil {
		return atx.failed
	}
	return tx.Commit()
}

func (t *agentRuntimeTx) SessionExists(sessionID string) error {
	var id string
	err := t.tx.QueryRowContext(t.ctx, `SELECT id FROM sessions WHERE id=?`, sessionID).Scan(&id)
	if err == sql.ErrNoRows {
		return fmt.Errorf("%w: session", agentrun.ErrNotFound)
	}
	return t.fail(err)
}

func (t *agentRuntimeTx) EnsureAccountingSession() (string, error) {
	const projectName = "execution-accounting"
	const projectCode = "ITM89001"
	var projectID string
	err := t.tx.QueryRowContext(t.ctx, `SELECT id FROM projects WHERE name=?`, projectName).Scan(&projectID)
	if err == sql.ErrNoRows {
		projectID = ulid.Make().String()
		now := rfc(time.Now().UTC())
		_, err = t.tx.ExecContext(t.ctx, `INSERT INTO projects(id,name,project_code,created_at,updated_at) VALUES(?,?,?,?,?)`,
			projectID, projectName, projectCode, now, now)
		if err != nil {
			return "", t.fail(err)
		}
	} else if err != nil {
		return "", t.fail(err)
	}
	_, err = t.tx.ExecContext(t.ctx, `INSERT OR IGNORE INTO message_project_usage(project_id,text_bytes) VALUES(?,0)`, projectID)
	if err != nil {
		return "", t.fail(err)
	}
	sessionID := ulid.Make().String()
	now := rfc(time.Now().UTC())
	_, err = t.tx.ExecContext(t.ctx, `INSERT INTO sessions(id,project_id,title,status,created_at,updated_at,version) VALUES(?,?,?,'active',?,?,1)`,
		sessionID, projectID, projectName, now, now)
	if err != nil {
		return "", t.fail(err)
	}
	_, err = t.tx.ExecContext(t.ctx, `INSERT INTO message_session_state(session_id,last_sequence,message_count,text_bytes) VALUES(?,0,0,0)`, sessionID)
	if err != nil {
		return "", t.fail(err)
	}
	return sessionID, nil
}

func (t *agentRuntimeTx) GetExecutionTask(taskID string) (agentrunapp.ExecutionTaskRow, error) {
	var row agentrunapp.ExecutionTaskRow
	var policyJSON, created, updated string
	err := t.tx.QueryRowContext(t.ctx, `SELECT task_id,owner_scope,session_id,goal_revision,spec_json,budget_policy_json,policy_revision,revision,outcome_json,active_elapsed_ms,active_since,last_heartbeat_at,runtime_epoch,activity_integrity,created_at,updated_at FROM execution_task_contracts WHERE task_id=?`, taskID).
		Scan(&row.TaskID, &row.OwnerScope, &row.SessionID, &row.GoalRevision, &row.SpecJSON, &policyJSON, &row.PolicyRevision, &row.Revision, &row.OutcomeJSON, &row.ActiveElapsedMS, &row.ActiveSince, &row.LastHeartbeatAt, &row.RuntimeEpoch, &row.ActivityIntegrity, &created, &updated)
	if err == sql.ErrNoRows {
		return row, agentrun.ErrNotFound
	}
	if err != nil {
		return row, t.fail(err)
	}
	if err = json.Unmarshal([]byte(policyJSON), &row.Policy); err != nil {
		return row, t.fail(err)
	}
	if row.CreatedAt, err = parseRFC(created); err != nil {
		return row, t.fail(err)
	}
	if row.UpdatedAt, err = parseRFC(updated); err != nil {
		return row, t.fail(err)
	}
	return row, nil
}

func (t *agentRuntimeTx) PutExecutionTask(row agentrunapp.ExecutionTaskRow) error {
	policyJSON, err := json.Marshal(row.Policy)
	if err != nil {
		return t.fail(err)
	}
	_, err = t.tx.ExecContext(t.ctx, `INSERT INTO execution_task_contracts(task_id,owner_scope,session_id,goal_revision,spec_json,budget_policy_json,policy_revision,revision,outcome_json,active_elapsed_ms,active_since,last_heartbeat_at,runtime_epoch,activity_integrity,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(task_id) DO UPDATE SET
			budget_policy_json=excluded.budget_policy_json,
			policy_revision=excluded.policy_revision,
			revision=excluded.revision,
			outcome_json=excluded.outcome_json,
			active_elapsed_ms=excluded.active_elapsed_ms,
			active_since=excluded.active_since,
			last_heartbeat_at=excluded.last_heartbeat_at,
			runtime_epoch=excluded.runtime_epoch,
			activity_integrity=excluded.activity_integrity,
			updated_at=excluded.updated_at`,
		row.TaskID, row.OwnerScope, row.SessionID, row.GoalRevision, row.SpecJSON, string(policyJSON), row.PolicyRevision, row.Revision, row.OutcomeJSON,
		row.ActiveElapsedMS, row.ActiveSince, row.LastHeartbeatAt, row.RuntimeEpoch, row.ActivityIntegrity, rfc(row.CreatedAt), rfc(row.UpdatedAt))
	return t.fail(err)
}

func (t *agentRuntimeTx) GetExecutionBinding(taskID, scopeID string) (agentrunapp.ExecutionBindingRow, error) {
	return t.scanBinding(t.tx.QueryRowContext(t.ctx, bindingSelect+` WHERE task_id=? AND scope_id=?`, taskID, scopeID))
}

func (t *agentRuntimeTx) GetExecutionBindingByRef(ownerScope, runKind, executionRef string) (agentrunapp.ExecutionBindingRow, error) {
	return t.scanBinding(t.tx.QueryRowContext(t.ctx, bindingSelect+` WHERE owner_scope=? AND run_kind=? AND execution_ref=?`, ownerScope, runKind, executionRef))
}

const bindingSelect = `SELECT run_id,owner_scope,task_id,scope_id,parent_scope_id,run_kind,execution_ref,execution_mode,scope_policy_json,scope_policy_revision,active_elapsed_ms,active_since,last_heartbeat_at,runtime_epoch,activity_integrity,created_at FROM execution_run_bindings`

func (t *agentRuntimeTx) scanBinding(s interface{ Scan(...any) error }) (agentrunapp.ExecutionBindingRow, error) {
	var row agentrunapp.ExecutionBindingRow
	var parent sql.NullString
	var policyJSON, created string
	err := s.Scan(&row.RunID, &row.OwnerScope, &row.TaskID, &row.ScopeID, &parent, &row.RunKind, &row.ExecutionRef, &row.ExecutionMode, &policyJSON, &row.PolicyRevision, &row.ActiveElapsedMS, &row.ActiveSince, &row.LastHeartbeatAt, &row.RuntimeEpoch, &row.ActivityIntegrity, &created)
	if err == sql.ErrNoRows {
		return row, agentrun.ErrNotFound
	}
	if err != nil {
		return row, t.fail(err)
	}
	if parent.Valid {
		row.ParentScopeID = parent.String
	}
	if err = json.Unmarshal([]byte(policyJSON), &row.Policy); err != nil {
		return row, t.fail(err)
	}
	if row.CreatedAt, err = parseRFC(created); err != nil {
		return row, t.fail(err)
	}
	return row, nil
}

func (t *agentRuntimeTx) PutExecutionBinding(row agentrunapp.ExecutionBindingRow) error {
	policyJSON, err := json.Marshal(row.Policy)
	if err != nil {
		return t.fail(err)
	}
	var parent any
	if row.ParentScopeID != "" {
		parent = row.ParentScopeID
	}
	_, err = t.tx.ExecContext(t.ctx, `INSERT INTO execution_run_bindings(run_id,owner_scope,task_id,scope_id,parent_scope_id,run_kind,execution_ref,execution_mode,scope_policy_json,scope_policy_revision,active_elapsed_ms,active_since,last_heartbeat_at,runtime_epoch,activity_integrity,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		row.RunID, row.OwnerScope, row.TaskID, row.ScopeID, parent, row.RunKind, row.ExecutionRef, row.ExecutionMode,
		string(policyJSON), row.PolicyRevision, row.ActiveElapsedMS, row.ActiveSince, row.LastHeartbeatAt, row.RuntimeEpoch, row.ActivityIntegrity, rfc(row.CreatedAt))
	return t.fail(err)
}

func (t *agentRuntimeTx) ActiveExecutionUsage(taskID, scopeID string) (agentrun.ExecutionUsageRollup, error) {
	q := `SELECT status, reserved_v2_json, settled_v2_json, isolated_json, integrity FROM run_usage_reservation WHERE task_id=? AND status IN ('reserved','committed','isolated')`
	args := []any{taskID}
	if scopeID != "" {
		q += ` AND scope_id=?`
		args = append(args, scopeID)
	}
	rows, err := t.tx.QueryContext(t.ctx, q, args...)
	if err != nil {
		return agentrun.ExecutionUsageRollup{}, t.fail(err)
	}
	defer rows.Close()
	var roll agentrun.ExecutionUsageRollup
	integrity := "reported"
	for rows.Next() {
		var status string
		var reservedRaw, settledRaw, isolatedRaw, integrityRaw sql.NullString
		if err = rows.Scan(&status, &reservedRaw, &settledRaw, &isolatedRaw, &integrityRaw); err != nil {
			return roll, t.fail(err)
		}
		roll.Attempts++
		var reserved struct {
			InputTokensUpper int64 `json:"inputTokensUpper"`
			OutputTokenCap   int64 `json:"outputTokenCap"`
			OutputBytesCap   int64 `json:"outputBytesCap"`
		}
		var settled struct {
			InputTokens      int64 `json:"inputTokens"`
			OutputTokens     int64 `json:"outputTokens"`
			ModelOutputBytes int64 `json:"modelOutputBytes"`
		}
		if reservedRaw.Valid && reservedRaw.String != "" {
			_ = json.Unmarshal([]byte(reservedRaw.String), &reserved)
		}
		if settledRaw.Valid && settledRaw.String != "" {
			_ = json.Unmarshal([]byte(settledRaw.String), &settled)
		}
		switch status {
		case "reserved":
			roll.ReservedTotal += reserved.InputTokensUpper + reserved.OutputTokenCap
			roll.ReservedOutputBytes += reserved.OutputBytesCap
		case "committed":
			roll.ConsumedTotal += settled.InputTokens + settled.OutputTokens
			roll.ConsumedOutput += settled.OutputTokens
			roll.ConsumedOutputBytes += settled.ModelOutputBytes
		case "isolated":
			known := settled.InputTokens + settled.OutputTokens
			cap := reserved.InputTokensUpper + reserved.OutputTokenCap
			if known > cap {
				roll.ConsumedTotal += known
				roll.ConsumedOutput += settled.OutputTokens
				roll.Overrun = true
			} else {
				roll.ConsumedTotal += known
				roll.ConsumedOutput += settled.OutputTokens
				roll.IsolatedTotal += cap - known
			}
			if settled.ModelOutputBytes > reserved.OutputBytesCap {
				roll.ConsumedOutputBytes += settled.ModelOutputBytes
			} else {
				roll.ConsumedOutputBytes += settled.ModelOutputBytes
				roll.IsolatedOutputBytes += reserved.OutputBytesCap - settled.ModelOutputBytes
			}
		}
		if integrityRaw.Valid && (integrityRaw.String == "unknown" || integrityRaw.String == "partial" || integrityRaw.String == "overrun" || integrityRaw.String == "receipt_conflict") {
			integrity = integrityRaw.String
			if integrityRaw.String == "overrun" {
				roll.Overrun = true
			}
		}
	}
	roll.Integrity = integrity
	return roll, t.fail(rows.Err())
}

func (t *agentRuntimeTx) InsertExecutionReservation(row agentrunapp.ExecutionReservationRow) error {
	reservedV2, err := json.Marshal(row.Reserved)
	if err != nil {
		return t.fail(err)
	}
	dispatched := 0
	if row.Dispatched {
		dispatched = 1
	}
	_, err = t.tx.ExecContext(t.ctx, `INSERT INTO run_usage_reservation(
		id,run_id,reserved_json,committed_json,status,created_at,updated_at,
		task_id,scope_id,attempt_id,request_digest,dispatched,integrity,reserved_v2_json)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		row.ID, row.RunID, row.ReservedJSON, nilIfEmpty(row.CommittedJSON), row.Status, rfc(row.CreatedAt), rfc(row.UpdatedAt),
		row.TaskID, row.ScopeID, row.AttemptID, row.RequestDigest, dispatched, nilIfEmpty(row.Integrity), string(reservedV2))
	return t.fail(err)
}

func (t *agentRuntimeTx) LoadExecutionReservation(id string) (agentrunapp.ExecutionReservationRow, error) {
	var row agentrunapp.ExecutionReservationRow
	var committed, integrity, reservedV2, settledV2, receipt, digest, isolated sql.NullString
	var dispatched int
	var revision sql.NullInt64
	var created, updated string
	err := t.tx.QueryRowContext(t.ctx, `SELECT id,run_id,reserved_json,committed_json,status,created_at,updated_at,task_id,scope_id,attempt_id,request_digest,dispatched,integrity,reserved_v2_json,settled_v2_json,receipt_id,settlement_revision,settlement_digest,isolated_json
		FROM run_usage_reservation WHERE id=?`, id).Scan(
		&row.ID, &row.RunID, &row.ReservedJSON, &committed, &row.Status, &created, &updated,
		&row.TaskID, &row.ScopeID, &row.AttemptID, &row.RequestDigest, &dispatched, &integrity, &reservedV2, &settledV2, &receipt, &revision, &digest, &isolated)
	if err == sql.ErrNoRows {
		return row, agentrun.ErrExecutionReservation
	}
	if err != nil {
		return row, t.fail(err)
	}
	row.Dispatched = dispatched == 1
	row.CommittedJSON = committed.String
	row.Integrity = integrity.String
	row.ReceiptID = receipt.String
	row.SettlementRevision = revision.Int64
	row.SettlementDigest = digest.String
	row.IsolatedJSON = isolated.String
	if reservedV2.Valid {
		_ = json.Unmarshal([]byte(reservedV2.String), &row.Reserved)
	}
	if settledV2.Valid {
		_ = json.Unmarshal([]byte(settledV2.String), &row.Settled)
	}
	if row.CreatedAt, err = parseRFC(created); err != nil {
		return row, t.fail(err)
	}
	if row.UpdatedAt, err = parseRFC(updated); err != nil {
		return row, t.fail(err)
	}
	return row, nil
}

func (t *agentRuntimeTx) UpdateExecutionReservation(row agentrunapp.ExecutionReservationRow) error {
	reservedV2, err := json.Marshal(row.Reserved)
	if err != nil {
		return t.fail(err)
	}
	settledV2, err := json.Marshal(row.Settled)
	if err != nil {
		return t.fail(err)
	}
	dispatched := 0
	if row.Dispatched {
		dispatched = 1
	}
	var revision any
	if row.SettlementRevision > 0 {
		revision = row.SettlementRevision
	}
	res, err := t.tx.ExecContext(t.ctx, `UPDATE run_usage_reservation SET
		committed_json=?, status=?, updated_at=?, dispatched=?, integrity=?,
		reserved_v2_json=?, settled_v2_json=?, receipt_id=?, settlement_revision=?, settlement_digest=?, isolated_json=?
		WHERE id=?`,
		nilIfEmpty(row.CommittedJSON), row.Status, rfc(row.UpdatedAt), dispatched, nilIfEmpty(row.Integrity),
		string(reservedV2), string(settledV2), nilIfEmpty(row.ReceiptID), revision, nilIfEmpty(row.SettlementDigest), nilIfEmpty(row.IsolatedJSON),
		row.ID)
	if err != nil {
		return t.fail(err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return t.fail(agentrun.ErrExecutionReservation)
	}
	return nil
}

func (t *agentRuntimeTx) InsertCallAttemptIntent(row agentrunapp.CallAttemptIntentRow) error {
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO model_call_attempts(
		id,owner_scope,task_id,turn_id,call_id,parent_call_id,attempt_id,purpose,provider,deployment_ref,model,protocol_revision,credential_generation,policy_version,status,started_at,ended_at,input_tokens,output_tokens,cached_input_tokens,cache_write_tokens,usage_integrity,provider_request_id,price_revision,currency,cost_status,bytes_before,bytes_after)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		row.ID, row.OwnerScope, row.TaskID, "", row.CallID, "", row.AttemptID, row.Purpose, "", "", "", "", "", "",
		string(modelfit.CallIntent), rfc(row.StartedAt), "", 0, 0, 0, 0, "unknown", "", "", "", "unknown", 0, 0)
	return t.fail(err)
}

func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

var (
	_ agentrunapp.ExecutionBudgetUoW = (*AgentRuntimeRepository)(nil)
	_ agentrunapp.ExecutionBudgetTx  = (*agentRuntimeTx)(nil)
)
