package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/lunitide/lunitide/internal/modelfit"
)

var errToolOperationVersion = errors.New("tool operation version conflict")

func (s *Store) PutToolOperationIntent(ctx context.Context, op modelfit.ToolOperation) error {
	if op.ExpectedVersion < 1 {
		op.ExpectedVersion = 1
	}
	if op.Attempt < 1 {
		op.Attempt = 1
	}
	arts, err := json.Marshal(op.ArtifactRefs)
	if err != nil {
		return err
	}
	if string(arts) == "null" {
		arts = []byte("[]")
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO tool_operations(
		id,owner_scope,session_id,turn_id,automation_run_id,tool_name,tool_version,target_ref,input_digest,effect_class,state,expected_version,external_id,checkpoint_ref,evidence_ref,artifact_refs_json,attempt,budget_used,next_check_at,created_at,updated_at,cancellation_requested_at,error_kind)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		op.ID, op.OwnerScope, op.SessionID, op.TurnID, op.AutomationRunID, op.ToolName, op.ToolVersion, op.TargetRef, op.InputDigest, string(op.EffectClass), string(op.State), op.ExpectedVersion, op.ExternalID, op.CheckpointRef, op.EvidenceRef, string(arts), op.Attempt, op.BudgetUsed, op.NextCheckAt, formatTime(op.CreatedAt), formatTime(op.UpdatedAt), op.CancellationRequestedAt, op.ErrorKind)
	return mapWriteError(err)
}

func (s *Store) GetToolOperation(ctx context.Context, ownerScope, id string) (modelfit.ToolOperation, error) {
	return scanToolOperation(s.db.QueryRowContext(ctx, `SELECT id,owner_scope,session_id,turn_id,automation_run_id,tool_name,tool_version,target_ref,input_digest,effect_class,state,expected_version,external_id,checkpoint_ref,evidence_ref,artifact_refs_json,attempt,budget_used,next_check_at,created_at,updated_at,cancellation_requested_at,error_kind FROM tool_operations WHERE owner_scope=? AND id=?`, ownerScope, id))
}

