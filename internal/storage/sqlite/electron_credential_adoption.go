package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/providerapp"
	"github.com/lunitide/lunitide/internal/secret"
)

type ElectronCredentialTuple = providerapp.ElectronCredentialTuple
type ElectronCredentialPlan = providerapp.ElectronCredentialPlan
type ElectronCredentialAdoption = providerapp.ElectronCredentialAdoption

// Each of the two allowlisted legacy data directories may contain up to
// maxElectronProviders entries. Planning accepts the bounded aggregate so one
// populated source cannot permanently starve the other during startup.
const maxElectronCredentialTuples = maxElectronProviders * 2

func (s *Store) PlanElectronCredentials(ctx context.Context, tuples []ElectronCredentialTuple) ([]ElectronCredentialPlan, error) {
	if len(tuples) > maxElectronCredentialTuples {
		return nil, errors.New("too many Electron credential tuples")
	}
	out := make([]ElectronCredentialPlan, 0, len(tuples))
	for _, tuple := range tuples {
		if len(tuple.SourceFingerprint) != 64 || len(tuple.ItemFingerprint) != 64 {
			return nil, errors.New("invalid Electron credential tuple")
		}
		var p ElectronCredentialPlan
		var base string
		err := s.db.QueryRowContext(ctx, `SELECT i.source_fingerprint,i.item_fingerprint,p.id,p.version,p.base_url,p.protocol FROM provider_metadata_migration_items i JOIN providers p ON p.id=i.provider_id WHERE i.source_fingerprint=? AND i.item_fingerprint=? AND i.credential_migration_state='pending' AND p.deleted_at IS NULL`, tuple.SourceFingerprint, tuple.ItemFingerprint).Scan(&p.SourceFingerprint, &p.ItemFingerprint, &p.ProviderID, &p.Version, &base, &p.Protocol)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, err
		}
		p.Origin, err = provider.NormalizeOrigin(base)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

// AdoptElectronCredential performs the exact migration/provider CAS and all
// durable receipts in one SQLite writer transaction. No credential bytes or
// encrypted source material enter this API.
func (s *Store) AdoptElectronCredential(ctx context.Context, key string, in ElectronCredentialAdoption) (string, error) {
	if key == "" || len(key) > providerapp.MaxIdempotencyKeyBytes {
		return "", providerapp.ErrIdempotencyKeyRequired
	}
	request, _ := json.Marshal(in)
	digestBytes := sha256Bytes(request)
	digest := hexBytes(digestBytes)
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return "", err
	}
	defer conn.Close()
	if _, err = conn.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
		return "", mapWriteError(err)
	}
	committed := false
	defer func() {
		if !committed {
			_, _ = conn.ExecContext(context.Background(), `ROLLBACK`)
		}
	}()
	var oldDigest, response string
	err = conn.QueryRowContext(ctx, `SELECT request_digest,response_json FROM idempotency_records WHERE operation='provider.update' AND idempotency_key=?`, key).Scan(&oldDigest, &response)
	if err == nil {
		if oldDigest != digest {
			return "", providerapp.ErrIdempotencyConflict
		}
		var replay struct {
			Receipt string `json:"receipt"`
		}
		if json.Unmarshal([]byte(response), &replay) != nil {
			return "", errors.New("invalid adoption replay")
		}
		_, err = conn.ExecContext(ctx, `COMMIT`)
		committed = err == nil
		return replay.Receipt, err
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	var providerID, migrationState, credentialState, currentRef, base, protocol string
	var version int64
	err = conn.QueryRowContext(ctx, `SELECT i.provider_id,i.credential_migration_state,p.version,p.credential_state,COALESCE(p.credential_ref,''),p.base_url,p.protocol FROM provider_metadata_migration_items i JOIN providers p ON p.id=i.provider_id WHERE i.source_fingerprint=? AND i.item_fingerprint=? AND p.deleted_at IS NULL`, in.SourceFingerprint, in.ItemFingerprint).Scan(&providerID, &migrationState, &version, &credentialState, &currentRef, &base, &protocol)
	if err != nil {
		return "", err
	}
	origin, normErr := provider.NormalizeOrigin(base)
	if migrationState != "pending" || credentialState != string(provider.CredentialRequiresReentry) || currentRef != "" || providerID != in.ProviderID || version != in.Version || origin != in.Origin || protocol != string(in.Protocol) || normErr != nil {
		return "", provider.ErrConflict
	}
	ref := secret.Ref{CredentialRef: in.CredentialRef, ProviderID: in.ProviderID, Origin: in.Origin, Protocol: string(in.Protocol)}
	if _, err = ref.Validate(); err != nil {
		return "", err
	}
	now := time.Now().UTC()
	receipt := deterministicULID("electron-adoption\x00" + digest)
	r, err := conn.ExecContext(ctx, `UPDATE providers SET credential_ref=?,credential_state='configured',updated_at=?,version=version+1 WHERE id=? AND version=? AND credential_ref IS NULL AND credential_state='requires_reentry' AND origin_fingerprint=?`, in.CredentialRef, formatTime(now), in.ProviderID, in.Version, mustOriginFingerprint(in.Protocol, base))
	if err != nil {
		return "", err
	}
	if n, _ := r.RowsAffected(); n != 1 {
		return "", provider.ErrConflict
	}
	if _, err = conn.ExecContext(ctx, `INSERT INTO credential_adoptions(credential_ref,provider_id,origin,protocol,receipt_id,adopted_at) VALUES(?,?,?,?,?,?)`, ref.CredentialRef, ref.ProviderID, ref.Origin, ref.Protocol, receipt, formatTime(now)); err != nil {
		return "", err
	}
	if _, err = conn.ExecContext(ctx, `UPDATE provider_metadata_migration_items SET credential_migration_state='adopted',credential_receipt_id=?,credential_updated_at=? WHERE source_fingerprint=? AND item_fingerprint=? AND credential_migration_state='pending'`, receipt, formatTime(now), in.SourceFingerprint, in.ItemFingerprint); err != nil {
		return "", err
	}
	meta, _ := json.Marshal(map[string]any{"version": in.Version + 1, "migration": "electron_credential_adoption"})
	if _, err = conn.ExecContext(ctx, `INSERT INTO audit_events(id,action,aggregate_id,actor,metadata_json,created_at) VALUES(?,?,?,?,?,?)`, deterministicULID("audit\x00"+digest), "provider.updated", in.ProviderID, "electron-credential-migration", string(meta), formatTime(now)); err != nil {
		return "", err
	}
	payload, _ := json.Marshal(map[string]any{"providerId": in.ProviderID, "version": in.Version + 1})
	if _, err = conn.ExecContext(ctx, `INSERT INTO outbox_events(id,topic,aggregate_id,payload_json,available_at,created_at) VALUES(?,?,?,?,?,?)`, deterministicULID("outbox\x00"+digest), "provider.updated", in.ProviderID, string(payload), formatTime(now), formatTime(now)); err != nil {
		return "", err
	}
	responseBytes, _ := json.Marshal(map[string]string{"receipt": receipt})
	if _, err = conn.ExecContext(ctx, `INSERT INTO idempotency_records(operation,idempotency_key,request_digest,response_json,created_at,expires_at) VALUES('provider.update',?,?,?,?,?)`, key, digest, string(responseBytes), formatTime(now), formatTime(now.Add(24*time.Hour))); err != nil {
		return "", err
	}
	if _, err = conn.ExecContext(ctx, `COMMIT`); err != nil {
		return "", err
	}
	committed = true
	return receipt, nil
}

func (s *Store) DispositionElectronCredential(ctx context.Context, tuple ElectronCredentialTuple, disposition string) error {
	if disposition != "rejected" && disposition != "superseded" {
		return errors.New("invalid adoption disposition")
	}
	_, err := s.db.ExecContext(ctx, `UPDATE provider_metadata_migration_items SET credential_migration_state=?,credential_updated_at=? WHERE source_fingerprint=? AND item_fingerprint=? AND credential_migration_state='pending'`, disposition, formatTime(time.Now().UTC()), tuple.SourceFingerprint, tuple.ItemFingerprint)
	return err
}

func sha256Bytes(b []byte) []byte { h := sha256.Sum256(b); return h[:] }
func hexBytes(b []byte) string    { return hex.EncodeToString(b) }
func mustOriginFingerprint(protocol provider.Protocol, base string) string {
	fp, _ := provider.OriginFingerprint(protocol, base)
	return fp
}
