package sqlite

import (
	"bytes"
	"context"
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/lunitide/lunitide/migrations"
)

// TestOpenAIResponsesProtocolMigrationKeepsRows replays 0160 over the exact
// post-0119 provider tables, proving existing providers, adoptions, triggers
// and the index survive the CHECK-enum rebuild and the new enum is accepted.
func TestOpenAIResponsesProtocolMigrationKeepsRows(t *testing.T) {
	body, err := migrations.Files.ReadFile("0160_openai_responses_protocol.sql")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(body, []byte{'\r'}) {
		t.Fatal("0160_openai_responses_protocol.sql must be LF; CRLF changes the checksum")
	}
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	pre := []string{
		`CREATE TABLE providers (
    id TEXT PRIMARY KEY,
    legacy_id TEXT UNIQUE,
    name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 500),
    protocol TEXT NOT NULL CHECK (protocol IN ('openai_compatible', 'anthropic', 'volc_speech')),
    base_url TEXT NOT NULL CHECK (length(base_url) BETWEEN 1 AND 2048),
    credential_ref TEXT CHECK (credential_ref IS NULL OR length(credential_ref) BETWEEN 1 AND 500),
    credential_state TEXT NOT NULL CHECK (credential_state IN ('configured', 'missing', 'unavailable', 'requires_reentry')),
    status TEXT NOT NULL DEFAULT 'enabled' CHECK (status IN ('enabled', 'disabled')),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    version INTEGER NOT NULL DEFAULT 1 CHECK (version > 0),
    deleted_at TEXT, origin_fingerprint TEXT NOT NULL
    DEFAULT '0000000000000000000000000000000000000000000000000000000000000000'
    CHECK (length(origin_fingerprint) = 64 AND origin_fingerprint NOT GLOB '*[^0-9a-f]*'), credential_ref_backups TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(credential_ref_backups)),
    CHECK ((credential_ref IS NOT NULL) = (credential_state = 'configured'))
)`,
		`CREATE TRIGGER providers_credential_ref_insert
BEFORE INSERT ON providers WHEN NEW.credential_ref IS NOT NULL AND length(NEW.credential_ref) > 256
BEGIN SELECT RAISE(ABORT, 'credential_ref exceeds 256'); END`,
		`CREATE TRIGGER providers_credential_ref_update
BEFORE UPDATE OF credential_ref ON providers WHEN NEW.credential_ref IS NOT NULL AND length(NEW.credential_ref) > 256
BEGIN SELECT RAISE(ABORT, 'credential_ref exceeds 256'); END`,
		`CREATE TABLE credential_adoptions (
    credential_ref TEXT PRIMARY KEY CHECK (length(credential_ref) BETWEEN 1 AND 256),
    provider_id TEXT NOT NULL REFERENCES providers(id),
    origin TEXT NOT NULL CHECK (length(origin) BETWEEN 1 AND 2048),
    protocol TEXT NOT NULL CHECK (protocol IN ('openai_compatible', 'anthropic', 'volc_speech')),
    receipt_id TEXT NOT NULL UNIQUE CHECK (length(receipt_id) BETWEEN 1 AND 64),
    adopted_at TEXT NOT NULL
)`,
		`CREATE INDEX ix_credential_adoptions_provider ON credential_adoptions(provider_id)`,
		`INSERT INTO providers(id,name,protocol,base_url,credential_ref,credential_state,created_at,updated_at,credential_ref_backups)
VALUES('p1','DeepSeek','openai_compatible','https://api.deepseek.com','cred-1','configured','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z','["old-1"]')`,
		`INSERT INTO credential_adoptions(credential_ref,provider_id,origin,protocol,receipt_id,adopted_at)
VALUES('cred-1','p1','https://api.deepseek.com','openai_compatible','r1','2026-01-01T00:00:00Z')`,
	}
	for _, stmt := range pre {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("%v\n%s", err, stmt)
		}
	}
	if _, err := db.ExecContext(ctx, string(body)); err != nil {
		t.Fatal(err)
	}
	var name, backups, adoptionProtocol string
	if err := db.QueryRowContext(ctx, `SELECT p.name, p.credential_ref_backups, a.protocol FROM providers p JOIN credential_adoptions a ON a.provider_id=p.id WHERE p.id='p1'`).Scan(&name, &backups, &adoptionProtocol); err != nil {
		t.Fatal(err)
	}
	if name != "DeepSeek" || backups != `["old-1"]` || adoptionProtocol != "openai_compatible" {
		t.Fatalf("rows lost across rebuild: %q %q %q", name, backups, adoptionProtocol)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO providers(id,name,protocol,base_url,credential_state,created_at,updated_at)
VALUES('p2','Ark Agent Plan','openai_responses','https://ark.cn-beijing.volces.com/api/plan/v3','missing','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`); err != nil {
		t.Fatalf("openai_responses must be accepted after 0160: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO credential_adoptions(credential_ref,provider_id,origin,protocol,receipt_id,adopted_at)
VALUES('cred-2','p2','https://ark.cn-beijing.volces.com','openai_responses','r2','2026-01-01T00:00:00Z')`); err != nil {
		t.Fatalf("adoption protocol enum not widened: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO providers(id,name,protocol,base_url,credential_state,created_at,updated_at)
VALUES('p3','Bad','grpc','https://x','missing','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`); err == nil {
		t.Fatal("unknown protocol must still be rejected")
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO providers(id,name,protocol,base_url,credential_ref,credential_state,created_at,updated_at)
VALUES('p4','Long','anthropic','https://x',?, 'configured','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`, string(bytes.Repeat([]byte("x"), 257))); err == nil {
		t.Fatal("credential_ref trigger must be recreated by 0160")
	}
	for _, object := range []string{"providers_credential_ref_insert", "providers_credential_ref_update", "ix_credential_adoptions_provider"} {
		var n int
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_schema WHERE name=?`, object).Scan(&n); err != nil || n != 1 {
			t.Fatalf("schema object %s count=%d err=%v", object, n, err)
		}
	}
	for _, leftover := range []string{"providers_0160_old", "credential_adoptions_0160_old"} {
		var n int
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_schema WHERE name=?`, leftover).Scan(&n); err != nil || n != 0 {
			t.Fatalf("leftover %s count=%d err=%v", leftover, n, err)
		}
	}
}
