package sqlite

import (
	"bytes"
	"context"
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/lunitide/lunitide/migrations"
)

func TestMemoryV2SchemaDump(t *testing.T) {
	for _, name := range []string{"0161_memory_fabric.sql", "0162_memory_retrieval.sql", "0163_memory_generations.sql"} {
		body, err := migrations.Files.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(body, []byte{'\r'}) {
			t.Fatalf("%s must be LF; CRLF changes the checksum", name)
		}
	}
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if _, err = db.ExecContext(ctx, `CREATE TABLE memory_settings (
    subject_id TEXT PRIMARY KEY,
    memory_enabled INTEGER NOT NULL,
    auto_nominate INTEGER NOT NULL,
    growth_days INTEGER NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    capture_mode TEXT NOT NULL
)`); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"0161_memory_fabric.sql", "0162_memory_retrieval.sql", "0163_memory_generations.sql"} {
		body, err := migrations.Files.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = db.ExecContext(ctx, string(body)); err != nil {
			t.Fatalf("apply %s: %v", name, err)
		}
	}
	rows, err := db.QueryContext(ctx, `SELECT type,name,coalesce(sql,'') FROM sqlite_schema
WHERE name NOT LIKE 'sqlite_autoindex_%'
  AND name NOT LIKE 'memory_search_fts%'
  AND name NOT LIKE 'trg_memory_search_fts%'
  AND name <> 'memory_settings'
ORDER BY type,name`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	got := map[string]string{}
	for rows.Next() {
		var typ, name, sqlText string
		if err := rows.Scan(&typ, &name, &sqlText); err != nil {
			t.Fatal(err)
		}
		got[typ+":"+name] = sqlText
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(got) != len(memoryV2ExpectedSchemaSQL) {
		t.Fatalf("expected schema map size %d, dump %d", len(memoryV2ExpectedSchemaSQL), len(got))
	}
	for key, want := range memoryV2ExpectedSchemaSQL {
		if got[key] != want {
			t.Fatalf("schema mismatch %s", key)
		}
	}
}
