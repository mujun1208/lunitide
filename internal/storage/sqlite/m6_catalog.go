// T-6.2.1/T-6.2.2 storage: connector metadata snapshots (0045) and cloud
// task rows for worker dispatch (0046).
package sqlite

import (
	"database/sql"
	"errors"

	"github.com/lunitide/lunitide/internal/domain/m6supply"
)

func (t *agentRuntimeTx) MaxM6ConnectorSnapshotVersion(connectorID string) (int64, error) {
	var version sql.NullInt64
	err := t.tx.QueryRowContext(t.ctx,
		`SELECT MAX(snapshot_version) FROM m6_connector_catalog WHERE connector_id = ?`,
		connectorID).Scan(&version)
	if err != nil {
		return 0, t.fail(err)
	}
	return version.Int64, nil
}

func (t *agentRuntimeTx) PutM6ConnectorSnapshot(s m6supply.ConnectorSnapshot) error {
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO m6_connector_catalog
		(id, connector_id, scope, snapshot_version, metadata_scope, objects_json, fetched_at)
		VALUES(?,?,?,?,?,?,?)`,
		s.ID, s.ConnectorID, s.Scope, s.SnapshotVersion, s.MetadataScope, s.ObjectsJSON, rfc(s.FetchedAt))
	return t.fail(err)
}

func (t *agentRuntimeTx) GetM6CloudTaskByIdempotencyKey(key string) (m6supply.CloudTask, error) {
	var c m6supply.CloudTask
	var leaseOwner, resultRef, leaseExpires sql.NullString
	var created, updated string
	err := t.tx.QueryRowContext(t.ctx, `SELECT id, idempotency_key, payload_digest,
		lease_owner, lease_expires_at, attempt, state, result_ref, version, created_at, updated_at
		FROM m6_cloud_task WHERE idempotency_key = ?`, key).
		Scan(&c.ID, &c.IdempotencyKey, &c.PayloadDigest, &leaseOwner, &leaseExpires,
			&c.Attempt, &c.State, &resultRef, &c.Version, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return c, m6supply.ErrNotFound
	}
	if err != nil {
		return c, t.fail(err)
	}
	c.LeaseOwner, c.ResultRef = leaseOwner.String, resultRef.String
	if leaseExpires.Valid && leaseExpires.String != "" {
		at, perr := parseRFC(leaseExpires.String)
		if perr != nil {
			return c, perr
		}
		c.LeaseExpiresAt = &at
	}
	if c.CreatedAt, err = parseRFC(created); err != nil {
		return c, err
	}
	if c.UpdatedAt, err = parseRFC(updated); err != nil {
		return c, err
	}
	return c, nil
}

func (t *agentRuntimeTx) PutM6CloudTask(c m6supply.CloudTask) error {
	var expires any
	if c.LeaseExpiresAt != nil {
		expires = rfc(*c.LeaseExpiresAt)
	}
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO m6_cloud_task
		(id, idempotency_key, payload_digest, lease_owner, lease_expires_at, attempt, state, result_ref, version, created_at, updated_at)
		VALUES(?,?,?,?,?,?,?,NULL,?,?,?)`,
		c.ID, c.IdempotencyKey, c.PayloadDigest, nullString(c.LeaseOwner), expires,
		c.Attempt, c.State, c.Version, rfc(c.CreatedAt), rfc(c.UpdatedAt))
	return t.fail(err)
}
