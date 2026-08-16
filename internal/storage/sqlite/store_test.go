package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/migrations"
	_ "modernc.org/sqlite"
)

func TestOpenMigratesAndListsEmptyProviders(t *testing.T) {
	store, err := Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	items, err := store.List(context.Background(), provider.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("expected empty provider list, got %d", len(items))
	}
}

func TestListLoadsProviderModelsWithoutConnectionDeadlock(t *testing.T) {
	// Migration replay is not part of the deadlock watch: under -race it alone
	// can exceed any small budget, so Open runs on an unbounded context and the
	// 2s watchdog below guards only the query path this test is named after.
	store, err := Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := store.db.ExecContext(ctx, `INSERT INTO providers(id,name,protocol,base_url,credential_state,created_at,updated_at) VALUES(?,?,?,?,?,?,?)`, "01K00000000000000000000000", "DeepSeek", "openai_compatible", "https://api.deepseek.com", "missing", now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, `INSERT INTO provider_models(provider_id,model_id,display_name,is_default) VALUES(?,?,?,1)`, "01K00000000000000000000000", "deepseek-chat", "DeepSeek Chat"); err != nil {
		t.Fatal(err)
	}
	items, err := store.List(ctx, provider.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || len(items[0].Models) != 1 || !items[0].Models[0].IsDefault {
		t.Fatalf("unexpected provider graph: %#v", items)
	}
}

func TestMigrationJournalRecordsAndEnforcesChecksum(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	store, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := migrations.Files.ReadFile("0001_provider.sql")
	want := sha256.Sum256(body)
	var got string
	if err := store.db.QueryRow(`SELECT checksum FROM schema_migrations WHERE version = '0001_provider.sql'`).Scan(&got); err != nil {
		t.Fatal(err)
	}
	store.Close()
	if got != hex.EncodeToString(want[:]) {
		t.Fatalf("checksum = %q", got)
	}
	db := openRaw(t, path)
	if _, err := db.Exec(`UPDATE schema_migrations SET checksum = ? WHERE version = '0001_provider.sql'`, strings.Repeat("0", 64)); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if _, err := Open(context.Background(), path); err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("expected checksum mismatch, got %v", err)
	}
}

func TestMigrationManifestMatchesEmbeddedBytes(t *testing.T) {
	for _, migration := range manifest {
		body, err := migrations.Files.ReadFile(migration.name)
		if err != nil {
			t.Fatal(err)
		}
		got := sha256.Sum256(body)
		if hex.EncodeToString(got[:]) != migration.checksum {
			t.Fatalf("%s embedded checksum = %x, manifest = %s", migration.name, got, migration.checksum)
		}
	}
}

func TestReleasedV1ManifestTypoIsCorrectedOnlyAfterFullValidation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "released-candidate.db")
	store, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	db := openRaw(t, path)
	if _, err := db.Exec(`UPDATE schema_migrations SET checksum=? WHERE version='0001_provider.sql'`, releasedV1ManifestTypo); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var got string
	if err := store.db.QueryRow(`SELECT checksum FROM schema_migrations WHERE version='0001_provider.sql'`).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != manifest[0].checksum {
		t.Fatalf("corrected checksum = %q", got)
	}
}

