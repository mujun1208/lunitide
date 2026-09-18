package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/lunitide/lunitide/internal/domain/m8core"
	"github.com/oklog/ulid/v2"
)

func TestMemoryV2Migration(t *testing.T) {
	ctx := context.Background()
	t.Run("empty", func(t *testing.T) {
		store, err := Open(ctx, filepath.Join(t.TempDir(), "empty.db"))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = store.Close() })
		assertMemoryV2JournalAndSchema(t, store)
	})
	t.Run("from_0159", func(t *testing.T) {
		path := seed0159Database(t, func(db *sql.DB) {
			if _, err := db.Exec(`INSERT INTO memory_settings(subject_id,memory_enabled,auto_nominate,growth_days,created_at,updated_at,capture_mode)
				VALUES('legacy-user',0,0,14,?,?,?)`, rfc(rtAt), rfc(rtAt), "manual"); err != nil {
				t.Fatal(err)
			}
		})
		store, err := Open(ctx, path)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = store.Close() })
		assertMemoryV2JournalAndSchema(t, store)
		row := scanV2SettingsRow(t, store, "legacy-user")
		if row.captureMode != "off" || row.lastNonOff != "manual" || row.migratedEnabled != 0 || row.migratedMode != "manual" || row.revision != 1 {
			t.Fatalf("0159 backfill %+v", row)
		}
	})
	t.Run("reopen", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "reopen.db")
		store, err := OpenTemplated(ctx, path)
		if err != nil {
			t.Fatal(err)
		}
		assertMemoryV2JournalAndSchema(t, store)
		if err = store.Close(); err != nil {
			t.Fatal(err)
		}
		store, err = Open(ctx, path)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = store.Close() })
		assertMemoryV2JournalAndSchema(t, store)
	})
	t.Run("rejects_checksum", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "bad-checksum.db")
		store, err := OpenTemplated(ctx, path)
		if err != nil {
			t.Fatal(err)
		}
		if err = store.Close(); err != nil {
			t.Fatal(err)
		}
		raw, err := sql.Open("sqlite", path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = raw.Exec(`UPDATE schema_migrations SET checksum=? WHERE version='0161_memory_fabric.sql'`, strings.Repeat("0", 64)); err != nil {
			_ = raw.Close()
			t.Fatal(err)
		}
		if err = raw.Close(); err != nil {
			t.Fatal(err)
		}
		if _, err = Open(ctx, path); err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
			t.Fatalf("expected checksum mismatch, got %v", err)
		}
	})
}

