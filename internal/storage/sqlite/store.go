package sqlite

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/providerapp"
	"github.com/lunitide/lunitide/internal/secret"
	"github.com/lunitide/lunitide/migrations"
	"github.com/oklog/ulid/v2"
	modernsqlite "modernc.org/sqlite"
)

// SecureRoot is the capability required to open production storage. The root
// implementation pins the directory and verifies each database sidecar.
type SecureRoot interface {
	FilePath(name string) (string, error)
	ProtectRegularFile(name string) error
}

type Store struct {
	db        *sql.DB
	root      SecureRoot
	names     []string
	idEntropy io.Reader
}

// OpenSecure is the only production open API. A caller cannot supply a DSN.
func OpenSecure(ctx context.Context, root SecureRoot, name string) (*Store, error) {
	if filepath.Base(name) != name || !strings.HasSuffix(strings.ToLower(name), ".db") {
		return nil, fmt.Errorf("unsafe database filename %q", name)
	}
	path, err := root.FilePath(name)
	if err != nil {
		return nil, err
	}
	names := []string{name, name + "-wal", name + "-shm", name + "-journal"}
	for _, n := range names {
		if err := root.ProtectRegularFile(n); err != nil {
			return nil, err
		}
	}
	return open(ctx, path, root, names)
}

// Open is retained for isolated non-Windows tests; production must use OpenSecure.
func Open(ctx context.Context, path string) (*Store, error) {
	if path == "" || !filepath.IsAbs(path) || filepath.Ext(path) != ".db" || strings.ContainsAny(path, "\x00\r\n") {
		return nil, fmt.Errorf("unsafe SQLite path %q", path)
	}
	return open(ctx, filepath.Clean(path), nil, nil)
}

func open(ctx context.Context, path string, root SecureRoot, names []string) (*Store, error) {
	// The driver receives a filename, not a URI/DSN; special characters remain data.
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db, root: root, names: names, idEntropy: rand.Reader}
	if err := s.initialize(ctx); err != nil {
		db.Close()
		return nil, err
	}
	if root != nil {
		for _, n := range names {
			if err := root.ProtectRegularFile(n); err != nil {
				db.Close()
				return nil, err
			}
		}
	}
	return s, nil
}

func (s *Store) Close() error {
	err := s.db.Close()
	if s.root != nil {
		for _, n := range s.names {
			if e := s.root.ProtectRegularFile(n); err == nil && e != nil {
				err = e
			}
		}
	}
	return err
}

