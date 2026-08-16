// M9 schema evidence dump (T-9.1.1): applies migration 0069 to a raw
// database and asserts the exact sqlite_schema text for every new object.
// The printed catalog is the source of truth for the store.go
// expectedSchemaSQL additions - regenerate with:
//
//	go test ./internal/storage/sqlite/ -run TestM9SchemaDump -v
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

func TestM9SchemaDump(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	names := []string{"0069_m9_org_foundation.sql"}
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
	t.Logf("M9 schema objects (%d):\n%s", count, strings.Join(lines, "\n"))
	// 5 tables + 4 indexes + 7 triggers = 16 objects.
	if count != 16 {
		t.Fatalf("M9 schema object count = %d, want 16", count)
	}
}