func (s *Store) MarkToolOperationRunning(ctx context.Context, ownerScope, id string, version int, at time.Time) error {
	res, err := s.db.ExecContext(ctx, `UPDATE tool_operations SET state='running', expected_version=expected_version+1, updated_at=? WHERE owner_scope=? AND id=? AND expected_version=? AND state IN ('pending','running')`, formatTime(at), ownerScope, id, version)
	if err != nil {
		return mapWriteError(err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return errToolOperationVersion
	}
	return nil
}

func (s *Store) BindToolOperationTrack(ctx context.Context, ownerScope, id string, version int, externalID, evidenceRef string, artifactRefs []string, at time.Time) error {
	arts := ""
	if artifactRefs != nil {
		raw, err := json.Marshal(artifactRefs)
		if err != nil {
			return err
		}
		arts = string(raw)
	}
	res, err := s.db.ExecContext(ctx, `UPDATE tool_operations SET
		external_id=CASE WHEN ?!='' THEN ? ELSE external_id END,
		evidence_ref=CASE WHEN ?!='' THEN ? ELSE evidence_ref END,
		artifact_refs_json=CASE WHEN ?!='' THEN ? ELSE artifact_refs_json END,
		expected_version=expected_version+1, updated_at=?
		WHERE owner_scope=? AND id=? AND expected_version=? AND state<>'cancelled'`,
		externalID, externalID, evidenceRef, evidenceRef, arts, arts, formatTime(at), ownerScope, id, version)
	if err != nil {
		return mapWriteError(err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return errToolOperationVersion
	}
	return nil
}

func (s *Store) FinishToolOperation(ctx context.Context, ownerScope, id string, version int, state modelfit.OperationState, errorKind string, at time.Time) error {
	res, err := s.db.ExecContext(ctx, `UPDATE tool_operations SET state=?, error_kind=?, expected_version=expected_version+1, updated_at=? WHERE owner_scope=? AND id=? AND expected_version=? AND state<>'cancelled' AND cancellation_requested_at=''`, string(state), errorKind, formatTime(at), ownerScope, id, version)
	if err != nil {
		return mapWriteError(err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return errToolOperationVersion
	}
	return nil
}

func (s *Store) RequestToolOperationCancel(ctx context.Context, ownerScope, id string, version int, at time.Time) error {
	res, err := s.db.ExecContext(ctx, `UPDATE tool_operations SET cancellation_requested_at=?, expected_version=expected_version+1, updated_at=?, state=CASE WHEN state IN ('pending','running') THEN 'cancelled' ELSE state END WHERE owner_scope=? AND id=? AND expected_version=?`, formatTime(at), formatTime(at), ownerScope, id, version)
	if err != nil {
		return mapWriteError(err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return errToolOperationVersion
	}
	return nil
}

func (s *Store) RecoverInterruptedToolOperations(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `UPDATE tool_operations SET state='unknown', updated_at=? WHERE state IN ('pending','running')`, formatTime(time.Now().UTC()))
	return mapWriteError(err)
}

func scanToolOperation(row interface{ Scan(...any) error }) (modelfit.ToolOperation, error) {
	var op modelfit.ToolOperation
	var effect, state, arts, created, updated string
	err := row.Scan(&op.ID, &op.OwnerScope, &op.SessionID, &op.TurnID, &op.AutomationRunID, &op.ToolName, &op.ToolVersion, &op.TargetRef, &op.InputDigest, &effect, &state, &op.ExpectedVersion, &op.ExternalID, &op.CheckpointRef, &op.EvidenceRef, &arts, &op.Attempt, &op.BudgetUsed, &op.NextCheckAt, &created, &updated, &op.CancellationRequestedAt, &op.ErrorKind)
	if err != nil {
		return op, err
	}
	op.EffectClass = modelfit.EffectClass(effect)
	op.State = modelfit.OperationState(state)
	if arts != "" && arts != "[]" {
		if err := json.Unmarshal([]byte(arts), &op.ArtifactRefs); err != nil {
			return op, err
		}
	}
	if op.CreatedAt, err = time.Parse(time.RFC3339Nano, created); err != nil {
		return op, err
	}
	if op.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated); err != nil {
		return op, err
	}
	return op, nil
}

type CallAttemptRecord struct {
	ID                   string
	OwnerScope           string
	TaskID               string
	TurnID               string
	CallID               string
	ParentCallID         string
	AttemptID            string
	Purpose              string
	Provider             string
	DeploymentRef        string
	Model                string
	ProtocolRevision     string
	CredentialGeneration string
	PolicyVersion        string
	Status               string
	Integrity            string
	InputTokens          int
	OutputTokens         int
	CachedInputTokens    int
	CacheWriteTokens     int
	ProviderRequestID    string
	PriceRevision        string
	Currency             string
	CostStatus           string
	BytesBefore          int
	BytesAfter           int
	StartedAt            time.Time
	EndedAt              time.Time
}

type CallAttemptReceipt struct {
	Status            string
	Integrity         string
	InputTokens       int
	OutputTokens      int
	CachedInputTokens int
	CacheWriteTokens  int
	ProviderRequestID string
	CostStatus        string
	EndedAt           time.Time
}

type CallAttemptSum struct {
	Calls        int
	InputTokens  int
	OutputTokens int
	Integrity    string
}

func (s *Store) RecoverInterruptedCallAttempts(ctx context.Context) error {
	now := formatTime(time.Now().UTC())
	_, err := s.db.ExecContext(ctx, `UPDATE model_call_attempts
		SET status='unknown',
		    ended_at=CASE WHEN ended_at='' THEN ? ELSE ended_at END,
		    usage_integrity=CASE WHEN usage_integrity='' THEN 'unknown' ELSE usage_integrity END
		WHERE status IN ('intent','sent')`, now)
	return mapWriteError(err)
}

func (s *Store) PutCallAttemptIntent(ctx context.Context, rec CallAttemptRecord) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO model_call_attempts(
		id,owner_scope,task_id,turn_id,call_id,parent_call_id,attempt_id,purpose,provider,deployment_ref,model,protocol_revision,credential_generation,policy_version,status,started_at,ended_at,input_tokens,output_tokens,cached_input_tokens,cache_write_tokens,usage_integrity,provider_request_id,price_revision,currency,cost_status,bytes_before,bytes_after)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		rec.ID, rec.OwnerScope, rec.TaskID, rec.TurnID, rec.CallID, rec.ParentCallID, rec.AttemptID, rec.Purpose, rec.Provider, rec.DeploymentRef, rec.Model, rec.ProtocolRevision, rec.CredentialGeneration, rec.PolicyVersion, rec.Status, formatTime(rec.StartedAt), endedAtSQL(rec.EndedAt), rec.InputTokens, rec.OutputTokens, rec.CachedInputTokens, rec.CacheWriteTokens, rec.Integrity, rec.ProviderRequestID, rec.PriceRevision, rec.Currency, rec.CostStatus, rec.BytesBefore, rec.BytesAfter)
	return mapWriteError(err)
}

