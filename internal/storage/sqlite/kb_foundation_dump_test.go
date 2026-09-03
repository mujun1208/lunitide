package sqlite

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/lunitide/lunitide/migrations"
)

func TestKBFoundationSchemaDump(t *testing.T) {
	for _, name := range []string{"0113_kb_chunk_body.sql", "0114_expert_foundation.sql"} {
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
	apply := func(name string) {
		t.Helper()
		body, err := migrations.Files.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if _, err := db.ExecContext(ctx, string(body)); err != nil {
			t.Fatalf("apply %s: %v", name, err)
		}
	}
	apply("0062_m8_knowledge_graph.sql")
	apply("0113_kb_chunk_body.sql")
	apply("0114_expert_foundation.sql")
	rows, err := db.QueryContext(ctx, `SELECT type,name,coalesce(sql,'') FROM sqlite_schema WHERE name IN (
		'kb_chunks','ux_kb_collections_scope','expert_growth_paths','kb_chunk_fts',
		'trg_kb_chunk_fts_ai','trg_kb_chunk_fts_au','trg_kb_chunk_fts_ad'
	) ORDER BY type,name`)
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
		lines = append(lines, fmt.Sprintf("%s:%s\n%s", typ, name, sqlText))
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	t.Log("\n" + strings.Join(lines, "\n---\n"))
}
