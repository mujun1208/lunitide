// M7 slice 5 storage (T-7.5.x): the AppUpdate split-track tables and the
// m7_audit_events hash-chain ledger on the agent-runtime single-writer
// transaction. The tx only addresses update_* / consumed_nonces /
// m7_audit_events - no cross-domain foreign key ever reaches the project
// release tables (02-技术设计 §05). Audit appends seal seq + prev_hash +
// event_hash inside the same transaction; the migration-0057 triggers make
// the ledger WORM (M7-DR-001) and the receipts append-only (M7-EVD-001).
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/lunitide/lunitide/internal/audit"
	"github.com/lunitide/lunitide/internal/domain/agentrun"
	"github.com/lunitide/lunitide/internal/domain/m7flow"
	"github.com/lunitide/lunitide/internal/m7app"
)

// TransactUpdate runs an m7app slice-5 use case on the shared single-writer
// transaction.
func (r *AgentRuntimeRepository) TransactUpdate(ctx context.Context, fn func(m7app.UpdateTx) error) error {
	return r.Transact(ctx, func(tx agentrun.Tx) error {
		utx, ok := tx.(m7app.UpdateTx)
		if !ok {
			return errors.New("agent runtime tx does not satisfy m7app.UpdateTx")
		}
		return fn(utx)
	})
}

// ── channels ────────────────────────────────────────────────────────────────

func (t *agentRuntimeTx) GetChannelByName(name string) (m7flow.UpdateChannel, error) {
	var ch m7flow.UpdateChannel
	var created string
	err := t.tx.QueryRowContext(t.ctx,
		`SELECT id,name,state,created_at FROM update_channels WHERE name=?`, name).
		Scan(&ch.ID, &ch.Name, &ch.State, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return ch, m7flow.ErrNotFound
	}
	if err != nil {
		return ch, t.fail(err)
	}
	if ch.CreatedAt, err = parseRFC(created); err != nil {
		return ch, t.fail(err)
	}
	return ch, nil
}

// ── packages ────────────────────────────────────────────────────────────────

const updpColumns = `id,channel_id,app_version,min_version,package_digest,signature,nonce,not_before,expires_at,key_id,state,created_at`

func scanUpdatePackage(s interface{ Scan(...any) error }) (m7flow.UpdatePackage, error) {
	var p m7flow.UpdatePackage
	var notBefore, expiresAt, created string
	if err := s.Scan(&p.ID, &p.ChannelID, &p.AppVersion, &p.MinVersion, &p.PackageDigest,
		&p.Signature, &p.Nonce, &notBefore, &expiresAt, &p.KeyID, &p.State, &created); err != nil {
		return p, err
	}
	var err error
	if p.NotBefore, err = parseRFC(notBefore); err != nil {
		return p, err
	}
	if p.ExpiresAt, err = parseRFC(expiresAt); err != nil {
		return p, err
	}
	if p.CreatedAt, err = parseRFC(created); err != nil {
		return p, err
	}
	return p, nil
}

func (t *agentRuntimeTx) PutUpdatePackage(p m7flow.UpdatePackage) error {
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO update_packages
		(id,channel_id,app_version,min_version,package_digest,signature,nonce,not_before,expires_at,key_id,state,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		p.ID, p.ChannelID, p.AppVersion, p.MinVersion, p.PackageDigest,
		p.Signature, p.Nonce, rfc(p.NotBefore), rfc(p.ExpiresAt), p.KeyID, p.State, rfc(p.CreatedAt))
	return t.fail(err)
}