func (s *Store) MarkCallAttemptSent(ctx context.Context, ownerScope, callID, attemptID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE model_call_attempts SET status=? WHERE owner_scope=? AND call_id=? AND attempt_id=? AND status=?`,
		string(modelfit.CallSent), ownerScope, callID, attemptID, string(modelfit.CallIntent))
	return mapWriteError(err)
}

func (s *Store) FinishCallAttempt(ctx context.Context, ownerScope, callID, attemptID string, rec CallAttemptReceipt) error {
	_, err := s.db.ExecContext(ctx, `UPDATE model_call_attempts SET status=?, usage_integrity=?, input_tokens=?, output_tokens=?, cached_input_tokens=?, cache_write_tokens=?, provider_request_id=?, cost_status=?, ended_at=? WHERE owner_scope=? AND call_id=? AND attempt_id=? AND status IN ('intent','sent','unknown')`,
		rec.Status, rec.Integrity, rec.InputTokens, rec.OutputTokens, rec.CachedInputTokens, rec.CacheWriteTokens, rec.ProviderRequestID, rec.CostStatus, endedAtSQL(rec.EndedAt), ownerScope, callID, attemptID)
	return mapWriteError(err)
}

func (s *Store) GetCallAttempt(ctx context.Context, ownerScope, callID, attemptID string) (CallAttemptRecord, error) {
	return scanCallAttempt(s.db.QueryRowContext(ctx, `SELECT id,owner_scope,task_id,turn_id,call_id,parent_call_id,attempt_id,purpose,provider,deployment_ref,model,protocol_revision,credential_generation,policy_version,status,started_at,ended_at,input_tokens,output_tokens,cached_input_tokens,cache_write_tokens,usage_integrity,provider_request_id,price_revision,currency,cost_status,bytes_before,bytes_after FROM model_call_attempts WHERE owner_scope=? AND call_id=? AND attempt_id=?`, ownerScope, callID, attemptID))
}

func (s *Store) SumCallAttemptsByTask(ctx context.Context, ownerScope, taskID string) (CallAttemptSum, error) {
	var sum CallAttemptSum
	var integrity sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(SUM(input_tokens),0), COALESCE(SUM(output_tokens),0), CASE WHEN SUM(CASE WHEN usage_integrity='unknown' THEN 1 ELSE 0 END)>0 THEN 'unknown' WHEN SUM(CASE WHEN usage_integrity='partial' THEN 1 ELSE 0 END)>0 THEN 'partial' WHEN COUNT(*)=0 THEN 'unknown' ELSE 'reported' END FROM model_call_attempts WHERE owner_scope=? AND task_id=?`, ownerScope, taskID).Scan(&sum.Calls, &sum.InputTokens, &sum.OutputTokens, &integrity)
	if err != nil {
		return sum, err
	}
	if integrity.Valid {
		sum.Integrity = integrity.String
	}
	return sum, nil
}

