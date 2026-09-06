package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/providerapp"
	"github.com/oklog/ulid/v2"
	modernsqlite "modernc.org/sqlite"
)

func (s *Store) List(ctx context.Context, filter provider.Filter) ([]provider.Provider, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	result, err := listProvidersWith(ctx, tx, filter)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return result, nil
}

func listProvidersWith(ctx context.Context, q sqlRunner, filter provider.Filter) ([]provider.Provider, error) {
	query := `SELECT id, COALESCE(legacy_id,''), name, protocol, base_url, COALESCE(credential_ref,''), credential_state, status, created_at, updated_at, version, COALESCE(credential_ref_backups,'[]') FROM providers WHERE deleted_at IS NULL`
	args := []any{}
	if filter.Protocol != "" {
		query += ` AND protocol = ?`
		args = append(args, filter.Protocol)
	}
	query += ` ORDER BY created_at, id`
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list providers: %w", err)
	}
	defer rows.Close()
	result := []provider.Provider{}
	for rows.Next() {
		var item provider.Provider
		var created, updated string
		var backups string
		if err := rows.Scan(&item.ID, &item.LegacyID, &item.Name, &item.Protocol, &item.BaseURL, &item.CredentialRef, &item.CredentialState, &item.Status, &created, &updated, &item.Version, &backups); err != nil {
			return nil, err
		}
		item.CredentialRefBackups, err = decodeCredentialBackups(backups)
		if err != nil {
			return nil, err
		}
		item.CredentialBackupCount = len(item.CredentialRefBackups)
		item.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
		if err != nil {
			return nil, err
		}
		item.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for i := range result {
		result[i].Models, err = listModelsWith(ctx, q, result[i].ID)
		if err != nil {
			return nil, err
		}
		if err = result[i].Validate(); err != nil {
			return nil, err
		}
	}
	return result, rows.Err()
}