func TestMemoryV2ScopeCollision(t *testing.T) {
	ctx := context.Background()
	store, err := OpenTemplated(ctx, filepath.Join(t.TempDir(), "scope.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	opA := ulid.Make().String()
	opB := ulid.Make().String()
	if _, err = store.CreateCanonicalMemoryItem(ctx, m8core.CanonicalMemoryWrite{
		SubjectID: "alice", ScopeKind: "project", ScopeID: "shared-project",
		Text: "alice project note", OperationID: opA,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err = store.CreateCanonicalMemoryItem(ctx, m8core.CanonicalMemoryWrite{
		SubjectID: "bob", ScopeKind: "project", ScopeID: "shared-project",
		Text: "bob project note", OperationID: opB,
	}); err != nil {
		t.Fatal(err)
	}
	alice, err := store.ListCanonicalMemoryItems(ctx, "alice", "project", "shared-project", 20)
	if err != nil || len(alice) != 1 || alice[0] != "alice project note" {
		t.Fatalf("alice=%v err=%v", alice, err)
	}
	bob, err := store.ListCanonicalMemoryItems(ctx, "bob", "project", "shared-project", 20)
	if err != nil || len(bob) != 1 || bob[0] != "bob project note" {
		t.Fatalf("bob=%v err=%v", bob, err)
	}
	replay, err := store.CreateCanonicalMemoryItem(ctx, m8core.CanonicalMemoryWrite{
		SubjectID: "alice", ScopeKind: "project", ScopeID: "shared-project",
		Text: "alice project note", OperationID: opA, IdempotencyKey: opA,
	})
	if err != nil || !replay.Replay {
		t.Fatalf("idempotent replay %+v err=%v", replay, err)
	}

	t.Run("illegal_kind", func(t *testing.T) {
		_, err := store.db.ExecContext(ctx, `INSERT INTO memory_content_versions(
			fact_id,fact_version,subject_id,scope_kind,scope_id,kind,body_ref,authority,stability,
			importance,confidence,ingested_at,origin_plane,origin_id,extractor_kind,extractor_model,content_digest,created_at)
			VALUES(?,1,'alice','user','alice','bogus','body','user_explicit','durable',0.5,1,?,?, 'native','x','deterministic','',?,?)`,
			ulid.Make().String(), rfc(rtAt), rfc(rtAt), strings.Repeat("a", 64), rfc(rtAt))
		if err == nil {
			t.Fatal("illegal kind accepted")
		}
	})
	t.Run("illegal_interval", func(t *testing.T) {
		_, err := store.db.ExecContext(ctx, `INSERT INTO memory_content_versions(
			fact_id,fact_version,subject_id,scope_kind,scope_id,kind,body_ref,authority,stability,
			importance,confidence,valid_from,valid_to,ingested_at,origin_plane,origin_id,extractor_kind,extractor_model,content_digest,created_at)
			VALUES(?,1,'alice','user','alice','episode','body','user_explicit','durable',0.5,1,'2026-09-18T00:00:00Z','2026-09-17T00:00:00Z',?,?, 'native','x','deterministic','',?,?)`,
			ulid.Make().String(), rfc(rtAt), rfc(rtAt), strings.Repeat("b", 64), rfc(rtAt))
		if err == nil {
			t.Fatal("illegal interval accepted")
		}
	})
	t.Run("illegal_vector_dimension", func(t *testing.T) {
		_, err := store.db.ExecContext(ctx, `INSERT INTO memory_embeddings(
			fact_id,fact_version,embedding_space_id,model_id,dimensions,vector_blob,vector_digest,state,embedded_at)
			VALUES(?,1,'space','model',0,x'00',?, 'ready',?)`,
			ulid.Make().String(), strings.Repeat("c", 64), rfc(rtAt))
		if err == nil {
			t.Fatal("illegal vector dimension accepted")
		}
	})
}

func TestMemoryV2SettingsMigrationMatrix(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		id, mode     string
		enabled      int
		wantMode     string
		wantLast     string
		wantMigrated int
	}{
		{"m-0-auto", "auto", 0, "off", "auto", 0},
		{"m-0-manual", "manual", 0, "off", "manual", 0},
		{"m-0-off", "off", 0, "off", "auto", 0},
		{"m-1-auto", "auto", 1, "auto", "auto", 1},
		{"m-1-manual", "manual", 1, "manual", "manual", 1},
		{"m-1-off", "off", 1, "off", "auto", 1},
	}
	path := seed0159Database(t, func(db *sql.DB) {
		for _, c := range cases {
			if _, err := db.Exec(`INSERT INTO memory_settings(subject_id,memory_enabled,auto_nominate,growth_days,created_at,updated_at,capture_mode)
				VALUES(?,?,0,14,?,?,?)`, c.id, c.enabled, rfc(rtAt), rfc(rtAt), c.mode); err != nil {
				t.Fatal(err)
			}
		}
	})
	store, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	for _, c := range cases {
		row := scanV2SettingsRow(t, store, c.id)
		if row.captureMode != c.wantMode || row.lastNonOff != c.wantLast || row.migratedEnabled != c.wantMigrated || row.migratedMode != c.mode {
			t.Fatalf("%s row=%+v want mode=%s last=%s migrated=%d/%s", c.id, row, c.wantMode, c.wantLast, c.wantMigrated, c.mode)
		}
		if row.revision != 1 || row.personal != 1 || row.project != 1 || row.write != 0 || row.read != 0 || row.auto != 0 || row.hybrid != 0 || row.consol != 0 {
			t.Fatalf("%s defaults %+v", c.id, row)
		}
	}
	fresh, err := store.GetMemoryV2Settings(ctx, "never-seen")
	if err != nil {
		t.Fatal(err)
	}
	lazy := scanV2SettingsRow(t, store, "never-seen")
	if fresh.CaptureMode != "auto" || fresh.LastNonOffCaptureMode != "auto" || !fresh.PersonalMemoryEnabled || !fresh.ProjectMemoryEnabled || fresh.Revision != 1 {
		t.Fatalf("lazy defaults %+v", fresh)
	}
	if lazy.migratedEnabled != -1 || lazy.migratedMode != "" {
		t.Fatalf("lazy migrated must be NULL %+v", lazy)
	}
}

func TestMemoryV2SettingsLegacyOmission(t *testing.T) {
	ctx := context.Background()
	store, err := OpenTemplated(ctx, filepath.Join(t.TempDir(), "omit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	got, err := store.GetMemoryV2Settings(ctx, "omit-user")
	if err != nil {
		t.Fatal(err)
	}
	got.PersonalMemoryEnabled = false
	got.ProjectMemoryEnabled = true
	if _, err = store.CompareAndSwapMemoryV2Settings(ctx, got, got.Revision); err != nil {
		t.Fatal(err)
	}
	legacy, err := store.GetMemorySettings(ctx, "omit-user")
	if err != nil {
		t.Fatal(err)
	}
	version := m8core.SettingsVersion(legacy)
	legacy.GrowthDays = 21
	if _, err = store.CompareAndSwapMemorySettings(ctx, legacy, version); err != nil {
		t.Fatal(err)
	}
	row := scanV2SettingsRow(t, store, "omit-user")
	if row.personal != 0 || row.project != 1 {
		t.Fatalf("legacy omission overwrote scopes %+v", row)
	}
	if row.migratedEnabled != -1 {
		t.Fatalf("legacy update must not write migrated_* %+v", row)
	}
}

func TestMemoryV2SettingsCAS(t *testing.T) {
	ctx := context.Background()
	store, err := OpenTemplated(ctx, filepath.Join(t.TempDir(), "cas.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	initial, err := store.GetMemoryV2Settings(ctx, "cas-user")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, mode := range []string{"manual", "off"} {
		wg.Add(1)
		go func(mode string) {
			defer wg.Done()
			next := initial
			next.CaptureMode = mode
			_, err := store.CompareAndSwapMemoryV2Settings(ctx, next, initial.Revision)
			errs <- err
		}(mode)
	}
	wg.Wait()
	close(errs)
	successes, conflicts := 0, 0
	for err := range errs {
		if err == nil {
			successes++
		} else if errors.Is(err, m8core.ErrSettingsConflict) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("success=%d conflict=%d", successes, conflicts)
	}
	current, err := store.GetMemoryV2Settings(ctx, "cas-user")
	if err != nil || current.Revision != 2 {
		t.Fatalf("revision %+v err=%v", current, err)
	}
	if _, err = store.CompareAndSwapMemoryV2Settings(ctx, current, initial.Revision); !errors.Is(err, m8core.ErrSettingsConflict) {
		t.Fatalf("stale revision: %v", err)
	}
}

func seed0159Database(t *testing.T, seed func(*sql.DB)) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "from-0159.db")
	raw := legacyBeforeMigration(t, path, "0160")
	var last string
	if err := raw.QueryRow(`SELECT version FROM schema_migrations ORDER BY rowid DESC LIMIT 1`).Scan(&last); err != nil || last != "0159_office_delivery_v2.sql" {
		t.Fatalf("fixture must stop at 0159, last=%q err=%v", last, err)
	}
	var n int
	if err := raw.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name='memory_v2_settings'`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("0159 fixture leaked fabric table n=%d err=%v", n, err)
	}
	if seed != nil {
		seed(raw)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func assertMemoryV2JournalAndSchema(t *testing.T, store *Store) {
	t.Helper()
	for _, name := range []string{"0161_memory_fabric.sql", "0162_memory_retrieval.sql", "0163_memory_generations.sql"} {
		var checksum string
		if err := store.db.QueryRow(`SELECT checksum FROM schema_migrations WHERE version=?`, name).Scan(&checksum); err != nil || checksum == "" {
			t.Fatalf("missing %s: %v", name, err)
		}
	}
	for _, table := range []string{"memory_v2_settings", "memory_fact_heads", "memory_content_versions", "memory_search_documents", "memory_generations"} {
		var n int
		if err := store.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&n); err != nil || n != 1 {
			t.Fatalf("missing table %s n=%d err=%v", table, n, err)
		}
	}
}

type v2SettingsRow struct {
	revision, personal, project, write, read, auto, hybrid, consol, migratedEnabled int
	captureMode, lastNonOff, migratedMode                                           string
}

func scanV2SettingsRow(t *testing.T, store *Store, subjectID string) v2SettingsRow {
	t.Helper()
	var row v2SettingsRow
	var migratedEnabled sql.NullInt64
	var migratedMode sql.NullString
	err := store.db.QueryRow(`SELECT revision,capture_mode,last_non_off_capture_mode,personal_memory_enabled,project_memory_enabled,
		migrated_memory_enabled,migrated_capture_mode,
		memory_v2_write,memory_v2_read,memory_v2_auto_capture,memory_v2_hybrid_recall,memory_v2_consolidation
		FROM memory_v2_settings WHERE subject_id=?`, subjectID).Scan(
		&row.revision, &row.captureMode, &row.lastNonOff, &row.personal, &row.project,
		&migratedEnabled, &migratedMode, &row.write, &row.read, &row.auto, &row.hybrid, &row.consol)
	if err != nil {
		t.Fatal(err)
	}
	if migratedEnabled.Valid {
		row.migratedEnabled = int(migratedEnabled.Int64)
	} else {
		row.migratedEnabled = -1
	}
	if migratedMode.Valid {
		row.migratedMode = migratedMode.String
	}
	return row
}

func TestForgetCanonicalMemoryItem(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "forget.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	created, err := store.CreateCanonicalMemoryItem(ctx, m8core.CanonicalMemoryWrite{
		SubjectID: "alice", Text: "forget me", OperationID: ulid.Make().String(),
	})
	if err != nil || created.FactID == "" {
		t.Fatalf("create %+v err=%v", created, err)
	}
	listed, err := store.ListCanonicalMemoryItems(ctx, "alice", "user", "alice", 8)
	if err != nil || len(listed) != 1 || listed[0] != "forget me" {
		t.Fatalf("listed=%v err=%v", listed, err)
	}
	if err := store.ForgetCanonicalMemoryItem(ctx, "alice", created.FactID, created.FactID, 1); err != nil {
		t.Fatal(err)
	}
	listed, err = store.ListCanonicalMemoryItems(ctx, "alice", "user", "alice", 8)
	if err != nil || len(listed) != 0 {
		t.Fatalf("forgotten still listed=%v err=%v", listed, err)
	}
	if err := store.ForgetCanonicalMemoryItem(ctx, "alice", created.FactID, created.FactID, 1); err != nil {
		t.Fatal(err)
	}
	var bodies, forgotten int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM memory_content_bodies WHERE fact_id=?`, created.FactID).Scan(&bodies); err != nil || bodies != 0 {
		t.Fatalf("bodies=%d err=%v", bodies, err)
	}
	if err := store.db.QueryRow(`SELECT is_forgotten FROM memory_fact_heads WHERE fact_id=?`, created.FactID).Scan(&forgotten); err != nil || forgotten != 1 {
		t.Fatalf("forgotten=%d err=%v", forgotten, err)
	}
	if err := store.ForgetCanonicalMemoryItem(ctx, "bob", created.FactID, "other", 1); !errors.Is(err, m8core.ErrNotFound) {
		t.Fatalf("cross-subject forget: %v", err)
	}
}

func TestCreateCanonicalMemoryItemUserMessageEvidence(t *testing.T) {
	ctx := context.Background()
	store, err := OpenTemplated(ctx, filepath.Join(t.TempDir(), "evidence.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	start, end := int64(0), int64(len([]byte("我喜欢简洁的回答")))
	created, err := store.CreateCanonicalMemoryItem(ctx, m8core.CanonicalMemoryWrite{
		SubjectID: "alice", Text: "我喜欢简洁的回答", OperationID: ulid.Make().String(),
		SourceKind: m8core.MemorySourceUserMessage, SourceRef: "01ARZ3NDEKTSV4RRFFQ69G5FAA",
		StartByte: &start, EndByte: &end, QuoteDigest: strings.Repeat("ab", 32),
	})
	if err != nil {
		t.Fatal(err)
	}
	kind, ref, startByte, endByte, digest, err := store.CanonicalEvidenceSpan(ctx, created.FactID)
	if err != nil || kind != m8core.MemorySourceUserMessage || ref != "01ARZ3NDEKTSV4RRFFQ69G5FAA" ||
		!startByte.Valid || startByte.Int64 != 0 || !endByte.Valid || endByte.Int64 != end || digest != strings.Repeat("ab", 32) {
		t.Fatalf("evidence kind=%s ref=%s start=%v end=%v digest=%s err=%v", kind, ref, startByte, endByte, digest, err)
	}
}
