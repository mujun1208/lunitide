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

func TestDatasourceBindingsSchemaDump(t *testing.T) {
	body, err := migrations.Files.ReadFile("0117_datasource_bindings.sql")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(body, []byte{'\r'}) {
		t.Fatal("0117_datasource_bindings.sql must be LF; CRLF changes the checksum")
	}
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	for _, name := range []string{"0059_m7_toolgap.sql", "0117_datasource_bindings.sql"} {
		sqlText, err := migrations.Files.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(ctx, string(sqlText)); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	rows, err := db.QueryContext(ctx, `SELECT type,name,coalesce(sql,'') FROM sqlite_schema WHERE name IN ('db_connections','datasource_bindings') ORDER BY type,name`)
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
	if len(lines) != 2 {
		t.Fatalf("objects = %d\n%s", len(lines), strings.Join(lines, "\n"))
	}
}
