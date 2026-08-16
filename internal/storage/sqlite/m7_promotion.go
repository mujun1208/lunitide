// M7 slice 4 storage (T-7.4.x): promotions, migration_executions,
// deployments and rollback_attempts on the agent-runtime single-writer
// transaction. agentRuntimeTx additionally satisfies m7app.PromotionTx;
// TransactPromotion asserts the interface at open time. Rollback attempts
// are append-only evidence - only the state columns ever move (the
// migration-0056 trigger guards deletes, M7-RBK-002).
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/lunitide/lunitide/internal/domain/agentrun"
	"github.com/lunitide/lunitide/internal/domain/m7flow"
	"github.com/lunitide/lunitide/internal/m7app"
)

// TransactPromotion runs an m7app slice-4 use case on the shared
// single-writer transaction.
func (r *AgentRuntimeRepository) TransactPromotion(ctx context.Context, fn func(m7app.PromotionTx) error) error {
	return r.Transact(ctx, func(tx agentrun.Tx) error {
		ptx, ok := tx.(m7app.PromotionTx)
		if !ok {
			return errors.New("agent runtime tx does not satisfy m7app.PromotionTx")
		}
		return fn(ptx)
	})
}

const prmColumns = `id,package_id,from_env,to_env,canonical_intent_digest,policy_version,approval_expiry,state,idempotency_key,requested_by,created_at,updated_at`

func scanPromotion(s interface{ Scan(...any) error }) (m7flow.Promotion, error) {
	var p m7flow.Promotion
	var expiry sql.NullString
	var created, updated string
	if err := s.Scan(&p.ID, &p.PackageID, &p.FromEnv, &p.ToEnv, &p.CanonicalIntentDigest,
		&p.PolicyVersion, &expiry, &p.State, &p.IdempotencyKey, &p.RequestedBy, &created, &updated); err != nil {
		return p, err
	}
	var err error
	if p.CreatedAt, err = parseRFC(created); err != nil {
		return p, err
	}
	if p.UpdatedAt, err = parseRFC(updated); err != nil {
		return p, err
	}
	if expiry.Valid {
		t, err := parseRFC(expiry.String)
		if err != nil {
			return p, err
		}
		p.ApprovalExpiry = &t
	}
	return p, nil
}

func (t *agentRuntimeTx) PutPromotion(p m7flow.Promotion) error {
	var expiry any
	if p.ApprovalExpiry != nil {
		expiry = rfc(*p.ApprovalExpiry)
	}
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO promotions
		(id,package_id,from_env,to_env,canonical_intent_digest,policy_version,approval_expiry,state,idempotency_key,requested_by,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		p.ID, p.PackageID, p.FromEnv, p.ToEnv, p.CanonicalIntentDigest,
		p.PolicyVersion, expiry, p.State, p.IdempotencyKey, p.RequestedBy,
		rfc(p.CreatedAt), rfc(p.UpdatedAt))
	return t.fail(err)
}

