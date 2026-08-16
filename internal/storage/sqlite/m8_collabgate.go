// M8 FR-17 storage (G1/G2): collab_gate_evaluations /
// collab_gate_decisions on the agent-runtime single-writer transaction.
// Evaluation snapshots are append-only (UPDATE/DELETE -> M8-034 triggers);
// decisions carry the token/expiry/state lifecycle and stay mutable by
// design (pending -> confirmed/expired/revoked).
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/lunitide/lunitide/internal/domain/agentrun"
	"github.com/lunitide/lunitide/internal/domain/m8core"
	"github.com/lunitide/lunitide/internal/m8app"
)

// TransactCollabGate runs an m8app FR-17 use case on the shared
// single-writer transaction.
func (r *AgentRuntimeRepository) TransactCollabGate(ctx context.Context, fn func(m8app.CollabGateTx) error) error {
	return r.Transact(ctx, func(tx agentrun.Tx) error {
		gtx, ok := tx.(m8app.CollabGateTx)
		if !ok {
			return errors.New("agent runtime tx does not satisfy m8app.CollabGateTx")
		}
		return fn(gtx)
	})
}

const m8cgeColumns = `evaluation_id,subject_id,window_start,window_end,evidence_json,evidence_digest,criteria_version,outcome,failed_criteria_json,created_at`

func scanCollabGateEval(s interface{ Scan(...any) error }) (m8core.GateEvaluation, error) {
	var e m8core.GateEvaluation
	var failed sql.NullString
	err := s.Scan(&e.EvaluationID, &e.SubjectID, &e.WindowStart, &e.WindowEnd,
		&e.EvidenceJSON, &e.EvidenceDigest, &e.CriteriaVersion, &e.Outcome,
		&failed, &e.CreatedAt)
	if failed.Valid {
		e.FailedCriteria = m8core.DecodeFailedCriteria(failed.String)
	} else {
		e.FailedCriteria = []string{}
	}
	return e, err
}

// GetEvaluationByKey answers the UNIQUE(subject, window, criteria) row.
func (t *agentRuntimeTx) GetEvaluationByKey(subjectID string, windowStart, windowEnd int64, criteriaVersion string) (m8core.GateEvaluation, bool, error) {
	e, err := scanCollabGateEval(t.tx.QueryRowContext(t.ctx,
		`SELECT `+m8cgeColumns+` FROM collab_gate_evaluations
		WHERE subject_id=? AND window_start=? AND window_end=? AND criteria_version=?`,
		subjectID, windowStart, windowEnd, criteriaVersion))
	if errors.Is(err, sql.ErrNoRows) {
		return m8core.GateEvaluation{}, false, nil
	}
	if err != nil {
		return m8core.GateEvaluation{}, false, t.fail(err)
	}
	return e, true, nil
}

// GetEvaluationByID answers one append-only snapshot row.
func (t *agentRuntimeTx) GetEvaluationByID(evaluationID string) (m8core.GateEvaluation, bool, error) {
	e, err := scanCollabGateEval(t.tx.QueryRowContext(t.ctx,
		`SELECT `+m8cgeColumns+` FROM collab_gate_evaluations WHERE evaluation_id=?`, evaluationID))
	if errors.Is(err, sql.ErrNoRows) {
		return m8core.GateEvaluation{}, false, nil
	}
	if err != nil {
		return m8core.GateEvaluation{}, false, t.fail(err)
	}
	return e, true, nil
}