func (s *Store) ResolveProvider(ctx context.Context, id string) (provider.Provider, error) {
	return s.Get(ctx, id)
}
func (s *Store) IsCredentialReferenceAdopted(ctx context.Context, ref secret.Ref) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM credential_adoptions a JOIN providers p ON p.id=a.provider_id WHERE a.credential_ref=? AND a.provider_id=? AND a.origin=? AND a.protocol=? AND p.credential_ref=a.credential_ref AND p.deleted_at IS NULL`, ref.CredentialRef, ref.ProviderID, ref.Origin, ref.Protocol).Scan(&n)
	return n == 1, err
}

func (s *Store) ResolveCredentialBinding(ctx context.Context, id string) (secret.Ref, bool, error) {
	var ref secret.Ref
	var baseURL string
	err := s.db.QueryRowContext(ctx, `SELECT id,COALESCE(credential_ref,''),base_url,protocol FROM providers WHERE id=? AND deleted_at IS NULL`, id).Scan(&ref.ProviderID, &ref.CredentialRef, &baseURL, &ref.Protocol)
	if err == sql.ErrNoRows {
		return ref, false, provider.ErrNotFound
	}
	if err != nil {
		return ref, false, err
	}
	if ref.CredentialRef == "" {
		return ref, false, nil
	}
	ref.Origin, err = provider.NormalizeOrigin(baseURL)
	return ref, true, err
}

var manifest = []struct{ name, checksum string }{
	{"0001_provider.sql", "ede2beec8f6d9f70edd2490688a5fd8b4e6631ddd2321f689b42abb12883d02d"},
	{"0002_provider_production.sql", "42934d53c6c27cdef40bf3a58fce16b1d2025d6a547e43d985457b702ea8f5cd"},
	{"0003_provider_app.sql", "bf7ed1d958fcc04e180a9b888edb1b0f0e51cd0071227f80fa588d737d622835"},
	{"0004_model_sync_claims.sql", "160970b0aac29327774957e19acebdbb1b2f463a3c742c772e4809c29096ffff"},
	{"0005_electron_provider_metadata.sql", "8b9ab1a5b7600555a7674113fa2b1cee106e16274629b4957d4d92234439fb1f"},
	{"0006_electron_credential_adoption.sql", "0417131a4abe5e9d2c5f70809c542a0bdde36385dc710ef031bf84c39ad0a936"},
}

type sqlRunner interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func (s *Store) initialize(ctx context.Context) (resultErr error) {
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	for _, p := range []struct {
		set, query string
		want       any
	}{
		{"PRAGMA foreign_keys=ON", "PRAGMA foreign_keys", int64(1)},
		{"PRAGMA busy_timeout=5000", "PRAGMA busy_timeout", int64(5000)},
		{"PRAGMA trusted_schema=OFF", "PRAGMA trusted_schema", int64(0)},
	} {
		if _, err := conn.ExecContext(ctx, p.set); err != nil {
			return fmt.Errorf("set %s: %w", p.set, err)
		}
		var got int64
		if err := conn.QueryRowContext(ctx, p.query).Scan(&got); err != nil || got != p.want {
			return fmt.Errorf("verify %s: got %d: %w", p.query, got, err)
		}
	}
	var mode string
	if err := conn.QueryRowContext(ctx, "PRAGMA journal_mode=WAL").Scan(&mode); err != nil || !strings.EqualFold(mode, "wal") {
		return fmt.Errorf("enable WAL: %q: %w", mode, err)
	}
	// Serializes journal validation, exact legacy fingerprint validation, and backfill.
	if _, err := conn.ExecContext(ctx, "BEGIN EXCLUSIVE"); err != nil {
		return fmt.Errorf("begin exclusive migration validation: %w", err)
	}
	defer func() {
		if resultErr != nil {
			_, _ = conn.ExecContext(context.Background(), "ROLLBACK")
		}
	}()
	if err := ensureMigrationJournal(ctx, conn); err != nil {
		return err
	}
	files, err := migrationFiles()
	if err != nil {
		return err
	}
	if err := validateJournal(ctx, conn, files); err != nil {
		return err
	}
	legacy := []int{}
	for i, m := range manifest {
		var checksum sql.NullString
		err := conn.QueryRowContext(ctx, `SELECT checksum FROM schema_migrations WHERE version=?`, m.name).Scan(&checksum)
		if err == sql.ErrNoRows {
			if i == 1 {
				if err = validateV1Schema(ctx, conn); err != nil {
					return err
				}
				if err = s.preflightV1(ctx, conn); err != nil {
					return err
				}
			}
			body, _ := migrations.Files.ReadFile(m.name)
			if _, err = conn.ExecContext(ctx, string(body)); err != nil {
				return fmt.Errorf("apply migration %s: %w", m.name, err)
			}
			if i == 1 {
				if err = s.migrateV1Data(ctx, conn); err != nil {
					return err
				}
			}
			if i == 2 {
				if err = backfillOriginFingerprints(ctx, conn); err != nil {
					return err
				}
			}
			if _, err = conn.ExecContext(ctx, `INSERT INTO schema_migrations(version,applied_at,checksum) VALUES(?,?,?)`, m.name, time.Now().UTC().Format(time.RFC3339Nano), m.checksum); err != nil {
				return err
			}
		} else if err != nil {
			return err
		} else if !checksum.Valid {
			legacy = append(legacy, i)
		} else if checksum.String != m.checksum {
			return fmt.Errorf("migration %s checksum mismatch", m.name)
		}
	}
	versionBefore, fingerprint, err := validateSchema(ctx, conn)
	if err != nil {
		return err
	}
	if err = validateDataInvariants(ctx, conn); err != nil {
		return err
	}
	for _, i := range legacy {
		r, err := conn.ExecContext(ctx, `UPDATE schema_migrations SET checksum=? WHERE version=? AND checksum IS NULL`, manifest[i].checksum, manifest[i].name)
		if err != nil {
			return err
		}
		n, _ := r.RowsAffected()
		if n != 1 {
			return fmt.Errorf("legacy journal changed concurrently")
		}
	}
	versionAfter, fingerprintAfter, err := validateSchema(ctx, conn)
	if err != nil {
		return err
	}
	if versionBefore != versionAfter || fingerprint != fingerprintAfter {
		return fmt.Errorf("schema changed during legacy checksum backfill")
	}
	if _, err = conn.ExecContext(ctx, "COMMIT"); err != nil {
		return err
	}
	return nil
}

func validateV1Schema(ctx context.Context, q sqlRunner) error {
	want := map[string]string{
		"providers":       "CREATE TABLE providers (\n    id TEXT PRIMARY KEY,\n    legacy_id TEXT UNIQUE,\n    name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 500),\n    protocol TEXT NOT NULL CHECK (protocol IN ('openai_compatible', 'anthropic')),\n    base_url TEXT NOT NULL CHECK (length(base_url) BETWEEN 1 AND 2048),\n    credential_ref TEXT,\n    credential_state TEXT NOT NULL CHECK (credential_state IN ('configured', 'missing', 'unavailable')),\n    created_at TEXT NOT NULL,\n    updated_at TEXT NOT NULL,\n    version INTEGER NOT NULL DEFAULT 1 CHECK (version > 0),\n    deleted_at TEXT\n)",
		"provider_models": "CREATE TABLE provider_models (\n    provider_id TEXT NOT NULL REFERENCES providers(id) ON DELETE CASCADE,\n    model_id TEXT NOT NULL CHECK (length(model_id) BETWEEN 1 AND 500),\n    display_name TEXT NOT NULL CHECK (length(display_name) BETWEEN 1 AND 500),\n    is_default INTEGER NOT NULL DEFAULT 0 CHECK (is_default IN (0, 1)),\n    position INTEGER NOT NULL DEFAULT 0,\n    PRIMARY KEY (provider_id, model_id)\n)",
	}
	for name, expected := range want {
		var got string
		if err := q.QueryRowContext(ctx, `SELECT sql FROM sqlite_schema WHERE type='table' AND name=?`, name).Scan(&got); err != nil || got != expected {
			return fmt.Errorf("schema definition mismatch for table:%s", name)
		}
	}
	return nil
}

type legacyProvider struct {
	id, legacyID, name, protocol, baseURL, credentialRef, credentialState, created, updated string
	version                                                                                 int64
	deleted                                                                                 sql.NullString
	newID                                                                                   string
}

func (s *Store) preflightV1(ctx context.Context, q sqlRunner) error {
	rows, err := q.QueryContext(ctx, `SELECT id,COALESCE(legacy_id,''),name,protocol,base_url,COALESCE(credential_ref,''),credential_state,created_at,updated_at,version,deleted_at FROM providers ORDER BY id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var p legacyProvider
		if err := rows.Scan(&p.id, &p.legacyID, &p.name, &p.protocol, &p.baseURL, &p.credentialRef, &p.credentialState, &p.created, &p.updated, &p.version, &p.deleted); err != nil {
			return err
		}
		if len(p.name) > 500 || len(p.baseURL) > 2048 || len(p.credentialRef) > 500 {
			return fmt.Errorf("provider migration required: provider %q has an overlong field", p.id)
		}
		if _, err := canonicalOrLegacyID(p.id, p.legacyID); err != nil {
			return err
		}
		var count, defaults, overlong int
		if err := q.QueryRowContext(ctx, `SELECT count(*),COALESCE(sum(is_default),0),COALESCE(sum(length(model_id)>200 OR length(display_name)>200),0) FROM provider_models WHERE provider_id=?`, p.id).Scan(&count, &defaults, &overlong); err != nil {
			return err
		}
		if count < 1 || count > 50 {
			return fmt.Errorf("provider migration required: provider %q must have 1-50 models (has %d)", p.id, count)
		}
		if defaults != 1 {
			return fmt.Errorf("provider migration required: provider %q must have exactly one default model (has %d); a default cannot be guessed", p.id, defaults)
		}
		if overlong != 0 {
			return fmt.Errorf("provider migration required: provider %q has overlong model fields", p.id)
		}
	}
	return rows.Err()
}