func TestUpgradeCanonicalV3DatabaseToModelSyncClaimsV4(t *testing.T) {
	const (
		v3Checksum = "bf7ed1d958fcc04e180a9b888edb1b0f0e51cd0071227f80fa588d737d622835"
		v4Checksum = "160970b0aac29327774957e19acebdbb1b2f463a3c742c772e4809c29096ffff"
	)
	path := filepath.Join(t.TempDir(), "v3.db")
	db := openRaw(t, path)
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE schema_migrations(version TEXT PRIMARY KEY, applied_at TEXT NOT NULL, checksum TEXT)`); err != nil {
		t.Fatal(err)
	}
	for i, migration := range manifest[:3] {
		body, err := migrations.Files.ReadFile(migration.name)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(body)
		gotChecksum := hex.EncodeToString(sum[:])
		if gotChecksum != migration.checksum {
			t.Fatalf("embedded %s checksum = %s, manifest = %s", migration.name, gotChecksum, migration.checksum)
		}
		if migration.name == "0003_provider_app.sql" && gotChecksum != v3Checksum {
			t.Fatalf("canonical historical 0003 checksum changed: %s", gotChecksum)
		}
		if _, err = db.Exec(string(body)); err != nil {
			t.Fatalf("apply exact %s: %v", migration.name, err)
		}
		// 0002 delegates data transformation and source-table cleanup to Go.
		// This fixture starts empty, so only its documented cleanup is needed.
		if i == 1 {
			if _, err = db.Exec(`DROP TABLE provider_models_v1; DROP TABLE providers_v1`); err != nil {
				t.Fatal(err)
			}
		}
		if _, err = db.Exec(`INSERT INTO schema_migrations(version,applied_at,checksum) VALUES(?,?,?)`, migration.name, "2025-01-01T00:00:00Z", gotChecksum); err != nil {
			t.Fatal(err)
		}
	}

	now := "2025-01-02T00:00:00Z"
	providerID := "01JGP3G0000000000000000000"
	fingerprint, err := provider.OriginFingerprint(provider.ProtocolOpenAICompatible, "https://api.example.test/v1")
	if err != nil {
		t.Fatal(err)
	}
	digest := strings.Repeat("a", 64)
	for _, insert := range []struct {
		query string
		args  []any
	}{
		{`INSERT INTO providers(id,name,protocol,base_url,credential_state,status,created_at,updated_at,version,origin_fingerprint) VALUES(?, 'Historical', 'openai_compatible', 'https://api.example.test/v1', 'missing', 'enabled', ?, ?, 3, ?)`, []any{providerID, now, now, fingerprint}},
		{`INSERT INTO provider_models(provider_id,model_id,display_name,is_default,position) VALUES(?, 'historical-model', 'Historical Model', 1, 0)`, []any{providerID}},
		{`INSERT INTO idempotency_records(operation,idempotency_key,request_digest,response_json,created_at,expires_at) VALUES('provider.update','historical-key',?,'{"version":3}',?,'2026-01-02T00:00:00Z')`, []any{digest, now}},
		{`INSERT INTO audit_events(id,action,aggregate_id,actor,metadata_json,created_at) VALUES('historical-audit','provider.updated',?,'fixture','{"version":3}',?)`, []any{providerID, now}},
	} {
		if _, err = db.Exec(insert.query, insert.args...); err != nil {
			t.Fatal(err)
		}
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}

	store, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var name, modelID, operation, response, action string
	if err = store.db.QueryRow(`SELECT p.name,m.model_id FROM providers p JOIN provider_models m ON m.provider_id=p.id WHERE p.id=?`, providerID).Scan(&name, &modelID); err != nil || name != "Historical" || modelID != "historical-model" {
		t.Fatalf("provider graph not preserved: name=%q model=%q err=%v", name, modelID, err)
	}
	if err = store.db.QueryRow(`SELECT operation,response_json FROM idempotency_records WHERE idempotency_key='historical-key'`).Scan(&operation, &response); err != nil || operation != "provider.update" || response != `{"version":3}` {
		t.Fatalf("idempotency row not preserved: operation=%q response=%q err=%v", operation, response, err)
	}
	if err = store.db.QueryRow(`SELECT action FROM audit_events WHERE id='historical-audit'`).Scan(&action); err != nil || action != "provider.updated" {
		t.Fatalf("audit row not preserved: action=%q err=%v", action, err)
	}
	v4Body, err := migrations.Files.ReadFile("0004_model_sync_claims.sql")
	if err != nil {
		t.Fatal(err)
	}
	v4Sum := sha256.Sum256(v4Body)
	if got := hex.EncodeToString(v4Sum[:]); got != v4Checksum || got != manifest[3].checksum {
		t.Fatalf("0004 checksum = %s, canonical=%s manifest=%s", got, v4Checksum, manifest[3].checksum)
	}
	var journalChecksum string
	if err = store.db.QueryRow(`SELECT checksum FROM schema_migrations WHERE version='0004_model_sync_claims.sql'`).Scan(&journalChecksum); err != nil || journalChecksum != v4Checksum {
		t.Fatalf("0004 journal checksum=%q err=%v", journalChecksum, err)
	}
	for _, insert := range []struct {
		query string
		args  []any
	}{
		{`INSERT INTO idempotency_records(operation,idempotency_key,request_digest,response_json,created_at,expires_at) VALUES('provider.model.sync','sync-key',?,'{"version":4}',?,'2026-01-02T00:00:00Z')`, []any{digest, now}},
		{`INSERT INTO audit_events(id,action,aggregate_id,actor,metadata_json,created_at) VALUES('sync-audit','provider.models.synced',?,'fixture','{"version":4}',?)`, []any{providerID, now}},
		{`INSERT INTO idempotency_claims(operation,idempotency_key,request_digest,owner,expires_at) VALUES('provider.model.sync','sync-claim',?,'fixture-owner','2026-01-02T00:00:00Z')`, []any{digest}},
	} {
		if _, err = store.db.Exec(insert.query, insert.args...); err != nil {
			t.Fatalf("v4 model-sync records unsupported: %v", err)
		}
	}
	var claims int
	if err = store.db.QueryRow(`SELECT count(*) FROM idempotency_claims WHERE operation='provider.model.sync'`).Scan(&claims); err != nil || claims != 1 {
		t.Fatalf("claims missing from upgraded schema: count=%d err=%v", claims, err)
	}
}

func TestLegacyJournalIsValidatedThenSafelyBackfilled(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	db := openRaw(t, path)
	body, _ := migrations.Files.ReadFile("0001_provider.sql")
	if _, err := db.Exec(string(body)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE schema_migrations(version TEXT PRIMARY KEY, applied_at TEXT NOT NULL); INSERT INTO schema_migrations VALUES('0001_provider.sql','2025-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	store, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var checksum sql.NullString
	if err := store.db.QueryRow(`SELECT checksum FROM schema_migrations WHERE version='0001_provider.sql'`).Scan(&checksum); err != nil || !checksum.Valid || len(checksum.String) != 64 {
		t.Fatalf("legacy checksum not backfilled: %#v, %v", checksum, err)
	}
}

func TestOpenRejectsInvalidSchemaDefinitionAndConstraints(t *testing.T) {
	path := filepath.Join(t.TempDir(), "invalid.db")
	db := openRaw(t, path)
	if _, err := db.Exec(`CREATE TABLE providers(id TEXT PRIMARY KEY); CREATE TABLE provider_models(provider_id TEXT, model_id TEXT); CREATE TABLE schema_migrations(version TEXT PRIMARY KEY, applied_at TEXT NOT NULL); INSERT INTO schema_migrations VALUES('0001_provider.sql','2025-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if _, err := Open(context.Background(), path); err == nil || !strings.Contains(err.Error(), "schema definition") {
		t.Fatalf("expected schema validation failure, got %v", err)
	}
}

