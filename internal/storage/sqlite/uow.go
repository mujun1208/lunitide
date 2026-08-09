package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/providerapp"
	"github.com/lunitide/lunitide/internal/secret"
	"github.com/oklog/ulid/v2"
)

// Do runs the complete provider application mutation under one SQLite writer
// lock. txAdapter methods only use this connection and never start nested
// transactions.
func (s *Store) Do(ctx context.Context, fn func(providerapp.Tx) error) (resultErr error) {
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err = conn.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
		return mapWriteError(err)
	}
	defer func() {
		if resultErr != nil {
			_, _ = conn.ExecContext(context.Background(), `ROLLBACK`)
		}
	}()
	if err = fn(&txAdapter{s: s, q: conn}); err != nil {
		return err
	}
	if _, err = conn.ExecContext(ctx, `COMMIT`); err != nil {
		return mapWriteError(err)
	}
	return nil
}

type txAdapter struct {
	s *Store
	q *sql.Conn
}

func (t *txAdapter) Get(ctx context.Context, id string) (provider.Provider, error) {
	return getProvider(ctx, t.q, id)
}
func (t *txAdapter) Create(ctx context.Context, p provider.Provider) (provider.Provider, error) {
	base, err := provider.NormalizeBaseURL(p.BaseURL)
	if err != nil {
		return p, err
	}
	p.BaseURL = base
	if p.ID == "" {
		p.ID, err = t.s.newULID(time.Now())
		if err != nil {
			return p, err
		}
	} else if id, e := ulid.ParseStrict(p.ID); e != nil || id.String() != p.ID {
		return p, fmt.Errorf("provider ID must be an uppercase canonical ULID")
	}
	if p.Status == "" {
		p.Status = provider.StatusEnabled
	}
	now := time.Now().UTC()
	p.CreatedAt, p.UpdatedAt, p.Version = now, now, 1
	if err = p.Validate(); err != nil {
		return p, err
	}
	fp, err := provider.OriginFingerprint(p.Protocol, p.BaseURL)
	if err != nil {
		return p, err
	}
	_, err = t.q.ExecContext(ctx, `INSERT INTO providers(id,legacy_id,name,protocol,base_url,credential_ref,credential_state,status,created_at,updated_at,version,origin_fingerprint) VALUES(?,?,?,?,?,NULLIF(?,''),?,?,?,?,?,?)`, p.ID, nullString(p.LegacyID), p.Name, p.Protocol, p.BaseURL, p.CredentialRef, p.CredentialState, p.Status, formatTime(p.CreatedAt), formatTime(p.UpdatedAt), p.Version, fp)
	if err != nil {
		return p, fmt.Errorf("create provider: %w", err)
	}
	if err = replaceModels(ctx, t.q, p.ID, p.Models); err != nil {
		return p, err
	}
	return p, nil
}
func (t *txAdapter) Update(ctx context.Context, p provider.Provider, v int64) (provider.Provider, error) {
	base, err := provider.NormalizeBaseURL(p.BaseURL)
	if err != nil {
		return p, err
	}
	p.BaseURL = base
	old, err := getProvider(ctx, t.q, p.ID)
	if err != nil {
		return p, err
	}
	if old.Version != v {
		return p, provider.ErrConflict
	}
	var oldFP string
	if err = t.q.QueryRowContext(ctx, `SELECT origin_fingerprint FROM providers WHERE id=?`, p.ID).Scan(&oldFP); err != nil {
		return p, err
	}
	verified, _ := provider.OriginFingerprint(old.Protocol, old.BaseURL)
	if oldFP != verified {
		return p, fmt.Errorf("provider origin fingerprint mismatch")
	}
	newFP, _ := provider.OriginFingerprint(p.Protocol, p.BaseURL)
	if oldFP != newFP {
		if p.CredentialRef == old.CredentialRef && old.CredentialRef != "" {
			return p, provider.ErrCredentialReentryRequired
		}
		if p.CredentialRef == "" && p.CredentialState != provider.CredentialRequiresReentry {
			return p, provider.ErrCredentialReentryRequired
		}
	}
	p.CreatedAt, p.UpdatedAt, p.Version = old.CreatedAt, time.Now().UTC(), v+1
	if err = p.Validate(); err != nil {
		return p, err
	}
	r, err := t.q.ExecContext(ctx, `UPDATE providers SET legacy_id=?,name=?,protocol=?,base_url=?,credential_ref=NULLIF(?,''),credential_state=?,status=?,updated_at=?,version=?,origin_fingerprint=? WHERE id=? AND version=? AND deleted_at IS NULL`, nullString(p.LegacyID), p.Name, p.Protocol, p.BaseURL, p.CredentialRef, p.CredentialState, p.Status, formatTime(p.UpdatedAt), p.Version, newFP, p.ID, v)
	if err != nil {
		return p, mapWriteError(err)
	}
	n, _ := r.RowsAffected()
	if n != 1 {
		return p, provider.ErrConflict
	}
	if err = replaceModels(ctx, t.q, p.ID, p.Models); err != nil {
		return p, err
	}
	return p, nil
}
func (t *txAdapter) Delete(ctx context.Context, id string, v int64) error {
	now := formatTime(time.Now().UTC())
	r, err := t.q.ExecContext(ctx, `UPDATE providers SET deleted_at=?,updated_at=?,version=version+1 WHERE id=? AND version=? AND deleted_at IS NULL`, now, now, id, v)
	if err != nil {
		return mapWriteError(err)
	}
	n, _ := r.RowsAffected()
	if n == 1 {
		return nil
	}
	var live int
	if err = t.q.QueryRowContext(ctx, `SELECT count(*) FROM providers WHERE id=? AND deleted_at IS NULL`, id).Scan(&live); err != nil {
		return err
	}
	if live == 1 {
		return provider.ErrConflict
	}
	return provider.ErrNotFound
}

