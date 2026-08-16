// M8 slice-4 storage (T-8.4.x): workflow_bundles / automation_runs on the
// agent-runtime single-writer transaction. automation_runs is a projection
// of M5/M6 canonical runs: this file only writes rows, never advances the
// execution kernel state machine (migration 0064).
package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"github.com/lunitide/lunitide/internal/domain/agentrun"
	"github.com/lunitide/lunitide/internal/domain/m8core"
	"github.com/lunitide/lunitide/internal/m8app"
)

// TransactAutomation runs an m8app slice-4 use case on the shared
// single-writer transaction.
func (r *AgentRuntimeRepository) TransactAutomation(ctx context.Context, fn func(m8app.AutomationTx) error) error {
	return r.Transact(ctx, func(tx agentrun.Tx) error {
		atx, ok := tx.(m8app.AutomationTx)
		if !ok {
			return errors.New("agent runtime tx does not satisfy m8app.AutomationTx")
		}
		return fn(atx)
	})
}

const m8bundleColumns = `id,version,checksum,permissions,rollback_ref,state,created_at`

func scanBundle(s interface{ Scan(...any) error }) (m8core.WorkflowBundle, error) {
	var b m8core.WorkflowBundle
	var rollback *string
	err := s.Scan(&b.ID, &b.Version, &b.Checksum, &b.Permissions, &rollback,
		&b.State, &b.CreatedAt)
	if rollback != nil {
		b.RollbackRef = *rollback
	}
	return b, err
}

func (t *agentRuntimeTx) GetBundle(bundleID string) (m8core.WorkflowBundle, error) {
	b, err := scanBundle(t.tx.QueryRowContext(t.ctx,
		`SELECT `+m8bundleColumns+` FROM workflow_bundles WHERE id=?`, bundleID))
	if errors.Is(err, sql.ErrNoRows) {
		return b, m8core.ErrNotFound
	}
	return b, t.fail(err)
}

func (t *agentRuntimeTx) PutBundle(b m8core.WorkflowBundle) error {
	var rollback any
	if b.RollbackRef != "" {
		rollback = b.RollbackRef
	}
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO workflow_bundles
		(id,version,checksum,permissions,rollback_ref,state,created_at)
		VALUES(?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET
			version=excluded.version,
			checksum=excluded.checksum,
			permissions=excluded.permissions,
			rollback_ref=excluded.rollback_ref,
			state=excluded.state`,
		b.ID, b.Version, b.Checksum, b.Permissions, rollback, b.State, b.CreatedAt)
	return t.fail(err)
}

const m8runColumns = `id,bundle_id,state,approval_ref,budget_json,checkpoint_json,idempotency_key,input_digest,created_at`

func scanAutomationRun(s interface{ Scan(...any) error }) (m8core.AutomationRun, error) {
	var r m8core.AutomationRun
	var approval, checkpoint *string
	err := s.Scan(&r.ID, &r.BundleID, &r.State, &approval, &r.BudgetJSON,
		&checkpoint, &r.IdempotencyKey, &r.InputDigest, &r.CreatedAt)
	if approval != nil {
		r.ApprovalRef = *approval
	}
	if checkpoint != nil {
		r.CheckpointJSON = *checkpoint
	}
	return r, err
}

// GetRunByIdempotencyKey answers the run a requestId already produced
// (migration 0064 unique index backs the replay).
func (t *agentRuntimeTx) GetRunByIdempotencyKey(key string) (m8core.AutomationRun, bool, error) {
	r, err := scanAutomationRun(t.tx.QueryRowContext(t.ctx,
		`SELECT `+m8runColumns+` FROM automation_runs WHERE idempotency_key=?`, key))
	if errors.Is(err, sql.ErrNoRows) {
		return m8core.AutomationRun{}, false, nil
	}
	if err != nil {
		return m8core.AutomationRun{}, false, t.fail(err)
	}
	return r, true, nil
}

func (t *agentRuntimeTx) PutAutomationRun(r m8core.AutomationRun) error {
	var approval, checkpoint any
	if r.ApprovalRef != "" {
		approval = r.ApprovalRef
	}
	if r.CheckpointJSON != "" {
		checkpoint = r.CheckpointJSON
	}
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO automation_runs
		(id,bundle_id,state,approval_ref,budget_json,checkpoint_json,idempotency_key,input_digest,created_at)
		VALUES(?,?,?,?,?,?,?,?,?)`,
		r.ID, r.BundleID, r.State, approval, r.BudgetJSON, checkpoint,
		r.IdempotencyKey, r.InputDigest, r.CreatedAt)
	return t.fail(err)
}