func TestCriticalProviderConstraintsAreEnforced(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "constraints.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := store.db.ExecContext(ctx, `INSERT INTO providers(id,name,protocol,base_url,credential_state,created_at,updated_at) VALUES(?,?,?,?,?,?,?)`, "bad", "Bad", "unsupported", "https://example.test", "missing", now, now); err == nil {
		t.Fatal("provider protocol CHECK constraint was not enforced")
	}
	if _, err := store.db.ExecContext(ctx, `INSERT INTO providers(id,name,protocol,base_url,credential_state,created_at,updated_at) VALUES(?,?,?,?,?,?,?)`, "good", "Good", "anthropic", "https://example.test", "missing", now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, `INSERT INTO provider_models(provider_id,model_id,display_name,is_default) VALUES(?,?,?,1),(?,?,?,1)`, "good", "one", "One", "good", "two", "Two"); err == nil {
		t.Fatal("single-default unique index was not enforced")
	}
	if _, err := store.db.ExecContext(ctx, `INSERT INTO provider_models(provider_id,model_id,display_name) VALUES(?,?,?)`, "missing", "orphan", "Orphan"); err == nil {
		t.Fatal("provider model foreign key was not enforced")
	}
}

func openRaw(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", path+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func TestOpenTreatsSpecialCharactersAsFilenameNotDSN(t *testing.T) {
	for _, name := range []string{"with space.db", "数据库.db", "hash#name.db", "percent%25.db"} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), name)
			store, err := Open(context.Background(), path)
			if err != nil {
				t.Fatal(err)
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestOpenRejectsDangerousPaths(t *testing.T) {
	for _, path := range []string{"relative.db", "", filepath.Join(t.TempDir(), "not-db.txt"), filepath.Join(t.TempDir(), "bad\n.db")} {
		if store, err := Open(context.Background(), path); err == nil {
			store.Close()
			t.Fatalf("dangerous path accepted: %q", path)
		}
	}
}

func TestOpenRejectsUnknownMigrationAndSchemaObjects(t *testing.T) {
	for _, mutation := range []string{
		`INSERT INTO schema_migrations(version,applied_at,checksum) VALUES('9999_future.sql','2025-01-01','x')`,
		`CREATE TRIGGER hostile AFTER INSERT ON providers BEGIN DELETE FROM providers; END`,
		`CREATE VIEW leaked AS SELECT * FROM providers`,
	} {
		path := filepath.Join(t.TempDir(), "mutated.db")
		store, err := Open(context.Background(), path)
		if err != nil {
			t.Fatal(err)
		}
		store.Close()
		db := openRaw(t, path)
		if _, err := db.Exec(mutation); err != nil {
			t.Fatal(err)
		}
		db.Close()
		if reopened, err := Open(context.Background(), path); err == nil {
			reopened.Close()
			t.Fatalf("unsafe mutation accepted: %s", mutation)
		}
	}
}

func TestLegacyBackfillRejectsCommentAndWeakenedConstraint(t *testing.T) {
	body, _ := migrations.Files.ReadFile("0001_provider.sql")
	for name, schema := range map[string]string{
		"comment":  strings.Replace(string(body), "id TEXT PRIMARY KEY", "id TEXT PRIMARY KEY /* forged */", 1),
		"weakened": strings.Replace(string(body), "name TEXT NOT NULL CHECK", "name TEXT CHECK", 1),
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "legacy.db")
			db := openRaw(t, path)
			if _, err := db.Exec(schema + `CREATE TABLE schema_migrations(version TEXT PRIMARY KEY, applied_at TEXT NOT NULL); INSERT INTO schema_migrations VALUES('0001_provider.sql','2025-01-01')`); err != nil {
				t.Fatal(err)
			}
			db.Close()
			if _, err := Open(context.Background(), path); err == nil || !strings.Contains(err.Error(), "schema definition mismatch") {
				t.Fatalf("forged schema accepted: %v", err)
			}
		})
	}
}

