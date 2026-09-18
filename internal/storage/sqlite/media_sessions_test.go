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

func TestMediaSessionMigration(t *testing.T) {
	ctx := context.Background()
	t.Run("empty", func(t *testing.T) {
		store, err := Open(ctx, filepath.Join(t.TempDir(), "media-empty.db"))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = store.Close() })
		assertMediaJournal(t, store)
	})
	t.Run("from_ocr_predecessor", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "from-ocr.db")
		raw := legacyBeforeMigration(t, path, "0165_")
		if err := raw.Close(); err != nil {
			t.Fatal(err)
		}
		store, err := Open(ctx, path)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = store.Close() })
		assertMediaJournal(t, store)
		var n int
		if err := store.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name='ocr_pack_gates'`).Scan(&n); err != nil || n != 1 {
			t.Fatalf("ocr predecessor missing n=%d err=%v", n, err)
		}
	})
	t.Run("reopen", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "media-reopen.db")
		store, err := Open(ctx, path)
		if err != nil {
			t.Fatal(err)
		}
		assertMediaJournal(t, store)
		if err = store.Close(); err != nil {
			t.Fatal(err)
		}
		store, err = Open(ctx, path)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = store.Close() })
		assertMediaJournal(t, store)
	})
}

func TestMediaSessionChecksumMismatch(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "media-checksum.db")
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
	if _, err = raw.Exec(`UPDATE schema_migrations SET checksum=? WHERE version='0165_media_sessions.sql'`, strings.Repeat("0", 64)); err != nil {
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

func TestMediaSessionV2Disabled(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "media-off.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	settings, err := store.MediaSettings(ctx)
	if err != nil || settings.MediaSessionV2 || settings.ActivityCenterV2 {
		t.Fatalf("%+v err=%v", settings, err)
	}
	if _, _, err := store.CreateMediaSession(ctx, "owner-a", "user", "owner-a", "owned", "", ulid.Make().String()); !errors.Is(err, ErrMediaSessionV2Disabled) {
		t.Fatalf("got %v", err)
	}
}

func TestEnableMediaSessionV2TurnsOnProductFlag(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "media-on.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.EnableMediaSessionV2(ctx); err != nil {
		t.Fatal(err)
	}
	settings, err := store.MediaSettings(ctx)
	if err != nil || !settings.MediaSessionV2 || !settings.ActivityCenterV2 {
		t.Fatalf("%+v err=%v", settings, err)
	}
	revision := settings.Revision
	if err := store.EnableMediaSessionV2(ctx); err != nil {
		t.Fatal(err)
	}
	again, err := store.MediaSettings(ctx)
	if err != nil || !again.MediaSessionV2 || !again.ActivityCenterV2 || again.Revision != revision {
		t.Fatalf("second enable must stay idempotent %+v err=%v", again, err)
	}
	if _, _, err := store.CreateMediaSession(ctx, "owner-a", "user", "owner-a", "owned", "", ulid.Make().String()); err != nil {
		t.Fatalf("create: %v", err)
	}
}

func TestMediaSessionRejectsCrossScope(t *testing.T) {
	ctx := context.Background()
	store := openMediaV2(t)
	snap, _, err := store.CreateMediaSession(ctx, "owner-a", "user", "owner-a", "owned", "", ulid.Make().String())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetMediaSession(ctx, "owner-b", "user", "owner-b", snap.MediaSessionID); !errors.Is(err, ErrMediaScopeDenied) {
		t.Fatalf("got %v", err)
	}
}

func TestMediaSessionIdempotencyConflict(t *testing.T) {
	ctx := context.Background()
	store := openMediaV2(t)
	snap, _, err := store.CreateMediaSession(ctx, "owner-a", "user", "owner-a", "owned", "", ulid.Make().String())
	if err != nil {
		t.Fatal(err)
	}
	first, _, err := store.InsertMediaOperation(ctx, "owner-a", "user", "owner-a", snap.MediaSessionID, "play", "same-key")
	if err != nil {
		t.Fatal(err)
	}
	replay, _, err := store.InsertMediaOperation(ctx, "owner-a", "user", "owner-a", snap.MediaSessionID, "play", "same-key")
	if err != nil || replay.OperationID != first.OperationID {
		t.Fatalf("replay %+v err=%v", replay, err)
	}
	if _, _, err := store.InsertMediaOperation(ctx, "owner-a", "user", "owner-a", snap.MediaSessionID, "pause", "same-key"); !errors.Is(err, ErrMediaIdempotencyConflict) {
		t.Fatalf("param change %v", err)
	}
}

func TestMediaSessionDoubleNextOnce(t *testing.T) {
	ctx := context.Background()
	store := openMediaV2(t)
	snap, _, err := store.CreateMediaSession(ctx, "owner-a", "user", "owner-a", "owned", "", ulid.Make().String())
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.AdvanceMediaQueueOnce(ctx, "owner-a", "user", "owner-a", snap.MediaSessionID, "next-1")
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.AdvanceMediaQueueOnce(ctx, "owner-a", "user", "owner-a", snap.MediaSessionID, "next-1")
	if err != nil || second.OperationID != first.OperationID {
		t.Fatalf("double next %+v %+v err=%v", first, second, err)
	}
	var n int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM media_operations WHERE media_session_id=? AND action='next'`, snap.MediaSessionID).Scan(&n); err != nil || n != 1 {
		t.Fatalf("next count %d err=%v", n, err)
	}
}

