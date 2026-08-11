package sqlite

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/lunitide/lunitide/internal/contextapp"
	"github.com/lunitide/lunitide/internal/domain/message"
	"github.com/lunitide/lunitide/internal/domain/project"
	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/domain/session"
	"github.com/lunitide/lunitide/internal/domain/stage"
	"github.com/lunitide/lunitide/internal/messageapp"
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

// ContextReader returns a contextapp.Reader backed by this store, enabling
// the chat engine to assemble durable session context for model input.
func (s *Store) ContextReader() contextapp.Reader {
	return newContextAdapter(s)
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
	{"0001_provider.sql", "d66080078c26cb33fe9761d6abe0e04c32cfb5a2e18bd921801a3fb1380adc94"},
	{"0002_provider_production.sql", "42934d53c6c27cdef40bf3a58fce16b1d2025d6a547e43d985457b702ea8f5cd"},
	{"0003_provider_app.sql", "bf7ed1d958fcc04e180a9b888edb1b0f0e51cd0071227f80fa588d737d622835"},
	{"0004_model_sync_claims.sql", "160970b0aac29327774957e19acebdbb1b2f463a3c742c772e4809c29096ffff"},
	{"0005_electron_provider_metadata.sql", "8b9ab1a5b7600555a7674113fa2b1cee106e16274629b4957d4d92234439fb1f"},
	{"0006_electron_credential_adoption.sql", "0417131a4abe5e9d2c5f70809c542a0bdde36385dc710ef031bf84c39ad0a936"},
	{"0007_project.sql", "eb31143f8347a75a3abee8670fd8c9a047fe712db64c4e8378720d08698864a3"},
	{"0008_session.sql", "08b1478bd48900da0f89a83de15c7517fbd6445790759d2374c449f67d436620"},
	{"0009_message.sql", "5695394548899d026a045f8cdbcb0ed869920563ba88c9f0b2766f85e4d06220"},
	{"0010_token_ledger.sql", "5149c8b0db94cbc87a1451351c47bc132fac75c1b420a45faef4e707c6dcbabc"},
	{"0011_compaction_checkpoint.sql", "e3cae92156656cedb1cf336b3987e046f60eafec68e3a325d7253d9c5f2a6061"},
	{"0012_planning.sql", "4356919714fb5b637ac011db8ef2899b2711282b8ce0b32c5621af11ea5ea7bf"},
	{"0013_governance.sql", "9def982ac0e9aa359d31e387c1a87000bdc365a8854f094f11757cb53bd685d0"},
	{"0014_memory.sql", "9c568f2422970421a9a30b34a096510af622b74961c3cad583a4cee206f03796"},
	{"0015_ontology.sql", "9bbd48db722e2e896367cb0c3e658c58d4564d213aadfa6955d7499dc0d7cdd9"},
	{"0016_skill.sql", "3c1b38188bc150f201a94daa5993626738154af2e6811edd66ccd56121455e58"},
	{"0017_stage.sql", "d2112f8276a176f84eab1c10bcb51cbbd3099fe1f5b4238873b1059b7af0d8ed"},
	{"0018_extended_entities.sql", "dc9b65b15dbb554f3750fcf721a37abd6540d7601ab4b0606b75ef3feb70a71d"},
	{"0019_durable_chat.sql", "4dd945a0e2c44c80a92a76079a0d0148470884f9fe036d02d66448d864d32a5a"},
	{"0020_model_context_window.sql", "ee48181f46441bba742086251bc11848a968f7c005d5a14f35e8b1dc3216c0ed"},
	{"0021_token_ledger_identity.sql", "74af0a4ee888a12173173341b0b5e4179a08aeb432f7e4f80ec322b24fb68ffc"},
	{"0022_message_tool_role.sql", "d7969dfc2afc8b97cbbec333c9414bb09ad6283fe46bcb738d27a22f8bf2de6e"},
}

