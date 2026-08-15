package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"sort"
	"testing"

	"github.com/lunitide/lunitide/migrations"
	_ "modernc.org/sqlite"
)

// Temporary helper: applies every embedded migration on a scratch database
// and prints the stored sqlite_schema DDL for the M5-touched objects.
func TestM5DumpSchema(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "dump.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, "PRAGMA foreign_keys=OFF"); err != nil {
		t.Fatal(err)
	}
	entries, err := migrations.Files.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".sql" {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for _, n := range names {
		b, _ := migrations.Files.ReadFile(n)
		if _, err := db.ExecContext(ctx, string(b)); err != nil {
			t.Fatalf("apply %s: %v", n, err)
		}
	}
	rows, err := db.QueryContext(ctx, `SELECT type,name,coalesce(sql,'') FROM sqlite_schema WHERE name = 'm5_changeset_item' ORDER BY type,name`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var typ, name, sqlText string
		if err := rows.Scan(&typ, &name, &sqlText); err != nil {
			t.Fatal(err)
		}
		fmt.Printf("DUMP|%s|%s|%q\n", typ, name, sqlText)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
}
