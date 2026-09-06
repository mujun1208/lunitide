package sqlite

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"strings"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/lunitide/lunitide/migrations"
)

func TestCapabilityRolesAndKeyBackupsSchemaDump(t *testing.T) {
	body, err := migrations.Files.ReadFile("0119_capability_roles_and_key_backups.sql")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(body, []byte{'\r'}) {
		t.Fatal("0119_capability_roles_and_key_backups.sql must be LF; CRLF changes the checksum")
	}
	sum := sha256.Sum256(body)
	t.Logf("0119 sha256 %x", sum)
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	pre0119Providers := `CREATE TABLE providers (
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
    CHECK (length(origin_fingerprint) = 64 AND origin_fingerprint NOT GLOB '*[^0-9a-f]*'),
    CHECK ((credential_ref IS NOT NULL) = (credential_state = 'configured'))
)`
	if _, err := db.ExecContext(ctx, pre0119Providers); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, string(body)); err != nil {
		t.Fatal(err)
	}
	rows, err := db.QueryContext(ctx, `SELECT type,name,coalesce(sql,'') FROM sqlite_schema WHERE name IN ('providers','capability_role_bindings') ORDER BY type,name`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var lines []string
	for rows.Next() {
		var typ, name, sqlText string
		if err := rows.Scan(&typ, &name, &sqlText); err != nil {
			t.Fatal(err)
		}
		lines = append(lines, fmt.Sprintf("\t%q: %q,", typ+":"+name, sqlText))
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	t.Logf("schema objects:\n%s", strings.Join(lines, "\n"))
	for _, table := range []string{"providers", "capability_role_bindings"} {
		info, err := db.QueryContext(ctx, fmt.Sprintf(`PRAGMA table_xinfo('%s')`, table))
		if err != nil {
			t.Fatal(err)
		}
		var cols []string
		for info.Next() {
			var cid, notNull, pk, hidden int
			var name, kind string
			var def sql.NullString
			if err := info.Scan(&cid, &name, &kind, &notNull, &def, &pk, &hidden); err != nil {
				info.Close()
				t.Fatal(err)
			}
			cols = append(cols, fmt.Sprintf("{%q, %q, %q, %d, %d, %d}", name, kind, def.String, notNull, pk, hidden))
		}
		info.Close()
		t.Logf("columns %s: %s", table, strings.Join(cols, ", "))
	}
}