const releasedV1ManifestTypo = "ede2beec8f6d9f70edd2490688a5fd8b4e6631ddd2321f689b42abb12883d02d"

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
	// foreign_keys is OFF during migration to allow table rebuilds that modify
	// CHECK constraints on tables referenced by foreign keys. It is re-enabled
	// and integrity-checked after migration completes.
	defer func() {
		if resultErr != nil {
			_, _ = conn.ExecContext(context.Background(), "ROLLBACK")
		}
		_, _ = conn.ExecContext(context.Background(), "PRAGMA foreign_keys=ON")
	}()
	for _, p := range []struct {
		set, query string
		want       any
	}{
		{"PRAGMA foreign_keys=OFF", "PRAGMA foreign_keys", int64(0)},
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
	corrections := []int{}
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
		} else if i == 0 && checksum.String == releasedV1ManifestTypo {
			// The first native release candidate applied the current embedded V1
			// bytes but journaled a stale manifest hash. Defer correction until the
			// complete schema fingerprint and data invariants pass below.
			corrections = append(corrections, i)
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
	for _, i := range corrections {
		r, err := conn.ExecContext(ctx, `UPDATE schema_migrations SET checksum=? WHERE version=? AND checksum=?`, manifest[i].checksum, manifest[i].name, releasedV1ManifestTypo)
		if err != nil {
			return err
		}
		n, _ := r.RowsAffected()
		if n != 1 {
			return fmt.Errorf("migration checksum correction changed concurrently")
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
	// Verify foreign key integrity after migration, then re-enable enforcement.
	fkRows, err := conn.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return fmt.Errorf("foreign key integrity check: %w", err)
	}
	if fkRows.Next() {
		fkRows.Close()
		return fmt.Errorf("foreign key integrity check failed after migration")
	}
	fkRows.Close()
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
	rows, err := q.QueryContext(ctx, `SELECT id,name,status,created_at,updated_at,version FROM projects`)
	if err != nil {
		return err
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		count++
		var p project.Project
		var created, updated string
		if err = rows.Scan(&p.ID, &p.Name, &p.Status, &created, &updated, &p.Version); err != nil {
			return err
		}
		if p.CreatedAt, err = time.Parse(time.RFC3339Nano, created); err != nil {
			return fmt.Errorf("project data invariant violation: invalid created_at: %w", err)
		}
		if p.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated); err != nil {
			return fmt.Errorf("project data invariant violation: invalid updated_at: %w", err)
		}
		if err = p.Validate(); err != nil {
			return fmt.Errorf("project data invariant violation: %w", err)
		}
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err = rows.Close(); err != nil {
		return err
	}
	if count > 100 {
		return fmt.Errorf("project data invariant violation: capacity %d exceeds 100", count)
	}
	rows, err = q.QueryContext(ctx, `SELECT id,project_id,title,status,created_at,updated_at,version FROM sessions`)
	if err != nil {
		return err
	}
	defer rows.Close()
	counts := map[string]int{}
	for rows.Next() {
		var v session.Session
		var created, updated string
		if err = rows.Scan(&v.ID, &v.ProjectID, &v.Title, &v.Status, &created, &updated, &v.Version); err != nil {
			return err
		}
		v.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
		if err != nil {
			return fmt.Errorf("session data invariant violation: %w", err)
		}
		v.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated)
		if err != nil {
			return fmt.Errorf("session data invariant violation: %w", err)
		}
		if err = v.Validate(); err != nil {
			return fmt.Errorf("session data invariant violation: %w", err)
		}
		counts[v.ProjectID]++
		if counts[v.ProjectID] > 100 {
			return fmt.Errorf("session data invariant violation: project capacity exceeds 100")
		}
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("session data invariant validation failed: %w", err)
	}
	if err = rows.Close(); err != nil {
		return err
	}
	rows, err = q.QueryContext(ctx, `SELECT id,project_id,phase,title,status,created_at,updated_at,version FROM stages`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var v stage.Stage
		var created, updated string
		if err = rows.Scan(&v.ID, &v.ProjectID, &v.Phase, &v.Title, &v.Status, &created, &updated, &v.Version); err != nil {
			rows.Close()
			return err
		}
		v.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
		if err != nil {
			rows.Close()
			return fmt.Errorf("stage data invariant violation: %w", err)
		}
		v.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated)
		if err != nil {
			rows.Close()
			return fmt.Errorf("stage data invariant violation: %w", err)
		}
		if err = v.Validate(); err != nil {
			rows.Close()
			return fmt.Errorf("stage data invariant violation: %w", err)
		}
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("stage data invariant validation failed: %w", err)
	}
	if err = rows.Close(); err != nil {
		return err
	}
	rows, err = q.QueryContext(ctx, `SELECT m.id,m.session_id,m.role,m.status,m.sequence,MAX(CASE WHEN p.ordinal=1 AND p.type='text' THEN p.text END),m.created_at,count(p.message_id),count(CASE WHEN p.ordinal=1 AND p.type='text' THEN 1 END) FROM messages m LEFT JOIN message_parts p ON p.message_id=m.id GROUP BY m.id ORDER BY m.session_id,m.sequence`)
	if err != nil {
		return err
	}
	messageCounts := map[string]int64{}
	for rows.Next() {
		var v message.Message
		var created string
		var text sql.NullString
		var parts, validParts int
		if err = rows.Scan(&v.ID, &v.SessionID, &v.Role, &v.Status, &v.Sequence, &text, &created, &parts, &validParts); err != nil {
			rows.Close()
			return err
		}
		if parts != 1 || validParts != 1 || !text.Valid {
			rows.Close()
			return fmt.Errorf("message data invariant violation")
		}
		v.Text = text.String
		v.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
		messageCounts[v.SessionID]++
		if err != nil || v.Validate() != nil || messageCounts[v.SessionID] != v.Sequence {
			rows.Close()
			return fmt.Errorf("message data invariant violation")
		}
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err = rows.Close(); err != nil {
		return err
	}
	var badState int
	if err = q.QueryRowContext(ctx, `SELECT count(*) FROM message_session_state st LEFT JOIN sessions s ON s.id=st.session_id WHERE s.id IS NULL OR st.text_bytes>? OR st.last_sequence<>(SELECT COALESCE(max(sequence),0) FROM messages WHERE session_id=st.session_id) OR st.message_count<>(SELECT count(*) FROM messages WHERE session_id=st.session_id) OR st.text_bytes<>(SELECT COALESCE(sum(length(CAST(p.text AS BLOB))),0) FROM messages m JOIN message_parts p ON p.message_id=m.id WHERE m.session_id=st.session_id)`, message.WorkspaceTextQuotaBytes).Scan(&badState); err != nil || badState != 0 {
		return fmt.Errorf("message state invariant violation: %w", err)
	}
	if err = q.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM sessions)<>(SELECT count(*) FROM message_session_state)`).Scan(&badState); err != nil || badState != 0 {
		return fmt.Errorf("message state invariant violation: %w", err)
	}
	if err = q.QueryRowContext(ctx, `SELECT count(*) FROM message_project_usage u LEFT JOIN projects p ON p.id=u.project_id WHERE p.id IS NULL OR u.text_bytes>? OR u.text_bytes<>(SELECT COALESCE(sum(length(CAST(mp.text AS BLOB))),0) FROM sessions s JOIN messages m ON m.session_id=s.id JOIN message_parts mp ON mp.message_id=m.id WHERE s.project_id=u.project_id)`, message.ProjectTextQuotaBytes).Scan(&badState); err != nil || badState != 0 {
		return fmt.Errorf("message project usage invariant violation: %w", err)
	}
	if err = q.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM projects)<>(SELECT count(*) FROM message_project_usage)`).Scan(&badState); err != nil || badState != 0 {
		return fmt.Errorf("message project usage invariant violation: %w", err)
	}
	var workspaceActual, workspaceStored int64
	if err = q.QueryRowContext(ctx, `SELECT COALESCE(sum(length(CAST(text AS BLOB))),0) FROM message_parts`).Scan(&workspaceActual); err != nil {
		return err
	}
	if err = q.QueryRowContext(ctx, `SELECT count(*),COALESCE(max(CASE WHEN singleton=1 THEN text_bytes END),-1) FROM message_workspace_usage`).Scan(&badState, &workspaceStored); err != nil || badState != 1 || workspaceStored != workspaceActual || workspaceStored > message.WorkspaceTextQuotaBytes {
		return fmt.Errorf("message workspace usage invariant violation: %w", err)
	}
	if err = q.QueryRowContext(ctx, `SELECT count(*) FROM messages m WHERE (SELECT count(*) FROM message_parts p WHERE p.message_id=m.id)<>1`).Scan(&bad); err != nil || bad != 0 {
		return fmt.Errorf("message data invariant violation: %w", err)
	}
	rows, err = q.QueryContext(ctx, `SELECT response_json FROM idempotency_records WHERE operation='message.append'`)
	if err != nil {
		return err
	}
	var replayResponses [][]byte
	for rows.Next() {
		var raw []byte
		if err = rows.Scan(&raw); err != nil {
			rows.Close()
			return fmt.Errorf("message idempotency data invariant violation")
		}
		replayResponses = append(replayResponses, append([]byte(nil), raw...))
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err = rows.Close(); err != nil {
		return err
	}
	for _, raw := range replayResponses {
		var replay message.Message
		if json.Unmarshal(raw, &replay) != nil || replay.Validate() != nil {
			return fmt.Errorf("message idempotency data invariant violation")
		}
		var authoritative message.Message
		var created string
		if err = q.QueryRowContext(ctx, `SELECT m.id,m.session_id,m.role,m.status,m.sequence,p.text,m.created_at FROM messages m JOIN message_parts p ON p.message_id=m.id AND p.ordinal=1 AND p.type='text' WHERE m.id=? AND (SELECT count(*) FROM message_parts x WHERE x.message_id=m.id)=1`, replay.ID).Scan(&authoritative.ID, &authoritative.SessionID, &authoritative.Role, &authoritative.Status, &authoritative.Sequence, &authoritative.Text, &created); err != nil {
			rows.Close()
			return fmt.Errorf("message idempotency data invariant violation")
		}
		authoritative.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
		if err != nil || authoritative != replay {
			return fmt.Errorf("message idempotency data invariant violation")
		}
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
	"index:ix_projects_status_created":                            "CREATE INDEX ix_projects_status_created ON projects(status, created_at, id)",
	"index:ix_sessions_project_created":                           "CREATE INDEX ix_sessions_project_created ON sessions(project_id, created_at, id)",
	"index:ix_messages_session_sequence":                          "CREATE INDEX ix_messages_session_sequence ON messages(session_id, sequence)",
	"index:ix_token_ledger_message":                               "CREATE INDEX ix_token_ledger_message ON token_ledger(message_id)",
	"index:ix_token_ledger_computed":                              "CREATE INDEX ix_token_ledger_computed ON token_ledger(computed_at)",
	"index:ix_token_ledger_identity":                              "CREATE INDEX ix_token_ledger_identity ON token_ledger(subject_type, subject_id, tokenizer_id, provider, model)",
	"index:ix_token_ledger_invalidation":                          "CREATE INDEX ix_token_ledger_invalidation ON token_ledger(tokenizer_id, invalidated_at)",
	"index:ux_token_ledger_subject_identity":                      "CREATE UNIQUE INDEX ux_token_ledger_subject_identity ON token_ledger(subject_type, subject_id, tokenizer_id, provider, model)",
	"table:token_ledger":                                          "CREATE TABLE token_ledger (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    message_id TEXT NOT NULL REFERENCES messages(id) ON DELETE CASCADE,\n    provider TEXT NOT NULL DEFAULT '' CHECK (length(provider) <= 128),\n    model TEXT NOT NULL DEFAULT '' CHECK (length(model) <= 128),\n    tokenizer_revision TEXT NOT NULL DEFAULT '' CHECK (length(tokenizer_revision) <= 64),\n    token_count INTEGER NOT NULL CHECK (token_count >= 0),\n    estimation_method TEXT NOT NULL CHECK (estimation_method IN ('char-ratio', 'tiktoken', 'provider-reported', 'manual')),\n    utf8_bytes INTEGER NOT NULL CHECK (utf8_bytes >= 0),\n    computed_at TEXT NOT NULL, subject_type TEXT NOT NULL DEFAULT 'message'\n    CHECK (subject_type IN ('message', 'message_part', 'tool_result', 'summary', 'injected_instruction')), subject_id TEXT NOT NULL DEFAULT '', tokenizer_id TEXT NOT NULL DEFAULT 'lunitide-canonical-v1'\n    CHECK (length(tokenizer_id) > 0 AND length(tokenizer_id) <= 128), invalidated_at TEXT,\n    UNIQUE (message_id, provider, model, tokenizer_revision)\n)",
	"table:audit_events":                                          "CREATE TABLE audit_events (\n    id TEXT PRIMARY KEY CHECK (length(id) BETWEEN 1 AND 64),\n    action TEXT NOT NULL CHECK (action IN ('provider.created', 'provider.updated', 'provider.models.synced', 'provider.deleted', 'project.created', 'session.created', 'message.appended', 'stage.created', 'stage.updated', 'message.assistant.appended')),\n    aggregate_id TEXT NOT NULL CHECK (length(aggregate_id) BETWEEN 1 AND 64),\n    actor TEXT NOT NULL CHECK (length(actor) BETWEEN 1 AND 128),\n    metadata_json TEXT NOT NULL CHECK (length(metadata_json) BETWEEN 2 AND 16384),\n    created_at TEXT NOT NULL\n)",
	"table:credential_adoptions":                                  "CREATE TABLE credential_adoptions (\n    credential_ref TEXT PRIMARY KEY CHECK (length(credential_ref) BETWEEN 1 AND 256),\n    provider_id TEXT NOT NULL REFERENCES providers(id),\n    origin TEXT NOT NULL CHECK (length(origin) BETWEEN 1 AND 2048),\n    protocol TEXT NOT NULL CHECK (protocol IN ('openai_compatible', 'anthropic')),\n    receipt_id TEXT NOT NULL UNIQUE CHECK (length(receipt_id) BETWEEN 1 AND 64),\n    adopted_at TEXT NOT NULL\n)",
	"table:idempotency_records":                                   "CREATE TABLE idempotency_records (\n    operation TEXT NOT NULL CHECK (operation IN ('provider.create', 'provider.update', 'provider.model.sync', 'provider.delete', 'project.create', 'session.create', 'message.append', 'stage.create', 'message.append-assistant')),\n    idempotency_key TEXT NOT NULL CHECK (length(idempotency_key) BETWEEN 1 AND 128),\n    request_digest TEXT NOT NULL CHECK (length(request_digest) = 64 AND request_digest NOT GLOB '*[^0-9a-f]*'),\n    response_json TEXT NOT NULL CHECK (length(response_json) BETWEEN 2 AND 65536),\n    created_at TEXT NOT NULL,\n    expires_at TEXT NOT NULL,\n    PRIMARY KEY (operation, idempotency_key)\n)",
	"table:idempotency_claims":                                    "CREATE TABLE idempotency_claims (\n    operation TEXT NOT NULL CHECK (operation = 'provider.model.sync'),\n    idempotency_key TEXT NOT NULL CHECK (length(idempotency_key) BETWEEN 1 AND 128),\n    request_digest TEXT NOT NULL CHECK (length(request_digest) = 64 AND request_digest NOT GLOB '*[^0-9a-f]*'),\n    owner TEXT NOT NULL CHECK (length(owner) BETWEEN 1 AND 128),\n    expires_at TEXT NOT NULL,\n    PRIMARY KEY (operation, idempotency_key)\n)",
	"table:outbox_events":                                         "CREATE TABLE outbox_events (\n    id TEXT PRIMARY KEY CHECK (length(id) BETWEEN 1 AND 64),\n    topic TEXT NOT NULL CHECK (length(topic) BETWEEN 1 AND 128),\n    aggregate_id TEXT NOT NULL CHECK (length(aggregate_id) BETWEEN 1 AND 64),\n    payload_json TEXT NOT NULL CHECK (length(payload_json) BETWEEN 2 AND 65536),\n    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'claimed', 'completed', 'failed', 'dead_letter')),\n    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts BETWEEN 0 AND 1000),\n    available_at TEXT NOT NULL,\n    lease_owner TEXT CHECK (lease_owner IS NULL OR length(lease_owner) BETWEEN 1 AND 128),\n    lease_until TEXT,\n    last_error TEXT CHECK (last_error IS NULL OR length(last_error) BETWEEN 1 AND 2000),\n    created_at TEXT NOT NULL,\n    completed_at TEXT,\n    CHECK ((status = 'claimed') = (lease_owner IS NOT NULL AND lease_until IS NOT NULL)),\n    CHECK ((status IN ('completed', 'failed', 'dead_letter')) = (completed_at IS NOT NULL))\n)",
	"table:provider_tests":                                        "CREATE TABLE provider_tests (\n    id TEXT PRIMARY KEY CHECK (length(id) BETWEEN 1 AND 64),\n    provider_id TEXT NOT NULL REFERENCES providers(id) ON DELETE CASCADE,\n    status TEXT NOT NULL CHECK (status IN ('pending', 'running', 'succeeded', 'failed', 'cancelled')),\n    error_code TEXT CHECK (error_code IS NULL OR length(error_code) BETWEEN 1 AND 64),\n    started_at TEXT,\n    completed_at TEXT,\n    created_at TEXT NOT NULL,\n    CHECK (completed_at IS NULL OR started_at IS NOT NULL)\n)",
	"table:provider_metadata_migrations":                          "CREATE TABLE provider_metadata_migrations (\n    source_fingerprint TEXT PRIMARY KEY CHECK (length(source_fingerprint) = 64 AND source_fingerprint NOT GLOB '*[^0-9a-f]*'),\n    source_path_hash TEXT NOT NULL CHECK (length(source_path_hash) = 64 AND source_path_hash NOT GLOB '*[^0-9a-f]*'),\n    source_version TEXT NOT NULL CHECK (source_version IN ('0.1', '0.2', '0.2.1')),\n    state TEXT NOT NULL CHECK (state IN ('running', 'completed', 'failed')),\n    processed INTEGER NOT NULL DEFAULT 0 CHECK (processed >= 0),\n    total INTEGER NOT NULL DEFAULT 0 CHECK (total BETWEEN 0 AND 100),\n    imported INTEGER NOT NULL DEFAULT 0 CHECK (imported >= 0),\n    duplicates INTEGER NOT NULL DEFAULT 0 CHECK (duplicates >= 0),\n    conflicts INTEGER NOT NULL DEFAULT 0 CHECK (conflicts >= 0),\n    error_code TEXT CHECK (error_code IS NULL OR length(error_code) BETWEEN 1 AND 64),\n    started_at TEXT NOT NULL,\n    updated_at TEXT NOT NULL,\n    CHECK (processed <= total),\n    CHECK (imported + duplicates + conflicts <= processed)\n)",
	"table:provider_metadata_migration_items":                     "CREATE TABLE provider_metadata_migration_items (\n    source_fingerprint TEXT NOT NULL REFERENCES provider_metadata_migrations(source_fingerprint) ON DELETE CASCADE,\n    item_fingerprint TEXT NOT NULL CHECK (length(item_fingerprint) = 64 AND item_fingerprint NOT GLOB '*[^0-9a-f]*'),\n    legacy_id TEXT NOT NULL CHECK (length(legacy_id) BETWEEN 1 AND 128),\n    result TEXT NOT NULL CHECK (result IN ('imported', 'duplicate', 'conflict')),\n    provider_id TEXT,\n    detail_code TEXT NOT NULL CHECK (length(detail_code) BETWEEN 1 AND 64), credential_migration_state TEXT NOT NULL DEFAULT 'none'\n    CHECK (credential_migration_state IN ('pending', 'adopted', 'superseded', 'rejected', 'none')), credential_receipt_id TEXT\n    CHECK (credential_receipt_id IS NULL OR length(credential_receipt_id) BETWEEN 1 AND 64), credential_updated_at TEXT,\n    PRIMARY KEY (source_fingerprint, item_fingerprint)\n)",
	"table:provider_models":                                       "CREATE TABLE provider_models (\n    provider_id TEXT NOT NULL REFERENCES providers(id) ON DELETE CASCADE,\n    model_id TEXT NOT NULL CHECK (length(model_id) BETWEEN 1 AND 200),\n    display_name TEXT NOT NULL CHECK (length(display_name) BETWEEN 1 AND 200),\n    is_default INTEGER NOT NULL DEFAULT 0 CHECK (is_default IN (0, 1)),\n    position INTEGER NOT NULL DEFAULT 0 CHECK (position BETWEEN 0 AND 49), context_window INTEGER,\n    PRIMARY KEY (provider_id, model_id),\n    UNIQUE (provider_id, position)\n)",
	"table:providers":                                             "CREATE TABLE providers (\n    id TEXT PRIMARY KEY,\n    legacy_id TEXT UNIQUE,\n    name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 500),\n    protocol TEXT NOT NULL CHECK (protocol IN ('openai_compatible', 'anthropic')),\n    base_url TEXT NOT NULL CHECK (length(base_url) BETWEEN 1 AND 2048),\n    credential_ref TEXT CHECK (credential_ref IS NULL OR length(credential_ref) BETWEEN 1 AND 500),\n    credential_state TEXT NOT NULL CHECK (credential_state IN ('configured', 'missing', 'unavailable', 'requires_reentry')),\n    status TEXT NOT NULL DEFAULT 'enabled' CHECK (status IN ('enabled', 'disabled')),\n    created_at TEXT NOT NULL,\n    updated_at TEXT NOT NULL,\n    version INTEGER NOT NULL DEFAULT 1 CHECK (version > 0),\n    deleted_at TEXT, origin_fingerprint TEXT NOT NULL\n    DEFAULT '0000000000000000000000000000000000000000000000000000000000000000'\n    CHECK (length(origin_fingerprint) = 64 AND origin_fingerprint NOT GLOB '*[^0-9a-f]*'),\n    CHECK ((credential_ref IS NOT NULL) = (credential_state = 'configured'))\n)",
	"table:projects":                                              "CREATE TABLE projects (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 200 AND name = trim(name)),\n    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'archived')),\n    created_at TEXT NOT NULL,\n    updated_at TEXT NOT NULL,\n    version INTEGER NOT NULL DEFAULT 1 CHECK (version > 0)\n)",
	"table:sessions":                                              "CREATE TABLE sessions (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,\n    title TEXT NOT NULL CHECK (length(title) BETWEEN 1 AND 200 AND title = trim(title)),\n    status TEXT NOT NULL DEFAULT 'active' CHECK (status = 'active'),\n    created_at TEXT NOT NULL,\n    updated_at TEXT NOT NULL,\n    version INTEGER NOT NULL DEFAULT 1 CHECK (version = 1)\n)",
	"table:messages":                                              "CREATE TABLE \"messages\" (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE RESTRICT,\n    role TEXT NOT NULL DEFAULT 'user' CHECK (role IN ('user', 'assistant', 'tool')),\n    status TEXT NOT NULL DEFAULT 'completed' CHECK (status IN ('completed', 'failed')),\n    sequence INTEGER NOT NULL CHECK (sequence BETWEEN 1 AND 9007199254740991),\n    created_at TEXT NOT NULL,\n    UNIQUE (session_id, sequence)\n)",
	"table:message_parts":                                         "CREATE TABLE \"message_parts\" (\n    message_id TEXT NOT NULL REFERENCES \"messages\"(id) ON DELETE CASCADE,\n    ordinal INTEGER NOT NULL CHECK (ordinal = 1),\n    type TEXT NOT NULL DEFAULT 'text' CHECK (type = 'text'),\n    text TEXT NOT NULL CHECK (length(text) BETWEEN 1 AND 16384 AND length(CAST(text AS BLOB)) <= 65536),\n    PRIMARY KEY (message_id, ordinal)\n)",
	"table:message_session_state":                                 "CREATE TABLE message_session_state (\n    session_id TEXT PRIMARY KEY REFERENCES sessions(id) ON DELETE RESTRICT,\n    last_sequence INTEGER NOT NULL CHECK (last_sequence BETWEEN 0 AND 9007199254740991),\n    message_count INTEGER NOT NULL CHECK (message_count BETWEEN 0 AND 9007199254740991),\n    text_bytes INTEGER NOT NULL CHECK (text_bytes BETWEEN 0 AND 268435456),\n    CHECK (last_sequence = message_count)\n)",
	"table:message_project_usage":                                 "CREATE TABLE message_project_usage (\n    project_id TEXT PRIMARY KEY REFERENCES projects(id) ON DELETE RESTRICT,\n    text_bytes INTEGER NOT NULL CHECK (text_bytes BETWEEN 0 AND 67108864)\n)",
	"table:message_workspace_usage":                               "CREATE TABLE message_workspace_usage (\n    singleton INTEGER PRIMARY KEY CHECK (singleton = 1),\n    text_bytes INTEGER NOT NULL CHECK (text_bytes BETWEEN 0 AND 268435456)\n)",
	"table:schema_migrations":                                     "CREATE TABLE schema_migrations(version TEXT PRIMARY KEY, applied_at TEXT NOT NULL, checksum TEXT)",
	"table:compaction_checkpoints":                                "CREATE TABLE compaction_checkpoints (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE RESTRICT,\n    version INTEGER NOT NULL CHECK (version > 0),\n    source_start_id TEXT NOT NULL REFERENCES messages(id) ON DELETE RESTRICT,\n    source_end_id TEXT NOT NULL REFERENCES messages(id) ON DELETE RESTRICT,\n    source_start_seq INTEGER NOT NULL CHECK (source_start_seq BETWEEN 1 AND 9007199254740991),\n    source_end_seq INTEGER NOT NULL CHECK (source_end_seq BETWEEN 1 AND 9007199254740991),\n    source_digest TEXT NOT NULL CHECK (length(source_digest) = 64 AND source_digest NOT GLOB '*[^0-9a-f]*'),\n    prev_checkpoint_id TEXT REFERENCES compaction_checkpoints(id),\n    prev_checkpoint_digest TEXT CHECK (prev_checkpoint_digest IS NULL OR (length(prev_checkpoint_digest) = 64 AND prev_checkpoint_digest NOT GLOB '*[^0-9a-f]*')),\n    summary_schema_version TEXT NOT NULL DEFAULT '1.0' CHECK (length(summary_schema_version) <= 16),\n    trigger TEXT NOT NULL CHECK (trigger IN ('automatic', 'manual', 'handoff')),\n    trigger_reason TEXT NOT NULL DEFAULT '' CHECK (length(trigger_reason) <= 1024),\n    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'running', 'succeeded', 'failed', 'superseded')),\n    provider TEXT NOT NULL DEFAULT '' CHECK (length(provider) <= 128),\n    model TEXT NOT NULL DEFAULT '' CHECK (length(model) <= 128),\n    summary_json TEXT NOT NULL DEFAULT '{}' CHECK (length(summary_json) BETWEEN 2 AND 65536),\n    human_summary TEXT NOT NULL DEFAULT '' CHECK (length(human_summary) <= 32768),\n    failure_code TEXT CHECK (failure_code IS NULL OR length(failure_code) <= 64),\n    created_at TEXT NOT NULL,\n    completed_at TEXT,\n    UNIQUE (session_id, version),\n    CHECK (source_start_seq <= source_end_seq),\n    CHECK ((status IN ('succeeded', 'failed', 'superseded')) = (completed_at IS NOT NULL))\n)",
	"table:handoff_capsules":                                      "CREATE TABLE handoff_capsules (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    source_session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE RESTRICT,\n    dest_session_id TEXT REFERENCES sessions(id) ON DELETE SET NULL,\n    checkpoint_id TEXT NOT NULL REFERENCES compaction_checkpoints(id) ON DELETE RESTRICT,\n    active_tasks_json TEXT NOT NULL DEFAULT '[]' CHECK (length(active_tasks_json) <= 65536),\n    recent_message_ids TEXT NOT NULL DEFAULT '[]' CHECK (length(recent_message_ids) <= 65536),\n    digest TEXT NOT NULL CHECK (length(digest) = 64 AND digest NOT GLOB '*[^0-9a-f]*'),\n    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'activated', 'expired', 'revoked')),\n    created_at TEXT NOT NULL,\n    activated_at TEXT,\n    expires_at TEXT\n)",
	"index:ix_compaction_checkpoints_session":                     "CREATE INDEX ix_compaction_checkpoints_session ON compaction_checkpoints(session_id, version DESC)",
	"index:ix_compaction_checkpoints_status":                      "CREATE INDEX ix_compaction_checkpoints_status ON compaction_checkpoints(session_id, status)",
	"index:ix_handoff_capsules_source":                            "CREATE INDEX ix_handoff_capsules_source ON handoff_capsules(source_session_id, created_at DESC)",
	"index:ix_handoff_capsules_dest":                              "CREATE INDEX ix_handoff_capsules_dest ON handoff_capsules(dest_session_id)",
	"table:plans":                                                 "CREATE TABLE plans (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,\n    stage_id TEXT,\n    name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 200),\n    description TEXT NOT NULL DEFAULT '' CHECK (length(description) <= 4096),\n    version INTEGER NOT NULL CHECK (version > 0),\n    status TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'active', 'paused', 'completed', 'cancelled', 'failed')),\n    created_at TEXT NOT NULL,\n    updated_at TEXT NOT NULL\n)",
	"table:plan_nodes":                                            "CREATE TABLE plan_nodes (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    plan_id TEXT NOT NULL REFERENCES plans(id) ON DELETE CASCADE,\n    parent_node_id TEXT REFERENCES plan_nodes(id),\n    name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 200),\n    description TEXT NOT NULL DEFAULT '' CHECK (length(description) <= 4096),\n    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'ready', 'running', 'paused', 'completed', 'failed', 'cancelled', 'blocked')),\n    risk_level TEXT NOT NULL DEFAULT 'low' CHECK (risk_level IN ('low', 'medium', 'high', 'critical')),\n    budget_tokens INTEGER,\n    estimate_tokens INTEGER,\n    worker_role TEXT NOT NULL DEFAULT '' CHECK (length(worker_role) <= 128),\n    sequence INTEGER NOT NULL CHECK (sequence > 0),\n    created_at TEXT NOT NULL,\n    updated_at TEXT NOT NULL,\n    UNIQUE (plan_id, sequence)\n)",
	"table:governance_reviews":                                    "CREATE TABLE governance_reviews (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    plan_id TEXT REFERENCES plans(id),\n    node_id TEXT REFERENCES plan_nodes(id),\n    action_type TEXT NOT NULL CHECK (length(action_type) BETWEEN 1 AND 64),\n    action_digest TEXT NOT NULL CHECK (length(action_digest) = 64 AND action_digest NOT GLOB '*[^0-9a-f]*'),\n    input_digest TEXT NOT NULL CHECK (length(input_digest) = 64 AND input_digest NOT GLOB '*[^0-9a-f]*'),\n    state_digest TEXT NOT NULL CHECK (length(state_digest) = 64 AND state_digest NOT GLOB '*[^0-9a-f]*'),\n    policy_version INTEGER NOT NULL CHECK (policy_version > 0),\n    risk_level TEXT NOT NULL CHECK (risk_level IN ('low', 'medium', 'high', 'critical')),\n    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'approved', 'rejected', 'expired', 'changed_after_approval')),\n    reviewer_note TEXT NOT NULL DEFAULT '' CHECK (length(reviewer_note) <= 4096),\n    expires_at TEXT,\n    created_at TEXT NOT NULL,\n    reviewed_at TEXT\n)",
	"table:governance_policies":                                   "CREATE TABLE governance_policies (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 200),\n    description TEXT NOT NULL DEFAULT '' CHECK (length(description) <= 4096),\n    version INTEGER NOT NULL CHECK (version > 0),\n    is_active INTEGER NOT NULL DEFAULT 1 CHECK (is_active IN (0, 1)),\n    rules_json TEXT NOT NULL CHECK (length(rules_json) BETWEEN 2 AND 65536),\n    created_at TEXT NOT NULL,\n    updated_at TEXT NOT NULL\n)",
	"table:memories":                                              "CREATE TABLE memories (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,\n    layer TEXT NOT NULL CHECK (layer IN ('working', 'episodic', 'semantic', 'procedural')),\n    scope TEXT NOT NULL CHECK (scope IN ('workspace', 'project', 'session')),\n    key TEXT NOT NULL CHECK (length(key) BETWEEN 1 AND 256),\n    content TEXT NOT NULL CHECK (length(content) BETWEEN 1 AND 65536),\n    embedding_id TEXT,\n    source_id TEXT,\n    source_type TEXT CHECK (source_type IS NULL OR length(source_type) <= 64),\n    confidence REAL NOT NULL DEFAULT 1.0 CHECK (confidence >= 0.0 AND confidence <= 1.0),\n    access_count INTEGER NOT NULL DEFAULT 0 CHECK (access_count >= 0),\n    last_accessed TEXT,\n    expires_at TEXT,\n    created_at TEXT NOT NULL,\n    updated_at TEXT NOT NULL\n)",
	"table:ontology_nodes":                                        "CREATE TABLE ontology_nodes (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,\n    type TEXT NOT NULL CHECK (type IN ('class', 'interface', 'function', 'module', 'table', 'file', 'requirement', 'artifact', 'component', 'endpoint', 'test')),\n    name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 256),\n    full_path TEXT NOT NULL DEFAULT '' CHECK (length(full_path) <= 1024),\n    description TEXT NOT NULL DEFAULT '' CHECK (length(description) <= 4096),\n    metadata_json TEXT NOT NULL DEFAULT '{}' CHECK (length(metadata_json) <= 65536),\n    version INTEGER NOT NULL CHECK (version > 0),\n    created_at TEXT NOT NULL,\n    updated_at TEXT NOT NULL\n)",
	"table:ontology_edges":                                        "CREATE TABLE ontology_edges (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    source_node_id TEXT NOT NULL REFERENCES ontology_nodes(id) ON DELETE CASCADE,\n    target_node_id TEXT NOT NULL REFERENCES ontology_nodes(id) ON DELETE CASCADE,\n    type TEXT NOT NULL CHECK (type IN ('implements', 'extends', 'depends_on', 'references', 'contains', 'tests', 'imports', 'satisfies', 'traces', 'generates', 'configures', 'authenticates', 'authorizes')),\n    label TEXT NOT NULL DEFAULT '' CHECK (length(label) <= 256),\n    properties_json TEXT NOT NULL DEFAULT '{}' CHECK (length(properties_json) <= 65536),\n    weight REAL NOT NULL DEFAULT 1.0 CHECK (weight >= 0.0 AND weight <= 1.0),\n    version INTEGER NOT NULL CHECK (version > 0),\n    created_at TEXT NOT NULL,\n    updated_at TEXT NOT NULL,\n    CHECK (source_node_id != target_node_id)\n)",
	"table:skills":                                                "CREATE TABLE skills (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 128),\n    display_name TEXT NOT NULL CHECK (length(display_name) BETWEEN 1 AND 200),\n    description TEXT NOT NULL DEFAULT '' CHECK (length(description) <= 4096),\n    version TEXT NOT NULL CHECK (length(version) BETWEEN 1 AND 32),\n    status TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'published', 'deprecated', 'disabled')),\n    permissions_json TEXT NOT NULL CHECK (length(permissions_json) BETWEEN 2 AND 2048),\n    entry_point TEXT NOT NULL CHECK (length(entry_point) BETWEEN 1 AND 512),\n    manifest_json TEXT NOT NULL CHECK (length(manifest_json) BETWEEN 2 AND 65536),\n    signature TEXT CHECK (signature IS NULL OR length(signature) <= 1024),\n    publisher_id TEXT,\n    min_engine_version TEXT CHECK (min_engine_version IS NULL OR length(min_engine_version) <= 32),\n    created_at TEXT NOT NULL,\n    updated_at TEXT NOT NULL\n)",
	"index:ix_plans_project_status":                               "CREATE INDEX ix_plans_project_status ON plans(project_id, status)",
	"index:ix_plan_nodes_plan_sequence":                           "CREATE INDEX ix_plan_nodes_plan_sequence ON plan_nodes(plan_id, sequence)",
	"index:ix_plan_nodes_status":                                  "CREATE INDEX ix_plan_nodes_status ON plan_nodes(plan_id, status)",
	"index:ix_governance_reviews_plan":                            "CREATE INDEX ix_governance_reviews_plan ON governance_reviews(plan_id, created_at DESC)",
	"index:ix_governance_reviews_node":                            "CREATE INDEX ix_governance_reviews_node ON governance_reviews(node_id)",
	"index:ix_memories_project_layer":                             "CREATE INDEX ix_memories_project_layer ON memories(project_id, layer)",
	"index:ix_memories_key":                                       "CREATE INDEX ix_memories_key ON memories(project_id, key)",
	"index:ix_ontology_nodes_project_type":                        "CREATE INDEX ix_ontology_nodes_project_type ON ontology_nodes(project_id, type)",
	"index:ux_ontology_nodes_project_path":                        "CREATE UNIQUE INDEX ux_ontology_nodes_project_path ON ontology_nodes(project_id, full_path) WHERE full_path != ''",
	"index:ix_ontology_edges_source":                              "CREATE INDEX ix_ontology_edges_source ON ontology_edges(source_node_id)",
	"index:ix_ontology_edges_target":                              "CREATE INDEX ix_ontology_edges_target ON ontology_edges(target_node_id)",
	"index:ux_skills_name_version":                                "CREATE UNIQUE INDEX ux_skills_name_version ON skills(name, version)",
	"table:stages":                                                "CREATE TABLE stages (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,\n    phase INTEGER NOT NULL CHECK (phase BETWEEN 1 AND 9),\n    title TEXT NOT NULL CHECK (length(title) BETWEEN 1 AND 200 AND title = trim(title)),\n    status TEXT NOT NULL DEFAULT 'not_started' CHECK (status IN ('not_started', 'in_progress', 'waiting_review', 'approved', 'completed', 'rejected', 'stale', 'paused', 'blocked', 'cancelled')),\n    created_at TEXT NOT NULL,\n    updated_at TEXT NOT NULL,\n    version INTEGER NOT NULL DEFAULT 1 CHECK (version > 0),\n    UNIQUE (project_id, phase)\n)",
	"index:ix_stages_project_phase":                               "CREATE INDEX ix_stages_project_phase ON stages(project_id, phase, id)",
	"table:plan_versions":                                          "CREATE TABLE plan_versions (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    plan_id TEXT NOT NULL REFERENCES plans(id) ON DELETE CASCADE,\n    version_no INTEGER NOT NULL CHECK (version_no > 0),\n    graph_hash TEXT NOT NULL CHECK (length(graph_hash) = 64 AND graph_hash NOT GLOB '*[^0-9a-f]*'),\n    created_at TEXT NOT NULL,\n    UNIQUE (plan_id, version_no)\n)",
	"index:ix_plan_versions_plan":                                  "CREATE INDEX ix_plan_versions_plan ON plan_versions(plan_id, version_no DESC)",
	"table:plan_edges":                                             "CREATE TABLE plan_edges (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    plan_version_id TEXT NOT NULL REFERENCES plan_versions(id) ON DELETE CASCADE,\n    from_node_id TEXT NOT NULL REFERENCES plan_nodes(id) ON DELETE CASCADE,\n    to_node_id TEXT NOT NULL REFERENCES plan_nodes(id) ON DELETE CASCADE,\n    condition_json TEXT NOT NULL DEFAULT '{}' CHECK (length(condition_json) BETWEEN 2 AND 8192),\n    created_at TEXT NOT NULL,\n    CHECK (from_node_id != to_node_id)\n)",
	"index:ix_plan_edges_version":                                  "CREATE INDEX ix_plan_edges_version ON plan_edges(plan_version_id)",
	"table:node_runs":                                              "CREATE TABLE node_runs (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    node_id TEXT NOT NULL REFERENCES plan_nodes(id) ON DELETE CASCADE,\n    attempt INTEGER NOT NULL CHECK (attempt > 0),\n    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'running', 'succeeded', 'failed', 'cancelled', 'timed_out')),\n    result_ref TEXT CHECK (result_ref IS NULL OR length(result_ref) BETWEEN 1 AND 512),\n    error_code TEXT CHECK (error_code IS NULL OR length(error_code) BETWEEN 1 AND 64),\n    started_at TEXT,\n    ended_at TEXT,\n    created_at TEXT NOT NULL,\n    UNIQUE (node_id, attempt),\n    CHECK (ended_at IS NULL OR started_at IS NOT NULL)\n)",
	"index:ix_node_runs_node":                                      "CREATE INDEX ix_node_runs_node ON node_runs(node_id, attempt DESC)",
	"index:ix_node_runs_status":                                    "CREATE INDEX ix_node_runs_status ON node_runs(status)",
	"table:node_run_checkpoints":                                   "CREATE TABLE node_run_checkpoints (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    node_run_id TEXT NOT NULL REFERENCES node_runs(id) ON DELETE CASCADE,\n    state_ref TEXT NOT NULL CHECK (length(state_ref) BETWEEN 1 AND 512),\n    external_effect_digest TEXT NOT NULL CHECK (length(external_effect_digest) = 64 AND external_effect_digest NOT GLOB '*[^0-9a-f]*'),\n    created_at TEXT NOT NULL\n)",
	"index:ix_node_run_checkpoints_run":                            "CREATE INDEX ix_node_run_checkpoints_run ON node_run_checkpoints(node_run_id, created_at DESC)",
	"table:tool_calls":                                             "CREATE TABLE tool_calls (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    node_run_id TEXT NOT NULL REFERENCES node_runs(id) ON DELETE CASCADE,\n    tool_id TEXT NOT NULL CHECK (length(tool_id) BETWEEN 1 AND 128),\n    args_hash TEXT NOT NULL CHECK (length(args_hash) = 64 AND args_hash NOT GLOB '*[^0-9a-f]*'),\n    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'running', 'succeeded', 'failed', 'cancelled')),\n    result_ref TEXT CHECK (result_ref IS NULL OR length(result_ref) BETWEEN 1 AND 512),\n    risk TEXT NOT NULL DEFAULT 'low' CHECK (risk IN ('low', 'medium', 'high', 'critical')),\n    approval_id TEXT REFERENCES governance_reviews(id),\n    created_at TEXT NOT NULL\n)",
	"index:ix_tool_calls_run":                                      "CREATE INDEX ix_tool_calls_run ON tool_calls(node_run_id)",
	"table:approval_decisions":                                     "CREATE TABLE approval_decisions (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    review_id TEXT NOT NULL REFERENCES governance_reviews(id) ON DELETE CASCADE,\n    decision TEXT NOT NULL CHECK (decision IN ('approved', 'rejected')),\n    actor TEXT NOT NULL CHECK (length(actor) BETWEEN 1 AND 128),\n    reason TEXT NOT NULL DEFAULT '' CHECK (length(reason) <= 4096),\n    decided_at TEXT NOT NULL,\n    UNIQUE (review_id)\n)",
	"index:ix_approval_decisions_review":                           "CREATE INDEX ix_approval_decisions_review ON approval_decisions(review_id)",
	"table:memory_sources":                                         "CREATE TABLE memory_sources (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    memory_id TEXT NOT NULL REFERENCES memories(id) ON DELETE CASCADE,\n    source_type TEXT NOT NULL CHECK (length(source_type) BETWEEN 1 AND 64),\n    source_id TEXT NOT NULL CHECK (length(source_id) BETWEEN 1 AND 256),\n    source_revision TEXT NOT NULL DEFAULT '' CHECK (length(source_revision) <= 128),\n    quote_ref TEXT CHECK (quote_ref IS NULL OR length(quote_ref) BETWEEN 1 AND 512),\n    created_at TEXT NOT NULL\n)",
	"index:ix_memory_sources_memory":                               "CREATE INDEX ix_memory_sources_memory ON memory_sources(memory_id)",
	"table:memory_revisions":                                       "CREATE TABLE memory_revisions (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    memory_id TEXT NOT NULL REFERENCES memories(id) ON DELETE CASCADE,\n    old_ref TEXT CHECK (old_ref IS NULL OR length(old_ref) BETWEEN 1 AND 512),\n    new_ref TEXT NOT NULL CHECK (length(new_ref) BETWEEN 1 AND 512),\n    reason TEXT NOT NULL DEFAULT '' CHECK (length(reason) <= 1024),\n    actor TEXT NOT NULL CHECK (length(actor) BETWEEN 1 AND 128),\n    created_at TEXT NOT NULL\n)",
	"index:ix_memory_revisions_memory":                             "CREATE INDEX ix_memory_revisions_memory ON memory_revisions(memory_id, created_at DESC)",
	"table:recall_events":                                          "CREATE TABLE recall_events (\n    id TEXT PRIMARY KEY CHECK (length(id) = 26 AND substr(id, 1, 1) GLOB '[0-7]' AND id NOT GLOB '*[^0123456789ABCDEFGHJKMNPQRSTVWXYZ]*'),\n    session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,\n    query_hash TEXT NOT NULL CHECK (length(query_hash) = 64 AND query_hash NOT GLOB '*[^0-9a-f]*'),\n    memory_id TEXT NOT NULL REFERENCES memories(id) ON DELETE CASCADE,\n    score REAL NOT NULL CHECK (score >= 0.0 AND score <= 1.0),\n    rank INTEGER NOT NULL CHECK (rank > 0),\n    injected_tokens INTEGER NOT NULL DEFAULT 0 CHECK (injected_tokens >= 0),\n    created_at TEXT NOT NULL\n)",
	"index:ix_recall_events_session":                               "CREATE INDEX ix_recall_events_session ON recall_events(session_id, created_at DESC)",
	"index:ix_recall_events_memory":                                "CREATE INDEX ix_recall_events_memory ON recall_events(memory_id)",
	"table:deletion_tombstones":                                    "CREATE TABLE deletion_tombstones (\n    owner_type TEXT NOT NULL CHECK (length(owner_type) BETWEEN 1 AND 64),\n    owner_id TEXT NOT NULL CHECK (length(owner_id) BETWEEN 1 AND 64),\n    deleted_at TEXT NOT NULL,\n    propagation_status TEXT NOT NULL DEFAULT 'pending' CHECK (propagation_status IN ('pending', 'propagated', 'failed')),\n    PRIMARY KEY (owner_type, owner_id)\n)",
	"index:ix_deletion_tombstones_status":                          "CREATE INDEX ix_deletion_tombstones_status ON deletion_tombstones(propagation_status, deleted_at)",
}

type columnSpec struct {
	name, kind, def     string
	notNull, pk, hidden int
}

var expectedColumns = map[string][]columnSpec{
	"providers":                         {{"id", "TEXT", "", 0, 1, 0}, {"legacy_id", "TEXT", "", 0, 0, 0}, {"name", "TEXT", "", 1, 0, 0}, {"protocol", "TEXT", "", 1, 0, 0}, {"base_url", "TEXT", "", 1, 0, 0}, {"credential_ref", "TEXT", "", 0, 0, 0}, {"credential_state", "TEXT", "", 1, 0, 0}, {"status", "TEXT", "'enabled'", 1, 0, 0}, {"created_at", "TEXT", "", 1, 0, 0}, {"updated_at", "TEXT", "", 1, 0, 0}, {"version", "INTEGER", "1", 1, 0, 0}, {"deleted_at", "TEXT", "", 0, 0, 0}, {"origin_fingerprint", "TEXT", "'0000000000000000000000000000000000000000000000000000000000000000'", 1, 0, 0}},
	"provider_models":                   {{"provider_id", "TEXT", "", 1, 1, 0}, {"model_id", "TEXT", "", 1, 2, 0}, {"display_name", "TEXT", "", 1, 0, 0}, {"is_default", "INTEGER", "0", 1, 0, 0}, {"position", "INTEGER", "0", 1, 0, 0}, {"context_window", "INTEGER", "", 0, 0, 0}},
	"schema_migrations":                 {{"version", "TEXT", "", 0, 1, 0}, {"applied_at", "TEXT", "", 1, 0, 0}, {"checksum", "TEXT", "", 0, 0, 0}},
	"provider_metadata_migrations":      {{"source_fingerprint", "TEXT", "", 0, 1, 0}, {"source_path_hash", "TEXT", "", 1, 0, 0}, {"source_version", "TEXT", "", 1, 0, 0}, {"state", "TEXT", "", 1, 0, 0}, {"processed", "INTEGER", "0", 1, 0, 0}, {"total", "INTEGER", "0", 1, 0, 0}, {"imported", "INTEGER", "0", 1, 0, 0}, {"duplicates", "INTEGER", "0", 1, 0, 0}, {"conflicts", "INTEGER", "0", 1, 0, 0}, {"error_code", "TEXT", "", 0, 0, 0}, {"started_at", "TEXT", "", 1, 0, 0}, {"updated_at", "TEXT", "", 1, 0, 0}},
	"provider_metadata_migration_items": {{"source_fingerprint", "TEXT", "", 1, 1, 0}, {"item_fingerprint", "TEXT", "", 1, 2, 0}, {"legacy_id", "TEXT", "", 1, 0, 0}, {"result", "TEXT", "", 1, 0, 0}, {"provider_id", "TEXT", "", 0, 0, 0}, {"detail_code", "TEXT", "", 1, 0, 0}, {"credential_migration_state", "TEXT", "'none'", 1, 0, 0}, {"credential_receipt_id", "TEXT", "", 0, 0, 0}, {"credential_updated_at", "TEXT", "", 0, 0, 0}},
	"provider_tests":                    {{"id", "TEXT", "", 0, 1, 0}, {"provider_id", "TEXT", "", 1, 0, 0}, {"status", "TEXT", "", 1, 0, 0}, {"error_code", "TEXT", "", 0, 0, 0}, {"started_at", "TEXT", "", 0, 0, 0}, {"completed_at", "TEXT", "", 0, 0, 0}, {"created_at", "TEXT", "", 1, 0, 0}},
	"idempotency_records":               {{"operation", "TEXT", "", 1, 1, 0}, {"idempotency_key", "TEXT", "", 1, 2, 0}, {"request_digest", "TEXT", "", 1, 0, 0}, {"response_json", "TEXT", "", 1, 0, 0}, {"created_at", "TEXT", "", 1, 0, 0}, {"expires_at", "TEXT", "", 1, 0, 0}},
	"idempotency_claims":                {{"operation", "TEXT", "", 1, 1, 0}, {"idempotency_key", "TEXT", "", 1, 2, 0}, {"request_digest", "TEXT", "", 1, 0, 0}, {"owner", "TEXT", "", 1, 0, 0}, {"expires_at", "TEXT", "", 1, 0, 0}},
	"outbox_events":                     {{"id", "TEXT", "", 0, 1, 0}, {"topic", "TEXT", "", 1, 0, 0}, {"aggregate_id", "TEXT", "", 1, 0, 0}, {"payload_json", "TEXT", "", 1, 0, 0}, {"status", "TEXT", "'pending'", 1, 0, 0}, {"attempts", "INTEGER", "0", 1, 0, 0}, {"available_at", "TEXT", "", 1, 0, 0}, {"lease_owner", "TEXT", "", 0, 0, 0}, {"lease_until", "TEXT", "", 0, 0, 0}, {"last_error", "TEXT", "", 0, 0, 0}, {"created_at", "TEXT", "", 1, 0, 0}, {"completed_at", "TEXT", "", 0, 0, 0}},
	"audit_events":                      {{"id", "TEXT", "", 0, 1, 0}, {"action", "TEXT", "", 1, 0, 0}, {"aggregate_id", "TEXT", "", 1, 0, 0}, {"actor", "TEXT", "", 1, 0, 0}, {"metadata_json", "TEXT", "", 1, 0, 0}, {"created_at", "TEXT", "", 1, 0, 0}},
	"credential_adoptions":              {{"credential_ref", "TEXT", "", 0, 1, 0}, {"provider_id", "TEXT", "", 1, 0, 0}, {"origin", "TEXT", "", 1, 0, 0}, {"protocol", "TEXT", "", 1, 0, 0}, {"receipt_id", "TEXT", "", 1, 0, 0}, {"adopted_at", "TEXT", "", 1, 0, 0}},
	"projects":                          {{"id", "TEXT", "", 0, 1, 0}, {"name", "TEXT", "", 1, 0, 0}, {"status", "TEXT", "'active'", 1, 0, 0}, {"created_at", "TEXT", "", 1, 0, 0}, {"updated_at", "TEXT", "", 1, 0, 0}, {"version", "INTEGER", "1", 1, 0, 0}},
	"sessions":                          {{"id", "TEXT", "", 0, 1, 0}, {"project_id", "TEXT", "", 1, 0, 0}, {"title", "TEXT", "", 1, 0, 0}, {"status", "TEXT", "'active'", 1, 0, 0}, {"created_at", "TEXT", "", 1, 0, 0}, {"updated_at", "TEXT", "", 1, 0, 0}, {"version", "INTEGER", "1", 1, 0, 0}},
	"messages":                          {{"id", "TEXT", "", 0, 1, 0}, {"session_id", "TEXT", "", 1, 0, 0}, {"role", "TEXT", "'user'", 1, 0, 0}, {"status", "TEXT", "'completed'", 1, 0, 0}, {"sequence", "INTEGER", "", 1, 0, 0}, {"created_at", "TEXT", "", 1, 0, 0}},
	"message_parts":                     {{"message_id", "TEXT", "", 1, 1, 0}, {"ordinal", "INTEGER", "", 1, 2, 0}, {"type", "TEXT", "'text'", 1, 0, 0}, {"text", "TEXT", "", 1, 0, 0}},
	"message_session_state":             {{"session_id", "TEXT", "", 0, 1, 0}, {"last_sequence", "INTEGER", "", 1, 0, 0}, {"message_count", "INTEGER", "", 1, 0, 0}, {"text_bytes", "INTEGER", "", 1, 0, 0}},
	"message_project_usage":             {{"project_id", "TEXT", "", 0, 1, 0}, {"text_bytes", "INTEGER", "", 1, 0, 0}},
	"message_workspace_usage":           {{"singleton", "INTEGER", "", 0, 1, 0}, {"text_bytes", "INTEGER", "", 1, 0, 0}},
	"token_ledger":                      {{"id", "TEXT", "", 0, 1, 0}, {"message_id", "TEXT", "", 1, 0, 0}, {"provider", "TEXT", "''", 1, 0, 0}, {"model", "TEXT", "''", 1, 0, 0}, {"tokenizer_revision", "TEXT", "''", 1, 0, 0}, {"token_count", "INTEGER", "", 1, 0, 0}, {"estimation_method", "TEXT", "", 1, 0, 0}, {"utf8_bytes", "INTEGER", "", 1, 0, 0}, {"computed_at", "TEXT", "", 1, 0, 0}, {"subject_type", "TEXT", "'message'", 1, 0, 0}, {"subject_id", "TEXT", "''", 1, 0, 0}, {"tokenizer_id", "TEXT", "'lunitide-canonical-v1'", 1, 0, 0}, {"invalidated_at", "TEXT", "", 0, 0, 0}},
	"compaction_checkpoints":           {{"id", "TEXT", "", 0, 1, 0}, {"session_id", "TEXT", "", 1, 0, 0}, {"version", "INTEGER", "", 1, 0, 0}, {"source_start_id", "TEXT", "", 1, 0, 0}, {"source_end_id", "TEXT", "", 1, 0, 0}, {"source_start_seq", "INTEGER", "", 1, 0, 0}, {"source_end_seq", "INTEGER", "", 1, 0, 0}, {"source_digest", "TEXT", "", 1, 0, 0}, {"prev_checkpoint_id", "TEXT", "", 0, 0, 0}, {"prev_checkpoint_digest", "TEXT", "", 0, 0, 0}, {"summary_schema_version", "TEXT", "'1.0'", 1, 0, 0}, {"trigger", "TEXT", "", 1, 0, 0}, {"trigger_reason", "TEXT", "''", 1, 0, 0}, {"status", "TEXT", "'pending'", 1, 0, 0}, {"provider", "TEXT", "''", 1, 0, 0}, {"model", "TEXT", "''", 1, 0, 0}, {"summary_json", "TEXT", "'{}'", 1, 0, 0}, {"human_summary", "TEXT", "''", 1, 0, 0}, {"failure_code", "TEXT", "", 0, 0, 0}, {"created_at", "TEXT", "", 1, 0, 0}, {"completed_at", "TEXT", "", 0, 0, 0}},
	"handoff_capsules":                 {{"id", "TEXT", "", 0, 1, 0}, {"source_session_id", "TEXT", "", 1, 0, 0}, {"dest_session_id", "TEXT", "", 0, 0, 0}, {"checkpoint_id", "TEXT", "", 1, 0, 0}, {"active_tasks_json", "TEXT", "'[]'", 1, 0, 0}, {"recent_message_ids", "TEXT", "'[]'", 1, 0, 0}, {"digest", "TEXT", "", 1, 0, 0}, {"status", "TEXT", "'active'", 1, 0, 0}, {"created_at", "TEXT", "", 1, 0, 0}, {"activated_at", "TEXT", "", 0, 0, 0}, {"expires_at", "TEXT", "", 0, 0, 0}},
	"plans":                             {{"id", "TEXT", "", 0, 1, 0}, {"project_id", "TEXT", "", 1, 0, 0}, {"stage_id", "TEXT", "", 0, 0, 0}, {"name", "TEXT", "", 1, 0, 0}, {"description", "TEXT", "''", 1, 0, 0}, {"version", "INTEGER", "", 1, 0, 0}, {"status", "TEXT", "'draft'", 1, 0, 0}, {"created_at", "TEXT", "", 1, 0, 0}, {"updated_at", "TEXT", "", 1, 0, 0}},
	"plan_nodes":                        {{"id", "TEXT", "", 0, 1, 0}, {"plan_id", "TEXT", "", 1, 0, 0}, {"parent_node_id", "TEXT", "", 0, 0, 0}, {"name", "TEXT", "", 1, 0, 0}, {"description", "TEXT", "''", 1, 0, 0}, {"status", "TEXT", "'pending'", 1, 0, 0}, {"risk_level", "TEXT", "'low'", 1, 0, 0}, {"budget_tokens", "INTEGER", "", 0, 0, 0}, {"estimate_tokens", "INTEGER", "", 0, 0, 0}, {"worker_role", "TEXT", "''", 1, 0, 0}, {"sequence", "INTEGER", "", 1, 0, 0}, {"created_at", "TEXT", "", 1, 0, 0}, {"updated_at", "TEXT", "", 1, 0, 0}},
	"governance_reviews":                {{"id", "TEXT", "", 0, 1, 0}, {"plan_id", "TEXT", "", 0, 0, 0}, {"node_id", "TEXT", "", 0, 0, 0}, {"action_type", "TEXT", "", 1, 0, 0}, {"action_digest", "TEXT", "", 1, 0, 0}, {"input_digest", "TEXT", "", 1, 0, 0}, {"state_digest", "TEXT", "", 1, 0, 0}, {"policy_version", "INTEGER", "", 1, 0, 0}, {"risk_level", "TEXT", "", 1, 0, 0}, {"status", "TEXT", "'pending'", 1, 0, 0}, {"reviewer_note", "TEXT", "''", 1, 0, 0}, {"expires_at", "TEXT", "", 0, 0, 0}, {"created_at", "TEXT", "", 1, 0, 0}, {"reviewed_at", "TEXT", "", 0, 0, 0}},
	"governance_policies":               {{"id", "TEXT", "", 0, 1, 0}, {"name", "TEXT", "", 1, 0, 0}, {"description", "TEXT", "''", 1, 0, 0}, {"version", "INTEGER", "", 1, 0, 0}, {"is_active", "INTEGER", "1", 1, 0, 0}, {"rules_json", "TEXT", "", 1, 0, 0}, {"created_at", "TEXT", "", 1, 0, 0}, {"updated_at", "TEXT", "", 1, 0, 0}},
	"memories":                          {{"id", "TEXT", "", 0, 1, 0}, {"project_id", "TEXT", "", 1, 0, 0}, {"layer", "TEXT", "", 1, 0, 0}, {"scope", "TEXT", "", 1, 0, 0}, {"key", "TEXT", "", 1, 0, 0}, {"content", "TEXT", "", 1, 0, 0}, {"embedding_id", "TEXT", "", 0, 0, 0}, {"source_id", "TEXT", "", 0, 0, 0}, {"source_type", "TEXT", "", 0, 0, 0}, {"confidence", "REAL", "1.0", 1, 0, 0}, {"access_count", "INTEGER", "0", 1, 0, 0}, {"last_accessed", "TEXT", "", 0, 0, 0}, {"expires_at", "TEXT", "", 0, 0, 0}, {"created_at", "TEXT", "", 1, 0, 0}, {"updated_at", "TEXT", "", 1, 0, 0}},
	"ontology_nodes":                    {{"id", "TEXT", "", 0, 1, 0}, {"project_id", "TEXT", "", 1, 0, 0}, {"type", "TEXT", "", 1, 0, 0}, {"name", "TEXT", "", 1, 0, 0}, {"full_path", "TEXT", "''", 1, 0, 0}, {"description", "TEXT", "''", 1, 0, 0}, {"metadata_json", "TEXT", "'{}'", 1, 0, 0}, {"version", "INTEGER", "", 1, 0, 0}, {"created_at", "TEXT", "", 1, 0, 0}, {"updated_at", "TEXT", "", 1, 0, 0}},
	"ontology_edges":                    {{"id", "TEXT", "", 0, 1, 0}, {"source_node_id", "TEXT", "", 1, 0, 0}, {"target_node_id", "TEXT", "", 1, 0, 0}, {"type", "TEXT", "", 1, 0, 0}, {"label", "TEXT", "''", 1, 0, 0}, {"properties_json", "TEXT", "'{}'", 1, 0, 0}, {"weight", "REAL", "1.0", 1, 0, 0}, {"version", "INTEGER", "", 1, 0, 0}, {"created_at", "TEXT", "", 1, 0, 0}, {"updated_at", "TEXT", "", 1, 0, 0}},
	"skills":                            {{"id", "TEXT", "", 0, 1, 0}, {"name", "TEXT", "", 1, 0, 0}, {"display_name", "TEXT", "", 1, 0, 0}, {"description", "TEXT", "''", 1, 0, 0}, {"version", "TEXT", "", 1, 0, 0}, {"status", "TEXT", "'draft'", 1, 0, 0}, {"permissions_json", "TEXT", "", 1, 0, 0}, {"entry_point", "TEXT", "", 1, 0, 0}, {"manifest_json", "TEXT", "", 1, 0, 0}, {"signature", "TEXT", "", 0, 0, 0}, {"publisher_id", "TEXT", "", 0, 0, 0}, {"min_engine_version", "TEXT", "", 0, 0, 0}, {"created_at", "TEXT", "", 1, 0, 0}, {"updated_at", "TEXT", "", 1, 0, 0}},
	"stages":                            {{"id", "TEXT", "", 0, 1, 0}, {"project_id", "TEXT", "", 1, 0, 0}, {"phase", "INTEGER", "", 1, 0, 0}, {"title", "TEXT", "", 1, 0, 0}, {"status", "TEXT", "'not_started'", 1, 0, 0}, {"created_at", "TEXT", "", 1, 0, 0}, {"updated_at", "TEXT", "", 1, 0, 0}, {"version", "INTEGER", "1", 1, 0, 0}},
	"plan_versions":                     {{"id", "TEXT", "", 0, 1, 0}, {"plan_id", "TEXT", "", 1, 0, 0}, {"version_no", "INTEGER", "", 1, 0, 0}, {"graph_hash", "TEXT", "", 1, 0, 0}, {"created_at", "TEXT", "", 1, 0, 0}},
	"plan_edges":                        {{"id", "TEXT", "", 0, 1, 0}, {"plan_version_id", "TEXT", "", 1, 0, 0}, {"from_node_id", "TEXT", "", 1, 0, 0}, {"to_node_id", "TEXT", "", 1, 0, 0}, {"condition_json", "TEXT", "'{}'", 1, 0, 0}, {"created_at", "TEXT", "", 1, 0, 0}},
	"node_runs":                         {{"id", "TEXT", "", 0, 1, 0}, {"node_id", "TEXT", "", 1, 0, 0}, {"attempt", "INTEGER", "", 1, 0, 0}, {"status", "TEXT", "'pending'", 1, 0, 0}, {"result_ref", "TEXT", "", 0, 0, 0}, {"error_code", "TEXT", "", 0, 0, 0}, {"started_at", "TEXT", "", 0, 0, 0}, {"ended_at", "TEXT", "", 0, 0, 0}, {"created_at", "TEXT", "", 1, 0, 0}},
	"node_run_checkpoints":              {{"id", "TEXT", "", 0, 1, 0}, {"node_run_id", "TEXT", "", 1, 0, 0}, {"state_ref", "TEXT", "", 1, 0, 0}, {"external_effect_digest", "TEXT", "", 1, 0, 0}, {"created_at", "TEXT", "", 1, 0, 0}},
	"tool_calls":                        {{"id", "TEXT", "", 0, 1, 0}, {"node_run_id", "TEXT", "", 1, 0, 0}, {"tool_id", "TEXT", "", 1, 0, 0}, {"args_hash", "TEXT", "", 1, 0, 0}, {"status", "TEXT", "'pending'", 1, 0, 0}, {"result_ref", "TEXT", "", 0, 0, 0}, {"risk", "TEXT", "'low'", 1, 0, 0}, {"approval_id", "TEXT", "", 0, 0, 0}, {"created_at", "TEXT", "", 1, 0, 0}},
	"approval_decisions":                {{"id", "TEXT", "", 0, 1, 0}, {"review_id", "TEXT", "", 1, 0, 0}, {"decision", "TEXT", "", 1, 0, 0}, {"actor", "TEXT", "", 1, 0, 0}, {"reason", "TEXT", "''", 1, 0, 0}, {"decided_at", "TEXT", "", 1, 0, 0}},
	"memory_sources":                    {{"id", "TEXT", "", 0, 1, 0}, {"memory_id", "TEXT", "", 1, 0, 0}, {"source_type", "TEXT", "", 1, 0, 0}, {"source_id", "TEXT", "", 1, 0, 0}, {"source_revision", "TEXT", "''", 1, 0, 0}, {"quote_ref", "TEXT", "", 0, 0, 0}, {"created_at", "TEXT", "", 1, 0, 0}},
	"memory_revisions":                  {{"id", "TEXT", "", 0, 1, 0}, {"memory_id", "TEXT", "", 1, 0, 0}, {"old_ref", "TEXT", "", 0, 0, 0}, {"new_ref", "TEXT", "", 1, 0, 0}, {"reason", "TEXT", "''", 1, 0, 0}, {"actor", "TEXT", "", 1, 0, 0}, {"created_at", "TEXT", "", 1, 0, 0}},
	"recall_events":                     {{"id", "TEXT", "", 0, 1, 0}, {"session_id", "TEXT", "", 1, 0, 0}, {"query_hash", "TEXT", "", 1, 0, 0}, {"memory_id", "TEXT", "", 1, 0, 0}, {"score", "REAL", "", 1, 0, 0}, {"rank", "INTEGER", "", 1, 0, 0}, {"injected_tokens", "INTEGER", "0", 1, 0, 0}, {"created_at", "TEXT", "", 1, 0, 0}},
	"deletion_tombstones":               {{"owner_type", "TEXT", "", 1, 1, 0}, {"owner_id", "TEXT", "", 1, 2, 0}, {"deleted_at", "TEXT", "", 1, 0, 0}, {"propagation_status", "TEXT", "'pending'", 1, 0, 0}},
}

func validateSchema(ctx context.Context, q sqlRunner) (int64, string, error) {
	var integrity string
	if err := q.QueryRowContext(ctx, `PRAGMA quick_check`).Scan(&integrity); err != nil || integrity != "ok" {
		return 0, "", fmt.Errorf("integrity check: %q: %w", integrity, err)
	}
	fkRows, err := q.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		return 0, "", fmt.Errorf("foreign key check: %w", err)
	}
	if fkRows.Next() {
		var table string
		var rowID sql.NullInt64
		var parent string
		var fkID int
		if err = fkRows.Scan(&table, &rowID, &parent, &fkID); err != nil {
			fkRows.Close()
			return 0, "", fmt.Errorf("foreign key check: %w", err)
		}
		fkRows.Close()
		return 0, "", fmt.Errorf("foreign key violation: table=%s rowid=%v parent=%s constraint=%d", table, rowID, parent, fkID)
	}
	if err = fkRows.Err(); err != nil {
		fkRows.Close()
		return 0, "", fmt.Errorf("foreign key check: %w", err)
	}
	if err = fkRows.Close(); err != nil {
		return 0, "", err
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
	for _, table := range []string{"providers", "provider_models", "projects", "sessions", "messages", "message_parts", "token_ledger", "compaction_checkpoints", "handoff_capsules", "schema_migrations", "provider_tests", "idempotency_records", "idempotency_claims", "outbox_events", "audit_events", "credential_adoptions", "provider_metadata_migrations", "provider_metadata_migration_items", "plans", "plan_nodes", "governance_reviews", "governance_policies", "memories", "ontology_nodes", "ontology_edges", "skills", "stages", "plan_versions", "plan_edges", "node_runs", "node_run_checkpoints", "tool_calls", "approval_decisions", "memory_sources", "memory_revisions", "recall_events", "deletion_tombstones"} {
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
	if err := q.QueryRowContext(ctx, `SELECT id,seq,"table","from","to",on_update,on_delete,match FROM pragma_foreign_key_list('sessions')`).Scan(&id, &seq, &table, &from, &to, &update, &del, &match); err != nil || id != 0 || seq != 0 || table != "projects" || from != "project_id" || to != "id" || update != "NO ACTION" || del != "RESTRICT" || match != "NONE" {
		return 0, "", fmt.Errorf("sessions foreign key mismatch: %w", err)
	}
	if err := q.QueryRowContext(ctx, `SELECT count(*) FROM pragma_foreign_key_list('sessions')`).Scan(&count); err != nil || count != 1 {
		return 0, "", fmt.Errorf("sessions foreign key set mismatch: %w", err)
	}
	canonical = append(canonical, fmt.Sprintf("fk:sessions:%d:%d:%s:%s:%s:%s:%s:%s", id, seq, table, from, to, update, del, match))
	// stages FK: project_id → projects
	if err := q.QueryRowContext(ctx, `SELECT id,seq,"table","from","to",on_update,on_delete,match FROM pragma_foreign_key_list('stages')`).Scan(&id, &seq, &table, &from, &to, &update, &del, &match); err != nil || id != 0 || seq != 0 || table != "projects" || from != "project_id" || to != "id" || update != "NO ACTION" || del != "RESTRICT" || match != "NONE" {
		return 0, "", fmt.Errorf("stages foreign key mismatch: %w", err)
	}
	if err := q.QueryRowContext(ctx, `SELECT count(*) FROM pragma_foreign_key_list('stages')`).Scan(&count); err != nil || count != 1 {
		return 0, "", fmt.Errorf("stages foreign key set mismatch: %w", err)
	}
	canonical = append(canonical, fmt.Sprintf("fk:stages:%d:%d:%s:%s:%s:%s:%s:%s", id, seq, table, from, to, update, del, match))
	// compaction_checkpoints FKs: session_id, source_start_id, source_end_id, prev_checkpoint_id
	if err := q.QueryRowContext(ctx, `SELECT count(*) FROM pragma_foreign_key_list('compaction_checkpoints')`).Scan(&count); err != nil || count != 4 {
		return 0, "", fmt.Errorf("compaction_checkpoints FK set mismatch: count=%d err=%w", count, err)
	}
	var cpFkRows *sql.Rows
	cpFkRows, err = q.QueryContext(ctx, `SELECT id,seq,"table","from","to",on_update,on_delete,match FROM pragma_foreign_key_list('compaction_checkpoints') ORDER BY id`)
	if err != nil {
		return 0, "", fmt.Errorf("compaction_checkpoints FK query: %w", err)
	}
	type fkRow struct {
		id, seq                          int
		table, from, to, update, del, match string
	}
	cpFKs := map[string]fkRow{}
	for cpFkRows.Next() {
		var r fkRow
		if err := cpFkRows.Scan(&r.id, &r.seq, &r.table, &r.from, &r.to, &r.update, &r.del, &r.match); err != nil {
			cpFkRows.Close()
			return 0, "", err
		}
		cpFKs[r.from] = r
	}
	cpFkRows.Close()
	if len(cpFKs) != 4 {
		return 0, "", fmt.Errorf("compaction_checkpoints FK count mismatch: got %d", len(cpFKs))
	}
	// FK: session_id → sessions
	if r, ok := cpFKs["session_id"]; !ok || r.seq != 0 || r.table != "sessions" || r.from != "session_id" || r.to != "id" || r.update != "NO ACTION" || r.del != "RESTRICT" || r.match != "NONE" {
		return 0, "", fmt.Errorf("compaction_checkpoints FK session_id mismatch: %+v", r)
	}
	canonical = append(canonical, fmt.Sprintf("fk:compaction_checkpoints:session_id:%d:%d:%s:%s:%s:%s:%s:%s", cpFKs["session_id"].id, cpFKs["session_id"].seq, cpFKs["session_id"].table, cpFKs["session_id"].from, cpFKs["session_id"].to, cpFKs["session_id"].update, cpFKs["session_id"].del, cpFKs["session_id"].match))
	// FK: source_start_id → messages
	if r, ok := cpFKs["source_start_id"]; !ok || r.seq != 0 || r.table != "messages" || r.from != "source_start_id" || r.to != "id" || r.update != "NO ACTION" || r.del != "RESTRICT" || r.match != "NONE" {
		return 0, "", fmt.Errorf("compaction_checkpoints FK source_start_id mismatch: %+v", r)
	}
	canonical = append(canonical, fmt.Sprintf("fk:compaction_checkpoints:source_start_id:%d:%d:%s:%s:%s:%s:%s:%s", cpFKs["source_start_id"].id, cpFKs["source_start_id"].seq, cpFKs["source_start_id"].table, cpFKs["source_start_id"].from, cpFKs["source_start_id"].to, cpFKs["source_start_id"].update, cpFKs["source_start_id"].del, cpFKs["source_start_id"].match))
	// FK: source_end_id → messages
	if r, ok := cpFKs["source_end_id"]; !ok || r.seq != 0 || r.table != "messages" || r.from != "source_end_id" || r.to != "id" || r.update != "NO ACTION" || r.del != "RESTRICT" || r.match != "NONE" {
		return 0, "", fmt.Errorf("compaction_checkpoints FK source_end_id mismatch: %+v", r)
	}
	canonical = append(canonical, fmt.Sprintf("fk:compaction_checkpoints:source_end_id:%d:%d:%s:%s:%s:%s:%s:%s", cpFKs["source_end_id"].id, cpFKs["source_end_id"].seq, cpFKs["source_end_id"].table, cpFKs["source_end_id"].from, cpFKs["source_end_id"].to, cpFKs["source_end_id"].update, cpFKs["source_end_id"].del, cpFKs["source_end_id"].match))
	// FK: prev_checkpoint_id → compaction_checkpoints
	if r, ok := cpFKs["prev_checkpoint_id"]; !ok || r.seq != 0 || r.table != "compaction_checkpoints" || r.from != "prev_checkpoint_id" || r.to != "id" || r.update != "NO ACTION" || r.del != "NO ACTION" || r.match != "NONE" {
		return 0, "", fmt.Errorf("compaction_checkpoints FK prev_checkpoint_id mismatch: %+v", r)
	}
	canonical = append(canonical, fmt.Sprintf("fk:compaction_checkpoints:prev_checkpoint_id:%d:%d:%s:%s:%s:%s:%s:%s", cpFKs["prev_checkpoint_id"].id, cpFKs["prev_checkpoint_id"].seq, cpFKs["prev_checkpoint_id"].table, cpFKs["prev_checkpoint_id"].from, cpFKs["prev_checkpoint_id"].to, cpFKs["prev_checkpoint_id"].update, cpFKs["prev_checkpoint_id"].del, cpFKs["prev_checkpoint_id"].match))
	// handoff_capsules FKs: source_session_id, dest_session_id, checkpoint_id
	if err := q.QueryRowContext(ctx, `SELECT count(*) FROM pragma_foreign_key_list('handoff_capsules')`).Scan(&count); err != nil || count != 3 {
		return 0, "", fmt.Errorf("handoff_capsules FK set mismatch: count=%d err=%w", count, err)
	}
	var hcFkRows *sql.Rows
	hcFkRows, err = q.QueryContext(ctx, `SELECT id,seq,"table","from","to",on_update,on_delete,match FROM pragma_foreign_key_list('handoff_capsules') ORDER BY id`)
	if err != nil {
		return 0, "", fmt.Errorf("handoff_capsules FK query: %w", err)
	}
	hcFKs := map[string]fkRow{}
	for hcFkRows.Next() {
		var r fkRow
		if err := hcFkRows.Scan(&r.id, &r.seq, &r.table, &r.from, &r.to, &r.update, &r.del, &r.match); err != nil {
			hcFkRows.Close()
			return 0, "", err
		}
		hcFKs[r.from] = r
	}
	hcFkRows.Close()
	if len(hcFKs) != 3 {
		return 0, "", fmt.Errorf("handoff_capsules FK count mismatch: got %d", len(hcFKs))
	}
	// FK: source_session_id → sessions
	if r, ok := hcFKs["source_session_id"]; !ok || r.seq != 0 || r.table != "sessions" || r.from != "source_session_id" || r.to != "id" || r.update != "NO ACTION" || r.del != "RESTRICT" || r.match != "NONE" {
		return 0, "", fmt.Errorf("handoff_capsules FK source_session_id mismatch: %+v", r)
	}
	canonical = append(canonical, fmt.Sprintf("fk:handoff_capsules:source_session_id:%d:%d:%s:%s:%s:%s:%s:%s", hcFKs["source_session_id"].id, hcFKs["source_session_id"].seq, hcFKs["source_session_id"].table, hcFKs["source_session_id"].from, hcFKs["source_session_id"].to, hcFKs["source_session_id"].update, hcFKs["source_session_id"].del, hcFKs["source_session_id"].match))
	// FK: dest_session_id → sessions
	if r, ok := hcFKs["dest_session_id"]; !ok || r.seq != 0 || r.table != "sessions" || r.from != "dest_session_id" || r.to != "id" || r.update != "NO ACTION" || r.del != "SET NULL" || r.match != "NONE" {
		return 0, "", fmt.Errorf("handoff_capsules FK dest_session_id mismatch: %+v", r)
	}
	canonical = append(canonical, fmt.Sprintf("fk:handoff_capsules:dest_session_id:%d:%d:%s:%s:%s:%s:%s:%s", hcFKs["dest_session_id"].id, hcFKs["dest_session_id"].seq, hcFKs["dest_session_id"].table, hcFKs["dest_session_id"].from, hcFKs["dest_session_id"].to, hcFKs["dest_session_id"].update, hcFKs["dest_session_id"].del, hcFKs["dest_session_id"].match))
	// FK: checkpoint_id → compaction_checkpoints
	if r, ok := hcFKs["checkpoint_id"]; !ok || r.seq != 0 || r.table != "compaction_checkpoints" || r.from != "checkpoint_id" || r.to != "id" || r.update != "NO ACTION" || r.del != "RESTRICT" || r.match != "NONE" {
		return 0, "", fmt.Errorf("handoff_capsules FK checkpoint_id mismatch: %+v", r)
	}
	canonical = append(canonical, fmt.Sprintf("fk:handoff_capsules:checkpoint_id:%d:%d:%s:%s:%s:%s:%s:%s", hcFKs["checkpoint_id"].id, hcFKs["checkpoint_id"].seq, hcFKs["checkpoint_id"].table, hcFKs["checkpoint_id"].from, hcFKs["checkpoint_id"].to, hcFKs["checkpoint_id"].update, hcFKs["checkpoint_id"].del, hcFKs["checkpoint_id"].match))
	// plans FKs: project_id → projects
	if err := q.QueryRowContext(ctx, `SELECT count(*) FROM pragma_foreign_key_list('plans')`).Scan(&count); err != nil || count != 1 {
		return 0, "", fmt.Errorf("plans FK set mismatch: count=%d err=%w", count, err)
	}
	var plansFkRows *sql.Rows
	plansFkRows, err = q.QueryContext(ctx, `SELECT id,seq,"table","from","to",on_update,on_delete,match FROM pragma_foreign_key_list('plans') ORDER BY id`)
	if err != nil {
		return 0, "", fmt.Errorf("plans FK query: %w", err)
	}
	plansFKs := map[string]fkRow{}
	for plansFkRows.Next() {
		var r fkRow
		if err := plansFkRows.Scan(&r.id, &r.seq, &r.table, &r.from, &r.to, &r.update, &r.del, &r.match); err != nil {
			plansFkRows.Close()
			return 0, "", err
		}
		plansFKs[r.from] = r
	}
	plansFkRows.Close()
	if len(plansFKs) != 1 {
		return 0, "", fmt.Errorf("plans FK count mismatch: got %d", len(plansFKs))
	}
	// FK: project_id → projects
	if r, ok := plansFKs["project_id"]; !ok || r.seq != 0 || r.table != "projects" || r.from != "project_id" || r.to != "id" || r.update != "NO ACTION" || r.del != "RESTRICT" || r.match != "NONE" {
		return 0, "", fmt.Errorf("plans FK project_id mismatch: %+v", r)
	}
	canonical = append(canonical, fmt.Sprintf("fk:plans:project_id:%d:%d:%s:%s:%s:%s:%s:%s", plansFKs["project_id"].id, plansFKs["project_id"].seq, plansFKs["project_id"].table, plansFKs["project_id"].from, plansFKs["project_id"].to, plansFKs["project_id"].update, plansFKs["project_id"].del, plansFKs["project_id"].match))
	// plan_nodes FKs: plan_id → plans, parent_node_id → plan_nodes
	if err := q.QueryRowContext(ctx, `SELECT count(*) FROM pragma_foreign_key_list('plan_nodes')`).Scan(&count); err != nil || count != 2 {
		return 0, "", fmt.Errorf("plan_nodes FK set mismatch: count=%d err=%w", count, err)
	}
	var pnFkRows *sql.Rows
	pnFkRows, err = q.QueryContext(ctx, `SELECT id,seq,"table","from","to",on_update,on_delete,match FROM pragma_foreign_key_list('plan_nodes') ORDER BY id`)
	if err != nil {
		return 0, "", fmt.Errorf("plan_nodes FK query: %w", err)
	}
	pnFKs := map[string]fkRow{}
	for pnFkRows.Next() {
		var r fkRow
		if err := pnFkRows.Scan(&r.id, &r.seq, &r.table, &r.from, &r.to, &r.update, &r.del, &r.match); err != nil {
			pnFkRows.Close()
			return 0, "", err
		}
		pnFKs[r.from] = r
	}
	pnFkRows.Close()
	if len(pnFKs) != 2 {
		return 0, "", fmt.Errorf("plan_nodes FK count mismatch: got %d", len(pnFKs))
	}
	// FK: plan_id → plans
	if r, ok := pnFKs["plan_id"]; !ok || r.seq != 0 || r.table != "plans" || r.from != "plan_id" || r.to != "id" || r.update != "NO ACTION" || r.del != "CASCADE" || r.match != "NONE" {
		return 0, "", fmt.Errorf("plan_nodes FK plan_id mismatch: %+v", r)
	}
	canonical = append(canonical, fmt.Sprintf("fk:plan_nodes:plan_id:%d:%d:%s:%s:%s:%s:%s:%s", pnFKs["plan_id"].id, pnFKs["plan_id"].seq, pnFKs["plan_id"].table, pnFKs["plan_id"].from, pnFKs["plan_id"].to, pnFKs["plan_id"].update, pnFKs["plan_id"].del, pnFKs["plan_id"].match))
	// FK: parent_node_id → plan_nodes
	if r, ok := pnFKs["parent_node_id"]; !ok || r.seq != 0 || r.table != "plan_nodes" || r.from != "parent_node_id" || r.to != "id" || r.update != "NO ACTION" || r.del != "NO ACTION" || r.match != "NONE" {
		return 0, "", fmt.Errorf("plan_nodes FK parent_node_id mismatch: %+v", r)
	}
	canonical = append(canonical, fmt.Sprintf("fk:plan_nodes:parent_node_id:%d:%d:%s:%s:%s:%s:%s:%s", pnFKs["parent_node_id"].id, pnFKs["parent_node_id"].seq, pnFKs["parent_node_id"].table, pnFKs["parent_node_id"].from, pnFKs["parent_node_id"].to, pnFKs["parent_node_id"].update, pnFKs["parent_node_id"].del, pnFKs["parent_node_id"].match))
	// governance_reviews FKs: plan_id → plans, node_id → plan_nodes
	if err := q.QueryRowContext(ctx, `SELECT count(*) FROM pragma_foreign_key_list('governance_reviews')`).Scan(&count); err != nil || count != 2 {
		return 0, "", fmt.Errorf("governance_reviews FK set mismatch: count=%d err=%w", count, err)
	}
	var grFkRows *sql.Rows
	grFkRows, err = q.QueryContext(ctx, `SELECT id,seq,"table","from","to",on_update,on_delete,match FROM pragma_foreign_key_list('governance_reviews') ORDER BY id`)
	if err != nil {
		return 0, "", fmt.Errorf("governance_reviews FK query: %w", err)
	}
	grFKs := map[string]fkRow{}
	for grFkRows.Next() {
		var r fkRow
		if err := grFkRows.Scan(&r.id, &r.seq, &r.table, &r.from, &r.to, &r.update, &r.del, &r.match); err != nil {
			grFkRows.Close()
			return 0, "", err
		}
		grFKs[r.from] = r
	}
	grFkRows.Close()
	if len(grFKs) != 2 {
		return 0, "", fmt.Errorf("governance_reviews FK count mismatch: got %d", len(grFKs))
	}
	// FK: plan_id → plans
	if r, ok := grFKs["plan_id"]; !ok || r.seq != 0 || r.table != "plans" || r.from != "plan_id" || r.to != "id" || r.update != "NO ACTION" || r.del != "NO ACTION" || r.match != "NONE" {
		return 0, "", fmt.Errorf("governance_reviews FK plan_id mismatch: %+v", r)
	}
	canonical = append(canonical, fmt.Sprintf("fk:governance_reviews:plan_id:%d:%d:%s:%s:%s:%s:%s:%s", grFKs["plan_id"].id, grFKs["plan_id"].seq, grFKs["plan_id"].table, grFKs["plan_id"].from, grFKs["plan_id"].to, grFKs["plan_id"].update, grFKs["plan_id"].del, grFKs["plan_id"].match))
	// FK: node_id → plan_nodes
	if r, ok := grFKs["node_id"]; !ok || r.seq != 0 || r.table != "plan_nodes" || r.from != "node_id" || r.to != "id" || r.update != "NO ACTION" || r.del != "NO ACTION" || r.match != "NONE" {
		return 0, "", fmt.Errorf("governance_reviews FK node_id mismatch: %+v", r)
	}
	canonical = append(canonical, fmt.Sprintf("fk:governance_reviews:node_id:%d:%d:%s:%s:%s:%s:%s:%s", grFKs["node_id"].id, grFKs["node_id"].seq, grFKs["node_id"].table, grFKs["node_id"].from, grFKs["node_id"].to, grFKs["node_id"].update, grFKs["node_id"].del, grFKs["node_id"].match))
	// memories FKs: project_id → projects
	if err := q.QueryRowContext(ctx, `SELECT count(*) FROM pragma_foreign_key_list('memories')`).Scan(&count); err != nil || count != 1 {
		return 0, "", fmt.Errorf("memories FK set mismatch: count=%d err=%w", count, err)
	}
	var memFkRows *sql.Rows
	memFkRows, err = q.QueryContext(ctx, `SELECT id,seq,"table","from","to",on_update,on_delete,match FROM pragma_foreign_key_list('memories') ORDER BY id`)
	if err != nil {
		return 0, "", fmt.Errorf("memories FK query: %w", err)
	}
	memFKs := map[string]fkRow{}
	for memFkRows.Next() {
		var r fkRow
		if err := memFkRows.Scan(&r.id, &r.seq, &r.table, &r.from, &r.to, &r.update, &r.del, &r.match); err != nil {
			memFkRows.Close()
			return 0, "", err
		}
		memFKs[r.from] = r
	}
	memFkRows.Close()
	if len(memFKs) != 1 {
		return 0, "", fmt.Errorf("memories FK count mismatch: got %d", len(memFKs))
	}
	// FK: project_id → projects
	if r, ok := memFKs["project_id"]; !ok || r.seq != 0 || r.table != "projects" || r.from != "project_id" || r.to != "id" || r.update != "NO ACTION" || r.del != "RESTRICT" || r.match != "NONE" {
		return 0, "", fmt.Errorf("memories FK project_id mismatch: %+v", r)
	}
	canonical = append(canonical, fmt.Sprintf("fk:memories:project_id:%d:%d:%s:%s:%s:%s:%s:%s", memFKs["project_id"].id, memFKs["project_id"].seq, memFKs["project_id"].table, memFKs["project_id"].from, memFKs["project_id"].to, memFKs["project_id"].update, memFKs["project_id"].del, memFKs["project_id"].match))
	// ontology_nodes FKs: project_id → projects
	if err := q.QueryRowContext(ctx, `SELECT count(*) FROM pragma_foreign_key_list('ontology_nodes')`).Scan(&count); err != nil || count != 1 {
		return 0, "", fmt.Errorf("ontology_nodes FK set mismatch: count=%d err=%w", count, err)
	}
	var onFkRows *sql.Rows
	onFkRows, err = q.QueryContext(ctx, `SELECT id,seq,"table","from","to",on_update,on_delete,match FROM pragma_foreign_key_list('ontology_nodes') ORDER BY id`)
	if err != nil {
		return 0, "", fmt.Errorf("ontology_nodes FK query: %w", err)
	}
	onFKs := map[string]fkRow{}
	for onFkRows.Next() {
		var r fkRow
		if err := onFkRows.Scan(&r.id, &r.seq, &r.table, &r.from, &r.to, &r.update, &r.del, &r.match); err != nil {
			onFkRows.Close()
			return 0, "", err
		}
		onFKs[r.from] = r
	}
	onFkRows.Close()
	if len(onFKs) != 1 {
		return 0, "", fmt.Errorf("ontology_nodes FK count mismatch: got %d", len(onFKs))
	}
	// FK: project_id → projects
	if r, ok := onFKs["project_id"]; !ok || r.seq != 0 || r.table != "projects" || r.from != "project_id" || r.to != "id" || r.update != "NO ACTION" || r.del != "RESTRICT" || r.match != "NONE" {
		return 0, "", fmt.Errorf("ontology_nodes FK project_id mismatch: %+v", r)
	}
	canonical = append(canonical, fmt.Sprintf("fk:ontology_nodes:project_id:%d:%d:%s:%s:%s:%s:%s:%s", onFKs["project_id"].id, onFKs["project_id"].seq, onFKs["project_id"].table, onFKs["project_id"].from, onFKs["project_id"].to, onFKs["project_id"].update, onFKs["project_id"].del, onFKs["project_id"].match))
	// ontology_edges FKs: source_node_id → ontology_nodes, target_node_id → ontology_nodes
	if err := q.QueryRowContext(ctx, `SELECT count(*) FROM pragma_foreign_key_list('ontology_edges')`).Scan(&count); err != nil || count != 2 {
		return 0, "", fmt.Errorf("ontology_edges FK set mismatch: count=%d err=%w", count, err)
	}
	var oeFkRows *sql.Rows
	oeFkRows, err = q.QueryContext(ctx, `SELECT id,seq,"table","from","to",on_update,on_delete,match FROM pragma_foreign_key_list('ontology_edges') ORDER BY id`)
	if err != nil {
		return 0, "", fmt.Errorf("ontology_edges FK query: %w", err)
	}
	oeFKs := map[string]fkRow{}
	for oeFkRows.Next() {
		var r fkRow
		if err := oeFkRows.Scan(&r.id, &r.seq, &r.table, &r.from, &r.to, &r.update, &r.del, &r.match); err != nil {
			oeFkRows.Close()
			return 0, "", err
		}
		oeFKs[r.from] = r
	}
	oeFkRows.Close()
	if len(oeFKs) != 2 {
		return 0, "", fmt.Errorf("ontology_edges FK count mismatch: got %d", len(oeFKs))
	}
	// FK: source_node_id → ontology_nodes
	if r, ok := oeFKs["source_node_id"]; !ok || r.seq != 0 || r.table != "ontology_nodes" || r.from != "source_node_id" || r.to != "id" || r.update != "NO ACTION" || r.del != "CASCADE" || r.match != "NONE" {
		return 0, "", fmt.Errorf("ontology_edges FK source_node_id mismatch: %+v", r)
	}
	canonical = append(canonical, fmt.Sprintf("fk:ontology_edges:source_node_id:%d:%d:%s:%s:%s:%s:%s:%s", oeFKs["source_node_id"].id, oeFKs["source_node_id"].seq, oeFKs["source_node_id"].table, oeFKs["source_node_id"].from, oeFKs["source_node_id"].to, oeFKs["source_node_id"].update, oeFKs["source_node_id"].del, oeFKs["source_node_id"].match))
	// FK: target_node_id → ontology_nodes
	if r, ok := oeFKs["target_node_id"]; !ok || r.seq != 0 || r.table != "ontology_nodes" || r.from != "target_node_id" || r.to != "id" || r.update != "NO ACTION" || r.del != "CASCADE" || r.match != "NONE" {
		return 0, "", fmt.Errorf("ontology_edges FK target_node_id mismatch: %+v", r)
	}
	canonical = append(canonical, fmt.Sprintf("fk:ontology_edges:target_node_id:%d:%d:%s:%s:%s:%s:%s:%s", oeFKs["target_node_id"].id, oeFKs["target_node_id"].seq, oeFKs["target_node_id"].table, oeFKs["target_node_id"].from, oeFKs["target_node_id"].to, oeFKs["target_node_id"].update, oeFKs["target_node_id"].del, oeFKs["target_node_id"].match))
	// Extended entity tables (0018): validate FK sets and append canonical entries.
	extendedFKExpectations := []struct {
		table string
		count int
		fks   map[string]fkRow
	}{
		{"plan_versions", 1, map[string]fkRow{"plan_id": {0, 0, "plans", "plan_id", "id", "NO ACTION", "CASCADE", "NONE"}}},
		{"plan_edges", 3, map[string]fkRow{
			"plan_version_id": {0, 0, "plan_versions", "plan_version_id", "id", "NO ACTION", "CASCADE", "NONE"},
			"from_node_id":    {1, 0, "plan_nodes", "from_node_id", "id", "NO ACTION", "CASCADE", "NONE"},
			"to_node_id":      {2, 0, "plan_nodes", "to_node_id", "id", "NO ACTION", "CASCADE", "NONE"},
		}},
		{"node_runs", 1, map[string]fkRow{"node_id": {0, 0, "plan_nodes", "node_id", "id", "NO ACTION", "CASCADE", "NONE"}}},
		{"node_run_checkpoints", 1, map[string]fkRow{"node_run_id": {0, 0, "node_runs", "node_run_id", "id", "NO ACTION", "CASCADE", "NONE"}}},
		{"tool_calls", 2, map[string]fkRow{
			"node_run_id": {0, 0, "node_runs", "node_run_id", "id", "NO ACTION", "CASCADE", "NONE"},
			"approval_id": {1, 0, "governance_reviews", "approval_id", "id", "NO ACTION", "NO ACTION", "NONE"},
		}},
		{"approval_decisions", 1, map[string]fkRow{"review_id": {0, 0, "governance_reviews", "review_id", "id", "NO ACTION", "CASCADE", "NONE"}}},
		{"memory_sources", 1, map[string]fkRow{"memory_id": {0, 0, "memories", "memory_id", "id", "NO ACTION", "CASCADE", "NONE"}}},
		{"memory_revisions", 1, map[string]fkRow{"memory_id": {0, 0, "memories", "memory_id", "id", "NO ACTION", "CASCADE", "NONE"}}},
		{"recall_events", 2, map[string]fkRow{
			"session_id": {0, 0, "sessions", "session_id", "id", "NO ACTION", "CASCADE", "NONE"},
			"memory_id":  {1, 0, "memories", "memory_id", "id", "NO ACTION", "CASCADE", "NONE"},
		}},
	}
	for _, exp := range extendedFKExpectations {
		if err := q.QueryRowContext(ctx, fmt.Sprintf(`SELECT count(*) FROM pragma_foreign_key_list('%s')`, exp.table)).Scan(&count); err != nil || count != exp.count {
			return 0, "", fmt.Errorf("%s FK set mismatch: count=%d err=%w", exp.table, count, err)
		}
		ekFkRows, e := q.QueryContext(ctx, fmt.Sprintf(`SELECT id,seq,"table","from","to",on_update,on_delete,match FROM pragma_foreign_key_list('%s') ORDER BY id`, exp.table))
		if e != nil {
			return 0, "", fmt.Errorf("%s FK query: %w", exp.table, e)
		}
		gotFKs := map[string]fkRow{}
		for ekFkRows.Next() {
			var r fkRow
			if e = ekFkRows.Scan(&r.id, &r.seq, &r.table, &r.from, &r.to, &r.update, &r.del, &r.match); e != nil {
				ekFkRows.Close()
				return 0, "", e
			}
			gotFKs[r.from] = r
		}
		ekFkRows.Close()
		if len(gotFKs) != exp.count {
			return 0, "", fmt.Errorf("%s FK count mismatch: got %d", exp.table, len(gotFKs))
		}
		// Iterate FK columns in deterministic id order to keep the canonical
		// fingerprint stable across runs (Go map iteration is randomized).
		orderedCols := make([]string, 0, len(exp.fks))
		for col := range exp.fks {
			orderedCols = append(orderedCols, col)
		}
		sort.Strings(orderedCols)
		for _, from := range orderedCols {
			want := exp.fks[from]
			r, ok := gotFKs[from]
			if !ok || r.seq != 0 || r.table != want.table || r.from != from || r.to != want.to || r.update != want.update || r.del != want.del || r.match != want.match {
				return 0, "", fmt.Errorf("%s FK %s mismatch: %+v", exp.table, from, r)
			}
			canonical = append(canonical, fmt.Sprintf("fk:%s:%s:%d:%d:%s:%s:%s:%s:%s:%s", exp.table, from, r.id, r.seq, r.table, r.from, r.to, r.update, r.del, r.match))
		}
	}
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

func (s *Store) ListProjects(ctx context.Context, filter project.Filter) ([]project.Project, error) {
	query := `SELECT id,name,status,created_at,updated_at,version FROM projects`
	args := []any{}
	if filter.Status != "" {
		query += ` WHERE status=?`
		args = append(args, filter.Status)
	}
	query += ` ORDER BY created_at,id LIMIT 101`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []project.Project{}
	for rows.Next() {
		var p project.Project
		var created, updated string
		if err = rows.Scan(&p.ID, &p.Name, &p.Status, &created, &updated, &p.Version); err != nil {
			return nil, err
		}
		p.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
		if err != nil {
			return nil, err
		}
		p.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated)
		if err != nil {
			return nil, err
		}
		if err = p.Validate(); err != nil {
			return nil, err
		}
		items = append(items, p)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if len(items) > 100 {
		return nil, errors.New("project data invariant violation: list exceeds capacity")
	}
	return items, nil
}

func (s *Store) ListSessions(ctx context.Context, filter session.Filter) ([]session.Session, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,project_id,title,status,created_at,updated_at,version FROM sessions WHERE project_id=? ORDER BY created_at,id LIMIT 101`, filter.ProjectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []session.Session{}
	for rows.Next() {
		var v session.Session
		var created, updated string
		if err = rows.Scan(&v.ID, &v.ProjectID, &v.Title, &v.Status, &created, &updated, &v.Version); err != nil {
			return nil, err
		}
		v.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
		if err != nil {
			return nil, err
		}
		v.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated)
		if err != nil {
			return nil, err
		}
		if err = v.Validate(); err != nil {
			return nil, err
		}
		items = append(items, v)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if len(items) > 100 {
		return nil, errors.New("session data invariant violation: list exceeds capacity")
	}
	return items, nil
}

func (s *Store) ListMessages(ctx context.Context, q messageapp.PageQuery) ([]message.Message, int64, bool, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, 0, false, err
	}
	defer tx.Rollback()
	var snapshot int64
	if err := tx.QueryRowContext(ctx, `SELECT last_sequence FROM message_session_state WHERE session_id=?`, q.SessionID).Scan(&snapshot); err != nil {
		if err == sql.ErrNoRows {
			return nil, 0, false, messageapp.ErrSessionNotFound
		}
		return nil, 0, false, err
	}
	if q.Snapshot != 0 {
		snapshot = q.Snapshot
	}
	boundary := q.Boundary
	if boundary == 0 && q.Direction == messageapp.Backward {
		boundary = snapshot + 1
	}
	op, order := ">", "ASC"
	if q.Direction == messageapp.Backward {
		op, order = "<", "DESC"
	}
	statement := fmt.Sprintf(`SELECT m.id,m.session_id,m.role,m.status,m.sequence,MAX(CASE WHEN p.ordinal=1 AND p.type='text' THEN p.text END),m.created_at,count(p.message_id),count(CASE WHEN p.ordinal=1 AND p.type='text' THEN 1 END) FROM messages m LEFT JOIN message_parts p ON p.message_id=m.id WHERE m.session_id=? AND m.sequence<=? AND m.sequence %s ? GROUP BY m.id ORDER BY m.sequence %s LIMIT ?`, op, order)
	rows, err := tx.QueryContext(ctx, statement, q.SessionID, snapshot, boundary, q.Limit+1)
	if err != nil {
		return nil, 0, false, err
	}
	defer rows.Close()
	items := make([]message.Message, 0, q.Limit+1)
	for rows.Next() {
		var v message.Message
		var created string
		var text sql.NullString
		var parts, validParts int
		if err = rows.Scan(&v.ID, &v.SessionID, &v.Role, &v.Status, &v.Sequence, &text, &created, &parts, &validParts); err != nil {
			return nil, 0, false, err
		}
		if parts != 1 || validParts != 1 || !text.Valid {
			return nil, 0, false, messageapp.ErrDataInvariantViolation
		}
		v.Text = text.String
		v.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
		expected := boundary + int64(len(items)) + 1
		if q.Direction == messageapp.Backward {
			expected = boundary - int64(len(items)) - 1
		}
		if err != nil || v.Validate() != nil || v.Sequence != expected {
			return nil, 0, false, messageapp.ErrDataInvariantViolation
		}
		items = append(items, v)
	}
	if err = rows.Err(); err != nil {
		return nil, 0, false, err
	}
	more := len(items) > q.Limit
	if more {
		items = items[:q.Limit]
	}
	if !more && snapshot > 0 {
		if q.Direction == messageapp.Forward && (len(items) == 0 || items[len(items)-1].Sequence != snapshot) {
			return nil, 0, false, messageapp.ErrDataInvariantViolation
		}
		if q.Direction == messageapp.Backward && (len(items) == 0 || items[len(items)-1].Sequence != 1) {
			return nil, 0, false, messageapp.ErrDataInvariantViolation
		}
	}
	if err = rows.Close(); err != nil {
		return nil, 0, false, err
	}
	if err = tx.Commit(); err != nil {
		return nil, 0, false, err
	}
	return items, snapshot, more, nil
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
		var cw any
		if model.ContextWindow > 0 {
			cw = model.ContextWindow
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO provider_models(provider_id,model_id,display_name,is_default,position,context_window) VALUES(?,?,?,?,?,?)`, id, model.ModelID, model.DisplayName, model.IsDefault, position, cw); err != nil {
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
	rows, err := q.QueryContext(ctx, `SELECT model_id, display_name, is_default, context_window FROM provider_models WHERE provider_id = ? ORDER BY position, model_id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []provider.Model{}
	for rows.Next() {
		var m provider.Model
		var cw sql.NullInt64
		if err := rows.Scan(&m.ModelID, &m.DisplayName, &m.IsDefault, &cw); err != nil {
			return nil, err
		}
		if cw.Valid {
			m.ContextWindow = cw.Int64
		}
		result = append(result, m)
	}
	return result, rows.Err()
}