func (t *agentRuntimeTx) GetUpdatePackage(id string) (m7flow.UpdatePackage, error) {
	p, err := scanUpdatePackage(t.tx.QueryRowContext(t.ctx,
		`SELECT `+updpColumns+` FROM update_packages WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return p, m7flow.ErrNotFound
	}
	return p, t.fail(err)
}

func (t *agentRuntimeTx) FindPackageByChannelVersion(channelID, appVersion string) (m7flow.UpdatePackage, error) {
	p, err := scanUpdatePackage(t.tx.QueryRowContext(t.ctx,
		`SELECT `+updpColumns+` FROM update_packages WHERE channel_id=? AND app_version=?`,
		channelID, appVersion))
	if errors.Is(err, sql.ErrNoRows) {
		return p, m7flow.ErrNotFound
	}
	return p, t.fail(err)
}

// FindLatestPublishedPackage answers the newest published package of one
// channel whose trust window covers now (not_before <= now < expires_at).
func (t *agentRuntimeTx) FindLatestPublishedPackage(channelID string, now time.Time) (m7flow.UpdatePackage, error) {
	p, err := scanUpdatePackage(t.tx.QueryRowContext(t.ctx,
		`SELECT `+updpColumns+` FROM update_packages
		 WHERE channel_id=? AND state='published' AND not_before<=? AND expires_at>?
		 ORDER BY app_version DESC, created_at DESC LIMIT 1`,
		channelID, rfc(now), rfc(now)))
	if errors.Is(err, sql.ErrNoRows) {
		return p, m7flow.ErrNotFound
	}
	return p, t.fail(err)
}

// ── installations ───────────────────────────────────────────────────────────

const updiColumns = `id,package_id,device_id,state,created_at,completed_at`

func scanInstallation(s interface{ Scan(...any) error }) (m7flow.UpdateInstallation, error) {
	var i m7flow.UpdateInstallation
	var completed sql.NullString
	var created string
	if err := s.Scan(&i.ID, &i.PackageID, &i.DeviceID, &i.State, &created, &completed); err != nil {
		return i, err
	}
	var err error
	if i.CreatedAt, err = parseRFC(created); err != nil {
		return i, err
	}
	if completed.Valid {
		t, err := parseRFC(completed.String)
		if err != nil {
			return i, err
		}
		i.CompletedAt = &t
	}
	return i, nil
}

func (t *agentRuntimeTx) PutUpdateInstallation(i m7flow.UpdateInstallation) error {
	var completed any
	if i.CompletedAt != nil {
		completed = rfc(*i.CompletedAt)
	}
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO update_installations
		(id,package_id,device_id,state,created_at,completed_at) VALUES(?,?,?,?,?,?)`,
		i.ID, i.PackageID, i.DeviceID, i.State, rfc(i.CreatedAt), completed)
	return t.fail(err)
}

func (t *agentRuntimeTx) GetUpdateInstallation(id string) (m7flow.UpdateInstallation, error) {
	i, err := scanInstallation(t.tx.QueryRowContext(t.ctx,
		`SELECT `+updiColumns+` FROM update_installations WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return i, m7flow.ErrNotFound
	}
	return i, t.fail(err)
}

func (t *agentRuntimeTx) FindInstallationByDevicePackage(deviceID, packageID string) (m7flow.UpdateInstallation, error) {
	i, err := scanInstallation(t.tx.QueryRowContext(t.ctx,
		`SELECT `+updiColumns+` FROM update_installations
		 WHERE device_id=? AND package_id=? ORDER BY created_at DESC LIMIT 1`, deviceID, packageID))
	if errors.Is(err, sql.ErrNoRows) {
		return i, m7flow.ErrNotFound
	}
	return i, t.fail(err)
}

// FindLastSucceededVersion answers the package version the device last
// installed successfully (the anti-downgrade floor).
func (t *agentRuntimeTx) FindLastSucceededVersion(deviceID string) (string, error) {
	var version string
	err := t.tx.QueryRowContext(t.ctx,
		`SELECT p.app_version FROM update_installations i
		 JOIN update_packages p ON p.id=i.package_id
		 WHERE i.device_id=? AND i.state='succeeded'
		 ORDER BY i.completed_at DESC LIMIT 1`, deviceID).Scan(&version)
	if errors.Is(err, sql.ErrNoRows) {
		return "", m7flow.ErrNotFound
	}
	return version, t.fail(err)
}

// UpdateInstallationState performs one legal state transition (CAS on from).
func (t *agentRuntimeTx) UpdateInstallationState(id, from, to string, completedAt *time.Time) error {
	var completed any
	if completedAt != nil {
		completed = rfc(*completedAt)
	}
	res, err := t.tx.ExecContext(t.ctx,
		`UPDATE update_installations SET state=?, completed_at=COALESCE(?, completed_at)
		 WHERE id=? AND state=?`, to, completed, id, from)
	if err != nil {
		return t.fail(err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return t.fail(sql.ErrNoRows)
	}
	return nil
}

// ── receipts (append-only, M7-EVD-001) ──────────────────────────────────────

func (t *agentRuntimeTx) PutUpdateReceipt(r m7flow.UpdateReceipt) error {
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO update_receipts
		(id,installation_id,receipt_json,digest,created_at) VALUES(?,?,?,?,?)`,
		r.ID, r.InstallationID, r.ReceiptJSON, r.Digest, rfc(r.CreatedAt))
	return t.fail(err)
}

// ── rollback attempts (append-only, M7-RBK-002) ─────────────────────────────

func (t *agentRuntimeTx) PutUpdateRollbackAttempt(a m7flow.UpdateRollbackAttempt) error {
	var completed any
	if a.CompletedAt != nil {
		completed = rfc(*a.CompletedAt)
	}
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO update_rollback_attempts
		(id,installation_id,state,operator_id,result_json,created_at,completed_at)
		VALUES(?,?,?,?,?,?,?)`,
		a.ID, a.InstallationID, a.State, a.OperatorID, a.ResultJSON, rfc(a.CreatedAt), completed)
	return t.fail(err)
}

// UpdateRollbackAttemptState moves one attempt pending -> running ->
// succeeded | failed; rows are never deleted.
func (t *agentRuntimeTx) UpdateRollbackAttemptState(id, from, to, resultJSON string, completedAt *time.Time) error {
	var completed any
	if completedAt != nil {
		completed = rfc(*completedAt)
	}
	res, err := t.tx.ExecContext(t.ctx,
		`UPDATE update_rollback_attempts SET state=?, result_json=?, completed_at=COALESCE(?, completed_at)
		 WHERE id=? AND state=?`, to, resultJSON, completed, id, from)
	if err != nil {
		return t.fail(err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return t.fail(sql.ErrNoRows)
	}
	return nil
}

// ── nonces ──────────────────────────────────────────────────────────────────

// ConsumeNonce records the single consumption of one manifest nonce; a
// primary-key conflict answers replayed=true (nonce replay is refused).
func (t *agentRuntimeTx) ConsumeNonce(nonce string, consumedAt time.Time) (bool, error) {
	_, err := t.tx.ExecContext(t.ctx,
		`INSERT INTO consumed_nonces(nonce, consumed_at) VALUES(?,?)`, nonce, rfc(consumedAt))
	if err == nil {
		return false, nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return true, nil
	}
	// SQLite surfaces UNIQUE violations as constraint failures; classify by
	// extended code string to keep the driver surface small.
	var msg = err.Error()
	if len(msg) > 0 && (containsFold(msg, "UNIQUE") || containsFold(msg, "constraint")) {
		return true, nil
	}
	return false, t.fail(err)
}

func containsFold(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if equalFold(s[i:i+len(sub)], sub) {
			return true
		}
	}
	return false
}

func equalFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca, cb := a[i], b[i]
		if 'A' <= ca && ca <= 'Z' {
			ca += 'a' - 'A'
		}
		if 'A' <= cb && cb <= 'Z' {
			cb += 'a' - 'A'
		}
		if ca != cb {
			return false
		}
	}
	return true
}

// ── audit ledger (m7_audit_events) ──────────────────────────────────────────

const m7aeColumns = `seq,id,action,resource_type,resource_id,actor,before_digest,after_digest,correlation_id,prev_hash,event_hash,created_at`

func scanAuditEvent(s interface{ Scan(...any) error }) (audit.Event, error) {
	var e audit.Event
	var before, after, correlation sql.NullString
	if err := s.Scan(&e.Seq, &e.ID, &e.Action, &e.ResourceType, &e.ResourceID, &e.Actor,
		&before, &after, &correlation, &e.PrevHash, &e.EventHash, &e.CreatedAt); err != nil {
		return e, err
	}
	if before.Valid {
		e.BeforeDigest = before.String
	}
	if after.Valid {
		e.AfterDigest = after.String
	}
	if correlation.Valid {
		e.CorrelationID = correlation.String
	}
	return e, nil
}

// AppendAuditEvent seals e onto the chain tail inside this transaction
// (single writer: MAX(seq)+1 with the tail prev_hash) and inserts the row.
func (t *agentRuntimeTx) AppendAuditEvent(e audit.Event) (audit.Event, error) {
	var prevPtr *audit.Event
	if prev, err := t.LastAuditEvent(); err == nil {
		prevPtr = &prev
	} else if !errors.Is(err, m7flow.ErrNotFound) {
		return e, err
	}
	sealed := audit.Link(prevPtr, e)
	var before, after, correlation any
	if sealed.BeforeDigest != "" {
		before = sealed.BeforeDigest
	}
	if sealed.AfterDigest != "" {
		after = sealed.AfterDigest
	}
	if sealed.CorrelationID != "" {
		correlation = sealed.CorrelationID
	}
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO m7_audit_events
		(seq,id,action,resource_type,resource_id,actor,before_digest,after_digest,correlation_id,prev_hash,event_hash,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		sealed.Seq, sealed.ID, sealed.Action, sealed.ResourceType, sealed.ResourceID,
		sealed.Actor, before, after, correlation, sealed.PrevHash, sealed.EventHash, sealed.CreatedAt)
	if err != nil {
		return sealed, t.fail(err)
	}
	return sealed, nil
}

func (t *agentRuntimeTx) LastAuditEvent() (audit.Event, error) {
	e, err := scanAuditEvent(t.tx.QueryRowContext(t.ctx,
		`SELECT `+m7aeColumns+` FROM m7_audit_events ORDER BY seq DESC LIMIT 1`))
	if errors.Is(err, sql.ErrNoRows) {
		return e, m7flow.ErrNotFound
	}
	return e, t.fail(err)
}

func (t *agentRuntimeTx) ListAuditEvents() ([]audit.Event, error) {
	rows, err := t.tx.QueryContext(t.ctx,
		`SELECT `+m7aeColumns+` FROM m7_audit_events ORDER BY seq ASC`)
	if err != nil {
		return nil, t.fail(err)
	}
	defer rows.Close()
	var out []audit.Event
	for rows.Next() {
		e, err := scanAuditEvent(rows)
		if err != nil {
			return nil, t.fail(err)
		}
		out = append(out, e)
	}
	return out, t.fail(rows.Err())
}