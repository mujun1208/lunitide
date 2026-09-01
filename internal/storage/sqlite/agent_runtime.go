package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/lunitide/lunitide/internal/agentrunapp"
	"github.com/lunitide/lunitide/internal/domain/agentrun"
	"github.com/lunitide/lunitide/internal/providerapp"
)

// AgentRuntimeRepository persists the M4 reliable single-agent runtime
// aggregates. SQLite is the single writer: every logical operation runs in
// one Transact so state, events, effect journal and outbox stay atomic.
type AgentRuntimeRepository struct{ db *sql.DB }

func (s *Store) AgentRuntimeRepository() *AgentRuntimeRepository {
	return &AgentRuntimeRepository{db: s.db}
}

func (r *AgentRuntimeRepository) Transact(ctx context.Context, fn func(agentrun.Tx) error) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	atx := &agentRuntimeTx{ctx: ctx, tx: tx}
	if err = fn(atx); err != nil {
		return err
	}
	// Fail closed: a failed statement poisons the transaction even when the
	// caller swallowed the error, so partial effects can never commit.
	if atx.failed != nil {
		return atx.failed
	}
	return tx.Commit()
}

// rfc3339NanoFixed keeps a 9-digit fraction so SQLite TEXT comparison of
// timestamps matches chronological order. time.RFC3339Nano strips trailing
// zeros, so "…45.5Z" > "…45.51Z" lexicographically even though 510ms is later
// than 500ms; tables CHECK (updated_at >= created_at) on those strings.
const rfc3339NanoFixed = "2006-01-02T15:04:05.000000000Z07:00"

// rfc formats t for SQLite TEXT timestamps. Zero time stays the historical
// sentinel used by lease CASE expressions (0001-01-01T00:00:00Z).
func rfc(t time.Time) string {
	u := t.UTC()
	if u.IsZero() {
		return "0001-01-01T00:00:00Z"
	}
	return u.Format(rfc3339NanoFixed)
}

func parseRFC(s string) (time.Time, error) { return time.Parse(time.RFC3339Nano, s) }

type agentRuntimeTx struct {
	ctx    context.Context
	tx     *sql.Tx
	failed error
}

// fail records the first statement error so Transact refuses to commit.
func (t *agentRuntimeTx) fail(err error) error {
	if err != nil && t.failed == nil {
		t.failed = err
	}
	return err
}

// ── Runs ────────────────────────────────────────────────────────────────────

func (t *agentRuntimeTx) PutRun(r agentrun.AgentRun) error {
	if err := r.Validate(); err != nil {
		return err
	}
	budget, err := json.Marshal(r.Budget)
	if err != nil {
		return err
	}
	used, err := json.Marshal(r.Used)
	if err != nil {
		return err
	}
	_, err = t.tx.ExecContext(t.ctx, `INSERT INTO agent_run(id,session_id,status,budget_json,used_json,version,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET status=excluded.status,budget_json=excluded.budget_json,used_json=excluded.used_json,version=excluded.version,updated_at=excluded.updated_at`,
		r.ID, r.SessionID, string(r.Status), string(budget), string(used), r.Version, rfc(r.CreatedAt), rfc(r.UpdatedAt))
	return t.fail(err)
}

const runColumns = `id,session_id,status,budget_json,used_json,version,created_at,updated_at`

func scanRun(s interface{ Scan(...any) error }) (agentrun.AgentRun, error) {
	var r agentrun.AgentRun
	var status, budget, used, created, updated string
	if err := s.Scan(&r.ID, &r.SessionID, &status, &budget, &used, &r.Version, &created, &updated); err != nil {
		return r, err
	}
	r.Status = agentrun.RunStatus(status)
	if err := json.Unmarshal([]byte(budget), &r.Budget); err != nil {
		return r, err
	}
	if err := json.Unmarshal([]byte(used), &r.Used); err != nil {
		return r, err
	}
	var err error
	if r.CreatedAt, err = parseRFC(created); err != nil {
		return r, err
	}
	if r.UpdatedAt, err = parseRFC(updated); err != nil {
		return r, err
	}
	return r, nil
}

func (t *agentRuntimeTx) GetRun(id string) (agentrun.AgentRun, error) {
	r, err := scanRun(t.tx.QueryRowContext(t.ctx, `SELECT `+runColumns+` FROM agent_run WHERE id=?`, id))
	if err == sql.ErrNoRows {
		return r, agentrun.ErrNotFound
	}
	return r, err
}