func (t *agentRuntimeTx) GetPromotion(id string) (m7flow.Promotion, error) {
	p, err := scanPromotion(t.tx.QueryRowContext(t.ctx,
		`SELECT `+prmColumns+` FROM promotions WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return p, m7flow.ErrNotFound
	}
	return p, t.fail(err)
}

func (t *agentRuntimeTx) FindPromotionByIdempotencyKey(key string) (m7flow.Promotion, error) {
	p, err := scanPromotion(t.tx.QueryRowContext(t.ctx,
		`SELECT `+prmColumns+` FROM promotions WHERE idempotency_key=?`, key))
	if errors.Is(err, sql.ErrNoRows) {
		return p, m7flow.ErrNotFound
	}
	return p, t.fail(err)
}

// FindActivePromotion answers the still-running saga on one package+env
// edge (terminal states never block a new promotion).
func (t *agentRuntimeTx) FindActivePromotion(packageID, toEnv string) (m7flow.Promotion, error) {
	p, err := scanPromotion(t.tx.QueryRowContext(t.ctx,
		`SELECT `+prmColumns+` FROM promotions
		 WHERE package_id=? AND to_env=?
		   AND state NOT IN ('denied','expired','succeeded','failed','rolled_back','rollback_failed')
		 ORDER BY created_at LIMIT 1`, packageID, toEnv))
	if errors.Is(err, sql.ErrNoRows) {
		return p, m7flow.ErrNotFound
	}
	return p, t.fail(err)
}

func (t *agentRuntimeTx) FindLastSucceededByPackage(packageID string) (m7flow.Promotion, error) {
	p, err := scanPromotion(t.tx.QueryRowContext(t.ctx,
		`SELECT `+prmColumns+` FROM promotions
		 WHERE package_id=? AND state='succeeded'
		 ORDER BY CASE to_env WHEN 'none' THEN 0 WHEN 'dev' THEN 1 WHEN 'stage' THEN 2
		                  WHEN 'prod' THEN 3 ELSE -1 END DESC, updated_at DESC, id DESC
		 LIMIT 1`, packageID))
	if errors.Is(err, sql.ErrNoRows) {
		return p, m7flow.ErrNotFound
	}
	return p, t.fail(err)
}

func (t *agentRuntimeTx) FindLastSucceededByEnv(toEnv string) (m7flow.Promotion, error) {
	p, err := scanPromotion(t.tx.QueryRowContext(t.ctx,
		`SELECT `+prmColumns+` FROM promotions
		 WHERE to_env=? AND state='succeeded' ORDER BY updated_at DESC LIMIT 1`, toEnv))
	if errors.Is(err, sql.ErrNoRows) {
		return p, m7flow.ErrNotFound
	}
	return p, t.fail(err)
}

// UpdatePromotionState performs one legal state transition (CAS on from).
func (t *agentRuntimeTx) UpdatePromotionState(id, from, to string, updatedAt time.Time) error {
	res, err := t.tx.ExecContext(t.ctx,
		`UPDATE promotions SET state=?, updated_at=? WHERE id=? AND state=?`,
		to, rfc(updatedAt), id, from)
	if err != nil {
		return t.fail(err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return t.fail(sql.ErrNoRows)
	}
	return nil
}

const migColumns = `id,promotion_id,plan_digest,state,rollback_ref,created_at`

func scanMigration(s interface{ Scan(...any) error }) (m7flow.MigrationExecution, error) {
	var m m7flow.MigrationExecution
	var rollback sql.NullString
	var created string
	if err := s.Scan(&m.ID, &m.PromotionID, &m.PlanDigest, &m.State, &rollback, &created); err != nil {
		return m, err
	}
	if rollback.Valid {
		m.RollbackRef = rollback.String
	}
	var err error
	if m.CreatedAt, err = parseRFC(created); err != nil {
		return m, err
	}
	return m, nil
}

func (t *agentRuntimeTx) PutMigrationExecution(m m7flow.MigrationExecution) error {
	var rollback any
	if m.RollbackRef != "" {
		rollback = m.RollbackRef
	}
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO migration_executions
		(id,promotion_id,plan_digest,state,rollback_ref,created_at) VALUES(?,?,?,?,?,?)`,
		m.ID, m.PromotionID, m.PlanDigest, m.State, rollback, rfc(m.CreatedAt))
	return t.fail(err)
}

func (t *agentRuntimeTx) FindMigrationExecution(promotionID string) (m7flow.MigrationExecution, error) {
	m, err := scanMigration(t.tx.QueryRowContext(t.ctx,
		`SELECT `+migColumns+` FROM migration_executions
		 WHERE promotion_id=? ORDER BY created_at LIMIT 1`, promotionID))
	if errors.Is(err, sql.ErrNoRows) {
		return m, m7flow.ErrNotFound
	}
	return m, t.fail(err)
}

func (t *agentRuntimeTx) ListMigrationExecutions(promotionID string) ([]m7flow.MigrationExecution, error) {
	rows, err := t.tx.QueryContext(t.ctx,
		`SELECT `+migColumns+` FROM migration_executions WHERE promotion_id=? ORDER BY created_at`, promotionID)
	if err != nil {
		return nil, t.fail(err)
	}
	defer rows.Close()
	var out []m7flow.MigrationExecution
	for rows.Next() {
		m, err := scanMigration(rows)
		if err != nil {
			return nil, t.fail(err)
		}
		out = append(out, m)
	}
	return out, t.fail(rows.Err())
}

// UpdateMigrationExecution performs one legal state transition; the
// rollback reference always carries the freshest value.
func (t *agentRuntimeTx) UpdateMigrationExecution(id, from, to, rollbackRef string) error {
	res, err := t.tx.ExecContext(t.ctx,
		`UPDATE migration_executions SET state=?, rollback_ref=? WHERE id=? AND state=?`,
		to, rollbackRef, id, from)
	if err != nil {
		return t.fail(err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return t.fail(sql.ErrNoRows)
	}
	return nil
}

const depColumns = `id,promotion_id,target_env,state,started_at,completed_at,receipt_json`

func scanDeployment(s interface{ Scan(...any) error }) (m7flow.Deployment, error) {
	var d m7flow.Deployment
	var started, completed sql.NullString
	var receipt sql.NullString
	if err := s.Scan(&d.ID, &d.PromotionID, &d.TargetEnv, &d.State, &started, &completed, &receipt); err != nil {
		return d, err
	}
	var err error
	if started.Valid {
		t, err := parseRFC(started.String)
		if err != nil {
			return d, err
		}
		d.StartedAt = &t
	}
	if completed.Valid {
		t, err2 := parseRFC(completed.String)
		if err2 != nil {
			return d, err2
		}
		d.CompletedAt = &t
	}
	if receipt.Valid {
		d.ReceiptJSON = receipt.String
	}
	return d, err
}

func (t *agentRuntimeTx) PutDeployment(d m7flow.Deployment) error {
	var started, completed, receipt any
	if d.StartedAt != nil {
		started = rfc(*d.StartedAt)
	}
	if d.CompletedAt != nil {
		completed = rfc(*d.CompletedAt)
	}
	if d.ReceiptJSON != "" {
		receipt = d.ReceiptJSON
	}
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO deployments
		(id,promotion_id,target_env,state,started_at,completed_at,receipt_json) VALUES(?,?,?,?,?,?,?)`,
		d.ID, d.PromotionID, d.TargetEnv, d.State, started, completed, receipt)
	return t.fail(err)
}

func (t *agentRuntimeTx) FindDeployment(promotionID string) (m7flow.Deployment, error) {
	d, err := scanDeployment(t.tx.QueryRowContext(t.ctx,
		`SELECT `+depColumns+` FROM deployments
		 WHERE promotion_id=? ORDER BY started_at LIMIT 1`, promotionID))
	if errors.Is(err, sql.ErrNoRows) {
		return d, m7flow.ErrNotFound
	}
	return d, t.fail(err)
}

func (t *agentRuntimeTx) ListDeployments(promotionID string) ([]m7flow.Deployment, error) {
	rows, err := t.tx.QueryContext(t.ctx,
		`SELECT `+depColumns+` FROM deployments WHERE promotion_id=? ORDER BY started_at`, promotionID)
	if err != nil {
		return nil, t.fail(err)
	}
	defer rows.Close()
	var out []m7flow.Deployment
	for rows.Next() {
		d, err := scanDeployment(rows)
		if err != nil {
			return nil, t.fail(err)
		}
		out = append(out, d)
	}
	return out, t.fail(rows.Err())
}

// UpdateDeploymentState performs one legal state transition; nil timestamps
// keep the stored value (COALESCE).
func (t *agentRuntimeTx) UpdateDeploymentState(id, from, to, receipt string, startedAt, completedAt *time.Time) error {
	var started, completed any
	if startedAt != nil {
		started = rfc(*startedAt)
	}
	if completedAt != nil {
		completed = rfc(*completedAt)
	}
	res, err := t.tx.ExecContext(t.ctx,
		`UPDATE deployments SET state=?, receipt_json=COALESCE(?, receipt_json),
		 started_at=COALESCE(?, started_at), completed_at=COALESCE(?, completed_at)
		 WHERE id=? AND state=?`,
		to, receipt, started, completed, id, from)
	if err != nil {
		return t.fail(err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return t.fail(sql.ErrNoRows)
	}
	return nil
}

const rbkColumns = `id,promotion_id,dimension,state,plan_digest,operator_id,result_json,created_at,completed_at`

func scanRollbackAttempt(s interface{ Scan(...any) error }) (m7flow.RollbackAttempt, error) {
	var r m7flow.RollbackAttempt
	var completed sql.NullString
	var created string
	if err := s.Scan(&r.ID, &r.PromotionID, &r.Dimension, &r.State, &r.PlanDigest,
		&r.OperatorID, &r.ResultJSON, &created, &completed); err != nil {
		return r, err
	}
	var err error
	if r.CreatedAt, err = parseRFC(created); err != nil {
		return r, err
	}
	if completed.Valid {
		t, err := parseRFC(completed.String)
		if err != nil {
			return r, err
		}
		r.CompletedAt = &t
	}
	return r, nil
}

func (t *agentRuntimeTx) PutRollbackAttempt(r m7flow.RollbackAttempt) error {
	var completed any
	if r.CompletedAt != nil {
		completed = rfc(*r.CompletedAt)
	}
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO rollback_attempts
		(id,promotion_id,dimension,state,plan_digest,operator_id,result_json,created_at,completed_at)
		VALUES(?,?,?,?,?,?,?,?,?)`,
		r.ID, r.PromotionID, r.Dimension, r.State, r.PlanDigest,
		r.OperatorID, r.ResultJSON, rfc(r.CreatedAt), completed)
	return t.fail(err)
}

func (t *agentRuntimeTx) ListRollbackAttempts(promotionID string) ([]m7flow.RollbackAttempt, error) {
	rows, err := t.tx.QueryContext(t.ctx,
		`SELECT `+rbkColumns+` FROM rollback_attempts WHERE promotion_id=? ORDER BY created_at`, promotionID)
	if err != nil {
		return nil, t.fail(err)
	}
	defer rows.Close()
	var out []m7flow.RollbackAttempt
	for rows.Next() {
		r, err := scanRollbackAttempt(rows)
		if err != nil {
			return nil, t.fail(err)
		}
		out = append(out, r)
	}
	return out, t.fail(rows.Err())
}

// UpdateRollbackAttempt moves one attempt along pending -> running ->
// succeeded | failed; rows are never deleted (M7-RBK-002).
func (t *agentRuntimeTx) UpdateRollbackAttempt(id, from, to, resultJSON string, completedAt *time.Time) error {
	var completed any
	if completedAt != nil {
		completed = rfc(*completedAt)
	}
	res, err := t.tx.ExecContext(t.ctx,
		`UPDATE rollback_attempts SET state=?, result_json=?, completed_at=COALESCE(?, completed_at)
		 WHERE id=? AND state=?`,
		to, resultJSON, completed, id, from)
	if err != nil {
		return t.fail(err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return t.fail(sql.ErrNoRows)
	}
	return nil
}