// Legacy S5 telemetry persistence (migration 0053): append-only inserts
// and window queries over m6_health_sample / m6_call_log. The tables'
// BEFORE UPDATE/DELETE triggers (M6-APPENDONLY) make the insert path the
// only write path; repository methods never issue UPDATE or DELETE.
package sqlite

import (
	"database/sql"
	"errors"
	"time"

	"github.com/lunitide/lunitide/internal/domain/m6supply"
)

// ── HealthSample ────────────────────────────────────────────────────────────

func (t *agentRuntimeTx) PutM6HealthSample(s m6supply.HealthSample) error {
	var codeClass any
	if s.CodeClass != "" {
		codeClass = s.CodeClass
	}
	success := 0
	if s.Success {
		success = 1
	}
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO m6_health_sample
		(id,integration_id,status,success,latency_ms,code_class,sampled_at)
		VALUES(?,?,?,?,?,?,?)`,
		s.ID, s.IntegrationID, s.Status, success, s.LatencyMS, codeClass, rfc(s.SampledAt))
	return t.fail(err)
}

// ListM6HealthSamples returns the trailing-window samples for one
// integration, oldest first (the aggregation order).
func (t *agentRuntimeTx) ListM6HealthSamples(integrationID string, since time.Time, limit int) ([]m6supply.HealthSample, error) {
	rows, err := t.tx.QueryContext(t.ctx,
		`SELECT id,integration_id,status,success,latency_ms,code_class,sampled_at
		 FROM m6_health_sample WHERE integration_id=? AND sampled_at>=? ORDER BY sampled_at LIMIT ?`,
		integrationID, rfc(since), limit)
	if err != nil {
		return nil, t.fail(err)
	}
	defer rows.Close()
	var out []m6supply.HealthSample
	for rows.Next() {
		var s m6supply.HealthSample
		var codeClass sql.NullString
		var success int
		var sampled string
		if err := rows.Scan(&s.ID, &s.IntegrationID, &s.Status, &success, &s.LatencyMS, &codeClass, &sampled); err != nil {
			return nil, t.fail(err)
		}
		s.Success = success == 1
		s.CodeClass = codeClass.String
		if s.SampledAt, err = parseRFC(sampled); err != nil {
			return nil, t.fail(err)
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// ── CallLog ─────────────────────────────────────────────────────────────────

const m6CallLogColumns = `id,integration_id,operation_id,trace_id,actor_id,subject_id,environment,grant_id,attempt,started_at,completed_at,request_bytes,response_bytes,status_class,request_digest,response_digest,outcome,error_code,latency_ms,cost_micros,retry_of_call_id,correction_of_call_id,idempotency_key_digest,policy_decision_id,created_at`

func scanM6CallLog(s interface{ Scan(...any) error }) (m6supply.CallLog, error) {
	var c m6supply.CallLog
	var traceID, actorID, subjectID, grantID sql.NullString
	var completed sql.NullString
	var requestBytes, responseBytes sql.NullInt64
	var statusClass sql.NullString
	var requestDigest, responseDigest sql.NullString
	var errorCode sql.NullString
	var latencyMS, costMicros sql.NullInt64
	var retryOf, correctionOf sql.NullString
	var idemDigest sql.NullString
	var policyDecisionID sql.NullString
	var startedAt, createdAt string
	if err := s.Scan(&c.ID, &c.IntegrationID, &c.OperationID, &traceID, &actorID, &subjectID, &c.Environment, &grantID,
		&c.Attempt, &startedAt, &completed, &requestBytes, &responseBytes, &statusClass,
		&requestDigest, &responseDigest, &c.Outcome, &errorCode, &latencyMS, &costMicros,
		&retryOf, &correctionOf, &idemDigest, &policyDecisionID, &createdAt); err != nil {
		return c, err
	}
	c.TraceID = traceID.String
	c.ActorID = actorID.String
	c.SubjectID = subjectID.String
	c.GrantID = grantID.String
	if completed.Valid {
		t, err := parseRFC(completed.String)
		if err != nil {
			return c, err
		}
		c.CompletedAt = &t
	}
	if requestBytes.Valid {
		v := requestBytes.Int64
		c.RequestBytes = &v
	}
	if responseBytes.Valid {
		v := responseBytes.Int64
		c.ResponseBytes = &v
	}
	c.StatusClass = statusClass.String
	c.RequestDigest = requestDigest.String
	c.ResponseDigest = responseDigest.String
	c.ErrorCode = errorCode.String
	if latencyMS.Valid {
		v := latencyMS.Int64
		c.LatencyMS = &v
	}
	if costMicros.Valid {
		v := costMicros.Int64
		c.CostMicros = &v
	}
	c.RetryOfCallID = retryOf.String
	c.CorrectionOfCallID = correctionOf.String
	c.IdempotencyKeyDigest = idemDigest.String
	c.PolicyDecisionID = policyDecisionID.String
	var err error
	if c.StartedAt, err = parseRFC(startedAt); err != nil {
		return c, err
	}
	if c.CreatedAt, err = parseRFC(createdAt); err != nil {
		return c, err
	}
	return c, nil
}

func (t *agentRuntimeTx) PutM6CallLog(c m6supply.CallLog) error {
	var traceID, actorID, subjectID, grantID any
	if c.TraceID != "" {
		traceID = c.TraceID
	}
	if c.ActorID != "" {
		actorID = c.ActorID
	}
	if c.SubjectID != "" {
		subjectID = c.SubjectID
	}
	if c.GrantID != "" {
		grantID = c.GrantID
	}
	var completed any
	if c.CompletedAt != nil {
		completed = rfc(*c.CompletedAt)
	}
	var requestBytes, responseBytes any
	if c.RequestBytes != nil {
		requestBytes = *c.RequestBytes
	}
	if c.ResponseBytes != nil {
		responseBytes = *c.ResponseBytes
	}
	var statusClass any
	if c.StatusClass != "" {
		statusClass = c.StatusClass
	}
	var requestDigest, responseDigest any
	if c.RequestDigest != "" {
		requestDigest = c.RequestDigest
	}
	if c.ResponseDigest != "" {
		responseDigest = c.ResponseDigest
	}
	var errorCode any
	if c.ErrorCode != "" {
		errorCode = c.ErrorCode
	}
	var latencyMS, costMicros any
	if c.LatencyMS != nil {
		latencyMS = *c.LatencyMS
	}
	if c.CostMicros != nil {
		costMicros = *c.CostMicros
	}
	var retryOf, correctionOf any
	if c.RetryOfCallID != "" {
		retryOf = c.RetryOfCallID
	}
	if c.CorrectionOfCallID != "" {
		correctionOf = c.CorrectionOfCallID
	}
	var idemDigest any
	if c.IdempotencyKeyDigest != "" {
		idemDigest = c.IdempotencyKeyDigest
	}
	var policyDecisionID any
	if c.PolicyDecisionID != "" {
		policyDecisionID = c.PolicyDecisionID
	}
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO m6_call_log
		(id,integration_id,operation_id,trace_id,actor_id,subject_id,environment,grant_id,attempt,started_at,completed_at,request_bytes,response_bytes,status_class,request_digest,response_digest,outcome,error_code,latency_ms,cost_micros,retry_of_call_id,correction_of_call_id,idempotency_key_digest,policy_decision_id,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		c.ID, c.IntegrationID, c.OperationID, traceID, actorID, subjectID, c.Environment, grantID,
		c.Attempt, rfc(c.StartedAt), completed, requestBytes, responseBytes, statusClass,
		requestDigest, responseDigest, c.Outcome, errorCode, latencyMS, costMicros,
		retryOf, correctionOf, idemDigest, policyDecisionID, rfc(c.CreatedAt))
	return t.fail(err)
}

func (t *agentRuntimeTx) GetM6CallLog(id string) (m6supply.CallLog, error) {
	c, err := scanM6CallLog(t.tx.QueryRowContext(t.ctx, `SELECT `+m6CallLogColumns+` FROM m6_call_log WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return c, m6supply.ErrNotFound
	}
	return c, err
}

// ListM6CallLogs returns the newest calls for one integration, newest
// first, bounded by limit.
func (t *agentRuntimeTx) ListM6CallLogs(integrationID string, limit int) ([]m6supply.CallLog, error) {
	rows, err := t.tx.QueryContext(t.ctx,
		`SELECT `+m6CallLogColumns+` FROM m6_call_log WHERE integration_id=? ORDER BY started_at DESC LIMIT ?`,
		integrationID, limit)
	if err != nil {
		return nil, t.fail(err)
	}
	defer rows.Close()
	var out []m6supply.CallLog
	for rows.Next() {
		c, err := scanM6CallLog(rows)
		if err != nil {
			return nil, t.fail(err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