func (t *txAdapter) Idempotency(ctx context.Context, op, key string, now time.Time) (providerapp.Record, bool, error) {
	// Reclaim expiry while holding the same BEGIN IMMEDIATE lock. This makes
	// cleanup an optimization rather than a correctness dependency.
	if _, err := t.q.ExecContext(ctx, `DELETE FROM idempotency_records WHERE operation=? AND idempotency_key=? AND expires_at<=?`, op, key, formatTime(now)); err != nil {
		return providerapp.Record{}, false, err
	}
	var r providerapp.Record
	var response, created, expires string
	err := t.q.QueryRowContext(ctx, `SELECT request_digest,response_json,created_at,expires_at FROM idempotency_records WHERE operation=? AND idempotency_key=?`, op, key).Scan(&r.Digest, &response, &created, &expires)
	if err == sql.ErrNoRows {
		return r, false, nil
	}
	if err != nil {
		return r, false, err
	}
	r.Operation, r.Key, r.Response = op, key, []byte(response)
	r.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
	if err != nil {
		return r, false, err
	}
	r.ExpiresAt, err = time.Parse(time.RFC3339Nano, expires)
	if err != nil {
		return r, false, err
	}
	return r, true, nil
}
func (t *txAdapter) PutIdempotency(ctx context.Context, r providerapp.Record) error {
	_, err := t.q.ExecContext(ctx, `INSERT INTO idempotency_records(operation,idempotency_key,request_digest,response_json,created_at,expires_at) VALUES(?,?,?,?,?,?)`, r.Operation, r.Key, r.Digest, string(r.Response), formatTime(r.CreatedAt), formatTime(r.ExpiresAt))
	return err
}
func (t *txAdapter) ClaimIdempotency(ctx context.Context, c providerapp.Claim, now time.Time, limit int) (bool, error) {
	if _, err := t.q.ExecContext(ctx, `DELETE FROM idempotency_claims WHERE expires_at<=?`, formatTime(now)); err != nil {
		return false, err
	}
	var digest string
	err := t.q.QueryRowContext(ctx, `SELECT request_digest FROM idempotency_claims WHERE operation=? AND idempotency_key=?`, c.Operation, c.Key).Scan(&digest)
	if err == nil {
		if digest != c.Digest {
			return false, providerapp.ErrIdempotencyConflict
		}
		return false, nil
	}
	if err != sql.ErrNoRows {
		return false, err
	}
	var count int
	if err = t.q.QueryRowContext(ctx, `SELECT count(*) FROM idempotency_claims`).Scan(&count); err != nil {
		return false, err
	}
	if count >= limit {
		return false, providerapp.ErrStorageBusy
	}
	_, err = t.q.ExecContext(ctx, `INSERT INTO idempotency_claims(operation,idempotency_key,request_digest,owner,expires_at) VALUES(?,?,?,?,?)`, c.Operation, c.Key, c.Digest, c.Owner, formatTime(c.ExpiresAt))
	return err == nil, err
}
func (t *txAdapter) ReleaseIdempotencyClaim(ctx context.Context, op, key, owner string) error {
	_, err := t.q.ExecContext(ctx, `DELETE FROM idempotency_claims WHERE operation=? AND idempotency_key=? AND owner=?`, op, key, owner)
	return err
}
func (t *txAdapter) PutAudit(ctx context.Context, a providerapp.Audit) error {
	_, err := t.q.ExecContext(ctx, `INSERT INTO audit_events(id,action,aggregate_id,actor,metadata_json,created_at) VALUES(?,?,?,?,?,?)`, a.ID, a.Action, a.AggregateID, a.Actor, string(a.Metadata), formatTime(a.CreatedAt))
	return err
}
func (t *txAdapter) PutOutbox(ctx context.Context, e providerapp.Event) error {
	if len(e.Payload) < 2 {
		return fmt.Errorf("invalid outbox payload")
	}
	_, err := t.q.ExecContext(ctx, `INSERT INTO outbox_events(id,topic,aggregate_id,payload_json,available_at,created_at) VALUES(?,?,?,?,?,?)`, e.ID, e.Topic, e.AggregateID, string(e.Payload), formatTime(e.CreatedAt), formatTime(e.CreatedAt))
	return err
}
func (t *txAdapter) PutCredentialAdoption(ctx context.Context, ref secret.Ref, receipt string, at time.Time) error {
	if _, err := ref.Validate(); err != nil {
		return err
	}
	_, err := t.q.ExecContext(ctx, `INSERT INTO credential_adoptions(credential_ref,provider_id,origin,protocol,receipt_id,adopted_at) VALUES(?,?,?,?,?,?) ON CONFLICT(credential_ref) DO UPDATE SET receipt_id=excluded.receipt_id WHERE provider_id=excluded.provider_id AND origin=excluded.origin AND protocol=excluded.protocol`, ref.CredentialRef, ref.ProviderID, ref.Origin, ref.Protocol, receipt, formatTime(at))
	return err
}

var _ providerapp.UnitOfWork = (*Store)(nil)
var _ providerapp.Tx = (*txAdapter)(nil)
