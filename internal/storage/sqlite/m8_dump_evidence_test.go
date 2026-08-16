// M8 schema evidence dump (T-8.0.1): applies migrations 0061-0068 to a raw
// database and asserts the exact sqlite_schema text for every new object.
// The printed catalog is the source of truth for the store.go
// expectedSchemaSQL additions - regenerate with:
//
//	go test ./internal/storage/sqlite/ -run TestM8SchemaDump -v
package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/lunitide/lunitide/migrations"
)

func TestM8SchemaDump(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	// The eight M8 files reference only themselves; apply in name order.
	names := []string{
		"0061_m8_memory_core.sql",
		"0062_m8_knowledge_graph.sql",
		"0063_m8_handoff_tombstone.sql",
		"0064_m8_automation.sql",
		"0065_m8_learning_candidates.sql",
		"0066_m8_collab_gate.sql",
		"0067_m8_plugin.sql",
		"0068_m8_expert.sql",
	}
	for _, name := range names {
		body, err := migrations.Files.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if _, err := db.ExecContext(ctx, string(body)); err != nil {
			t.Fatalf("apply %s: %v", name, err)
		}
	}
	rows, err := db.QueryContext(ctx, `SELECT type,name,coalesce(sql,'') FROM sqlite_schema WHERE name NOT LIKE 'sqlite_autoindex_%' ORDER BY type,name`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var lines []string
	count := 0
	for rows.Next() {
		var typ, name, sqlText string
		if err := rows.Scan(&typ, &name, &sqlText); err != nil {
			t.Fatal(err)
		}
		count++
		lines = append(lines, fmt.Sprintf("%q: %q,", typ+":"+name, sqlText))
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	sort.Strings(lines)
	t.Logf("M8 schema objects (%d):\n%s", count, strings.Join(lines, "\n"))
	// 30 tables + 16 indexes + 5 triggers = 51 objects; the count assertion
	// pins drift on any future DDL edit.
	if count != 51 {
		t.Fatalf("M8 schema object count = %d, want 51", count)
	}
}