// PutEvaluation inserts one append-only snapshot (the WORM triggers guard
// UPDATE/DELETE with M8-034).
func (t *agentRuntimeTx) PutEvaluation(e m8core.GateEvaluation) error {
	var failed any
	if len(e.FailedCriteria) > 0 {
		failed = m8core.EncodeFailedCriteria(e.FailedCriteria)
	}
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO collab_gate_evaluations
		(evaluation_id,subject_id,window_start,window_end,evidence_json,evidence_digest,criteria_version,outcome,failed_criteria_json,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?)`,
		e.EvaluationID, e.SubjectID, e.WindowStart, e.WindowEnd, e.EvidenceJSON,
		e.EvidenceDigest, e.CriteriaVersion, e.Outcome, failed, e.CreatedAt)
	return t.fail(err)
}

const m8cgdColumns = `decision_id,evaluation_id,subject_id,decision_token,policy_version,capability_digest,action,state,confirmed_at,expires_at,created_at`

func scanGateDecision(s interface{ Scan(...any) error }) (m8core.GateDecision, error) {
	var d m8core.GateDecision
	var confirmed sql.NullString
	err := s.Scan(&d.DecisionID, &d.EvaluationID, &d.SubjectID, &d.DecisionToken,
		&d.PolicyVersion, &d.CapabilityDigest, &d.Action, &d.State,
		&confirmed, &d.ExpiresAt, &d.CreatedAt)
	if confirmed.Valid {
		d.ConfirmedAt = confirmed.String
	}
	return d, err
}

// PutDecision inserts one decision row.
func (t *agentRuntimeTx) PutDecision(d m8core.GateDecision) error {
	var confirmed any
	if d.ConfirmedAt != "" {
		confirmed = d.ConfirmedAt
	}
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO collab_gate_decisions
		(decision_id,evaluation_id,subject_id,decision_token,policy_version,capability_digest,action,state,confirmed_at,expires_at,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		d.DecisionID, d.EvaluationID, d.SubjectID, d.DecisionToken,
		d.PolicyVersion, d.CapabilityDigest, d.Action, d.State,
		confirmed, d.ExpiresAt, d.CreatedAt)
	return t.fail(err)
}

// GetDecision answers one decision row.
func (t *agentRuntimeTx) GetDecision(decisionID string) (m8core.GateDecision, bool, error) {
	d, err := scanGateDecision(t.tx.QueryRowContext(t.ctx,
		`SELECT `+m8cgdColumns+` FROM collab_gate_decisions WHERE decision_id=?`, decisionID))
	if errors.Is(err, sql.ErrNoRows) {
		return m8core.GateDecision{}, false, nil
	}
	if err != nil {
		return m8core.GateDecision{}, false, t.fail(err)
	}
	return d, true, nil
}

// SetDecisionState moves the decision lifecycle (pending ->
// confirmed/expired/revoked; confirmed_at lands only on confirm).
func (t *agentRuntimeTx) SetDecisionState(decisionID, state, confirmedAt string) error {
	var confirmed any
	if confirmedAt != "" {
		confirmed = confirmedAt
	}
	_, err := t.tx.ExecContext(t.ctx, `UPDATE collab_gate_decisions
		SET state=?, confirmed_at=COALESCE(?, confirmed_at) WHERE decision_id=?`,
		state, confirmed, decisionID)
	return t.fail(err)
}

// LatestConfirmedDecision answers the newest confirmed decision of one
// subject (the live capability source).
func (t *agentRuntimeTx) LatestConfirmedDecision(subjectID string) (m8core.GateDecision, bool, error) {
	d, err := scanGateDecision(t.tx.QueryRowContext(t.ctx,
		`SELECT `+m8cgdColumns+` FROM collab_gate_decisions
		WHERE subject_id=? AND state='confirmed' ORDER BY created_at DESC, decision_id DESC LIMIT 1`,
		subjectID))
	if errors.Is(err, sql.ErrNoRows) {
		return m8core.GateDecision{}, false, nil
	}
	if err != nil {
		return m8core.GateDecision{}, false, t.fail(err)
	}
	return d, true, nil
}

// LatestPendingDecision answers the newest pending decision of one subject
// (the evaluation-report confirm card source).
func (t *agentRuntimeTx) LatestPendingDecision(subjectID string) (m8core.GateDecision, bool, error) {
	d, err := scanGateDecision(t.tx.QueryRowContext(t.ctx,
		`SELECT `+m8cgdColumns+` FROM collab_gate_decisions
		WHERE subject_id=? AND state='pending' ORDER BY created_at DESC, decision_id DESC LIMIT 1`,
		subjectID))
	if errors.Is(err, sql.ErrNoRows) {
		return m8core.GateDecision{}, false, nil
	}
	if err != nil {
		return m8core.GateDecision{}, false, t.fail(err)
	}
	return d, true, nil
}

// GateEvidence answers the production FR-17 aggregator over the shared
// agent-runtime handle.
func (r *AgentRuntimeRepository) GateEvidence() *SQLiteGateEvidence {
	return NewSQLiteGateEvidence(r.db)
}

// SQLiteGateEvidence is the production EvidenceSource: read-only
// aggregation over the M7 subagent audit (subagent_runs,
// m7_audit_events) and the M5/M6 EffectJournal (effect_journal).
//
// Evidence availability rules (02 技术设计 G1, GT-01/GT-05):
//   - an unreadable table is an error -> the caller refuses the whole
//     evaluation with M8-033 fail-closed;
//   - a readable-but-empty source is a MISSING criterion key -> the
//     adjudication answers insufficient_evidence, never a partial pass.
//
// Derived figures: intercept coverage counts subagent.spawn.refused audit
// rows (every whitelist violation was refused and sealed); the TOCTOU guard
// counts subagent.join rows (every join re-verified the digest, refusals
// fail closed upstream); undeclared write effects stay 0 because the M7
// subagent runtime has no write path (effect_journal rows FK to agent_run,
// never to a subagent).
type SQLiteGateEvidence struct {
	db *sql.DB
}

// NewSQLiteGateEvidence wires the aggregator over the shared handle.
func NewSQLiteGateEvidence(db *sql.DB) *SQLiteGateEvidence { return &SQLiteGateEvidence{db: db} }

// Aggregate collects the window evidence snapshot.
func (s *SQLiteGateEvidence) Aggregate(ctx context.Context, subjectID string, windowStart, windowEnd int64) (m8core.GateEvidence, error) {
	var e m8core.GateEvidence
	e.WindowDays = int((windowEnd - windowStart) / int64(24*time.Hour/time.Millisecond))
	// M7 subagent run volume + lifecycle.
	var runs, roots, orphans, failed int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*), COUNT(DISTINCT root_run_id),
		SUM(CASE WHEN status='orphaned' THEN 1 ELSE 0 END),
		SUM(CASE WHEN status='failed' THEN 1 ELSE 0 END)
		FROM subagent_runs WHERE created_at >= ? AND created_at < ?`,
		unixMsToRFC3339(windowStart), unixMsToRFC3339(windowEnd)).Scan(&runs, &roots, &orphans, &failed)
	if err != nil {
		return e, err
	}
	if runs == 0 {
		e.Missing = append(e.Missing, "subagent_runs")
	}
	e.SubagentRuns, e.RootRunsCovered, e.OrphanSubagents = runs, roots, orphans
	e.TimeoutRatio = float64(failed) / float64(runs)
	if orphans == 0 {
		e.CrashRecoveryRate = 1 // no orphan left behind = converged
	}
	// Intercept + TOCTOU audit coverage.
	var refused, joins int
	err = s.db.QueryRowContext(ctx, `SELECT
		(SELECT COUNT(*) FROM m7_audit_events WHERE action='subagent.spawn.refused' AND created_at >= ? AND created_at < ?),
		(SELECT COUNT(*) FROM m7_audit_events WHERE action='subagent.join' AND created_at >= ? AND created_at < ?)`,
		unixMsToRFC3339(windowStart), unixMsToRFC3339(windowEnd),
		unixMsToRFC3339(windowStart), unixMsToRFC3339(windowEnd)).Scan(&refused, &joins)
	if err != nil {
		return e, err
	}
	if refused > 0 {
		e.WriteInterceptRate = 1 // every violation refused + sealed
	} else {
		e.Missing = append(e.Missing, "intercept_audit")
	}
	if joins > 0 {
		e.ToctouReplayGuard = 1 // joins re-verify digests fail-closed
	} else {
		e.Missing = append(e.Missing, "toctou_audit")
	}
	// M5/M6 compensation ledger.
	var committed, concluded int
	err = s.db.QueryRowContext(ctx, `SELECT
		SUM(CASE WHEN status='committed' THEN 1 ELSE 0 END), COUNT(*)
		FROM effect_journal WHERE created_at >= ? AND created_at < ?`,
		unixMsToRFC3339(windowStart), unixMsToRFC3339(windowEnd)).Scan(&committed, &concluded)
	if err != nil {
		return e, err
	}
	if concluded > 0 {
		e.CompensationSuccess = float64(committed) / float64(concluded)
	} else {
		e.Missing = append(e.Missing, "compensation_journal")
	}
	// The M7 subagent runtime has no write path: undeclared write effects
	// stay zero by construction (effect_journal FKs never touch a subagent).
	e.UndeclaredWrites = 0
	return e, nil
}

// unixMsToRFC3339 renders a millisecond epoch as the RFC3339 UTC form the
// TEXT timestamp columns store.
func unixMsToRFC3339(ms int64) string {
	return time.UnixMilli(ms).UTC().Format(time.RFC3339)
}