// Strict policy: retain only uppercase canonical ULIDs. Other IDs are copied
// verbatim to legacy_id and replaced; conflicting pre-existing legacy_id fails.
func canonicalOrLegacyID(id, legacyID string) (bool, error) {
	parsed, err := ulid.ParseStrict(id)
	if err == nil && parsed.String() == id {
		return true, nil
	}
	if legacyID != "" && legacyID != id {
		return false, fmt.Errorf("provider migration required: legacy provider %q already has different legacy_id %q", id, legacyID)
	}
	return false, nil
}

func (s *Store) newULID(now time.Time) (string, error) {
	id, err := ulid.New(ulid.Timestamp(now.UTC()), s.idEntropy)
	if err != nil {
		return "", fmt.Errorf("generate provider ULID: %w", err)
	}
	return id.String(), nil
}

func (s *Store) migrateV1Data(ctx context.Context, q sqlRunner) error {
	rows, err := q.QueryContext(ctx, `SELECT id,COALESCE(legacy_id,''),name,protocol,base_url,COALESCE(credential_ref,''),credential_state,created_at,updated_at,version,deleted_at FROM providers_v1 ORDER BY id`)
	if err != nil {
		return err
	}
	var items []legacyProvider
	for rows.Next() {
		var p legacyProvider
		if err = rows.Scan(&p.id, &p.legacyID, &p.name, &p.protocol, &p.baseURL, &p.credentialRef, &p.credentialState, &p.created, &p.updated, &p.version, &p.deleted); err != nil {
			rows.Close()
			return err
		}
		items = append(items, p)
	}
	if err = rows.Close(); err != nil {
		return err
	}
	used := map[string]bool{}
	for i := range items {
		keep, _ := canonicalOrLegacyID(items[i].id, items[i].legacyID)
		if keep {
			items[i].newID = items[i].id
			used[items[i].id] = true
		}
	}
	for i := range items {
		p := &items[i]
		if p.newID == "" {
			for attempts := 0; attempts < 16; attempts++ {
				p.newID, err = s.newULID(time.Now())
				if err != nil {
					return err
				}
				if !used[p.newID] {
					break
				}
			}
			if used[p.newID] {
				return fmt.Errorf("generate unique provider ULID: retry limit reached")
			}
			used[p.newID] = true
			p.legacyID = p.id
		}
		ref, state := p.credentialRef, p.credentialState
		if ref == "" && state == "configured" {
			state = "missing"
		}
		if ref != "" && state != "configured" {
			ref = ""
			state = "requires_reentry"
		}
		if _, err = q.ExecContext(ctx, `INSERT INTO providers(id,legacy_id,name,protocol,base_url,credential_ref,credential_state,status,created_at,updated_at,version,deleted_at) VALUES(?,?,?,?,?,NULLIF(?,''),?,'enabled',?,?,?,?)`, p.newID, nullString(p.legacyID), p.name, p.protocol, p.baseURL, ref, state, p.created, p.updated, p.version, p.deleted); err != nil {
			return fmt.Errorf("migrate provider %q: %w", p.id, err)
		}
		mr, e := q.QueryContext(ctx, `SELECT model_id,display_name,is_default FROM provider_models_v1 WHERE provider_id=? ORDER BY position,rowid,model_id`, p.id)
		if e != nil {
			return e
		}
		pos := 0
		for mr.Next() {
			var mid, dn string
			var def bool
			if e = mr.Scan(&mid, &dn, &def); e != nil {
				mr.Close()
				return e
			}
			if _, e = q.ExecContext(ctx, `INSERT INTO provider_models(provider_id,model_id,display_name,is_default,position) VALUES(?,?,?,?,?)`, p.newID, mid, dn, def, pos); e != nil {
				mr.Close()
				return e
			}
			pos++
		}
		if e = mr.Close(); e != nil {
			return e
		}
	}
	if _, err = q.ExecContext(ctx, `DROP TABLE provider_models_v1`); err != nil {
		return err
	}
	_, err = q.ExecContext(ctx, `DROP TABLE providers_v1`)
	return err
}

func validateDataInvariants(ctx context.Context, q sqlRunner) error {
	var bad int
	err := q.QueryRowContext(ctx, `SELECT count(*) FROM providers p WHERE (SELECT count(*) FROM provider_models m WHERE m.provider_id=p.id) NOT BETWEEN 1 AND 50 OR (SELECT count(*) FROM provider_models m WHERE m.provider_id=p.id AND m.is_default=1)<>1 OR (credential_ref IS NOT NULL)<>(credential_state='configured')`).Scan(&bad)
	if err != nil {
		return err
	}
	if bad != 0 {
		return fmt.Errorf("provider data invariant violation: %d invalid provider graphs", bad)
	}
	return nil
}

