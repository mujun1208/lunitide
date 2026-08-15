// T-6.3.x storage: delegation envelopes, budget accounts and join
// barriers (0046). Optimistic concurrency everywhere: updates match the
// expected version and bump it; zero rows affected answers ErrVersionConflict
// (or ErrNotFound when the row is gone).
package sqlite

import (
	"database/sql"
	"errors"
	"time"

	"github.com/lunitide/lunitide/internal/domain/m6supply"
)

// --- budget accounts -------------------------------------------------------

func (t *agentRuntimeTx) GetM6BudgetAccount(rootID, dimension string) (m6supply.BudgetAccount, error) {
	var b m6supply.BudgetAccount
	var created, updated string
	err := t.tx.QueryRowContext(t.ctx, `SELECT id, root_id, dimension, granted, reserved,
		consumed, refundable, version, created_at, updated_at
		FROM m6_budget_account WHERE root_id = ? AND dimension = ?`, rootID, dimension).
		Scan(&b.ID, &b.RootID, &b.Dimension, &b.Granted, &b.Reserved,
			&b.Consumed, &b.Refundable, &b.Version, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return b, m6supply.ErrNotFound
	}
	if err != nil {
		return b, t.fail(err)
	}
	if b.CreatedAt, err = parseRFC(created); err != nil {
		return b, err
	}
	if b.UpdatedAt, err = parseRFC(updated); err != nil {
		return b, err
	}
	return b, nil
}

func (t *agentRuntimeTx) PutM6BudgetAccount(b m6supply.BudgetAccount) error {
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO m6_budget_account
		(id, root_id, dimension, granted, reserved, consumed, refundable, version, created_at, updated_at)
		VALUES(?,?,?,?,?,?,?,1,?,?)`,
		b.ID, b.RootID, b.Dimension, b.Granted, b.Reserved, b.Consumed, b.Refundable,
		rfc(b.CreatedAt), rfc(b.UpdatedAt))
	return t.fail(err)
}

func (t *agentRuntimeTx) UpdateM6BudgetAccountBalances(id string, expectedVersion int64,
	granted, reserved, consumed, refundable int64, at time.Time) (m6supply.BudgetAccount, error) {
	res, err := t.tx.ExecContext(t.ctx, `UPDATE m6_budget_account
		SET granted = ?, reserved = ?, consumed = ?, refundable = ?, version = version + 1, updated_at = ?
		WHERE id = ? AND version = ?`,
		granted, reserved, consumed, refundable, rfc(at), id, expectedVersion)
	if err != nil {
		return m6supply.BudgetAccount{}, t.fail(err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return m6supply.BudgetAccount{}, t.conflictOrMissing("m6_budget_account", id)
	}
	return t.getM6BudgetAccountByID(id)
}

func (t *agentRuntimeTx) getM6BudgetAccountByID(id string) (m6supply.BudgetAccount, error) {
	var b m6supply.BudgetAccount
	var created, updated string
	err := t.tx.QueryRowContext(t.ctx, `SELECT id, root_id, dimension, granted, reserved,
		consumed, refundable, version, created_at, updated_at
		FROM m6_budget_account WHERE id = ?`, id).
		Scan(&b.ID, &b.RootID, &b.Dimension, &b.Granted, &b.Reserved,
			&b.Consumed, &b.Refundable, &b.Version, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return b, m6supply.ErrNotFound
	}
	if err != nil {
		return b, t.fail(err)
	}
	if b.CreatedAt, err = parseRFC(created); err != nil {
		return b, err
	}
	if b.UpdatedAt, err = parseRFC(updated); err != nil {
		return b, err
	}
	return b, nil
}

func (t *agentRuntimeTx) ListM6BudgetAccounts(rootID string) ([]m6supply.BudgetAccount, error) {
	rows, err := t.tx.QueryContext(t.ctx, `SELECT id, root_id, dimension, granted, reserved,
		consumed, refundable, version, created_at, updated_at
		FROM m6_budget_account WHERE root_id = ? ORDER BY dimension`, rootID)
	if err != nil {
		return nil, t.fail(err)
	}
	defer rows.Close()
	var out []m6supply.BudgetAccount
	for rows.Next() {
		var b m6supply.BudgetAccount
		var created, updated string
		if err := rows.Scan(&b.ID, &b.RootID, &b.Dimension, &b.Granted, &b.Reserved,
			&b.Consumed, &b.Refundable, &b.Version, &created, &updated); err != nil {
			return nil, t.fail(err)
		}
		if b.CreatedAt, err = parseRFC(created); err != nil {
			return nil, err
		}
		if b.UpdatedAt, err = parseRFC(updated); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, t.fail(rows.Err())
}

// conflictOrMissing distinguishes a version bump race from a deleted row.
func (t *agentRuntimeTx) conflictOrMissing(table, id string) error {
	var one int
	err := t.tx.QueryRowContext(t.ctx, "SELECT 1 FROM "+table+" WHERE id = ?", id).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return m6supply.ErrNotFound
	}
	if err != nil {
		return t.fail(err)
	}
	return m6supply.ErrVersionConflict
}

// --- delegations -----------------------------------------------------------

const delegationColumns = `id, root_id, parent_id, child_task_id, envelope, envelope_digest,
	nonce, depth, state, version, created_at, settled_at, updated_at`

func scanM6Delegation(scan func(dest ...any) error) (m6supply.Delegation, error) {
	var d m6supply.Delegation
	var childTaskID, settledAt sql.NullString
	var created, updated string
	if err := scan(&d.ID, &d.RootID, &d.ParentID, &childTaskID, &d.Envelope, &d.EnvelopeDigest,
		&d.Nonce, &d.Depth, &d.State, &d.Version, &created, &settledAt, &updated); err != nil {
		return d, err
	}
	d.ChildTaskID = childTaskID.String
	if settledAt.Valid && settledAt.String != "" {
		at, err := parseRFC(settledAt.String)
		if err != nil {
			return d, err
		}
		d.SettledAt = &at
	}
	var err error
	if d.CreatedAt, err = parseRFC(created); err != nil {
		return d, err
	}
	if d.UpdatedAt, err = parseRFC(updated); err != nil {
		return d, err
	}
	return d, nil
}

func (t *agentRuntimeTx) GetM6Delegation(id string) (m6supply.Delegation, error) {
	d, err := scanM6Delegation(func(dest ...any) error {
		return t.tx.QueryRowContext(t.ctx,
			"SELECT "+delegationColumns+" FROM m6_delegation WHERE id = ?", id).Scan(dest...)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return d, m6supply.ErrNotFound
	}
	if err != nil {
		return d, t.fail(err)
	}
	return d, nil
}

func (t *agentRuntimeTx) PutM6Delegation(d m6supply.Delegation) error {
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO m6_delegation
		(id, root_id, parent_id, child_task_id, envelope, envelope_digest, nonce, depth, state, version, created_at, settled_at, updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,NULL,?)`,
		d.ID, d.RootID, d.ParentID, nullString(d.ChildTaskID), d.Envelope, d.EnvelopeDigest,
		d.Nonce, d.Depth, d.State, d.Version, rfc(d.CreatedAt), rfc(d.UpdatedAt))
	return t.fail(err)
}