func validProvider() provider.Provider {
	return provider.Provider{Name: "Example", Protocol: provider.ProtocolOpenAICompatible, BaseURL: "HTTPS://EXAMPLE.COM:443/v1/", Status: provider.StatusEnabled, CredentialRef: "vault-item-1", CredentialState: provider.CredentialConfigured, Models: []provider.Model{{ModelID: "model-1", DisplayName: "Model One", IsDefault: true}}}
}

func TestProviderTransactionalCRUDCASOriginAndSoftDelete(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "crud.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	created, err := store.Create(ctx, validProvider())
	if err != nil {
		t.Fatal(err)
	}
	if len(created.ID) != 26 || created.Version != 1 || created.BaseURL != "https://example.com/v1" {
		t.Fatalf("unexpected create: %#v", created)
	}
	got, err := store.Get(ctx, created.ID)
	if err != nil || len(got.Models) != 1 || got.CredentialRef != "vault-item-1" {
		t.Fatalf("unexpected graph: %#v %v", got, err)
	}
	changed := got
	changed.Name, changed.Status = "Changed", provider.StatusDisabled
	updated, err := store.Update(ctx, changed, 1)
	if err != nil || updated.Version != 2 {
		t.Fatalf("update: %#v %v", updated, err)
	}
	if _, err = store.Update(ctx, changed, 1); !errors.Is(err, provider.ErrConflict) {
		t.Fatalf("want conflict, got %v", err)
	}
	originChange := updated
	originChange.BaseURL = "https://other.example/v1"
	if _, err = store.Update(ctx, originChange, 2); !errors.Is(err, provider.ErrCredentialReentryRequired) {
		t.Fatalf("old ref reused: %v", err)
	}
	originChange.CredentialRef, originChange.CredentialState = "", provider.CredentialRequiresReentry
	updated, err = store.Update(ctx, originChange, 2)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Delete(ctx, created.ID, updated.Version); err != nil {
		t.Fatal(err)
	}
	if _, err = store.Get(ctx, created.ID); !errors.Is(err, provider.ErrNotFound) {
		t.Fatalf("soft-deleted item visible: %v", err)
	}
	if err = store.Delete(ctx, created.ID, updated.Version); !errors.Is(err, provider.ErrNotFound) {
		t.Fatalf("delete semantics unavailable: %v", err)
	}
}