func backfillOriginFingerprints(ctx context.Context, q sqlRunner) error {
	rows, err := q.QueryContext(ctx, `SELECT id,protocol,base_url FROM providers WHERE origin_fingerprint='0000000000000000000000000000000000000000000000000000000000000000'`)
	if err != nil {
		return err
	}
	type row struct{ id, protocol, base string }
	var items []row
	for rows.Next() {
		var r row
		if err = rows.Scan(&r.id, &r.protocol, &r.base); err != nil {
			rows.Close()
			return err
		}
		items = append(items, r)
	}
	if err = rows.Close(); err != nil {
		return err
	}
	for _, r := range items {
		fp, e := provider.OriginFingerprint(provider.Protocol(r.protocol), r.base)
		if e != nil {
			return fmt.Errorf("backfill provider %q origin fingerprint: %w", r.id, e)
		}
		if _, e = q.ExecContext(ctx, `UPDATE providers SET origin_fingerprint=? WHERE id=?`, fp, r.id); e != nil {
			return e
		}
	}
	return nil
}

func migrationFiles() ([]string, error) {
	entries, err := migrations.Files.ReadDir(".")
	if err != nil {
		return nil, err
	}
	var got []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			got = append(got, e.Name())
		}
	}
	sort.Strings(got)
	if len(got) != len(manifest) {
		return nil, fmt.Errorf("migration manifest/file count mismatch")
	}
	for i, m := range manifest {
		if got[i] != m.name {
			return nil, fmt.Errorf("migration manifest mismatch at %d", i)
		}
		body, _ := migrations.Files.ReadFile(m.name)
		sum := sha256.Sum256(body)
		if hex.EncodeToString(sum[:]) != m.checksum {
			return nil, fmt.Errorf("embedded migration %s checksum mismatch", m.name)
		}
	}
	return got, nil
}