func (t *agentRuntimeTx) ListRunsBySession(sessionID string) ([]agentrun.AgentRun, error) {
	rows, err := t.tx.QueryContext(t.ctx, `SELECT `+runColumns+` FROM agent_run WHERE session_id=? ORDER BY created_at,id`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []agentrun.AgentRun
	for rows.Next() {
		r, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (t *agentRuntimeTx) ListActiveRuns() ([]agentrun.AgentRun, error) {
	rows, err := t.tx.QueryContext(t.ctx, `SELECT `+runColumns+` FROM agent_run WHERE status IN ('queued','running','paused_review','paused_budget') ORDER BY created_at,id`)
	if err != nil {
		return nil, t.fail(err)
	}
	defer rows.Close()
	var out []agentrun.AgentRun
	for rows.Next() {
		v, e := scanRun(rows)
		if e != nil {
			return nil, t.fail(e)
		}
		out = append(out, v)
	}
	return out, t.fail(rows.Err())
}

func (t *agentRuntimeTx) TransitionRun(id string, expectedVersion int64, to agentrun.RunStatus, at time.Time) (agentrun.AgentRun, error) {
	r, err := t.GetRun(id)
	if err != nil {
		return r, err
	}
	if r.Version != expectedVersion {
		return r, agentrun.ErrVersionConflict
	}
	next, err := r.Transition(to, at)
	if err != nil {
		return r, err
	}
	res, err := t.tx.ExecContext(t.ctx, `UPDATE agent_run SET status=?,version=?,updated_at=? WHERE id=? AND version=?`,
		string(next.Status), next.Version, rfc(next.UpdatedAt), id, expectedVersion)
	if err != nil {
		return r, t.fail(err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return r, agentrun.ErrVersionConflict
	}
	return next, nil
}

func (t *agentRuntimeTx) ReplaceBudget(id string, expectedVersion int64, budget agentrun.Budget, at time.Time) (agentrun.AgentRun, error) {
	if err := budget.Validate(); err != nil {
		return agentrun.AgentRun{}, fmt.Errorf("%w: %v", agentrun.ErrInvalid, err)
	}
	r, err := t.GetRun(id)
	if err != nil {
		return r, err
	}
	if r.Version != expectedVersion {
		return r, agentrun.ErrVersionConflict
	}
	if r.Status.Terminal() {
		return r, agentrun.ErrTerminal
	}
	if !budget.StrictlyExpands(r.Budget) {
		return r, agentrun.ErrBudgetExceeded
	}
	pending, err := t.ActiveReservedUsage(id)
	if err != nil {
		return r, err
	}
	if !budget.Covers(r.Used.Add(pending)) {
		return r, agentrun.ErrBudgetExceeded
	}
	body, _ := json.Marshal(budget)
	res, err := t.tx.ExecContext(t.ctx, `UPDATE agent_run SET budget_json=?,version=version+1,updated_at=? WHERE id=? AND version=?`, string(body), rfc(at), id, expectedVersion)
	if err != nil {
		return r, t.fail(err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return r, agentrun.ErrVersionConflict
	}
	r.Budget, r.Version, r.UpdatedAt = budget, r.Version+1, at
	return r, nil
}

func usageLE(a, b agentrun.Usage) bool {
	return a.ModelTurns <= b.ModelTurns && a.ToolCalls <= b.ToolCalls && a.Tokens <= b.Tokens && a.CostMicros <= b.CostMicros && a.WallClockSeconds <= b.WallClockSeconds && a.OutputBytes <= b.OutputBytes && a.Retries <= b.Retries && a.NoProgress <= b.NoProgress
}

func (t *agentRuntimeTx) ReserveUsage(runID, reservationID string, delta agentrun.Usage, at time.Time) (agentrun.AgentRun, error) {
	if err := delta.Validate(); err != nil {
		return agentrun.AgentRun{}, err
	}
	r, err := t.GetRun(runID)
	if err != nil {
		return r, err
	}
	if r.Status.Terminal() {
		return r, agentrun.ErrTerminal
	}
	if r.Status != agentrun.RunRunning {
		return r, agentrun.ErrInvalidTransition
	}
	pending, err := t.ActiveReservedUsage(runID)
	if err != nil {
		return r, err
	}
	if dim := r.Budget.ExceededBy(r.Used.Add(pending).Add(delta)); dim != "" {
		return r, fmt.Errorf("%w: %s", agentrun.ErrBudgetExceeded, dim)
	}
	body, _ := json.Marshal(delta)
	_, err = t.tx.ExecContext(t.ctx, `INSERT INTO run_usage_reservation(id,run_id,reserved_json,status,created_at,updated_at) VALUES(?,?,?,'reserved',?,?)`, reservationID, runID, string(body), rfc(at), rfc(at))
	return r, t.fail(err)
}

func (t *agentRuntimeTx) ActiveReservedUsage(runID string) (agentrun.Usage, error) {
	rows, err := t.tx.QueryContext(t.ctx, `SELECT reserved_json FROM run_usage_reservation WHERE run_id=? AND status='reserved' ORDER BY id`, runID)
	if err != nil {
		return agentrun.Usage{}, t.fail(err)
	}
	defer rows.Close()
	pending := agentrun.Usage{}
	for rows.Next() {
		var raw string
		if err = rows.Scan(&raw); err != nil {
			return agentrun.Usage{}, t.fail(err)
		}
		var u agentrun.Usage
		if err = json.Unmarshal([]byte(raw), &u); err != nil {
			return agentrun.Usage{}, t.fail(err)
		}
		pending = pending.Add(u)
	}
	return pending, t.fail(rows.Err())
}

func (t *agentRuntimeTx) CommitUsage(runID, reservationID string, actual agentrun.Usage, at time.Time) (agentrun.AgentRun, error) {
	if err := actual.Validate(); err != nil {
		return agentrun.AgentRun{}, err
	}
	r, err := t.GetRun(runID)
	if err != nil {
		return r, err
	}
	if r.Status.Terminal() {
		return r, agentrun.ErrTerminal
	}
	var raw string
	err = t.tx.QueryRowContext(t.ctx, `SELECT reserved_json FROM run_usage_reservation WHERE id=? AND run_id=? AND status='reserved'`, reservationID, runID).Scan(&raw)
	if err == sql.ErrNoRows {
		return r, agentrun.ErrReservation
	}
	if err != nil {
		return r, t.fail(err)
	}
	var reserved agentrun.Usage
	if err = json.Unmarshal([]byte(raw), &reserved); err != nil {
		return r, t.fail(err)
	}
	if !usageLE(actual, reserved) {
		return r, agentrun.ErrBudgetExceeded
	}
	next := r.Used.Add(actual)
	if dim := r.Budget.ExceededBy(next); dim != "" {
		return r, fmt.Errorf("%w: %s", agentrun.ErrBudgetExceeded, dim)
	}
	used, _ := json.Marshal(next)
	committed, _ := json.Marshal(actual)
	resv, err := t.tx.ExecContext(t.ctx, `UPDATE run_usage_reservation SET committed_json=?,status='committed',updated_at=? WHERE id=? AND status='reserved'`, string(committed), rfc(at), reservationID)
	if err != nil {
		return r, t.fail(err)
	}
	if n, _ := resv.RowsAffected(); n != 1 {
		// the reservation already left 'reserved' (double commit in one
		// transaction) — refuse instead of charging used_json twice
		return r, agentrun.ErrReservation
	}
	res, err := t.tx.ExecContext(t.ctx, `UPDATE agent_run SET used_json=?,version=version+1,updated_at=? WHERE id=? AND version=?`, string(used), rfc(at), runID, r.Version)
	if err != nil {
		return r, t.fail(err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return r, agentrun.ErrVersionConflict
	}
	r.Used, r.Version, r.UpdatedAt = next, r.Version+1, at
	return r, nil
}

func (t *agentRuntimeTx) RecoverRun(id string, to agentrun.RunStatus, at time.Time) (agentrun.AgentRun, error) {
	if to != agentrun.RunInterrupted && to != agentrun.RunOutcomeUnknown {
		return agentrun.AgentRun{}, agentrun.ErrInvalidTransition
	}
	r, err := t.GetRun(id)
	if err != nil {
		return r, err
	}
	if r.Status.Terminal() {
		return r, agentrun.ErrTerminal
	}
	res, err := t.tx.ExecContext(t.ctx, `UPDATE agent_run SET status=?,version=version+1,updated_at=? WHERE id=? AND version=?`, string(to), rfc(at), id, r.Version)
	if err != nil {
		return r, t.fail(err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return r, agentrun.ErrVersionConflict
	}
	r.Status, r.Version, r.UpdatedAt = to, r.Version+1, at
	return r, nil
}

// ── Turns and steps ─────────────────────────────────────────────────────────

func (t *agentRuntimeTx) PutTurn(v agentrun.AgentTurn) error {
	if err := v.Validate(); err != nil {
		return err
	}
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO agent_turn(id,run_id,turn_no,status,version,created_at,updated_at) VALUES(?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET status=excluded.status,version=excluded.version,updated_at=excluded.updated_at`,
		v.ID, v.RunID, v.TurnNo, string(v.Status), v.Version, rfc(v.CreatedAt), rfc(v.UpdatedAt))
	return t.fail(err)
}

const turnColumns = `id,run_id,turn_no,status,version,created_at,updated_at`

func scanTurn(s interface{ Scan(...any) error }) (agentrun.AgentTurn, error) {
	var v agentrun.AgentTurn
	var status, created, updated string
	if err := s.Scan(&v.ID, &v.RunID, &v.TurnNo, &status, &v.Version, &created, &updated); err != nil {
		return v, err
	}
	v.Status = agentrun.TurnStatus(status)
	var err error
	if v.CreatedAt, err = parseRFC(created); err != nil {
		return v, err
	}
	if v.UpdatedAt, err = parseRFC(updated); err != nil {
		return v, err
	}
	return v, nil
}

func (t *agentRuntimeTx) GetTurn(id string) (agentrun.AgentTurn, error) {
	v, err := scanTurn(t.tx.QueryRowContext(t.ctx, `SELECT `+turnColumns+` FROM agent_turn WHERE id=?`, id))
	if err == sql.ErrNoRows {
		return v, agentrun.ErrNotFound
	}
	return v, err
}

func (t *agentRuntimeTx) ListTurns(runID string) ([]agentrun.AgentTurn, error) {
	rows, err := t.tx.QueryContext(t.ctx, `SELECT `+turnColumns+` FROM agent_turn WHERE run_id=? ORDER BY turn_no`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []agentrun.AgentTurn
	for rows.Next() {
		v, err := scanTurn(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (t *agentRuntimeTx) PutStep(v agentrun.AgentStep) error {
	if err := v.Validate(); err != nil {
		return err
	}
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO agent_step(id,turn_id,step_no,kind,status,created_at,updated_at) VALUES(?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET status=excluded.status,updated_at=excluded.updated_at`,
		v.ID, v.TurnID, v.StepNo, string(v.Kind), string(v.Status), rfc(v.CreatedAt), rfc(v.UpdatedAt))
	return t.fail(err)
}

const stepColumns = `id,turn_id,step_no,kind,status,created_at,updated_at`

func scanStep(s interface{ Scan(...any) error }) (agentrun.AgentStep, error) {
	var v agentrun.AgentStep
	var kind, status, created, updated string
	if err := s.Scan(&v.ID, &v.TurnID, &v.StepNo, &kind, &status, &created, &updated); err != nil {
		return v, err
	}
	v.Kind = agentrun.StepKind(kind)
	v.Status = agentrun.StepStatus(status)
	var err error
	if v.CreatedAt, err = parseRFC(created); err != nil {
		return v, err
	}
	if v.UpdatedAt, err = parseRFC(updated); err != nil {
		return v, err
	}
	return v, nil
}

func (t *agentRuntimeTx) ListSteps(turnID string) ([]agentrun.AgentStep, error) {
	rows, err := t.tx.QueryContext(t.ctx, `SELECT `+stepColumns+` FROM agent_step WHERE turn_id=? ORDER BY step_no`, turnID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []agentrun.AgentStep
	for rows.Next() {
		v, err := scanStep(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (t *agentRuntimeTx) ListRunningSteps() ([]agentrun.AgentStep, error) {
	rows, err := t.tx.QueryContext(t.ctx, `SELECT `+stepColumns+` FROM agent_step WHERE status='running' ORDER BY created_at,id`)
	if err != nil {
		return nil, t.fail(err)
	}
	defer rows.Close()
	var out []agentrun.AgentStep
	for rows.Next() {
		v, e := scanStep(rows)
		if e != nil {
			return nil, t.fail(e)
		}
		out = append(out, v)
	}
	return out, t.fail(rows.Err())
}

func (t *agentRuntimeTx) RunIDForStep(stepID string) (string, error) {
	var runID string
	err := t.tx.QueryRowContext(t.ctx, `SELECT t.run_id FROM agent_step s JOIN agent_turn t ON t.id=s.turn_id WHERE s.id=?`, stepID).Scan(&runID)
	if err == sql.ErrNoRows {
		return "", agentrun.ErrNotFound
	}
	return runID, t.fail(err)
}

// ── Tool calls and observations ─────────────────────────────────────────────

func (t *agentRuntimeTx) PutToolCall(v agentrun.ToolCall) error {
	if err := v.Validate(); err != nil {
		return err
	}
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO tool_call(id,step_id,tool_name,args_digest,status,created_at,updated_at) VALUES(?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET status=excluded.status,updated_at=excluded.updated_at`,
		v.ID, v.StepID, v.ToolName, v.ArgsDigest, string(v.Status), rfc(v.CreatedAt), rfc(v.UpdatedAt))
	return t.fail(err)
}

const toolCallColumns = `id,step_id,tool_name,args_digest,status,created_at,updated_at`

func scanToolCall(s interface{ Scan(...any) error }) (agentrun.ToolCall, error) {
	var v agentrun.ToolCall
	var status, created, updated string
	if err := s.Scan(&v.ID, &v.StepID, &v.ToolName, &v.ArgsDigest, &status, &created, &updated); err != nil {
		return v, err
	}
	v.Status = agentrun.CallStatus(status)
	var err error
	if v.CreatedAt, err = parseRFC(created); err != nil {
		return v, err
	}
	if v.UpdatedAt, err = parseRFC(updated); err != nil {
		return v, err
	}
	return v, nil
}

func (t *agentRuntimeTx) GetToolCall(id string) (agentrun.ToolCall, error) {
	v, err := scanToolCall(t.tx.QueryRowContext(t.ctx, `SELECT `+toolCallColumns+` FROM tool_call WHERE id=?`, id))
	if err == sql.ErrNoRows {
		return v, agentrun.ErrNotFound
	}
	return v, err
}

func (t *agentRuntimeTx) ListToolCalls(stepID string) ([]agentrun.ToolCall, error) {
	rows, err := t.tx.QueryContext(t.ctx, `SELECT `+toolCallColumns+` FROM tool_call WHERE step_id=? ORDER BY created_at,id`, stepID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []agentrun.ToolCall
	for rows.Next() {
		v, err := scanToolCall(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (t *agentRuntimeTx) ListActiveToolCalls() ([]agentrun.ToolCall, error) {
	rows, err := t.tx.QueryContext(t.ctx, `SELECT `+toolCallColumns+` FROM tool_call WHERE status IN ('proposed','policy_checked','awaiting_review','approved','running') ORDER BY created_at,id`)
	if err != nil {
		return nil, t.fail(err)
	}
	defer rows.Close()
	var out []agentrun.ToolCall
	for rows.Next() {
		v, e := scanToolCall(rows)
		if e != nil {
			return nil, t.fail(e)
		}
		out = append(out, v)
	}
	return out, t.fail(rows.Err())
}

func (t *agentRuntimeTx) AppendObservation(v agentrun.Observation) error {
	if err := v.Validate(); err != nil {
		return err
	}
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO observation(id,step_id,kind,content_digest,captured_at,created_at) VALUES(?,?,?,?,?,?)`,
		v.ID, v.StepID, v.Kind, v.ContentDigest, rfc(v.CapturedAt), rfc(v.CreatedAt))
	return t.fail(err)
}

func (t *agentRuntimeTx) ListObservations(stepID string) ([]agentrun.Observation, error) {
	rows, err := t.tx.QueryContext(t.ctx, `SELECT id,step_id,kind,content_digest,captured_at,created_at FROM observation WHERE step_id=? ORDER BY captured_at,id`, stepID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []agentrun.Observation
	for rows.Next() {
		var v agentrun.Observation
		var captured, created string
		if err = rows.Scan(&v.ID, &v.StepID, &v.Kind, &v.ContentDigest, &captured, &created); err != nil {
			return nil, err
		}
		if v.CapturedAt, err = parseRFC(captured); err != nil {
			return nil, err
		}
		if v.CreatedAt, err = parseRFC(created); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// ── Effect journal ──────────────────────────────────────────────────────────

func (t *agentRuntimeTx) PutEffect(v agentrun.EffectJournal) error {
	if err := v.Validate(); err != nil {
		return err
	}
	var receipt any
	if v.ReceiptID != "" {
		receipt = v.ReceiptID
	}
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO effect_journal(id,run_id,effect_key,request_digest,receipt_id,status,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET receipt_id=excluded.receipt_id,status=excluded.status,updated_at=excluded.updated_at`,
		v.ID, v.RunID, v.EffectKey, v.RequestDigest, receipt, string(v.Status), rfc(v.CreatedAt), rfc(v.UpdatedAt))
	return t.fail(err)
}

const effectColumns = `id,run_id,effect_key,request_digest,COALESCE(receipt_id,''),status,created_at,updated_at`

func scanEffect(s interface{ Scan(...any) error }) (agentrun.EffectJournal, error) {
	var v agentrun.EffectJournal
	var status, created, updated string
	if err := s.Scan(&v.ID, &v.RunID, &v.EffectKey, &v.RequestDigest, &v.ReceiptID, &status, &created, &updated); err != nil {
		return v, err
	}
	v.Status = agentrun.EffectStatus(status)
	var err error
	if v.CreatedAt, err = parseRFC(created); err != nil {
		return v, err
	}
	if v.UpdatedAt, err = parseRFC(updated); err != nil {
		return v, err
	}
	return v, nil
}

func (t *agentRuntimeTx) GetEffectByKey(effectKey string) (agentrun.EffectJournal, error) {
	v, err := scanEffect(t.tx.QueryRowContext(t.ctx, `SELECT `+effectColumns+` FROM effect_journal WHERE effect_key=?`, effectKey))
	if err == sql.ErrNoRows {
		return v, agentrun.ErrNotFound
	}
	return v, err
}

func (t *agentRuntimeTx) ListEffects(runID string) ([]agentrun.EffectJournal, error) {
	rows, err := t.tx.QueryContext(t.ctx, `SELECT `+effectColumns+` FROM effect_journal WHERE run_id=? ORDER BY created_at,id`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []agentrun.EffectJournal
	for rows.Next() {
		v, err := scanEffect(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (t *agentRuntimeTx) ListPreparedEffects() ([]agentrun.EffectJournal, error) {
	rows, err := t.tx.QueryContext(t.ctx, `SELECT `+effectColumns+` FROM effect_journal WHERE status='prepared' ORDER BY created_at,id`)
	if err != nil {
		return nil, t.fail(err)
	}
	defer rows.Close()
	var out []agentrun.EffectJournal
	for rows.Next() {
		v, e := scanEffect(rows)
		if e != nil {
			return nil, t.fail(e)
		}
		out = append(out, v)
	}
	return out, t.fail(rows.Err())
}

// ── Run events ──────────────────────────────────────────────────────────────

func (t *agentRuntimeTx) AppendEvent(v agentrun.RunEvent) error {
	if err := v.Validate(); err != nil {
		return err
	}
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO run_event(id,run_id,sequence,event_type,payload_json,created_at) VALUES(?,?,?,?,?,?)`,
		v.ID, v.RunID, v.Sequence, v.EventType, string(v.Payload), rfc(v.CreatedAt))
	return t.fail(err)
}

func (t *agentRuntimeTx) ListEvents(runID string) ([]agentrun.RunEvent, error) {
	rows, err := t.tx.QueryContext(t.ctx, `SELECT id,run_id,sequence,event_type,payload_json,created_at FROM run_event WHERE run_id=? ORDER BY sequence`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []agentrun.RunEvent
	for rows.Next() {
		var v agentrun.RunEvent
		var payload, created string
		if err = rows.Scan(&v.ID, &v.RunID, &v.Sequence, &v.EventType, &payload, &created); err != nil {
			return nil, err
		}
		v.Payload = []byte(payload)
		if v.CreatedAt, err = parseRFC(created); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// ── Workspace registration / grant / lease ──────────────────────────────────

func (t *agentRuntimeTx) PutRegistration(v agentrun.WorkspaceRegistration) error {
	if err := v.Validate(); err != nil {
		return err
	}
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO workspace_registration(id,canonical_root,root_digest,status,version,created_at,updated_at) VALUES(?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET status=excluded.status,version=excluded.version,updated_at=excluded.updated_at`,
		v.ID, v.CanonicalRoot, v.RootDigest, string(v.Status), v.Version, rfc(v.CreatedAt), rfc(v.UpdatedAt))
	return t.fail(err)
}

func scanRegistration(s interface{ Scan(...any) error }) (agentrun.WorkspaceRegistration, error) {
	var v agentrun.WorkspaceRegistration
	var status, created, updated string
	if err := s.Scan(&v.ID, &v.CanonicalRoot, &v.RootDigest, &status, &v.Version, &created, &updated); err != nil {
		return v, err
	}
	v.Status = agentrun.RegistrationStatus(status)
	var err error
	if v.CreatedAt, err = parseRFC(created); err != nil {
		return v, err
	}
	if v.UpdatedAt, err = parseRFC(updated); err != nil {
		return v, err
	}
	return v, nil
}

const registrationColumns = `id,canonical_root,root_digest,status,version,created_at,updated_at`

func (t *agentRuntimeTx) GetRegistration(id string) (agentrun.WorkspaceRegistration, error) {
	v, err := scanRegistration(t.tx.QueryRowContext(t.ctx, `SELECT `+registrationColumns+` FROM workspace_registration WHERE id=?`, id))
	if err == sql.ErrNoRows {
		return v, agentrun.ErrNotFound
	}
	return v, err
}

func (t *agentRuntimeTx) GetRegistrationByRoot(canonicalRoot string) (agentrun.WorkspaceRegistration, error) {
	v, err := scanRegistration(t.tx.QueryRowContext(t.ctx, `SELECT `+registrationColumns+` FROM workspace_registration WHERE canonical_root=?`, canonicalRoot))
	if err == sql.ErrNoRows {
		return v, agentrun.ErrNotFound
	}
	return v, err
}

func (t *agentRuntimeTx) PutGrant(v agentrun.WorkspaceGrant) error {
	if err := v.Validate(); err != nil {
		return err
	}
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO workspace_grant(id,registration_id,scope_json,expires_at,status,created_at,updated_at) VALUES(?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET status=excluded.status,updated_at=excluded.updated_at`,
		v.ID, v.RegistrationID, string(v.Scope), rfc(v.ExpiresAt), string(v.Status), rfc(v.CreatedAt), rfc(v.UpdatedAt))
	return t.fail(err)
}

const grantColumns = `id,registration_id,scope_json,expires_at,status,created_at,updated_at`

func scanGrant(s interface{ Scan(...any) error }) (agentrun.WorkspaceGrant, error) {
	var v agentrun.WorkspaceGrant
	var scope, expires, status, created, updated string
	if err := s.Scan(&v.ID, &v.RegistrationID, &scope, &expires, &status, &created, &updated); err != nil {
		return v, err
	}
	v.Scope = []byte(scope)
	v.Status = agentrun.GrantStatus(status)
	var err error
	if v.ExpiresAt, err = parseRFC(expires); err != nil {
		return v, err
	}
	if v.CreatedAt, err = parseRFC(created); err != nil {
		return v, err
	}
	if v.UpdatedAt, err = parseRFC(updated); err != nil {
		return v, err
	}
	return v, nil
}

func (t *agentRuntimeTx) GetGrant(id string) (agentrun.WorkspaceGrant, error) {
	v, err := scanGrant(t.tx.QueryRowContext(t.ctx, `SELECT `+grantColumns+` FROM workspace_grant WHERE id=?`, id))
	if err == sql.ErrNoRows {
		return v, agentrun.ErrNotFound
	}
	return v, err
}

func (t *agentRuntimeTx) NextFencingToken(grantID string) (int64, error) {
	var next int64
	err := t.tx.QueryRowContext(t.ctx, `SELECT COALESCE(MAX(fencing_token),0)+1 FROM workspace_lease WHERE grant_id=?`, grantID).Scan(&next)
	return next, err
}

func (t *agentRuntimeTx) PutLease(v agentrun.WorkspaceLease) error {
	if err := v.Validate(); err != nil {
		return err
	}
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO workspace_lease(id,grant_id,fencing_token,expires_at,status,created_at,updated_at) VALUES(?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET expires_at=excluded.expires_at,status=excluded.status,updated_at=excluded.updated_at`,
		v.ID, v.GrantID, v.FencingToken, rfc(v.ExpiresAt), string(v.Status), rfc(v.CreatedAt), rfc(v.UpdatedAt))
	return t.fail(err)
}

func (t *agentRuntimeTx) GetLease(id string) (agentrun.WorkspaceLease, error) {
	var v agentrun.WorkspaceLease
	var expires, status, created, updated string
	err := t.tx.QueryRowContext(t.ctx, `SELECT id,grant_id,fencing_token,expires_at,status,created_at,updated_at FROM workspace_lease WHERE id=?`, id).
		Scan(&v.ID, &v.GrantID, &v.FencingToken, &expires, &status, &created, &updated)
	if err == sql.ErrNoRows {
		return v, agentrun.ErrNotFound
	}
	if err != nil {
		return v, err
	}
	v.Status = agentrun.LeaseStatus(status)
	if v.ExpiresAt, err = parseRFC(expires); err != nil {
		return v, err
	}
	if v.CreatedAt, err = parseRFC(created); err != nil {
		return v, err
	}
	if v.UpdatedAt, err = parseRFC(updated); err != nil {
		return v, err
	}
	return v, nil
}

// ── Change sets ─────────────────────────────────────────────────────────────

func (t *agentRuntimeTx) PutChangeSet(v agentrun.ChangeSet) error {
	if err := v.Validate(); err != nil {
		return err
	}
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO change_set(id,run_id,base_digest,approval_digest,status,version,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET status=excluded.status,version=excluded.version,updated_at=excluded.updated_at`,
		v.ID, v.RunID, v.BaseDigest, v.ApprovalDigest, string(v.Status), v.Version, rfc(v.CreatedAt), rfc(v.UpdatedAt))
	return t.fail(err)
}

func (t *agentRuntimeTx) GetChangeSet(id string) (agentrun.ChangeSet, error) {
	var v agentrun.ChangeSet
	var status, created, updated string
	err := t.tx.QueryRowContext(t.ctx, `SELECT id,run_id,base_digest,approval_digest,status,version,created_at,updated_at FROM change_set WHERE id=?`, id).
		Scan(&v.ID, &v.RunID, &v.BaseDigest, &v.ApprovalDigest, &status, &v.Version, &created, &updated)
	if err == sql.ErrNoRows {
		return v, agentrun.ErrNotFound
	}
	if err != nil {
		return v, err
	}
	v.Status = agentrun.ChangeSetStatus(status)
	if v.CreatedAt, err = parseRFC(created); err != nil {
		return v, err
	}
	if v.UpdatedAt, err = parseRFC(updated); err != nil {
		return v, err
	}
	return v, nil
}

// changeSetOperationColumns matches the change_set_operation DDL order.
const changeSetOperationColumns = `id,change_set_id,ordinal,op,path,content,content_digest,original_content,original_digest,applied_digest`

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func (t *agentRuntimeTx) PutChangeSetOperation(v agentrun.ChangeSetOperation) error {
	if err := v.Validate(); err != nil {
		return err
	}
	var content, original any
	if v.Content != nil {
		content = *v.Content
	}
	if v.OriginalContent != nil {
		original = *v.OriginalContent
	}
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO change_set_operation(`+changeSetOperationColumns+`) VALUES(?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET applied_digest=excluded.applied_digest`,
		v.ID, v.ChangeSetID, v.Ordinal, string(v.Op), v.Path, content, nullable(v.ContentDigest), original, nullable(v.OriginalDigest), nullable(v.AppliedDigest))
	return t.fail(err)
}

func scanChangeSetOperation(s interface{ Scan(...any) error }) (agentrun.ChangeSetOperation, error) {
	var v agentrun.ChangeSetOperation
	var op string
	var content, contentDigest, original, originalDigest, appliedDigest sql.NullString
	if err := s.Scan(&v.ID, &v.ChangeSetID, &v.Ordinal, &op, &v.Path, &content, &contentDigest, &original, &originalDigest, &appliedDigest); err != nil {
		return v, err
	}
	v.Op = agentrun.ChangeSetOp(op)
	if content.Valid {
		v.Content = &content.String
	}
	v.ContentDigest = contentDigest.String
	if original.Valid {
		v.OriginalContent = &original.String
	}
	v.OriginalDigest = originalDigest.String
	v.AppliedDigest = appliedDigest.String
	return v, nil
}

func (t *agentRuntimeTx) ListChangeSetOperations(changeSetID string) ([]agentrun.ChangeSetOperation, error) {
	rows, err := t.tx.QueryContext(t.ctx, `SELECT `+changeSetOperationColumns+` FROM change_set_operation WHERE change_set_id=? ORDER BY ordinal`, changeSetID)
	if err != nil {
		return nil, t.fail(err)
	}
	defer rows.Close()
	out := []agentrun.ChangeSetOperation{}
	for rows.Next() {
		v, err := scanChangeSetOperation(rows)
		if err != nil {
			return nil, t.fail(err)
		}
		out = append(out, v)
	}
	return out, t.fail(rows.Err())
}

// ── Command jobs ────────────────────────────────────────────────────────────

func (t *agentRuntimeTx) PutCommandJob(v agentrun.CommandJob) error {
	if err := v.Validate(); err != nil {
		return err
	}
	var exit any
	if v.ExitCode != nil {
		exit = *v.ExitCode
	}
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO command_job(id,run_id,command_spec_digest,status,exit_code,created_at,updated_at) VALUES(?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET status=excluded.status,exit_code=excluded.exit_code,updated_at=excluded.updated_at`,
		v.ID, v.RunID, v.CommandSpecDigest, string(v.Status), exit, rfc(v.CreatedAt), rfc(v.UpdatedAt))
	return t.fail(err)
}

func (t *agentRuntimeTx) GetCommandJob(id string) (agentrun.CommandJob, error) {
	var v agentrun.CommandJob
	var status, created, updated string
	var exit sql.NullInt64
	err := t.tx.QueryRowContext(t.ctx, `SELECT id,run_id,command_spec_digest,status,exit_code,created_at,updated_at FROM command_job WHERE id=?`, id).
		Scan(&v.ID, &v.RunID, &v.CommandSpecDigest, &status, &exit, &created, &updated)
	if err == sql.ErrNoRows {
		return v, agentrun.ErrNotFound
	}
	if err != nil {
		return v, err
	}
	v.Status = agentrun.JobStatus(status)
	if exit.Valid {
		v.ExitCode = &exit.Int64
	}
	if v.CreatedAt, err = parseRFC(created); err != nil {
		return v, err
	}
	if v.UpdatedAt, err = parseRFC(updated); err != nil {
		return v, err
	}
	return v, nil
}

// ListActiveCommandJobs returns every job still in queued/running state.
// Boot-time reconcile resolves these to outcome_unknown.
func (t *agentRuntimeTx) ListActiveCommandJobs() ([]agentrun.CommandJob, error) {
	rows, err := t.tx.QueryContext(t.ctx, `SELECT id,run_id,command_spec_digest,status,exit_code,created_at,updated_at FROM command_job WHERE status IN ('queued','running') ORDER BY created_at`)
	if err != nil {
		return nil, t.fail(err)
	}
	defer rows.Close()
	var out []agentrun.CommandJob
	for rows.Next() {
		var v agentrun.CommandJob
		var status, created, updated string
		var exit sql.NullInt64
		if err := rows.Scan(&v.ID, &v.RunID, &v.CommandSpecDigest, &status, &exit, &created, &updated); err != nil {
			return nil, t.fail(err)
		}
		v.Status = agentrun.JobStatus(status)
		if exit.Valid {
			v.ExitCode = &exit.Int64
		}
		if v.CreatedAt, err = parseRFC(created); err != nil {
			return nil, t.fail(err)
		}
		if v.UpdatedAt, err = parseRFC(updated); err != nil {
			return nil, t.fail(err)
		}
		out = append(out, v)
	}
	return out, t.fail(rows.Err())
}

// ── Run plan ────────────────────────────────────────────────────────────────

func (t *agentRuntimeTx) PutRunPlan(v agentrun.RunPlan) error {
	if err := v.Validate(); err != nil {
		return err
	}
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO run_plan(id,run_id,plan_digest,content_json,version,created_at,updated_at) VALUES(?,?,?,?,?,?,?)
		ON CONFLICT(run_id) DO UPDATE SET plan_digest=excluded.plan_digest,content_json=excluded.content_json,version=excluded.version,updated_at=excluded.updated_at`,
		v.ID, v.RunID, v.PlanDigest, string(v.Content), v.Version, rfc(v.CreatedAt), rfc(v.UpdatedAt))
	return t.fail(err)
}

func (t *agentRuntimeTx) GetRunPlan(runID string) (agentrun.RunPlan, error) {
	var v agentrun.RunPlan
	var content, created, updated string
	err := t.tx.QueryRowContext(t.ctx, `SELECT id,run_id,plan_digest,content_json,version,created_at,updated_at FROM run_plan WHERE run_id=?`, runID).
		Scan(&v.ID, &v.RunID, &v.PlanDigest, &content, &v.Version, &created, &updated)
	if err == sql.ErrNoRows {
		return v, agentrun.ErrNotFound
	}
	if err != nil {
		return v, err
	}
	v.Content = []byte(content)
	if v.CreatedAt, err = parseRFC(created); err != nil {
		return v, err
	}
	if v.UpdatedAt, err = parseRFC(updated); err != nil {
		return v, err
	}
	return v, nil
}

// ── Evidence ────────────────────────────────────────────────────────────────

func (t *agentRuntimeTx) AppendEvidence(v agentrun.Evidence) error {
	if err := v.Validate(); err != nil {
		return err
	}
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO evidence(id,run_id,kind,source_uri,content_digest,captured_at,created_at) VALUES(?,?,?,?,?,?,?)`,
		v.ID, v.RunID, v.Kind, v.SourceURI, v.ContentDigest, rfc(v.CapturedAt), rfc(v.CreatedAt))
	return t.fail(err)
}

func (t *agentRuntimeTx) ListEvidence(runID string) ([]agentrun.Evidence, error) {
	rows, err := t.tx.QueryContext(t.ctx, `SELECT id,run_id,kind,source_uri,content_digest,captured_at,created_at FROM evidence WHERE run_id=? ORDER BY captured_at,id`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []agentrun.Evidence
	for rows.Next() {
		var v agentrun.Evidence
		var captured, created string
		if err = rows.Scan(&v.ID, &v.RunID, &v.Kind, &v.SourceURI, &v.ContentDigest, &captured, &created); err != nil {
			return nil, err
		}
		if v.CapturedAt, err = parseRFC(captured); err != nil {
			return nil, err
		}
		if v.CreatedAt, err = parseRFC(created); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// ── Reviews ─────────────────────────────────────────────────────────────────

func (t *agentRuntimeTx) AppendReview(v agentrun.RunReview) error {
	if err := v.Validate(); err != nil {
		return err
	}
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO run_review(id,run_id,approval_digest,decision,decided_by,decided_at,created_at,action,resource_digest,base_digest,config_digest,policy_digest,descriptor_digest,consumed_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		v.ID, v.RunID, v.ApprovalDigest, string(v.Decision), v.DecidedBy, rfc(v.DecidedAt), rfc(v.CreatedAt), v.Action, v.ResourceDigest, v.BaseDigest, v.ConfigDigest, v.PolicyDigest, v.DescriptorDigest, nullableReviewTime(v.ConsumedAt))
	return t.fail(err)
}

func (t *agentRuntimeTx) ListReviews(runID string) ([]agentrun.RunReview, error) {
	rows, err := t.tx.QueryContext(t.ctx, `SELECT id,run_id,approval_digest,decision,decided_by,decided_at,created_at,action,resource_digest,base_digest,config_digest,policy_digest,descriptor_digest,consumed_at FROM run_review WHERE run_id=? ORDER BY decided_at,id`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []agentrun.RunReview
	for rows.Next() {
		var v agentrun.RunReview
		var decision, decided, created string
		var consumed sql.NullString
		if err = rows.Scan(&v.ID, &v.RunID, &v.ApprovalDigest, &decision, &v.DecidedBy, &decided, &created, &v.Action, &v.ResourceDigest, &v.BaseDigest, &v.ConfigDigest, &v.PolicyDigest, &v.DescriptorDigest, &consumed); err != nil {
			return nil, err
		}
		v.Decision = agentrun.ReviewDecision(decision)
		if v.DecidedAt, err = parseRFC(decided); err != nil {
			return nil, err
		}
		if v.CreatedAt, err = parseRFC(created); err != nil {
			return nil, err
		}
		if consumed.Valid {
			parsed, parseErr := parseRFC(consumed.String)
			if parseErr != nil {
				return nil, parseErr
			}
			v.ConsumedAt = &parsed
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func nullableReviewTime(v *time.Time) any {
	if v == nil {
		return nil
	}
	return rfc(*v)
}

func (t *agentRuntimeTx) ConsumeReview(runID, approvalDigest, action string, at time.Time) (agentrun.RunReview, error) {
	res, err := t.tx.ExecContext(t.ctx, `UPDATE run_review SET consumed_at=? WHERE id=(SELECT id FROM run_review WHERE run_id=? AND approval_digest=? AND action=? AND decision='approved' AND consumed_at IS NULL ORDER BY decided_at DESC LIMIT 1) AND consumed_at IS NULL`, rfc(at), runID, approvalDigest, action)
	if err != nil {
		return agentrun.RunReview{}, t.fail(err)
	}
	n, err := res.RowsAffected()
	if err != nil || n != 1 {
		return agentrun.RunReview{}, agentrun.ErrReviewRequired
	}
	reviews, err := t.ListReviews(runID)
	if err != nil {
		return agentrun.RunReview{}, err
	}
	for i := len(reviews) - 1; i >= 0; i-- {
		if reviews[i].ApprovalDigest == approvalDigest && reviews[i].Action == action && reviews[i].ConsumedAt != nil {
			return reviews[i], nil
		}
	}
	return agentrun.RunReview{}, agentrun.ErrReviewRequired
}

// ── Idempotency and audit (same writer transaction) ────────────────────────

// Idempotency looks up a stored idempotency record, reclaiming expired
// entries while holding the writer lock (same semantics as txAdapter).
func (t *agentRuntimeTx) Idempotency(op, key string, now time.Time) (providerapp.Record, bool, error) {
	if _, err := t.tx.ExecContext(t.ctx, `DELETE FROM idempotency_records WHERE operation=? AND idempotency_key=? AND expires_at<=?`, op, key, rfc(now)); err != nil {
		return providerapp.Record{}, false, t.fail(err)
	}
	var r providerapp.Record
	var response, created, expires string
	err := t.tx.QueryRowContext(t.ctx, `SELECT request_digest,response_json,created_at,expires_at FROM idempotency_records WHERE operation=? AND idempotency_key=?`, op, key).Scan(&r.Digest, &response, &created, &expires)
	if err == sql.ErrNoRows {
		return r, false, nil
	}
	if err != nil {
		return r, false, t.fail(err)
	}
	r.Operation, r.Key, r.Response = op, key, []byte(response)
	if r.CreatedAt, err = parseRFC(created); err != nil {
		return r, false, t.fail(err)
	}
	if r.ExpiresAt, err = parseRFC(expires); err != nil {
		return r, false, t.fail(err)
	}
	return r, true, nil
}

func (t *agentRuntimeTx) PutIdempotency(r providerapp.Record) error {
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO idempotency_records(operation,idempotency_key,request_digest,response_json,created_at,expires_at) VALUES(?,?,?,?,?,?)`, r.Operation, r.Key, r.Digest, string(r.Response), rfc(r.CreatedAt), rfc(r.ExpiresAt))
	return t.fail(err)
}

func (t *agentRuntimeTx) PutAudit(a providerapp.Audit) error {
	return t.fail(appendAuditChained(t.ctx, t.tx, a.ID, a.Action, a.AggregateID, a.Actor, string(a.Metadata), rfc(a.CreatedAt)))
}

// TransactAgentRuntime runs an agentrunapp use case in the same single-writer
// transaction as Transact, with idempotency and audit available on the Tx.
func (r *AgentRuntimeRepository) TransactAgentRuntime(ctx context.Context, fn func(agentrunapp.Tx) error) error {
	return r.Transact(ctx, func(tx agentrun.Tx) error {
		appTx, ok := tx.(agentrunapp.Tx)
		if !ok {
			return errors.New("agent runtime tx does not satisfy agentrunapp.Tx")
		}
		return fn(appTx)
	})
}

var (
	_ agentrun.Repository = (*AgentRuntimeRepository)(nil)
	_ agentrun.Tx         = (*agentRuntimeTx)(nil)
	_ agentrunapp.Tx      = (*agentRuntimeTx)(nil)
)
