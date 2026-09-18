package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMemoryPreMigrationBackupContains0159(t *testing.T) {
	ctx := context.Background()
	path := seed0159Database(t, func(db *sql.DB) {
		if _, err := db.Exec(`CREATE TABLE r3_canary(id INTEGER PRIMARY KEY, note TEXT NOT NULL)`); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO r3_canary(note) VALUES('keep-me')`); err != nil {
			t.Fatal(err)
		}
	})
	dest := filepath.Join(t.TempDir(), "pre-0159.db")
	called := 0
	store, err := OpenWithOptions(ctx, path, OpenOptions{
		BeforeMigrate: func(_ context.Context, info PreMigrationInfo, backupFn func(string) error) error {
			called++
			if info.CurrentVersion != "0159_office_delivery_v2.sql" {
				t.Fatalf("current %q", info.CurrentVersion)
			}
			joined := strings.Join(info.PendingMigrations, ",")
			if !strings.Contains(joined, "0160_openai_responses_protocol.sql") {
				t.Fatalf("pending missing 0160: %v", info.PendingMigrations)
			}
			if !strings.Contains(joined, "0161_memory_fabric.sql") {
				t.Fatalf("pending missing fabric: %v", info.PendingMigrations)
			}
			if !strings.Contains(joined, "0164_ocr_model_packs.sql") || !strings.Contains(joined, "0165_media_sessions.sql") {
				t.Fatalf("pending missing ocr/media: %v", info.PendingMigrations)
			}
			return backupFn(dest)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if called != 1 {
		t.Fatalf("hook calls %d", called)
	}
	raw, err := sql.Open("sqlite", dest)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	var last string
	if err := raw.QueryRow(`SELECT version FROM schema_migrations ORDER BY rowid DESC LIMIT 1`).Scan(&last); err != nil || last != "0159_office_delivery_v2.sql" {
		t.Fatalf("backup journal last=%q err=%v", last, err)
	}
	var note string
	if err := raw.QueryRow(`SELECT note FROM r3_canary`).Scan(&note); err != nil || note != "keep-me" {
		t.Fatalf("canary %q err=%v", note, err)
	}
	var n int
	if err := raw.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name='ocr_pack_gates'`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("backup must not include ocr tables n=%d err=%v", n, err)
	}
}

func TestMemoryPreMigrationBackupHookFailure(t *testing.T) {
	ctx := context.Background()
	path := seed0159Database(t, nil)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(before)
	_, err = OpenWithOptions(ctx, path, OpenOptions{
		BeforeMigrate: func(context.Context, PreMigrationInfo, func(string) error) error {
			return errors.New("backup refused")
		},
	})
	if err == nil || !strings.Contains(err.Error(), "backup refused") {
		t.Fatalf("want hook error, got %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if hex.EncodeToString(sum[:]) != hex.EncodeToString(sha256Sum(after)) {
		t.Fatal("hook failure must leave database bytes unchanged")
	}
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	var last string
	if err := raw.QueryRow(`SELECT version FROM schema_migrations ORDER BY rowid DESC LIMIT 1`).Scan(&last); err != nil || last != "0159_office_delivery_v2.sql" {
		t.Fatalf("journal moved last=%q err=%v", last, err)
	}
}

func TestMemoryPreMigrationBackupSkippedWhenCurrent(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "current.db")
	store, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	called := 0
	store, err = OpenWithOptions(ctx, path, OpenOptions{
		BeforeMigrate: func(context.Context, PreMigrationInfo, func(string) error) error {
			called++
			return errors.New("must not backup fully migrated database")
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if called != 0 {
		t.Fatalf("hook calls %d", called)
	}
}

func TestMemoryPreMigrationBackupRequiredWithoutHook(t *testing.T) {
	ctx := context.Background()
	path := seed0159Database(t, nil)
	_, err := openWithOptions(ctx, path, nil, nil, OpenOptions{}, true)
	if !errors.Is(err, ErrPreMigrationBackupRequired) {
		t.Fatalf("got %v", err)
	}
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	var last string
	if err := raw.QueryRow(`SELECT version FROM schema_migrations ORDER BY rowid DESC LIMIT 1`).Scan(&last); err != nil || last != "0159_office_delivery_v2.sql" {
		t.Fatalf("journal moved last=%q err=%v", last, err)
	}
}

func sha256Sum(b []byte) []byte {
	sum := sha256.Sum256(b)
	return sum[:]
}
