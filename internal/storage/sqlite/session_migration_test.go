package sqlite

import (
	"crypto/sha256"
	"encoding/hex"
	"github.com/lunitide/lunitide/migrations"
	"strings"
	"testing"
)

func TestSessionMigrationPreservesOperationalRowsAndRejectsInvalidReferences(t *testing.T) {
	db := openRaw(t, t.TempDir()+"/v8.db")
	defer db.Close()
	for _, name := range []string{"0001_provider.sql", "0002_provider_production.sql", "0003_provider_app.sql", "0004_model_sync_claims.sql", "0005_electron_provider_metadata.sql", "0006_electron_credential_adoption.sql", "0007_project.sql"} {
		body, e := migrations.Files.ReadFile(name)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = db.Exec(string(body)); e != nil {
			t.Fatalf("%s: %v", name, e)
		}
	}
	_, e := db.Exec(`INSERT INTO idempotency_records VALUES('project.create','legacy','` + strings.Repeat("a", 64) + `','{}','2026-01-01T00:00:00Z','2026-01-02T00:00:00Z'); INSERT INTO audit_events VALUES('old','project.created','01ARZ3NDEKTSV4RRFFQ69G5FAV','test','{}','2026-01-01T00:00:00Z')`)
	if e != nil {
		t.Fatal(e)
	}
	body, _ := migrations.Files.ReadFile("0008_session.sql")
	if _, e = db.Exec(string(body)); e != nil {
		t.Fatal(e)
	}
	var n int
	if e = db.QueryRow(`SELECT count(*) FROM idempotency_records WHERE idempotency_key='legacy'`).Scan(&n); e != nil || n != 1 {
		t.Fatalf("preservation n=%d err=%v", n, e)
	}
	for _, q := range []string{`INSERT INTO sessions VALUES('01ARZ3NDEKTSV4RRFFQ69G5FAV','01ARZ3NDEKTSV4RRFFQ69G5FAW','Bad','active','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z',1)`, `INSERT INTO sessions VALUES('81ARZ3NDEKTSV4RRFFQ69G5FAV','01ARZ3NDEKTSV4RRFFQ69G5FAV','Bad','active','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z',1)`} {
		if _, e = db.Exec(q); e == nil {
			t.Fatalf("invalid session accepted: %s", q)
		}
	}
}
func TestSessionMigrationImmutableChecksum(t *testing.T) {
	body, e := migrations.Files.ReadFile("0008_session.sql")
	if e != nil {
		t.Fatal(e)
	}
	sum := sha256.Sum256(body)
	got := hex.EncodeToString(sum[:])
	if got != "08b1478bd48900da0f89a83de15c7517fbd6445790759d2374c449f67d436620" || got != manifest[7].checksum {
		t.Fatalf("checksum=%s manifest=%s", got, manifest[7].checksum)
	}
}
