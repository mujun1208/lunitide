package sqlite

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBackupOnlineWALAndRestoreRoundTrip(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	livePath := filepath.Join(dir, "live.db")
	s, err := Open(ctx, livePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.ExecContext(ctx, `CREATE TABLE backup_probe(id INTEGER PRIMARY KEY, payload TEXT); PRAGMA wal_autocheckpoint=0`); err != nil {
		t.Fatal(err)
	}
	payload := strings.Repeat("committed-in-wal-", 8192)
	if _, err = s.db.ExecContext(ctx, `INSERT INTO backup_probe(payload) VALUES(?)`, payload); err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(dir, "snapshots", "online.db")
	started := time.Now()
	if err = s.CreateBackup(ctx, backup); err != nil {
		t.Fatal(err)
	}
	// Hosted -race has measured ~14s for this 128KiB copy; keep a hang ceiling,
	// not a workstation-only speed gate.
	if elapsed := time.Since(started); elapsed > 45*time.Second {
		t.Fatalf("128KiB online backup took %s (recording ceiling 45s)", elapsed)
	}

	// Mutate live after backup, then restore. The snapshot must retain exactly
	// the committed state visible at CreateBackup time.
	if _, err = s.db.ExecContext(ctx, `UPDATE backup_probe SET payload='newer-live-state'`); err != nil {
		t.Fatal(err)
	}
	if err = s.RestoreBackup(ctx, backup); err != nil {
		t.Fatal(err)
	}
	restored, err := sql.Open("sqlite", livePath)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	var got string
	if err = restored.QueryRowContext(ctx, `SELECT payload FROM backup_probe`).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != payload {
		t.Fatalf("restored payload mismatch: len=%d want=%d", len(got), len(payload))
	}
}

func TestRestoreRejectsTruncationAndBitrotWithoutTouchingLive(t *testing.T) {
	for _, tc := range []struct {
		name    string
		corrupt func([]byte) []byte
	}{
		{"truncated", func(b []byte) []byte { return b[:len(b)/2] }},
		{"bitrot", func(b []byte) []byte {
			// Byte 100 is the first b-tree page header; an impossible page type is
			// deterministic structural bitrot rather than payload-only corruption.
			b[100] = 0xff
			return b
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			dir := t.TempDir()
			live := filepath.Join(dir, "live.db")
			s, err := Open(ctx, live)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = s.db.ExecContext(ctx, `CREATE TABLE restore_probe(v TEXT); INSERT INTO restore_probe VALUES('live-survives')`); err != nil {
				t.Fatal(err)
			}
			good := filepath.Join(dir, "good.db")
			if err = s.CreateBackup(ctx, good); err != nil {
				t.Fatal(err)
			}
			b, err := os.ReadFile(good)
			if err != nil {
				t.Fatal(err)
			}
			bad := filepath.Join(dir, "bad.db")
			if err = os.WriteFile(bad, tc.corrupt(b), 0o600); err != nil {
				t.Fatal(err)
			}
			if err = s.RestoreBackup(ctx, bad); err == nil {
				t.Fatal("corrupt restore unexpectedly succeeded")
			}
			var got string
			if err = s.db.QueryRowContext(ctx, `SELECT v FROM restore_probe`).Scan(&got); err != nil {
				t.Fatalf("live store touched by failed restore: %v", err)
			}
			if got != "live-survives" {
				t.Fatalf("live value=%q", got)
			}
			s.Close()
		})
	}
}

func TestCreateBackupFailureLeavesExistingDestination(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	live := filepath.Join(dir, "live.db")
	s, err := Open(ctx, live)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	destination := filepath.Join(dir, "existing.db")
	want := []byte("not-a-database-but-must-not-be-clobbered-on-cancel")
	if err = os.WriteFile(destination, want, 0o600); err != nil {
		t.Fatal(err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if err = s.CreateBackup(cancelled, destination); err == nil {
		t.Fatal("cancelled backup unexpectedly succeeded")
	}
	got, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatal("failed backup clobbered prior destination")
	}
}

func TestBackupValidationClosesImageAndIgnoresWALSidecars(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	live := filepath.Join(dir, "live.db")
	s, err := Open(ctx, live)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err = s.db.ExecContext(ctx, `CREATE TABLE handle_probe(v TEXT); INSERT INTO handle_probe VALUES('main-image')`); err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(dir, "backup.db")
	if err = s.CreateBackup(ctx, backup); err != nil {
		t.Fatal(err)
	}
	// A bogus same-named WAL must not participate in standalone-image validation.
	if err = os.WriteFile(backup+"-wal", []byte("not a sqlite wal"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err = validateSQLiteImage(ctx, backup); err != nil {
		t.Fatal(err)
	}
	// This specifically catches leaked integrity-check handles on Windows.
	if err = os.Rename(backup, filepath.Join(dir, "renamed.db")); err != nil {
		t.Fatalf("validated image handle remained open: %v", err)
	}
}

func TestRestoreIgnoresCrashAbandonedStagingFile(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	live := filepath.Join(dir, "live.db")
	s, err := Open(ctx, live)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.ExecContext(ctx, `CREATE TABLE crash_probe(v TEXT); INSERT INTO crash_probe VALUES('durable')`); err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(dir, "backup.db")
	if err = s.CreateBackup(ctx, backup); err != nil {
		t.Fatal(err)
	}
	// Simulate process death after staging but before atomic publication.
	if err = os.WriteFile(filepath.Join(dir, ".lunitide-restore-crashed.db"), []byte("partial"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err = s.RestoreBackup(ctx, backup); err != nil {
		t.Fatal(err)
	}
	reopened, err := sql.Open("sqlite", live)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	var got string
	if err = reopened.QueryRowContext(ctx, `SELECT v FROM crash_probe`).Scan(&got); err != nil || got != "durable" {
		t.Fatalf("crash recovery got=%q err=%v", got, err)
	}
}
