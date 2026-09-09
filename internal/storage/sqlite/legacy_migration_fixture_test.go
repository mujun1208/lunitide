package sqlite

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/migrations"
)

// Build an actual historical schema instead of deleting one journal entry
// from the latest schema: later migrations must never survive the boundary.
func legacyBeforeMigration(t *testing.T, path, pendingPrefix string) *sql.DB {
	t.Helper()
	ctx := context.Background()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	if err = ensureMigrationJournal(ctx, db); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, m := range manifest {
		if strings.HasPrefix(m.name, pendingPrefix) {
			found = true
			break
		}
		body, e := migrations.Files.ReadFile(m.name)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = db.ExecContext(ctx, string(body)); e != nil {
			t.Fatalf("legacy migration %s: %v", m.name, e)
		}
		if m.name == "0002_provider_production.sql" {
			if e = (&Store{}).migrateV1Data(ctx, db); e != nil {
				t.Fatal(e)
			}
		}
		if _, e = db.ExecContext(ctx, `INSERT INTO schema_migrations(version,applied_at,checksum) VALUES(?,?,?)`, m.name, rfc(rtAt), m.checksum); e != nil {
			t.Fatal(e)
		}
	}
	if !found {
		t.Fatalf("unknown migration prefix %q", pendingPrefix)
	}
	return db
}