func (s *Store) Create(ctx context.Context, item provider.Provider) (provider.Provider, error) {
	origin, err := provider.NormalizeBaseURL(item.BaseURL)
	if err != nil {
		return provider.Provider{}, err
	}
	item.BaseURL = origin
	if item.ID == "" {
		item.ID, err = s.newULID(time.Now())
		if err != nil {
			return provider.Provider{}, err
		}
	} else if parsed, parseErr := ulid.ParseStrict(item.ID); parseErr != nil || parsed.String() != item.ID {
		return provider.Provider{}, fmt.Errorf("provider ID must be an uppercase canonical ULID")
	}
	if item.Status == "" {
		item.Status = provider.StatusEnabled
	}
	now := time.Now().UTC()
	item.CreatedAt, item.UpdatedAt, item.Version = now, now, 1
	if err := item.Validate(); err != nil {
		return provider.Provider{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return provider.Provider{}, err
	}
	defer tx.Rollback()
	fingerprint, err := provider.OriginFingerprint(item.Protocol, item.BaseURL)
	if err != nil {
		return provider.Provider{}, err
	}
	backups, err := encodeCredentialBackups(item.CredentialRefBackups)
	if err != nil {
		return provider.Provider{}, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO providers(id,legacy_id,name,protocol,base_url,credential_ref,credential_state,status,created_at,updated_at,version,origin_fingerprint,credential_ref_backups) VALUES(?,?,?,?,?,NULLIF(?,''),?,?,?,?,?,?,?)`, item.ID, nullString(item.LegacyID), item.Name, item.Protocol, item.BaseURL, item.CredentialRef, item.CredentialState, item.Status, formatTime(item.CreatedAt), formatTime(item.UpdatedAt), item.Version, fingerprint, backups)
	if err != nil {
		return provider.Provider{}, fmt.Errorf("create provider: %w", err)
	}
	if err = replaceModels(ctx, tx, item.ID, item.Models); err != nil {
		return provider.Provider{}, err
	}
	if err = tx.Commit(); err != nil {
		return provider.Provider{}, err
	}
	return item, nil
}

func (s *Store) Get(ctx context.Context, id string) (provider.Provider, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return provider.Provider{}, err
	}
	defer tx.Rollback()
	item, err := getProvider(ctx, tx, id)
	if err != nil {
		return provider.Provider{}, err
	}
	if err = tx.Commit(); err != nil {
		return provider.Provider{}, err
	}
	return item, nil
}

func (s *Store) Update(ctx context.Context, item provider.Provider, expectedVersion int64) (provider.Provider, error) {
	origin, err := provider.NormalizeBaseURL(item.BaseURL)
	if err != nil {
		return provider.Provider{}, err
	}
	item.BaseURL = origin
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return provider.Provider{}, mapWriteError(err)
	}
	defer conn.Close()
	if _, err = conn.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
		return provider.Provider{}, mapWriteError(err)
	}
	defer conn.ExecContext(context.Background(), `ROLLBACK`)
	old, err := getProvider(ctx, conn, item.ID)
	if err != nil {
		return provider.Provider{}, err
	}
	if old.Version != expectedVersion {
		return provider.Provider{}, provider.ErrConflict
	}
	var oldFingerprint string
	if err = conn.QueryRowContext(ctx, `SELECT origin_fingerprint FROM providers WHERE id=?`, item.ID).Scan(&oldFingerprint); err != nil {
		return provider.Provider{}, err
	}
	wantOld, _ := provider.OriginFingerprint(old.Protocol, old.BaseURL)
	if oldFingerprint != wantOld {
		return provider.Provider{}, fmt.Errorf("provider origin fingerprint mismatch")
	}
	newFingerprint, _ := provider.OriginFingerprint(item.Protocol, item.BaseURL)
	if oldFingerprint != newFingerprint {
		if item.CredentialRef == old.CredentialRef && old.CredentialRef != "" {
			return provider.Provider{}, provider.ErrCredentialReentryRequired
		}
		if item.CredentialRef == "" && item.CredentialState != provider.CredentialRequiresReentry {
			return provider.Provider{}, provider.ErrCredentialReentryRequired
		}
	}
	item.CreatedAt = old.CreatedAt
	item.UpdatedAt = time.Now().UTC()
	item.Version = expectedVersion + 1
	if err = item.Validate(); err != nil {
		return provider.Provider{}, err
	}
	backups, err := encodeCredentialBackups(item.CredentialRefBackups)
	if err != nil {
		return provider.Provider{}, err
	}
	r, err := conn.ExecContext(ctx, `UPDATE providers SET legacy_id=?,name=?,protocol=?,base_url=?,credential_ref=NULLIF(?,''),credential_state=?,status=?,updated_at=?,version=?,origin_fingerprint=?,credential_ref_backups=? WHERE id=? AND version=? AND deleted_at IS NULL`, nullString(item.LegacyID), item.Name, item.Protocol, item.BaseURL, item.CredentialRef, item.CredentialState, item.Status, formatTime(item.UpdatedAt), item.Version, newFingerprint, backups, item.ID, expectedVersion)
	if err != nil {
		return provider.Provider{}, mapWriteError(err)
	}
	n, _ := r.RowsAffected()
	if n != 1 {
		return provider.Provider{}, provider.ErrConflict
	}
	if err = replaceModels(ctx, conn, item.ID, item.Models); err != nil {
		return provider.Provider{}, err
	}
	if _, err = conn.ExecContext(ctx, `COMMIT`); err != nil {
		return provider.Provider{}, mapWriteError(err)
	}
	return item, nil
}

// Delete soft-deletes a live provider. ErrNotFound for an already deleted ID
// lets the service layer deliberately choose strict or idempotent semantics.
func (s *Store) Delete(ctx context.Context, id string, expectedVersion int64) error {
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return mapWriteError(err)
	}
	defer conn.Close()
	if _, err = conn.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
		return mapWriteError(err)
	}
	defer conn.ExecContext(context.Background(), `ROLLBACK`)
	now := formatTime(time.Now().UTC())
	r, err := conn.ExecContext(ctx, `UPDATE providers SET deleted_at=?,updated_at=?,version=version+1 WHERE id=? AND version=? AND deleted_at IS NULL`, now, now, id, expectedVersion)
	if err != nil {
		return mapWriteError(err)
	}
	n, _ := r.RowsAffected()
	if n == 1 {
		_, err = conn.ExecContext(ctx, `COMMIT`)
		return mapWriteError(err)
	}
	var live int
	err = conn.QueryRowContext(ctx, `SELECT count(*) FROM providers WHERE id=? AND deleted_at IS NULL`, id).Scan(&live)
	if err != nil {
		return err
	}
	if live == 1 {
		return provider.ErrConflict
	}
	return provider.ErrNotFound
}

func getProvider(ctx context.Context, q sqlRunner, id string) (provider.Provider, error) {
	var item provider.Provider
	var created, updated string
	var backups string
	err := q.QueryRowContext(ctx, `SELECT id,COALESCE(legacy_id,''),name,protocol,base_url,COALESCE(credential_ref,''),credential_state,status,created_at,updated_at,version,COALESCE(credential_ref_backups,'[]') FROM providers WHERE id=? AND deleted_at IS NULL`, id).Scan(&item.ID, &item.LegacyID, &item.Name, &item.Protocol, &item.BaseURL, &item.CredentialRef, &item.CredentialState, &item.Status, &created, &updated, &item.Version, &backups)
	if err == sql.ErrNoRows {
		return item, provider.ErrNotFound
	}
	if err != nil {
		return item, err
	}
	item.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
	if err == nil {
		item.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated)
	}
	if err == nil {
		item.CredentialRefBackups, err = decodeCredentialBackups(backups)
	}
	if err == nil {
		item.CredentialBackupCount = len(item.CredentialRefBackups)
		item.Models, err = listModelsWith(ctx, q, id)
	}
	return item, err
}

func replaceModels(ctx context.Context, tx sqlRunner, id string, models []provider.Model) error {
	claimed := map[provider.Kind]struct{}{}
	for _, model := range models {
		if model.KindDefault {
			claimed[model.EffectiveKind()] = struct{}{}
		}
	}
	for kind := range claimed {
		if _, err := tx.ExecContext(ctx, `UPDATE provider_models SET kind_default=0 WHERE kind=? AND kind_default=1`, string(kind)); err != nil {
			return err
		}
		if kind == provider.KindASR {
			if _, err := tx.ExecContext(ctx, `UPDATE provider_models SET kind_default=0 WHERE kind=? AND kind_default=1`, string(provider.KindVoice)); err != nil {
				return err
			}
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM provider_models WHERE provider_id=?`, id); err != nil {
		return err
	}
	for position, model := range models {
		var cw any
		if model.ContextWindow > 0 {
			cw = model.ContextWindow
		}
		sv, kd := 0, 0
		if model.SupportsVision {
			sv = 1
		}
		if model.KindDefault {
			kd = 1
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO provider_models(provider_id,model_id,display_name,is_default,position,context_window,kind,supports_vision,kind_default) VALUES(?,?,?,?,?,?,?,?,?)`, id, model.ModelID, model.DisplayName, model.IsDefault, position, cw, string(model.EffectiveKind()), sv, kd); err != nil {
			return fmt.Errorf("write provider models: %w", err)
		}
	}
	return nil
}

// SQLite BUSY means another Store owns the write lock; it is retryable and is
// deliberately distinct from a Provider CAS version conflict.
func mapWriteError(err error) error {
	if err == nil {
		return nil
	}
	var sqliteErr *modernsqlite.Error
	if errors.As(err, &sqliteErr) && (sqliteErr.Code()&0xff == 5 || sqliteErr.Code()&0xff == 6) {
		return fmt.Errorf("%w: sqlite writer busy", providerapp.ErrStorageBusy)
	}
	return err
}

func formatTime(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }
func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func listModelsWith(ctx context.Context, q sqlRunner, id string) ([]provider.Model, error) {
	rows, err := q.QueryContext(ctx, `SELECT model_id, display_name, is_default, context_window, kind, supports_vision, kind_default FROM provider_models WHERE provider_id = ? ORDER BY position, model_id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []provider.Model{}
	for rows.Next() {
		var m provider.Model
		var cw sql.NullInt64
		var kind string
		if err := rows.Scan(&m.ModelID, &m.DisplayName, &m.IsDefault, &cw, &kind, &m.SupportsVision, &m.KindDefault); err != nil {
			return nil, err
		}
		if cw.Valid {
			m.ContextWindow = cw.Int64
		}
		m.Kind = provider.NormalizeKind(kind)
		if m.Kind == provider.KindLLM {
			m.Kind = ""
		}
		result = append(result, m)
	}
	return result, rows.Err()
}
