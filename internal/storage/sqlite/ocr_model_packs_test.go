package sqlite

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/migrations"
	"github.com/oklog/ulid/v2"
)

func TestOCRModelPacksMigration(t *testing.T) {
	ctx := context.Background()
	t.Run("empty", func(t *testing.T) {
		store, err := Open(ctx, filepath.Join(t.TempDir(), "ocr-empty.db"))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = store.Close() })
		assertOCRPackJournal(t, store)
	})
	t.Run("from_ocr_predecessor", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "from-memory.db")
		raw := legacyBeforeMigration(t, path, "0164_")
		t.Cleanup(func() { _ = raw.Close() })
		if err := raw.Close(); err != nil {
			t.Fatal(err)
		}
		store, err := Open(ctx, path)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = store.Close() })
		assertOCRPackJournal(t, store)
	})
	t.Run("reopen", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "ocr-reopen.db")
		store, err := Open(ctx, path)
		if err != nil {
			t.Fatal(err)
		}
		assertOCRPackJournal(t, store)
		if err = store.Close(); err != nil {
			t.Fatal(err)
		}
		store, err = Open(ctx, path)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = store.Close() })
		assertOCRPackJournal(t, store)
	})
}

func TestOCRModelPacksMigrationIsIdempotent(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "ocr-idempotent.db")
	store, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	assertOCRPackJournal(t, store)
}

func TestOCRModelPacksMigrationChecksumMismatch(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "ocr-checksum.db")
	store, err := Open(ctx, path)
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
	if _, err = raw.Exec(`UPDATE schema_migrations SET checksum=? WHERE version='0164_ocr_model_packs.sql'`, strings.Repeat("0", 64)); err != nil {
		_ = raw.Close()
		t.Fatal(err)
	}
	if err = raw.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = Open(ctx, path); err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("got %v", err)
	}
}

