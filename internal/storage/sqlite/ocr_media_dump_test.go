package sqlite

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/lunitide/lunitide/migrations"
)

func TestGenerateOCRMediaExpectedSchema(t *testing.T) {
	if os.Getenv("GENERATE_R3_SCHEMA") == "" {
		t.Skip("set GENERATE_R3_SCHEMA=1 to regenerate ocr_media_expected_schema.go")
	}
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	for _, name := range []string{"0164_ocr_model_packs.sql", "0165_media_sessions.sql"} {
		body, err := migrations.Files.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(body, []byte{'\r'}) {
			t.Fatalf("%s must be LF", name)
		}
		if _, err = db.ExecContext(ctx, string(body)); err != nil {
			t.Fatalf("apply %s: %v", name, err)
		}
	}
	rows, err := db.QueryContext(ctx, `SELECT type,name,coalesce(sql,'') FROM sqlite_schema WHERE name NOT LIKE 'sqlite_autoindex_%' ORDER BY type,name`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	type item struct{ key, sql string }
	var items []item
	for rows.Next() {
		var typ, name, sqlText string
		if err := rows.Scan(&typ, &name, &sqlText); err != nil {
			t.Fatal(err)
		}
		items = append(items, item{typ + ":" + name, sqlText})
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].key < items[j].key })
	var b strings.Builder
	b.WriteString("package sqlite\n\n")
	b.WriteString("func init() {\n")
	b.WriteString("\tfor k, v := range ocrMediaExpectedSchemaSQL {\n")
	b.WriteString("\t\texpectedSchemaSQL[k] = v\n")
	b.WriteString("\t}\n")
	b.WriteString("}\n\n")
	b.WriteString("var ocrMediaExpectedSchemaSQL = map[string]string{\n")
	for _, it := range items {
		fmt.Fprintf(&b, "\t%q: %q,\n", it.key, it.sql)
	}
	b.WriteString("}\n")
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller")
	}
	out := filepath.Join(filepath.Dir(thisFile), "ocr_media_expected_schema.go")
	if err := os.WriteFile(out, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestOCRMediaSchemaLF(t *testing.T) {
	for _, name := range []string{"0164_ocr_model_packs.sql", "0165_media_sessions.sql"} {
		body, err := migrations.Files.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(body, []byte{'\r'}) {
			t.Fatalf("%s must be LF", name)
		}
	}
}

func TestOCRMediaSchemaDump(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	for _, name := range []string{"0164_ocr_model_packs.sql", "0165_media_sessions.sql"} {
		body, err := migrations.Files.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = db.ExecContext(ctx, string(body)); err != nil {
			t.Fatalf("apply %s: %v", name, err)
		}
	}
	rows, err := db.QueryContext(ctx, `SELECT type,name,coalesce(sql,'') FROM sqlite_schema WHERE name NOT LIKE 'sqlite_autoindex_%' ORDER BY type,name`)
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
	if len(got) != len(ocrMediaExpectedSchemaSQL) {
		t.Fatalf("expected schema map size %d, dump %d", len(ocrMediaExpectedSchemaSQL), len(got))
	}
	for key, want := range ocrMediaExpectedSchemaSQL {
		if got[key] != want {
			t.Fatalf("schema mismatch %s", key)
		}
	}
}