const activeDelegationStates = `('planned','grant_reserved','dispatched','arrived')`

func (t *agentRuntimeTx) CountActiveM6DelegationsByParent(rootID, parentID string) (int64, error) {
	var n int64
	err := t.tx.QueryRowContext(t.ctx,
		`SELECT COUNT(*) FROM m6_delegation WHERE root_id = ? AND parent_id = ? AND state IN `+activeDelegationStates,
		rootID, parentID).Scan(&n)
	return n, t.fail(err)
}

func (t *agentRuntimeTx) CountActiveM6DelegationsByRoot(rootID string) (int64, error) {
	var n int64
	err := t.tx.QueryRowContext(t.ctx,
		`SELECT COUNT(*) FROM m6_delegation WHERE root_id = ? AND state IN `+activeDelegationStates,
		rootID).Scan(&n)
	return n, t.fail(err)
}

func (t *agentRuntimeTx) UpdateM6DelegationState(id string, expectedVersion int64, to string, at time.Time, settledAt *time.Time) (m6supply.Delegation, error) {
	var settled any
	if settledAt != nil {
		settled = rfc(*settledAt)
	}
	res, err := t.tx.ExecContext(t.ctx, `UPDATE m6_delegation
		SET state = ?, settled_at = COALESCE(?, settled_at), version = version + 1, updated_at = ?
		WHERE id = ? AND version = ?`, to, settled, rfc(at), id, expectedVersion)
	if err != nil {
		return m6supply.Delegation{}, t.fail(err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return m6supply.Delegation{}, t.conflictOrMissing("m6_delegation", id)
	}
	return t.GetM6Delegation(id)
}

// --- barriers ---------------------------------------------------------------

const barrierColumns = `id, root_id, policy, expected_children, quorum, state, closed_reason,
	version, created_at, closed_at, updated_at`

func scanM6Barrier(scan func(dest ...any) error) (m6supply.Barrier, error) {
	var b m6supply.Barrier
	var quorum sql.NullInt64
	var closedReason sql.NullString
	var closedAt sql.NullString
	var created, updated string
	if err := scan(&b.ID, &b.RootID, &b.Policy, &b.ExpectedChildren, &quorum, &b.State,
		&closedReason, &b.Version, &created, &closedAt, &updated); err != nil {
		return b, err
	}
	if quorum.Valid {
		b.Quorum = int(quorum.Int64)
	}
	b.ClosedReason = closedReason.String
	if closedAt.Valid && closedAt.String != "" {
		at, err := parseRFC(closedAt.String)
		if err != nil {
			return b, err
		}
		b.ClosedAt = &at
	}
	var err error
	if b.CreatedAt, err = parseRFC(created); err != nil {
		return b, err
	}
	if b.UpdatedAt, err = parseRFC(updated); err != nil {
		return b, err
	}
	return b, nil
}

func (t *agentRuntimeTx) GetM6Barrier(id string) (m6supply.Barrier, error) {
	b, err := scanM6Barrier(func(dest ...any) error {
		return t.tx.QueryRowContext(t.ctx,
			"SELECT "+barrierColumns+" FROM m6_barrier WHERE id = ?", id).Scan(dest...)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return b, m6supply.ErrNotFound
	}
	if err != nil {
		return b, t.fail(err)
	}
	return b, nil
}

func (t *agentRuntimeTx) FindOpenM6BarrierByRoot(rootID string) (m6supply.Barrier, error) {
	b, err := scanM6Barrier(func(dest ...any) error {
		return t.tx.QueryRowContext(t.ctx,
			"SELECT "+barrierColumns+" FROM m6_barrier WHERE root_id = ? AND state = 'open' ORDER BY created_at DESC LIMIT 1",
			rootID).Scan(dest...)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return b, m6supply.ErrNotFound
	}
	if err != nil {
		return b, t.fail(err)
	}
	return b, nil
}

func (t *agentRuntimeTx) PutM6Barrier(b m6supply.Barrier) error {
	var quorum any
	if b.Quorum > 0 {
		quorum = b.Quorum
	}
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO m6_barrier
		(id, root_id, policy, expected_children, quorum, state, closed_reason, version, created_at, closed_at, updated_at)
		VALUES(?,?,?,?,?,?,NULL,?,?,NULL,?)`,
		b.ID, b.RootID, b.Policy, b.ExpectedChildren, quorum, b.State,
		b.Version, rfc(b.CreatedAt), rfc(b.UpdatedAt))
	return t.fail(err)
}

func (t *agentRuntimeTx) CloseM6Barrier(id string, expectedVersion int64, reason string, at time.Time) (m6supply.Barrier, error) {
	res, err := t.tx.ExecContext(t.ctx, `UPDATE m6_barrier
		SET state = 'closed', closed_reason = ?, closed_at = ?, version = version + 1, updated_at = ?
		WHERE id = ? AND version = ? AND state = 'open'`,
		reason, rfc(at), rfc(at), id, expectedVersion)
	if err != nil {
		return m6supply.Barrier{}, t.fail(err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return m6supply.Barrier{}, t.conflictOrMissing("m6_barrier", id)
	}
	return t.GetM6Barrier(id)
}

// --- barrier arrivals --------------------------------------------------------

func (t *agentRuntimeTx) PutM6BarrierArrival(a m6supply.BarrierArrival) error {
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO m6_barrier_arrival
		(id, barrier_id, child_id, attempt, outcome, result_digest, arrived_at)
		VALUES(?,?,?,?,?,?,?)`,
		a.ID, a.BarrierID, a.ChildID, a.Attempt, a.Outcome, a.ResultDigest, rfc(a.ArrivedAt))
	return t.fail(err)
}

func (t *agentRuntimeTx) GetM6BarrierArrival(barrierID, childID string) (m6supply.BarrierArrival, error) {
	var a m6supply.BarrierArrival
	var arrived string
	err := t.tx.QueryRowContext(t.ctx, `SELECT id, barrier_id, child_id, attempt, outcome, result_digest, arrived_at
		FROM m6_barrier_arrival WHERE barrier_id = ? AND child_id = ?`, barrierID, childID).
		Scan(&a.ID, &a.BarrierID, &a.ChildID, &a.Attempt, &a.Outcome, &a.ResultDigest, &arrived)
	if errors.Is(err, sql.ErrNoRows) {
		return a, m6supply.ErrNotFound
	}
	if err != nil {
		return a, t.fail(err)
	}
	if a.ArrivedAt, err = parseRFC(arrived); err != nil {
		return a, err
	}
	return a, nil
}

func (t *agentRuntimeTx) ListM6BarrierArrivals(barrierID string) ([]m6supply.BarrierArrival, error) {
	rows, err := t.tx.QueryContext(t.ctx, `SELECT id, barrier_id, child_id, attempt, outcome, result_digest, arrived_at
		FROM m6_barrier_arrival WHERE barrier_id = ? ORDER BY arrived_at`, barrierID)
	if err != nil {
		return nil, t.fail(err)
	}
	defer rows.Close()
	var out []m6supply.BarrierArrival
	for rows.Next() {
		var a m6supply.BarrierArrival
		var arrived string
		if err := rows.Scan(&a.ID, &a.BarrierID, &a.ChildID, &a.Attempt, &a.Outcome, &a.ResultDigest, &arrived); err != nil {
			return nil, t.fail(err)
		}
		if a.ArrivedAt, err = parseRFC(arrived); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, t.fail(rows.Err())
}
