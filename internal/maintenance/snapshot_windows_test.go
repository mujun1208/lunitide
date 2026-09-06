//go:build windows

package maintenance

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

	"github.com/lunitide/lunitide/internal/datadir"
	storage "github.com/lunitide/lunitide/internal/storage/sqlite"
)

func backupFixture(t *testing.T) (*datadir.SecureRoot, string) {
	t.Helper()
	parent := t.TempDir()
	root, err := datadir.PrepareForTest(filepath.Join(parent, "live"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	if err = os.Mkdir(filepath.Join(root.Path(), "attachments"), 0700); err != nil {
		t.Fatal(err)
	}
	bytes := []byte("可验证的附件原文\n")
	if err = os.WriteFile(filepath.Join(root.Path(), "attachments", "one.txt"), bytes, 0600); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(root.Path(), "lunitide.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`CREATE TABLE probe(value TEXT);INSERT INTO probe VALUES('before');CREATE TABLE attachments(file_ref TEXT,sha256 TEXT,deleted_at TEXT)`); err != nil {
		t.Fatal(err)
	}
	h := sha256.Sum256(bytes)
	if _, err = db.Exec(`INSERT INTO attachments VALUES(?,?,NULL)`, "one.txt", hex.EncodeToString(h[:])); err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(filepath.Join(root.Path(), "tool-workspaces", "empty"), 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root.Path(), "settings.json"), []byte(`{"before":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	return root, filepath.Join(parent, "backup")
}

func changeLive(t *testing.T, root string) {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(root, "lunitide.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`UPDATE probe SET value='after'`); err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string]string{"attachments/one.txt": "changed", "new-file.txt": "must be moved to rollback", "engine.pid": "12345", "gateway-session.nonce": "obsolete"} {
		if err = os.WriteFile(filepath.Join(root, filepath.FromSlash(name)), []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
}
func assertRestored(t *testing.T, root string) {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(root, "lunitide.db"))
	if err != nil {
		t.Fatal(err)
	}
	var got string
	err = db.QueryRow(`SELECT value FROM probe`).Scan(&got)
	_ = db.Close()
	if err != nil || got != "before" {
		t.Fatalf("restored database %q: %v", got, err)
	}
	b, err := os.ReadFile(filepath.Join(root, "attachments", "one.txt"))
	if err != nil || string(b) != "可验证的附件原文\n" {
		t.Fatalf("restored file %q: %v", b, err)
	}
	for _, name := range []string{"new-file.txt", "engine.pid", "gateway-session.nonce", "lunitide.db-wal", "lunitide.db-shm"} {
		if _, err = os.Stat(filepath.Join(root, name)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("obsolete file survived %s: %v", name, err)
		}
	}
}

func TestDirectoryBackupIncludesFilesVerifiesAndRestoresTogether(t *testing.T) {
	root, dest := backupFixture(t)
	ctx := context.Background()
	parser := filepath.Join(root.Path(), "document-parser")
	if err := os.Mkdir(parser, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(parser, "temporary-input"), []byte("parser scratch only"), 0600); err != nil {
		t.Fatal(err)
	}
	m, err := Backup(ctx, root, dest)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Files) != 3 {
		t.Fatalf("files=%d", len(m.Files))
	}
	if _, err = os.Stat(filepath.Join(dest, "document-parser")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("parser scratch entered backup: %v", err)
	}
	if _, err = Verify(ctx, dest); err != nil {
		t.Fatal(err)
	}
	changeLive(t, root.Path())
	if err = Restore(ctx, root, dest); err != nil {
		t.Fatal(err)
	}
	assertRestored(t, root.Path())
	var j restoreJournal
	if err = readJSON(root.Path(), journalName, &j); err != nil || !j.Complete {
		t.Fatalf("journal %+v: %v", j, err)
	}
	if b, err := os.ReadFile(filepath.Join(root.Path(), controlDir, j.Job, "rollback", "attachments", "one.txt")); err != nil || string(b) != "changed" {
		t.Fatalf("previous bytes not retained %q %v", b, err)
	}
	lock, err := OpenRuntime(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	_ = lock.Close()
}

func TestDirectoryBackupRecoveryAfterBothRenameWindows(t *testing.T) {
	for _, point := range []string{"old:attachments", "new:attachments", "old:lunitide.db", "new:lunitide.db", "old:new-file.txt"} {
		t.Run(point, func(t *testing.T) {
			root, dest := backupFixture(t)
			ctx := context.Background()
			if _, err := Backup(ctx, root, dest); err != nil {
				t.Fatal(err)
			}
			changeLive(t, root.Path())
			crash := errors.New("simulated process termination after rename")
			err := restoreWithHook(ctx, root, dest, func(step string) error {
				if step == point {
					return crash
				}
				return nil
			})
			if !errors.Is(err, crash) {
				t.Fatalf("expected crash at %s got %v", point, err)
			}
			pending, err := pendingRestore(root.Path())
			if err != nil || !pending {
				t.Fatalf("pending=%v err=%v", pending, err)
			}
			lock, err := OpenRuntime(ctx, root)
			if err != nil {
				t.Fatal(err)
			}
			_ = lock.Close()
			assertRestored(t, root.Path())
			lock, err = OpenRuntime(ctx, root)
			if err != nil {
				t.Fatal(err)
			}
			_ = lock.Close()
		})
	}
}

func TestDirectoryMaintenanceExcludesBothHostAndEngineHandles(t *testing.T) {
	root, dest := backupFixture(t)
	ctx := context.Background()
	host, err := OpenRuntime(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer host.Close()
	engine, err := OpenRuntime(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	if _, err = Backup(ctx, root, dest); !errors.Is(err, datadir.ErrMaintenanceBusy) {
		t.Fatalf("live backup allowed: %v", err)
	}
	_ = host.Close()
	if _, err = root.AcquireDataLock(true); !errors.Is(err, datadir.ErrMaintenanceBusy) {
		t.Fatalf("engine lock bypassed: %v", err)
	}
	_ = engine.Close()
	exclusive, err := root.AcquireDataLock(true)
	if err != nil {
		t.Fatal(err)
	}
	defer exclusive.Close()
	if _, err = OpenRuntime(ctx, root); !errors.Is(err, datadir.ErrMaintenanceBusy) {
		t.Fatalf("runtime allowed during maintenance: %v", err)
	}
}

func TestDirectoryRestoreRejectsBitrotAndMissingReferencedFileBeforeLiveMutation(t *testing.T) {
	for _, mode := range []string{"bitrot", "missing", "unlisted", "path-escape"} {
		t.Run(mode, func(t *testing.T) {
			root, dest := backupFixture(t)
			ctx := context.Background()
			if _, err := Backup(ctx, root, dest); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dest, "data", "attachments", "one.txt")
			switch mode {
			case "bitrot":
				if err := os.WriteFile(path, []byte("bad"), 0600); err != nil {
					t.Fatal(err)
				}
			case "missing":
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			case "unlisted":
				if err := os.WriteFile(filepath.Join(dest, "data", "surprise"), []byte("x"), 0600); err != nil {
					t.Fatal(err)
				}
			case "path-escape":
				m, err := readManifest(dest)
				if err != nil {
					t.Fatal(err)
				}
				m.Files[0].Path = "../escape"
				if err = writeJSON(dest, "manifest.json", m); err != nil {
					t.Fatal(err)
				}
			}
			if err := Restore(ctx, root, dest); err == nil {
				t.Fatal("corrupt restore accepted")
			}
			b, err := os.ReadFile(filepath.Join(root.Path(), "attachments", "one.txt"))
			if err != nil || string(b) != "可验证的附件原文\n" {
				t.Fatal("live file changed")
			}
		})
	}
	root, dest := backupFixture(t)
	if err := os.Remove(filepath.Join(root.Path(), "attachments", "one.txt")); err != nil {
		t.Fatal(err)
	}
	if _, err := Backup(context.Background(), root, dest); err == nil || !strings.Contains(err.Error(), "references missing") {
		t.Fatalf("missing DB reference accepted: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "manifest.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("failed backup published manifest")
	}
}

func TestDirectoryBackupRealSchemaReopensAfterRestore(t *testing.T) {
	parent := t.TempDir()
	root, err := datadir.PrepareForTest(filepath.Join(parent, "live"))
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	ctx := context.Background()
	s, err := storage.OpenSecure(ctx, root, "lunitide.db")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(parent, "backup")
	if _, err = Backup(ctx, root, dest); err != nil {
		t.Fatal(err)
	}
	if err = Restore(ctx, root, dest); err != nil {
		t.Fatal(err)
	}
	lock, err := OpenRuntime(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	s, err = storage.OpenSecure(ctx, root, "lunitide.db")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.CheckReadiness(ctx); err != nil {
		t.Fatal(err)
	}
}