func ensureMigrationJournal(ctx context.Context, q sqlRunner) error {
	if _, err := q.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations(version TEXT PRIMARY KEY, applied_at TEXT NOT NULL, checksum TEXT)`); err != nil {
		return err
	}
	rows, err := q.QueryContext(ctx, `PRAGMA table_info('schema_migrations')`)
	if err != nil {
		return err
	}
	defer rows.Close()
	has := false
	for rows.Next() {
		var cid, nn, pk int
		var name, kind string
		var def any
		if err := rows.Scan(&cid, &name, &kind, &nn, &def, &pk); err != nil {
			return err
		}
		if name == "checksum" {
			has = true
		}
	}
	if !has {
		_, err = q.ExecContext(ctx, `ALTER TABLE schema_migrations ADD COLUMN checksum TEXT`)
		return err
	}
	return rows.Err()
}

func validateJournal(ctx context.Context, q sqlRunner, _ []string) error {
	rows, err := q.QueryContext(ctx, `SELECT version FROM schema_migrations ORDER BY rowid`)
	if err != nil {
		return err
	}
	defer rows.Close()
	i := 0
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return err
		}
		if i >= len(manifest) {
			return fmt.Errorf("unknown/future migration %q", v)
		}
		if v != manifest[i].name {
			return fmt.Errorf("missing or out-of-order migration: got %q want %q", v, manifest[i].name)
		}
		i++
	}
	// A prefix is valid (remaining migrations are applied below); gaps and extras are not.
	return rows.Err()
}

var expectedSchemaSQL = map[string]string{
	"trigger:providers_credential_ref_insert":                     "CREATE TRIGGER providers_credential_ref_insert\nBEFORE INSERT ON providers WHEN NEW.credential_ref IS NOT NULL AND length(NEW.credential_ref) > 256\nBEGIN SELECT RAISE(ABORT, 'credential_ref exceeds 256'); END",
	"trigger:providers_credential_ref_update":                     "CREATE TRIGGER providers_credential_ref_update\nBEFORE UPDATE OF credential_ref ON providers WHEN NEW.credential_ref IS NOT NULL AND length(NEW.credential_ref) > 256\nBEGIN SELECT RAISE(ABORT, 'credential_ref exceeds 256'); END",
	"index:ux_provider_default_model":                             "CREATE UNIQUE INDEX ux_provider_default_model\nON provider_models(provider_id) WHERE is_default = 1",
	"index:ix_audit_aggregate_created":                            "CREATE INDEX ix_audit_aggregate_created ON audit_events(aggregate_id, created_at DESC)",
	"index:ix_idempotency_expires":                                "CREATE INDEX ix_idempotency_expires ON idempotency_records(expires_at)",
	"index:ix_idempotency_claims_expires":                         "CREATE INDEX ix_idempotency_claims_expires ON idempotency_claims(expires_at)",
	"index:ix_outbox_claim":                                       "CREATE INDEX ix_outbox_claim ON outbox_events(status, available_at, lease_until)",
	"index:ix_provider_tests_provider_created":                    "CREATE INDEX ix_provider_tests_provider_created ON provider_tests(provider_id, created_at DESC)",
	"index:ix_provider_metadata_migration_items_legacy":           "CREATE INDEX ix_provider_metadata_migration_items_legacy ON provider_metadata_migration_items(legacy_id)",
	"index:ix_provider_metadata_migration_items_credential_state": "CREATE INDEX ix_provider_metadata_migration_items_credential_state\n    ON provider_metadata_migration_items(credential_migration_state, source_fingerprint)",
	"index:ix_credential_adoptions_provider":                      "CREATE INDEX ix_credential_adoptions_provider ON credential_adoptions(provider_id)",
	"table:audit_events":                                          "CREATE TABLE audit_events (\n    id TEXT PRIMARY KEY CHECK (length(id) BETWEEN 1 AND 64),\n    action TEXT NOT NULL CHECK (action IN ('provider.created', 'provider.updated', 'provider.models.synced', 'provider.deleted')),\n    aggregate_id TEXT NOT NULL CHECK (length(aggregate_id) BETWEEN 1 AND 64),\n    actor TEXT NOT NULL CHECK (length(actor) BETWEEN 1 AND 128),\n    metadata_json TEXT NOT NULL CHECK (length(metadata_json) BETWEEN 2 AND 16384),\n    created_at TEXT NOT NULL\n)",
	"table:credential_adoptions":                                  "CREATE TABLE credential_adoptions (\n    credential_ref TEXT PRIMARY KEY CHECK (length(credential_ref) BETWEEN 1 AND 256),\n    provider_id TEXT NOT NULL REFERENCES providers(id),\n    origin TEXT NOT NULL CHECK (length(origin) BETWEEN 1 AND 2048),\n    protocol TEXT NOT NULL CHECK (protocol IN ('openai_compatible', 'anthropic')),\n    receipt_id TEXT NOT NULL UNIQUE CHECK (length(receipt_id) BETWEEN 1 AND 64),\n    adopted_at TEXT NOT NULL\n)",
	"table:idempotency_records":                                   "CREATE TABLE idempotency_records (\n    operation TEXT NOT NULL CHECK (operation IN ('provider.create', 'provider.update', 'provider.model.sync', 'provider.delete')),\n    idempotency_key TEXT NOT NULL CHECK (length(idempotency_key) BETWEEN 1 AND 128),\n    request_digest TEXT NOT NULL CHECK (length(request_digest) = 64 AND request_digest NOT GLOB '*[^0-9a-f]*'),\n    response_json TEXT NOT NULL CHECK (length(response_json) BETWEEN 2 AND 65536),\n    created_at TEXT NOT NULL,\n    expires_at TEXT NOT NULL,\n    PRIMARY KEY (operation, idempotency_key)\n)",
	"table:idempotency_claims":                                    "CREATE TABLE idempotency_claims (\n    operation TEXT NOT NULL CHECK (operation = 'provider.model.sync'),\n    idempotency_key TEXT NOT NULL CHECK (length(idempotency_key) BETWEEN 1 AND 128),\n    request_digest TEXT NOT NULL CHECK (length(request_digest) = 64 AND request_digest NOT GLOB '*[^0-9a-f]*'),\n    owner TEXT NOT NULL CHECK (length(owner) BETWEEN 1 AND 128),\n    expires_at TEXT NOT NULL,\n    PRIMARY KEY (operation, idempotency_key)\n)",
	"table:outbox_events":                                         "CREATE TABLE outbox_events (\n    id TEXT PRIMARY KEY CHECK (length(id) BETWEEN 1 AND 64),\n    topic TEXT NOT NULL CHECK (length(topic) BETWEEN 1 AND 128),\n    aggregate_id TEXT NOT NULL CHECK (length(aggregate_id) BETWEEN 1 AND 64),\n    payload_json TEXT NOT NULL CHECK (length(payload_json) BETWEEN 2 AND 65536),\n    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'claimed', 'completed', 'failed', 'dead_letter')),\n    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts BETWEEN 0 AND 1000),\n    available_at TEXT NOT NULL,\n    lease_owner TEXT CHECK (lease_owner IS NULL OR length(lease_owner) BETWEEN 1 AND 128),\n    lease_until TEXT,\n    last_error TEXT CHECK (last_error IS NULL OR length(last_error) BETWEEN 1 AND 2000),\n    created_at TEXT NOT NULL,\n    completed_at TEXT,\n    CHECK ((status = 'claimed') = (lease_owner IS NOT NULL AND lease_until IS NOT NULL)),\n    CHECK ((status IN ('completed', 'failed', 'dead_letter')) = (completed_at IS NOT NULL))\n)",
	"table:provider_tests":                                        "CREATE TABLE provider_tests (\n    id TEXT PRIMARY KEY CHECK (length(id) BETWEEN 1 AND 64),\n    provider_id TEXT NOT NULL REFERENCES providers(id) ON DELETE CASCADE,\n    status TEXT NOT NULL CHECK (status IN ('pending', 'running', 'succeeded', 'failed', 'cancelled')),\n    error_code TEXT CHECK (error_code IS NULL OR length(error_code) BETWEEN 1 AND 64),\n    started_at TEXT,\n    completed_at TEXT,\n    created_at TEXT NOT NULL,\n    CHECK (completed_at IS NULL OR started_at IS NOT NULL)\n)",
	"table:provider_metadata_migrations":                          "CREATE TABLE provider_metadata_migrations (\n    source_fingerprint TEXT PRIMARY KEY CHECK (length(source_fingerprint) = 64 AND source_fingerprint NOT GLOB '*[^0-9a-f]*'),\n    source_path_hash TEXT NOT NULL CHECK (length(source_path_hash) = 64 AND source_path_hash NOT GLOB '*[^0-9a-f]*'),\n    source_version TEXT NOT NULL CHECK (source_version IN ('0.1', '0.2', '0.2.1')),\n    state TEXT NOT NULL CHECK (state IN ('running', 'completed', 'failed')),\n    processed INTEGER NOT NULL DEFAULT 0 CHECK (processed >= 0),\n    total INTEGER NOT NULL DEFAULT 0 CHECK (total BETWEEN 0 AND 100),\n    imported INTEGER NOT NULL DEFAULT 0 CHECK (imported >= 0),\n    duplicates INTEGER NOT NULL DEFAULT 0 CHECK (duplicates >= 0),\n    conflicts INTEGER NOT NULL DEFAULT 0 CHECK (conflicts >= 0),\n    error_code TEXT CHECK (error_code IS NULL OR length(error_code) BETWEEN 1 AND 64),\n    started_at TEXT NOT NULL,\n    updated_at TEXT NOT NULL,\n    CHECK (processed <= total),\n    CHECK (imported + duplicates + conflicts <= processed)\n)",
	"table:provider_metadata_migration_items":                     "CREATE TABLE provider_metadata_migration_items (\n    source_fingerprint TEXT NOT NULL REFERENCES provider_metadata_migrations(source_fingerprint) ON DELETE CASCADE,\n    item_fingerprint TEXT NOT NULL CHECK (length(item_fingerprint) = 64 AND item_fingerprint NOT GLOB '*[^0-9a-f]*'),\n    legacy_id TEXT NOT NULL CHECK (length(legacy_id) BETWEEN 1 AND 128),\n    result TEXT NOT NULL CHECK (result IN ('imported', 'duplicate', 'conflict')),\n    provider_id TEXT,\n    detail_code TEXT NOT NULL CHECK (length(detail_code) BETWEEN 1 AND 64), credential_migration_state TEXT NOT NULL DEFAULT 'none'\n    CHECK (credential_migration_state IN ('pending', 'adopted', 'superseded', 'rejected', 'none')), credential_receipt_id TEXT\n    CHECK (credential_receipt_id IS NULL OR length(credential_receipt_id) BETWEEN 1 AND 64), credential_updated_at TEXT,\n    PRIMARY KEY (source_fingerprint, item_fingerprint)\n)",
	"table:provider_models":                                       "CREATE TABLE provider_models (\n    provider_id TEXT NOT NULL REFERENCES providers(id) ON DELETE CASCADE,\n    model_id TEXT NOT NULL CHECK (length(model_id) BETWEEN 1 AND 200),\n    display_name TEXT NOT NULL CHECK (length(display_name) BETWEEN 1 AND 200),\n    is_default INTEGER NOT NULL DEFAULT 0 CHECK (is_default IN (0, 1)),\n    position INTEGER NOT NULL DEFAULT 0 CHECK (position BETWEEN 0 AND 49),\n    PRIMARY KEY (provider_id, model_id),\n    UNIQUE (provider_id, position)\n)",
	"table:providers":                                             "CREATE TABLE providers (\n    id TEXT PRIMARY KEY,\n    legacy_id TEXT UNIQUE,\n    name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 500),\n    protocol TEXT NOT NULL CHECK (protocol IN ('openai_compatible', 'anthropic')),\n    base_url TEXT NOT NULL CHECK (length(base_url) BETWEEN 1 AND 2048),\n    credential_ref TEXT CHECK (credential_ref IS NULL OR length(credential_ref) BETWEEN 1 AND 500),\n    credential_state TEXT NOT NULL CHECK (credential_state IN ('configured', 'missing', 'unavailable', 'requires_reentry')),\n    status TEXT NOT NULL DEFAULT 'enabled' CHECK (status IN ('enabled', 'disabled')),\n    created_at TEXT NOT NULL,\n    updated_at TEXT NOT NULL,\n    version INTEGER NOT NULL DEFAULT 1 CHECK (version > 0),\n    deleted_at TEXT, origin_fingerprint TEXT NOT NULL\n    DEFAULT '0000000000000000000000000000000000000000000000000000000000000000'\n    CHECK (length(origin_fingerprint) = 64 AND origin_fingerprint NOT GLOB '*[^0-9a-f]*'),\n    CHECK ((credential_ref IS NOT NULL) = (credential_state = 'configured'))\n)",
	"table:schema_migrations":                                     "CREATE TABLE schema_migrations(version TEXT PRIMARY KEY, applied_at TEXT NOT NULL, checksum TEXT)",
}

type columnSpec struct {
	name, kind, def     string
	notNull, pk, hidden int
}

var expectedColumns = map[string][]columnSpec{
	"providers":                         {{"id", "TEXT", "", 0, 1, 0}, {"legacy_id", "TEXT", "", 0, 0, 0}, {"name", "TEXT", "", 1, 0, 0}, {"protocol", "TEXT", "", 1, 0, 0}, {"base_url", "TEXT", "", 1, 0, 0}, {"credential_ref", "TEXT", "", 0, 0, 0}, {"credential_state", "TEXT", "", 1, 0, 0}, {"status", "TEXT", "'enabled'", 1, 0, 0}, {"created_at", "TEXT", "", 1, 0, 0}, {"updated_at", "TEXT", "", 1, 0, 0}, {"version", "INTEGER", "1", 1, 0, 0}, {"deleted_at", "TEXT", "", 0, 0, 0}, {"origin_fingerprint", "TEXT", "'0000000000000000000000000000000000000000000000000000000000000000'", 1, 0, 0}},
	"provider_models":                   {{"provider_id", "TEXT", "", 1, 1, 0}, {"model_id", "TEXT", "", 1, 2, 0}, {"display_name", "TEXT", "", 1, 0, 0}, {"is_default", "INTEGER", "0", 1, 0, 0}, {"position", "INTEGER", "0", 1, 0, 0}},
	"schema_migrations":                 {{"version", "TEXT", "", 0, 1, 0}, {"applied_at", "TEXT", "", 1, 0, 0}, {"checksum", "TEXT", "", 0, 0, 0}},
	"provider_metadata_migrations":      {{"source_fingerprint", "TEXT", "", 0, 1, 0}, {"source_path_hash", "TEXT", "", 1, 0, 0}, {"source_version", "TEXT", "", 1, 0, 0}, {"state", "TEXT", "", 1, 0, 0}, {"processed", "INTEGER", "0", 1, 0, 0}, {"total", "INTEGER", "0", 1, 0, 0}, {"imported", "INTEGER", "0", 1, 0, 0}, {"duplicates", "INTEGER", "0", 1, 0, 0}, {"conflicts", "INTEGER", "0", 1, 0, 0}, {"error_code", "TEXT", "", 0, 0, 0}, {"started_at", "TEXT", "", 1, 0, 0}, {"updated_at", "TEXT", "", 1, 0, 0}},
	"provider_metadata_migration_items": {{"source_fingerprint", "TEXT", "", 1, 1, 0}, {"item_fingerprint", "TEXT", "", 1, 2, 0}, {"legacy_id", "TEXT", "", 1, 0, 0}, {"result", "TEXT", "", 1, 0, 0}, {"provider_id", "TEXT", "", 0, 0, 0}, {"detail_code", "TEXT", "", 1, 0, 0}, {"credential_migration_state", "TEXT", "'none'", 1, 0, 0}, {"credential_receipt_id", "TEXT", "", 0, 0, 0}, {"credential_updated_at", "TEXT", "", 0, 0, 0}},
	"provider_tests":                    {{"id", "TEXT", "", 0, 1, 0}, {"provider_id", "TEXT", "", 1, 0, 0}, {"status", "TEXT", "", 1, 0, 0}, {"error_code", "TEXT", "", 0, 0, 0}, {"started_at", "TEXT", "", 0, 0, 0}, {"completed_at", "TEXT", "", 0, 0, 0}, {"created_at", "TEXT", "", 1, 0, 0}},
	"idempotency_records":               {{"operation", "TEXT", "", 1, 1, 0}, {"idempotency_key", "TEXT", "", 1, 2, 0}, {"request_digest", "TEXT", "", 1, 0, 0}, {"response_json", "TEXT", "", 1, 0, 0}, {"created_at", "TEXT", "", 1, 0, 0}, {"expires_at", "TEXT", "", 1, 0, 0}},
	"idempotency_claims":                {{"operation", "TEXT", "", 1, 1, 0}, {"idempotency_key", "TEXT", "", 1, 2, 0}, {"request_digest", "TEXT", "", 1, 0, 0}, {"owner", "TEXT", "", 1, 0, 0}, {"expires_at", "TEXT", "", 1, 0, 0}},
	"outbox_events":                     {{"id", "TEXT", "", 0, 1, 0}, {"topic", "TEXT", "", 1, 0, 0}, {"aggregate_id", "TEXT", "", 1, 0, 0}, {"payload_json", "TEXT", "", 1, 0, 0}, {"status", "TEXT", "'pending'", 1, 0, 0}, {"attempts", "INTEGER", "0", 1, 0, 0}, {"available_at", "TEXT", "", 1, 0, 0}, {"lease_owner", "TEXT", "", 0, 0, 0}, {"lease_until", "TEXT", "", 0, 0, 0}, {"last_error", "TEXT", "", 0, 0, 0}, {"created_at", "TEXT", "", 1, 0, 0}, {"completed_at", "TEXT", "", 0, 0, 0}},
	"audit_events":                      {{"id", "TEXT", "", 0, 1, 0}, {"action", "TEXT", "", 1, 0, 0}, {"aggregate_id", "TEXT", "", 1, 0, 0}, {"actor", "TEXT", "", 1, 0, 0}, {"metadata_json", "TEXT", "", 1, 0, 0}, {"created_at", "TEXT", "", 1, 0, 0}},
	"credential_adoptions":              {{"credential_ref", "TEXT", "", 0, 1, 0}, {"provider_id", "TEXT", "", 1, 0, 0}, {"origin", "TEXT", "", 1, 0, 0}, {"protocol", "TEXT", "", 1, 0, 0}, {"receipt_id", "TEXT", "", 1, 0, 0}, {"adopted_at", "TEXT", "", 1, 0, 0}},
}

func validateSchema(ctx context.Context, q sqlRunner) (int64, string, error) {
	var integrity string
	if err := q.QueryRowContext(ctx, `PRAGMA quick_check`).Scan(&integrity); err != nil || integrity != "ok" {
		return 0, "", fmt.Errorf("integrity check: %q: %w", integrity, err)
	}
	rows, err := q.QueryContext(ctx, `SELECT type,name,coalesce(sql,'') FROM sqlite_schema WHERE name NOT LIKE 'sqlite_autoindex_%' ORDER BY type,name`)
	if err != nil {
		return 0, "", err
	}
	defer rows.Close()
	seen := map[string]bool{}
	var canonical []string
	for rows.Next() {
		var typ, name, sqlText string
		if err := rows.Scan(&typ, &name, &sqlText); err != nil {
			return 0, "", err
		}
		key := typ + ":" + name
		want, ok := expectedSchemaSQL[key]
		if !ok {
			return 0, "", fmt.Errorf("unknown schema object %s", key)
		}
		if sqlText != want {
			return 0, "", fmt.Errorf("schema definition mismatch for %s", key)
		}
		seen[key] = true
		canonical = append(canonical, key+":"+sqlText)
	}
	if err := rows.Err(); err != nil {
		return 0, "", err
	}
	if len(seen) != len(expectedSchemaSQL) {
		return 0, "", fmt.Errorf("schema definition object set incomplete")
	}
	for _, table := range []string{"providers", "provider_models", "schema_migrations", "provider_tests", "idempotency_records", "idempotency_claims", "outbox_events", "audit_events", "credential_adoptions", "provider_metadata_migrations", "provider_metadata_migration_items"} {
		r, e := q.QueryContext(ctx, fmt.Sprintf(`PRAGMA table_xinfo('%s')`, table))
		if e != nil {
			return 0, "", e
		}
		var got []columnSpec
		for r.Next() {
			var cid int
			var c columnSpec
			var def sql.NullString
			if e = r.Scan(&cid, &c.name, &c.kind, &c.notNull, &def, &c.pk, &c.hidden); e != nil {
				r.Close()
				return 0, "", e
			}
			c.def = def.String
			got = append(got, c)
		}
		r.Close()
		want := expectedColumns[table]
		if len(got) != len(want) {
			return 0, "", fmt.Errorf("column set mismatch for %s", table)
		}
		for i := range want {
			if got[i] != want[i] {
				return 0, "", fmt.Errorf("column definition mismatch for %s.%s", table, want[i].name)
			}
			canonical = append(canonical, fmt.Sprintf("column:%s:%#v", table, got[i]))
		}
	}
	var table, from, to, update, del, match string
	var id, seq int
	if err := q.QueryRowContext(ctx, `SELECT id,seq,"table","from","to",on_update,on_delete,match FROM pragma_foreign_key_list('provider_models')`).Scan(&id, &seq, &table, &from, &to, &update, &del, &match); err != nil || id != 0 || seq != 0 || table != "providers" || from != "provider_id" || to != "id" || update != "NO ACTION" || del != "CASCADE" || match != "NONE" {
		return 0, "", fmt.Errorf("foreign key mismatch: %w", err)
	}
	var count int
	if err := q.QueryRowContext(ctx, `SELECT count(*) FROM pragma_foreign_key_list('provider_models')`).Scan(&count); err != nil || count != 1 {
		return 0, "", fmt.Errorf("foreign key set mismatch: %w", err)
	}
	canonical = append(canonical, fmt.Sprintf("fk:%d:%d:%s:%s:%s:%s:%s:%s", id, seq, table, from, to, update, del, match))
	var unique, partial int
	var origin, columns string
	if err := q.QueryRowContext(ctx, `SELECT "unique",origin,partial FROM pragma_index_list('provider_models') WHERE name='ux_provider_default_model'`).Scan(&unique, &origin, &partial); err != nil || unique != 1 || origin != "c" || partial != 1 {
		return 0, "", fmt.Errorf("index mismatch: %w", err)
	}
	if err := q.QueryRowContext(ctx, `SELECT group_concat(name, ',') FROM pragma_index_info('ux_provider_default_model')`).Scan(&columns); err != nil || columns != "provider_id" {
		return 0, "", fmt.Errorf("index columns mismatch: %w", err)
	}
	canonical = append(canonical, fmt.Sprintf("index:%d:%s:%d:%s", unique, origin, partial, columns))
	var version int64
	if err := q.QueryRowContext(ctx, `PRAGMA schema_version`).Scan(&version); err != nil {
		return 0, "", err
	}
	sum := sha256.Sum256([]byte(strings.Join(canonical, "\n")))
	return version, hex.EncodeToString(sum[:]), nil
}

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
	query := `SELECT id, COALESCE(legacy_id,''), name, protocol, base_url, COALESCE(credential_ref,''), credential_state, status, created_at, updated_at, version FROM providers WHERE deleted_at IS NULL`
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
		if err := rows.Scan(&item.ID, &item.LegacyID, &item.Name, &item.Protocol, &item.BaseURL, &item.CredentialRef, &item.CredentialState, &item.Status, &created, &updated, &item.Version); err != nil {
			return nil, err
		}
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
	_, err = tx.ExecContext(ctx, `INSERT INTO providers(id,legacy_id,name,protocol,base_url,credential_ref,credential_state,status,created_at,updated_at,version,origin_fingerprint) VALUES(?,?,?,?,?,NULLIF(?,''),?,?,?,?,?,?)`, item.ID, nullString(item.LegacyID), item.Name, item.Protocol, item.BaseURL, item.CredentialRef, item.CredentialState, item.Status, formatTime(item.CreatedAt), formatTime(item.UpdatedAt), item.Version, fingerprint)
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
	r, err := conn.ExecContext(ctx, `UPDATE providers SET legacy_id=?,name=?,protocol=?,base_url=?,credential_ref=NULLIF(?,''),credential_state=?,status=?,updated_at=?,version=?,origin_fingerprint=? WHERE id=? AND version=? AND deleted_at IS NULL`, nullString(item.LegacyID), item.Name, item.Protocol, item.BaseURL, item.CredentialRef, item.CredentialState, item.Status, formatTime(item.UpdatedAt), item.Version, newFingerprint, item.ID, expectedVersion)
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
	err := q.QueryRowContext(ctx, `SELECT id,COALESCE(legacy_id,''),name,protocol,base_url,COALESCE(credential_ref,''),credential_state,status,created_at,updated_at,version FROM providers WHERE id=? AND deleted_at IS NULL`, id).Scan(&item.ID, &item.LegacyID, &item.Name, &item.Protocol, &item.BaseURL, &item.CredentialRef, &item.CredentialState, &item.Status, &created, &updated, &item.Version)
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
		item.Models, err = listModelsWith(ctx, q, id)
	}
	return item, err
}

func replaceModels(ctx context.Context, tx sqlRunner, id string, models []provider.Model) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM provider_models WHERE provider_id=?`, id); err != nil {
		return err
	}
	for position, model := range models {
		if _, err := tx.ExecContext(ctx, `INSERT INTO provider_models(provider_id,model_id,display_name,is_default,position) VALUES(?,?,?,?,?)`, id, model.ModelID, model.DisplayName, model.IsDefault, position); err != nil {
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

func (s *Store) listModels(ctx context.Context, id string) ([]provider.Model, error) {
	return listModelsWith(ctx, s.db, id)
}

func listModelsWith(ctx context.Context, q sqlRunner, id string) ([]provider.Model, error) {
	rows, err := q.QueryContext(ctx, `SELECT model_id, display_name, is_default FROM provider_models WHERE provider_id = ? ORDER BY position, model_id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []provider.Model{}
	for rows.Next() {
		var m provider.Model
		if err := rows.Scan(&m.ModelID, &m.DisplayName, &m.IsDefault); err != nil {
			return nil, err
		}
		result = append(result, m)
	}
	return result, rows.Err()
}
