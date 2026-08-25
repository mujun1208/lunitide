package sqlite

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// databaseFingerprint describes a database by everything a caller can observe
// about its shape: every schema object with its exact text, and every migration
// the journal claims was applied with the checksum it was applied under.
func databaseFingerprint(ctx context.Context, t *testing.T, s *Store) []string {
	t.Helper()
	var out []string
	rows, err := s.db.QueryContext(ctx, `SELECT type,name,coalesce(sql,'') FROM sqlite_schema ORDER BY type,name`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var typ, name, text string
		if err := rows.Scan(&typ, &name, &text); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		out = append(out, "object\x00"+typ+"\x00"+name+"\x00"+text)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		t.Fatal(err)
	}
	rows.Close()
	journal, err := s.db.QueryContext(ctx, `SELECT version,checksum FROM schema_migrations ORDER BY version`)
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	for journal.Next() {
		var version, checksum string
		if err := journal.Scan(&version, &checksum); err != nil {
			t.Fatal(err)
		}
		out = append(out, "migration\x00"+version+"\x00"+checksum)
	}
	if err := journal.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// The template only earns the right to skip the replay if what it produces is
// indistinguishable from what the replay produces. Anything the snapshot lost
// or altered — a dropped index, a rewritten CHECK, a missing journal row —
// shows up here as a difference rather than as a puzzling failure in whichever
// unrelated test happened to depend on it.
func TestTemplatedOpenMatchesReplay(t *testing.T) {
	ctx := context.Background()
	replayed, err := Open(ctx, filepath.Join(t.TempDir(), "replayed.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer replayed.Close()
	templated, err := OpenTemplated(ctx, filepath.Join(t.TempDir(), "templated.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer templated.Close()

	want := databaseFingerprint(ctx, t, replayed)
	got := databaseFingerprint(ctx, t, templated)
	if len(want) == 0 {
		t.Fatal("replayed database reported no schema objects")
	}
	if len(got) != len(want) {
		t.Fatalf("templated database has %d fingerprint entries, replayed has %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("entry %d differs:\n templated %q\n  replayed %q", i, got[i], want[i])
		}
	}

	// Shape is not the whole contract: a database opened either way has to
	// enforce foreign keys and journal through the WAL.
	for _, p := range []struct {
		query string
		want  string
	}{{"PRAGMA foreign_keys", "1"}, {"PRAGMA journal_mode", "wal"}} {
		var replayedValue, templatedValue string
		if err := replayed.db.QueryRowContext(ctx, p.query).Scan(&replayedValue); err != nil {
			t.Fatal(err)
		}
		if err := templated.db.QueryRowContext(ctx, p.query).Scan(&templatedValue); err != nil {
			t.Fatal(err)
		}
		if templatedValue != replayedValue || templatedValue != p.want {
			t.Fatalf("%s: templated=%q replayed=%q want %q", p.query, templatedValue, replayedValue, p.want)
		}
	}
}

// Seeding is for databases that do not exist yet. Handed one that does, the
// templated path has to leave it alone — overwriting it would silently discard
// whatever it held.
func TestTemplatedOpenKeepsAnExistingDatabase(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "existing.db")
	first, err := OpenTemplated(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	created, err := first.Create(ctx, validProvider())
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	second, err := OpenTemplated(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	found, err := second.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("provider written before reopen is gone: %v", err)
	}
	if found.ID != created.ID {
		t.Fatalf("reopened database returned %q, want %q", found.ID, created.ID)
	}
}

func TestTemplatedOpenRejectsUnsafePaths(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	for _, path := range []string{
		"",
		"relative.db",
		filepath.Join(dir, "wrong.sqlite"),
		filepath.Join(dir, "null\x00.db"),
	} {
		if s, err := OpenTemplated(ctx, path); err == nil {
			s.Close()
			t.Fatalf("accepted unsafe path %q", path)
		}
		if _, err := os.Stat(path); err == nil {
			t.Fatalf("rejected path %q was still created", path)
		}
	}
}