func TestMediaSessionQueueCASConflict(t *testing.T) {
	ctx := context.Background()
	store := openMediaV2(t)
	assetA, err := store.InsertMediaAsset(ctx, "owner-a", "user", "owner-a", "user_selected", "ref-a", "audio/mpeg", "audio", "A", 1)
	if err != nil {
		t.Fatal(err)
	}
	assetB, err := store.InsertMediaAsset(ctx, "owner-a", "user", "owner-a", "user_selected", "ref-b", "audio/mpeg", "audio", "B", 1)
	if err != nil {
		t.Fatal(err)
	}
	snap, _, err := store.CreateMediaSession(ctx, "owner-a", "user", "owner-a", "owned", assetA, ulid.Make().String())
	if err != nil {
		t.Fatal(err)
	}
	next, err := store.ReplaceMediaQueue(ctx, "owner-a", "user", "owner-a", snap.MediaSessionID, snap.QueueRevision, []string{assetA, assetB})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReplaceMediaQueue(ctx, "owner-a", "user", "owner-a", snap.MediaSessionID, snap.QueueRevision, []string{assetB}); !errors.Is(err, ErrMediaQueueConflict) {
		t.Fatalf("stale cas %v next=%d", err, next)
	}
}

func TestMediaQueueJumpByAssetIDAndNext(t *testing.T) {
	ctx := context.Background()
	store := openMediaV2(t)
	assetA, err := store.InsertMediaAsset(ctx, "owner-a", "user", "owner-a", "user_selected", "ref-a", "audio/mpeg", "audio", "A", 1)
	if err != nil {
		t.Fatal(err)
	}
	assetB, err := store.InsertMediaAsset(ctx, "owner-a", "user", "owner-a", "user_selected", "ref-b", "audio/mpeg", "audio", "B", 1)
	if err != nil {
		t.Fatal(err)
	}
	snap, _, err := store.CreateMediaSession(ctx, "owner-a", "user", "owner-a", "owned", assetA, ulid.Make().String())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReplaceMediaQueue(ctx, "owner-a", "user", "owner-a", snap.MediaSessionID, snap.QueueRevision, []string{assetA, assetB}); err != nil {
		t.Fatal(err)
	}
	snap, err = store.GetMediaSessionForOwner(ctx, "owner-a", snap.MediaSessionID)
	if err != nil {
		t.Fatal(err)
	}
	if snap.AssetID != assetA {
		t.Fatalf("replace must point at first asset %s", snap.AssetID)
	}
	jumped, _, err := store.ApplyMediaQueueCommand(ctx, "owner-a", snap.MediaSessionID, "jump", assetB, ulid.Make().String(), nil, snap.QueueRevision)
	if err != nil {
		t.Fatal(err)
	}
	if jumped.AssetID != assetB {
		t.Fatalf("jump by assetId %s", jumped.AssetID)
	}
	next, _, err := store.ApplyMediaSessionCommand(ctx, "owner-a", jumped.MediaSessionID, "next", ulid.Make().String(), jumped.Revision, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if next.AssetID != assetA {
		t.Fatalf("next wrap %s", next.AssetID)
	}
}

func openMediaV2(t *testing.T) *Store {
	t.Helper()
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "media-on.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.EnableMediaSessionV2ForTest(ctx); err != nil {
		t.Fatal(err)
	}
	return store
}

func assertMediaJournal(t *testing.T, store *Store) {
	t.Helper()
	var checksum string
	if err := store.db.QueryRow(`SELECT checksum FROM schema_migrations WHERE version='0165_media_sessions.sql'`).Scan(&checksum); err != nil || checksum == "" {
		t.Fatalf("missing 0165: %v", err)
	}
	body, err := migrations.Files.ReadFile("0165_media_sessions.sql")
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
	for _, table := range []string{"media_assets", "media_sessions", "media_operations", "media_queue_items", "media_settings", "media_audio_focus"} {
		var n int
		if err := store.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&n); err != nil || n != 1 {
			t.Fatalf("missing %s", table)
		}
	}
}
