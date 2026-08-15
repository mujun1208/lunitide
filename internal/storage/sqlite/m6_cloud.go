// Cloud execution registry persistence (migration 0053).
package sqlite

import (
	"database/sql"
	"errors"
	"time"

	"github.com/lunitide/lunitide/internal/domain/m6supply"
)

// ── CloudRunner ─────────────────────────────────────────────────────────────

const m6RunnerColumns = `id,region,workload_identity,attestation_digest,attestation_status,mtls_fingerprint,capabilities,state,version,created_at,updated_at`

func scanM6CloudRunner(s interface{ Scan(...any) error }) (m6supply.CloudRunner, error) {
	var r m6supply.CloudRunner
	var created, updated string
	if err := s.Scan(&r.ID, &r.Region, &r.WorkloadIdentity, &r.AttestationDigest, &r.AttestationStatus,
		&r.MTLSFingerprint, &r.Capabilities, &r.State, &r.Version, &created, &updated); err != nil {
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

func (t *agentRuntimeTx) PutM6CloudRunner(r m6supply.CloudRunner) error {
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO m6_cloudrunner
		(id,region,workload_identity,attestation_digest,attestation_status,mtls_fingerprint,capabilities,state,version,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		r.ID, r.Region, r.WorkloadIdentity, r.AttestationDigest, r.AttestationStatus,
		r.MTLSFingerprint, r.Capabilities, r.State, r.Version, rfc(r.CreatedAt), rfc(r.UpdatedAt))
	return t.fail(err)
}

func (t *agentRuntimeTx) GetM6CloudRunner(id string) (m6supply.CloudRunner, error) {
	r, err := scanM6CloudRunner(t.tx.QueryRowContext(t.ctx, `SELECT `+m6RunnerColumns+` FROM m6_cloudrunner WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return r, m6supply.ErrNotFound
	}
	return r, err
}

func (t *agentRuntimeTx) FindM6CloudRunnerByIdentity(workloadIdentity string) (m6supply.CloudRunner, error) {
	r, err := scanM6CloudRunner(t.tx.QueryRowContext(t.ctx, `SELECT `+m6RunnerColumns+` FROM m6_cloudrunner WHERE workload_identity=?`, workloadIdentity))
	if errors.Is(err, sql.ErrNoRows) {
		return r, m6supply.ErrNotFound
	}
	return r, err
}

func (t *agentRuntimeTx) ListM6CloudRunnersByState(state string) ([]m6supply.CloudRunner, error) {
	rows, err := t.tx.QueryContext(t.ctx, `SELECT `+m6RunnerColumns+` FROM m6_cloudrunner WHERE state=? ORDER BY created_at`, state)
	if err != nil {
		return nil, t.fail(err)
	}
	defer rows.Close()
	var out []m6supply.CloudRunner
	for rows.Next() {
		r, err := scanM6CloudRunner(rows)
		if err != nil {
			return nil, t.fail(err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// TransitionM6CloudRunner CAS-updates the runner state.
func (t *agentRuntimeTx) TransitionM6CloudRunner(id string, expectedVersion int64, to string, at time.Time) (m6supply.CloudRunner, error) {
	res, err := t.tx.ExecContext(t.ctx, `UPDATE m6_cloudrunner SET state=?, version=version+1, updated_at=? WHERE id=? AND version=?`,
		to, rfc(at), id, expectedVersion)
	if err != nil {
		return m6supply.CloudRunner{}, t.fail(err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return m6supply.CloudRunner{}, m6supply.ErrVersionConflict
	}
	return t.GetM6CloudRunner(id)
}

// ── RegionPolicy ────────────────────────────────────────────────────────────

const m6RegionPolicyColumns = `id,version,allowed_regions,egress_policy,data_classification,created_at`

func (t *agentRuntimeTx) PutM6RegionPolicy(p m6supply.RegionPolicy) error {
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO m6_region_policy
		(id,version,allowed_regions,egress_policy,data_classification,created_at)
		VALUES(?,?,?,?,?,?)`,
		p.ID, p.Version, p.AllowedRegions, p.EgressPolicy, p.DataClassification, rfc(p.CreatedAt))
	return t.fail(err)
}

func (t *agentRuntimeTx) LatestM6RegionPolicy() (m6supply.RegionPolicy, error) {
	row := t.tx.QueryRowContext(t.ctx, `SELECT `+m6RegionPolicyColumns+` FROM m6_region_policy ORDER BY version DESC LIMIT 1`)
	var p m6supply.RegionPolicy
	var created string
	err := row.Scan(&p.ID, &p.Version, &p.AllowedRegions, &p.EgressPolicy, &p.DataClassification, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return p, m6supply.ErrNotFound
	}
	if err != nil {
		return p, err
	}
	if p.CreatedAt, err = parseRFC(created); err != nil {
		return p, err
	}
	return p, nil
}

func (t *agentRuntimeTx) MaxM6RegionPolicyVersion() (int64, error) {
	var v sql.NullInt64
	if err := t.tx.QueryRowContext(t.ctx, `SELECT max(version) FROM m6_region_policy`).Scan(&v); err != nil {
		return 0, err
	}
	return v.Int64, nil
}

// ── WorkerLease ─────────────────────────────────────────────────────────────

const m6LeaseColumns = `id,runner_id,task_id,epoch,expires_at,state,created_at,updated_at`

func scanM6WorkerLease(s interface{ Scan(...any) error }) (m6supply.WorkerLease, error) {
	var l m6supply.WorkerLease
	var expires, created, updated string
	if err := s.Scan(&l.ID, &l.RunnerID, &l.TaskID, &l.Epoch, &expires, &l.State, &created, &updated); err != nil {
		return l, err
	}
	var err error
	if l.ExpiresAt, err = parseRFC(expires); err != nil {
		return l, err
	}
	if l.CreatedAt, err = parseRFC(created); err != nil {
		return l, err
	}
	if l.UpdatedAt, err = parseRFC(updated); err != nil {
		return l, err
	}
	return l, nil
}

func (t *agentRuntimeTx) PutM6WorkerLease(l m6supply.WorkerLease) error {
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO m6_worker_lease
		(id,runner_id,task_id,epoch,expires_at,state,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?)`,
		l.ID, l.RunnerID, l.TaskID, l.Epoch, rfc(l.ExpiresAt), l.State, rfc(l.CreatedAt), rfc(l.UpdatedAt))
	return t.fail(err)
}

func (t *agentRuntimeTx) GetM6WorkerLeaseByTask(taskID string) (m6supply.WorkerLease, error) {
	l, err := scanM6WorkerLease(t.tx.QueryRowContext(t.ctx, `SELECT `+m6LeaseColumns+` FROM m6_worker_lease WHERE task_id=?`, taskID))
	if errors.Is(err, sql.ErrNoRows) {
		return l, m6supply.ErrNotFound
	}
	return l, err
}

// TransitionM6WorkerLease CAS-updates the lease state under the fencing
// epoch: a write carrying an epoch <= the stored epoch loses outright.
func (t *agentRuntimeTx) TransitionM6WorkerLease(id string, expectedEpoch int64, to string, at time.Time) (m6supply.WorkerLease, error) {
	res, err := t.tx.ExecContext(t.ctx, `UPDATE m6_worker_lease SET state=?, epoch=?, updated_at=? WHERE id=? AND epoch<?`,
		to, expectedEpoch, rfc(at), id, expectedEpoch)
	if err != nil {
		return m6supply.WorkerLease{}, t.fail(err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return m6supply.WorkerLease{}, m6supply.ErrVersionConflict
	}
	return t.GetM6WorkerLeaseByID(id)
}

// RenewM6WorkerLease re-leases the row (task_id is UNIQUE, so re-leasing
// reuses the row) to a runner at a strictly higher fencing epoch: state
// back to active, runner repointed, expiry reset. A writer carrying an
// epoch <= the stored epoch loses.
func (t *agentRuntimeTx) RenewM6WorkerLease(id string, newEpoch int64, runnerID string, expiresAt, at time.Time) (m6supply.WorkerLease, error) {
	res, err := t.tx.ExecContext(t.ctx, `UPDATE m6_worker_lease SET state='active', epoch=?, runner_id=?, expires_at=?, updated_at=? WHERE id=? AND epoch<?`,
		newEpoch, runnerID, rfc(expiresAt), rfc(at), id, newEpoch)
	if err != nil {
		return m6supply.WorkerLease{}, t.fail(err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return m6supply.WorkerLease{}, m6supply.ErrVersionConflict
	}
	return t.GetM6WorkerLeaseByID(id)
}

func (t *agentRuntimeTx) GetM6WorkerLeaseByID(id string) (m6supply.WorkerLease, error) {
	l, err := scanM6WorkerLease(t.tx.QueryRowContext(t.ctx, `SELECT `+m6LeaseColumns+` FROM m6_worker_lease WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return l, m6supply.ErrNotFound
	}
	return l, err
}

// ExpireM6WorkerLeases moves every active lease whose expiry passed to
// expired, bumping its epoch (the next lease on the same task must carry
// a higher epoch). Returns the number of leases expired.
func (t *agentRuntimeTx) ExpireM6WorkerLeases(now time.Time) (int64, error) {
	res, err := t.tx.ExecContext(t.ctx, `UPDATE m6_worker_lease SET state='expired', epoch=epoch+1, updated_at=? WHERE state='active' AND expires_at<=?`,
		rfc(now), rfc(now))
	if err != nil {
		return 0, t.fail(err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// ── RemoteReceipt ───────────────────────────────────────────────────────────

const m6ReceiptColumns = `id,task_id,runner_id,outcome,result_digest,usage,received_at,reconcile_state,version,created_at,updated_at`

func scanM6RemoteReceipt(s interface{ Scan(...any) error }) (m6supply.RemoteReceipt, error) {
	var r m6supply.RemoteReceipt
	var resultDigest sql.NullString
	var received, created, updated string
	if err := s.Scan(&r.ID, &r.TaskID, &r.RunnerID, &r.Outcome, &resultDigest, &r.Usage, &received, &r.ReconcileState, &r.Version, &created, &updated); err != nil {
		return r, err
	}
	r.ResultDigest = resultDigest.String
	var err error
	if r.ReceivedAt, err = parseRFC(received); err != nil {
		return r, err
	}
	if r.CreatedAt, err = parseRFC(created); err != nil {
		return r, err
	}
	if r.UpdatedAt, err = parseRFC(updated); err != nil {
		return r, err
	}
	return r, nil
}

func (t *agentRuntimeTx) PutM6RemoteReceipt(r m6supply.RemoteReceipt) error {
	var resultDigest any
	if r.ResultDigest != "" {
		resultDigest = r.ResultDigest
	}
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO m6_remote_receipt
		(id,task_id,runner_id,outcome,result_digest,usage,received_at,reconcile_state,version,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		r.ID, r.TaskID, r.RunnerID, r.Outcome, resultDigest, r.Usage, rfc(r.ReceivedAt),
		r.ReconcileState, r.Version, rfc(r.CreatedAt), rfc(r.UpdatedAt))
	return t.fail(err)
}

func (t *agentRuntimeTx) GetM6RemoteReceipt(id string) (m6supply.RemoteReceipt, error) {
	r, err := scanM6RemoteReceipt(t.tx.QueryRowContext(t.ctx, `SELECT `+m6ReceiptColumns+` FROM m6_remote_receipt WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return r, m6supply.ErrNotFound
	}
	return r, err
}

func (t *agentRuntimeTx) ListM6RemoteReceiptsByTask(taskID string) ([]m6supply.RemoteReceipt, error) {
	rows, err := t.tx.QueryContext(t.ctx, `SELECT `+m6ReceiptColumns+` FROM m6_remote_receipt WHERE task_id=? ORDER BY received_at`, taskID)
	if err != nil {
		return nil, t.fail(err)
	}
	defer rows.Close()
	var out []m6supply.RemoteReceipt
	for rows.Next() {
		r, err := scanM6RemoteReceipt(rows)
		if err != nil {
			return nil, t.fail(err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (t *agentRuntimeTx) ListM6PendingRemoteReceipts(limit int) ([]m6supply.RemoteReceipt, error) {
	rows, err := t.tx.QueryContext(t.ctx, `SELECT `+m6ReceiptColumns+` FROM m6_remote_receipt WHERE reconcile_state='pending' ORDER BY received_at LIMIT ?`, limit)
	if err != nil {
		return nil, t.fail(err)
	}
	defer rows.Close()
	var out []m6supply.RemoteReceipt
	for rows.Next() {
		r, err := scanM6RemoteReceipt(rows)
		if err != nil {
			return nil, t.fail(err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// SetM6ReceiptReconcileState CAS-updates the reconcile state.
func (t *agentRuntimeTx) SetM6ReceiptReconcileState(id string, expectedVersion int64, state string, at time.Time) error {
	res, err := t.tx.ExecContext(t.ctx, `UPDATE m6_remote_receipt SET reconcile_state=?, version=version+1, updated_at=? WHERE id=? AND version=?`,
		state, rfc(at), id, expectedVersion)
	if err != nil {
		return t.fail(err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return m6supply.ErrVersionConflict
	}
	return nil
}

// ── ReconcileDecision ───────────────────────────────────────────────────────

func (t *agentRuntimeTx) PutM6ReconcileDecision(d m6supply.ReconcileDecision) error {
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO m6_reconcile_decision
		(id,receipt_id,task_id,decision,reason,decided_at,created_at)
		VALUES(?,?,?,?,?,?,?)`,
		d.ID, d.ReceiptID, d.TaskID, d.Decision, d.Reason, rfc(d.DecidedAt), rfc(d.CreatedAt))
	return t.fail(err)
}

func (t *agentRuntimeTx) ListM6ReconcileDecisionsByTask(taskID string) ([]m6supply.ReconcileDecision, error) {
	rows, err := t.tx.QueryContext(t.ctx, `SELECT id,receipt_id,task_id,decision,reason,decided_at,created_at FROM m6_reconcile_decision WHERE task_id=? ORDER BY decided_at`, taskID)
	if err != nil {
		return nil, t.fail(err)
	}
	defer rows.Close()
	var out []m6supply.ReconcileDecision
	for rows.Next() {
		var d m6supply.ReconcileDecision
		var decided, created string
		if err := rows.Scan(&d.ID, &d.ReceiptID, &d.TaskID, &d.Decision, &d.Reason, &decided, &created); err != nil {
			return nil, t.fail(err)
		}
		var err error
		if d.DecidedAt, err = parseRFC(decided); err != nil {
			return nil, t.fail(err)
		}
		if d.CreatedAt, err = parseRFC(created); err != nil {
			return nil, t.fail(err)
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