func TestOCRGateDefaultsNoVerifiedRuntimeProfile(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "ocr-gate.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	gate, err := store.OCRPackGate(ctx, "paddleocr-vl-1.6")
	if err != nil {
		t.Fatal(err)
	}
	if gate.InstallEnabled || gate.AutoRouteEnabled || gate.VerifiedRuntimeProfileDigest.Valid || gate.DisabledReason != "NO_VERIFIED_RUNTIME_PROFILE" {
		t.Fatalf("%+v", gate)
	}
	digest := sha256Hex("install-1")
	if _, err := store.OCRInsertPackOperation(ctx, "paddleocr-vl-1.6", "user-1", "install", "key-1", digest); !errors.Is(err, ErrNoVerifiedRuntimeProfile) {
		t.Fatalf("got %v", err)
	}
	var n int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM ocr_pack_operations`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("mutation n=%d err=%v", n, err)
	}
}

func TestOCRPackRejectsSecondActiveMutation(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "ocr-active.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	profile := strings.Repeat("a", 64)
	if _, err := store.db.Exec(`UPDATE ocr_pack_gates SET verified_runtime_profile_digest=?, install_enabled=1 WHERE pack_id='paddleocr-vl-1.6'`, profile); err != nil {
		t.Fatal(err)
	}
	first, err := store.OCRInsertPackOperation(ctx, "paddleocr-vl-1.6", "user-1", "install", "key-a", sha256Hex("a"))
	if err != nil {
		t.Fatal(err)
	}
	replay, err := store.OCRInsertPackOperation(ctx, "paddleocr-vl-1.6", "user-1", "install", "key-a", sha256Hex("a"))
	if err != nil || replay.OperationID != first.OperationID {
		t.Fatalf("replay %+v err=%v", replay, err)
	}
	if _, err := store.OCRInsertPackOperation(ctx, "paddleocr-vl-1.6", "user-1", "install", "key-b", sha256Hex("b")); !errors.Is(err, ErrOCRActiveOperation) {
		t.Fatalf("second active %v", err)
	}
}

func TestOCRNoticeRetentionOffline(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "ocr-notice.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	manifest := strings.Repeat("b", 64)
	if err := store.OCRRetainNotice(ctx, "paddleocr-vl-1.6", "1.6.0", manifest, "NOTICE text", "cas:notice"); err != nil {
		t.Fatal(err)
	}
	items, err := store.OCRNoticeList(ctx, "paddleocr-vl-1.6")
	if err != nil || len(items) != 1 || string(items[0].NoticeBytes) != "NOTICE text" {
		t.Fatalf("list %+v err=%v", items, err)
	}
	got, err := store.OCRNoticeRead(ctx, "paddleocr-vl-1.6", manifest)
	if err != nil || string(got.NoticeBytes) != "NOTICE text" {
		t.Fatalf("read %+v err=%v", got, err)
	}
	if _, err := store.OCRNoticeRead(ctx, "paddleocr-vl-1.6", strings.Repeat("c", 64)); !errors.Is(err, ErrOCRNoticeUnavailable) {
		t.Fatalf("unknown digest %v", err)
	}
	if err := store.OCRMarkNoticeUninstalled(ctx, "paddleocr-vl-1.6", manifest); err != nil {
		t.Fatal(err)
	}
	after, err := store.OCRNoticeList(ctx, "paddleocr-vl-1.6")
	if err != nil || len(after) != 1 || !after[0].UninstalledAt.Valid || string(after[0].NoticeBytes) != "NOTICE text" {
		t.Fatalf("retain after uninstall %+v err=%v", after, err)
	}
	if _, err := store.db.Exec(`UPDATE ocr_pack_notices SET notice_bytes=? WHERE manifest_digest=?`, []byte("tampered"), manifest); err != nil {
		t.Fatal(err)
	}
	if _, err := store.OCRNoticeRead(ctx, "paddleocr-vl-1.6", manifest); !errors.Is(err, ErrOCRNoticeUnavailable) {
		t.Fatalf("tamper %v", err)
	}
}

func TestOCRRunReadRequiresOwnerScope(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "ocr-run.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	doc := strings.Repeat("d", 64)
	snap := strings.Repeat("e", 64)
	runID, err := store.OCRInsertRun(ctx, "owner-a", "user", "owner-a", ulid.Make().String(), doc, snap)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.OCRGetRun(ctx, "owner-a", "user", "owner-a", runID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.OCRGetRun(ctx, "owner-b", "user", "owner-b", runID); !errors.Is(err, ErrOCRScopeDenied) {
		t.Fatalf("cross scope %v", err)
	}
}

func TestOCRPersistRecognitionListsAndReads(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "ocr-persist.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	runID, artifactID, err := store.OCRPersistRecognition(ctx, "owner-a", "user", "owner-a", []byte("png"), []byte("识别正文"), "ppocr", 1, true, false)
	if err != nil || runID == "" || artifactID == "" {
		t.Fatalf("persist %s %s %v", runID, artifactID, err)
	}
	items, err := store.OCRListRuns(ctx, "owner-a", "user", "owner-a", 10)
	if err != nil || len(items) != 1 || items[0].RunID != runID || items[0].ArtifactID != artifactID || items[0].State != "succeeded" {
		t.Fatalf("list %+v err=%v", items, err)
	}
	pages, err := store.OCRListRunPages(ctx, runID, 20)
	if err != nil || len(pages) != 1 || pages[0].Page != 1 || !pages[0].Complete {
		t.Fatalf("pages %+v err=%v", pages, err)
	}
	rec, err := store.OCRGetArtifact(ctx, "owner-a", artifactID)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := store.OCRReadArtifactBytes(rec)
	if err != nil || string(raw) != "识别正文" {
		t.Fatalf("bytes %q err=%v", raw, err)
	}
}

func TestOCRLegacyPPOCRMigrationRegisteredUnwired(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "ocr-legacy.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.OCRImportPPOCR(ctx, "owner-a", "user", "owner-a", "private-root", true); err != nil {
		t.Fatal(err)
	}
	state, available, marker, err := store.OCRGetPPOCR(ctx, "owner-a", "user", "owner-a")
	if err != nil || state != "registered_unwired" || available != 0 || marker != 1 {
		t.Fatalf("legacy %s avail=%d marker=%d err=%v", state, available, marker, err)
	}
	_, err = store.db.Exec(`INSERT INTO ocr_legacy_registrations(
		registration_id,owner_subject_id,scope_kind,scope_id,engine_id,root_ref,marker_detected,state,available,imported_at,revision)
		VALUES(?,?, 'user', ?, 'ppocr', 'x', 0, 'ready', 1, '1970-01-01T00:00:00Z', 1)`,
		ulid.Make().String(), "owner-b", "owner-b")
	if err == nil {
		t.Fatal("ready/available must fail closed")
	}
}

func TestLegacyPPOCRSettingsMigration(t *testing.T) {
	TestOCRLegacyPPOCRMigrationRegisteredUnwired(t)
}

func assertOCRPackJournal(t *testing.T, store *Store) {
	t.Helper()
	var checksum string
	if err := store.db.QueryRow(`SELECT checksum FROM schema_migrations WHERE version='0164_ocr_model_packs.sql'`).Scan(&checksum); err != nil || checksum == "" {
		t.Fatalf("missing 0164: %v", err)
	}
	body, err := migrations.Files.ReadFile("0164_ocr_model_packs.sql")
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(body)
	if checksum != hex.EncodeToString(sum[:]) {
		t.Fatalf("checksum %s", checksum)
	}
	if bytes.Contains(body, []byte{'\r'}) {
		t.Fatal("CRLF")
	}
	for _, table := range []string{"ocr_pack_state", "ocr_pack_gates", "ocr_pack_notices", "ocr_document_runs", "ocr_legacy_registrations"} {
		var n int
		if err := store.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&n); err != nil || n != 1 {
			t.Fatalf("missing %s", table)
		}
	}
}
