package sqlite

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/migrations"
)

func TestProjectMigrationPreservesExistingOperationalRows(t *testing.T) {
	db := openRaw(t, t.TempDir()+"/project-v6.db")
	defer db.Close()
	_, err := db.Exec(`
CREATE TABLE idempotency_records (
 operation TEXT NOT NULL, idempotency_key TEXT NOT NULL CHECK (length(idempotency_key) BETWEEN 1 AND 128),
 request_digest TEXT NOT NULL, response_json TEXT NOT NULL,
 created_at TEXT NOT NULL, expires_at TEXT NOT NULL,
 PRIMARY KEY(operation,idempotency_key));
CREATE INDEX ix_idempotency_expires ON idempotency_records(expires_at);
CREATE TABLE audit_events (
 id TEXT PRIMARY KEY, action TEXT NOT NULL, aggregate_id TEXT NOT NULL,
 actor TEXT NOT NULL, metadata_json TEXT NOT NULL, created_at TEXT NOT NULL);
CREATE INDEX ix_audit_aggregate_created ON audit_events(aggregate_id,created_at DESC);
INSERT INTO idempotency_records VALUES(
 'provider.update',' legacy-月汐 key ','` + strings.Repeat("a", 64) + `','{"version":3}',
 '2026-01-01T00:00:00Z','2026-01-02T00:00:00Z');
INSERT INTO audit_events VALUES(
 'historical-audit','provider.updated','01ARZ3NDEKTSV4RRFFQ69G5FAV',
 'fixture','{"version":3}','2026-01-01T00:00:00Z');`)
	if err != nil {
		t.Fatal(err)
	}
	body, err := migrations.Files.ReadFile("0007_project.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(string(body)); err != nil {
		t.Fatalf("apply 0007: %v", err)
	}
	var operation, response, action string
	if err = db.QueryRow(`SELECT operation,response_json FROM idempotency_records WHERE idempotency_key=' legacy-月汐 key '`).Scan(&operation, &response); err != nil || operation != "provider.update" || response != `{"version":3}` {
		t.Fatalf("idempotency not preserved: operation=%q response=%q err=%v", operation, response, err)
	}
	if err = db.QueryRow(`SELECT action FROM audit_events WHERE id='historical-audit'`).Scan(&action); err != nil || action != "provider.updated" {
		t.Fatalf("audit not preserved: action=%q err=%v", action, err)
	}
	for _, invalidID := range []string{"81ARZ3NDEKTSV4RRFFQ69G5FAV", "01ARZ3NDEKTSV4RRFFQ69G5FAI", "01ARZ3NDEKTSV4RRFFQ69G5FAU", "01arz3ndektsv4rrffq69g5fav"} {
		if _, err = db.Exec(`INSERT INTO projects(id,name,status,created_at,updated_at,version) VALUES(?, 'Bad', 'active', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z', 1)`, invalidID); err == nil {
			t.Fatalf("non-Crockford project ID accepted: %q", invalidID)
		}
	}
}

func TestProjectMigrationImmutableChecksum(t *testing.T) {
	body, err := migrations.Files.ReadFile("0007_project.sql")
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(body)
	got := hex.EncodeToString(sum[:])
	if got != "eb31143f8347a75a3abee8670fd8c9a047fe712db64c4e8378720d08698864a3" || got != manifest[6].checksum {
		t.Fatalf("0007 checksum=%s manifest=%s", got, manifest[6].checksum)
	}
}