func (s *Store) ListToolOperations(ctx context.Context, ownerScope string, limit int) ([]modelfit.ToolOperation, error) {
	if limit < 1 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,owner_scope,session_id,turn_id,automation_run_id,tool_name,tool_version,target_ref,input_digest,effect_class,state,expected_version,external_id,checkpoint_ref,evidence_ref,artifact_refs_json,attempt,budget_used,next_check_at,created_at,updated_at,cancellation_requested_at,error_kind FROM tool_operations WHERE owner_scope=? ORDER BY updated_at DESC LIMIT ?`, ownerScope, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []modelfit.ToolOperation
	for rows.Next() {
		op, scanErr := scanToolOperation(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, op)
	}
	return out, rows.Err()
}

func (s *Store) ListCallAttempts(ctx context.Context, ownerScope, taskID string, limit int) ([]CallAttemptRecord, error) {
	if limit < 1 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	q := `SELECT id,owner_scope,task_id,turn_id,call_id,parent_call_id,attempt_id,purpose,provider,deployment_ref,model,protocol_revision,credential_generation,policy_version,status,started_at,ended_at,input_tokens,output_tokens,cached_input_tokens,cache_write_tokens,usage_integrity,provider_request_id,price_revision,currency,cost_status,bytes_before,bytes_after FROM model_call_attempts WHERE owner_scope=?`
	args := []any{ownerScope}
	if taskID != "" {
		q += ` AND task_id=?`
		args = append(args, taskID)
	}
	q += ` ORDER BY started_at DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CallAttemptRecord
	for rows.Next() {
		rec, scanErr := scanCallAttempt(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

func (s *Store) SumCallAttemptsByOwner(ctx context.Context, ownerScope, taskID string) (CallAttemptSum, error) {
	q := `SELECT COUNT(*), COALESCE(SUM(input_tokens),0), COALESCE(SUM(output_tokens),0), CASE WHEN SUM(CASE WHEN usage_integrity='unknown' THEN 1 ELSE 0 END)>0 THEN 'unknown' WHEN SUM(CASE WHEN usage_integrity='partial' THEN 1 ELSE 0 END)>0 THEN 'partial' WHEN COUNT(*)=0 THEN 'unknown' ELSE 'reported' END FROM model_call_attempts WHERE owner_scope=?`
	args := []any{ownerScope}
	if taskID != "" {
		q += ` AND task_id=?`
		args = append(args, taskID)
	}
	var sum CallAttemptSum
	var integrity sql.NullString
	err := s.db.QueryRowContext(ctx, q, args...).Scan(&sum.Calls, &sum.InputTokens, &sum.OutputTokens, &integrity)
	if err != nil {
		return sum, err
	}
	if integrity.Valid {
		sum.Integrity = integrity.String
	}
	return sum, nil
}

func scanCallAttempt(row interface{ Scan(...any) error }) (CallAttemptRecord, error) {
	var rec CallAttemptRecord
	var started, ended string
	err := row.Scan(&rec.ID, &rec.OwnerScope, &rec.TaskID, &rec.TurnID, &rec.CallID, &rec.ParentCallID, &rec.AttemptID, &rec.Purpose, &rec.Provider, &rec.DeploymentRef, &rec.Model, &rec.ProtocolRevision, &rec.CredentialGeneration, &rec.PolicyVersion, &rec.Status, &started, &ended, &rec.InputTokens, &rec.OutputTokens, &rec.CachedInputTokens, &rec.CacheWriteTokens, &rec.Integrity, &rec.ProviderRequestID, &rec.PriceRevision, &rec.Currency, &rec.CostStatus, &rec.BytesBefore, &rec.BytesAfter)
	if err != nil {
		return rec, err
	}
	if rec.StartedAt, err = time.Parse(time.RFC3339Nano, started); err != nil {
		return rec, err
	}
	if ended != "" {
		if rec.EndedAt, err = time.Parse(time.RFC3339Nano, ended); err != nil {
			return rec, err
		}
	}
	return rec, nil
}

func endedAtSQL(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return formatTime(t)
}