func TestProviderCreateRollsBackInvalidGraph(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "rollback.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	p := validProvider()
	p.Models = append(p.Models, provider.Model{ModelID: "model-1", DisplayName: "Duplicate"})
	if _, err = store.Create(ctx, p); err == nil {
		t.Fatal("invalid graph accepted")
	}
	items, err := store.List(ctx, provider.Filter{})
	if err != nil || len(items) != 0 {
		t.Fatalf("partial provider persisted: %#v %v", items, err)
	}
}

func TestCreateRejectsExternalNonULIDAndReturnsEntropyError(t *testing.T) {
	store, err := Open(context.Background(), filepath.Join(t.TempDir(), "ids.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	p := validProvider()
	p.ID = "legacy-id"
	if _, err = store.Create(context.Background(), p); err == nil {
		t.Fatal("external legacy ID accepted")
	}
	p.ID = ""
	store.idEntropy = errorReader{}
	if _, err = store.Create(context.Background(), p); err == nil {
		t.Fatal("entropy failure did not return error")
	}
}

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

func TestMigratesRealV1BoundaryFixture(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v1.db")
	db := openRaw(t, path)
	body, _ := migrations.Files.ReadFile("0001_provider.sql")
	if _, err := db.Exec(string(body)); err != nil {
		t.Fatal(err)
	}
	now := "2025-01-01T00:00:00Z"
	if _, err := db.Exec(`INSERT INTO providers(id,name,protocol,base_url,credential_ref,credential_state,created_at,updated_at) VALUES('legacy/provider','Legacy','anthropic','https://example.test','stale','missing',?,?); INSERT INTO provider_models(provider_id,model_id,display_name,is_default,position) VALUES('legacy/provider','second','Second',0,9),('legacy/provider','first','First',1,-5); CREATE TABLE schema_migrations(version TEXT PRIMARY KEY, applied_at TEXT NOT NULL); INSERT INTO schema_migrations VALUES('0001_provider.sql',?)`, now, now, now); err != nil {
		t.Fatal(err)
	}
	db.Close()
	store, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	items, err := store.List(context.Background(), provider.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ID == "legacy/provider" || items[0].LegacyID != "legacy/provider" || items[0].CredentialState != provider.CredentialRequiresReentry || items[0].CredentialRef != "" || items[0].Models[0].ModelID != "first" {
		t.Fatalf("bad migration: %#v", items)
	}
}

func TestStartupRejectsProviderInvariantCorruption(t *testing.T) {
	path := filepath.Join(t.TempDir(), "corrupt.db")
	store, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	created, err := store.Create(context.Background(), validProvider())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.db.Exec(`DELETE FROM provider_models WHERE provider_id=?`, created.ID); err != nil {
		t.Fatal(err)
	}
	store.Close()
	if _, err = Open(context.Background(), path); err == nil || !strings.Contains(err.Error(), "invariant") {
		t.Fatalf("corruption accepted: %v", err)
	}
}

func TestTwoStoresConcurrentUpdateUpdateAndUpdateDelete(t *testing.T) {
	for _, mode := range []string{"update-update", "update-delete"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			path := filepath.Join(t.TempDir(), "concurrent.db")
			a, err := Open(ctx, path)
			if err != nil {
				t.Fatal(err)
			}
			defer a.Close()
			b, err := Open(ctx, path)
			if err != nil {
				t.Fatal(err)
			}
			defer b.Close()
			created, err := a.Create(ctx, validProvider())
			if err != nil {
				t.Fatal(err)
			}
			left, _ := a.Get(ctx, created.ID)
			right := left
			left.Name, right.Name = "left", "right"
			start := make(chan struct{})
			results := make(chan error, 2)
			go func() { <-start; _, e := a.Update(ctx, left, 1); results <- e }()
			go func() {
				<-start
				if mode == "update-delete" {
					results <- b.Delete(ctx, created.ID, 1)
				} else {
					_, e := b.Update(ctx, right, 1)
					results <- e
				}
			}()
			close(start)
			e1, e2 := <-results, <-results
			successes := 0
			conflicts := 0
			for _, e := range []error{e1, e2} {
				if e == nil {
					successes++
				} else if errors.Is(e, provider.ErrConflict) || errors.Is(e, provider.ErrNotFound) {
					conflicts++
				} else {
					t.Fatalf("unstable concurrency error: %v", e)
				}
			}
			if successes != 1 || conflicts != 1 {
				t.Fatalf("want one winner/loser, got %v / %v", e1, e2)
			}
		})
	}
}
