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

func TestMROOpsLedgersSchemaDump(t *testing.T) {
	body, err := migrations.Files.ReadFile("0118_mro_ops_ledgers.sql")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(body, []byte{'\r'}) {
		t.Fatal("0118_mro_ops_ledgers.sql must be LF; CRLF changes the checksum")
	}
	sum := sha256.Sum256(body)
	t.Logf("0118 sha256 %x", sum)
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.ExecContext(context.Background(), string(body)); err != nil {
		t.Fatal(err)
	}
	rows, err := db.QueryContext(context.Background(), `SELECT type,name,coalesce(sql,'') FROM sqlite_schema WHERE name NOT LIKE 'sqlite_autoindex_%' ORDER BY type,name`)
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
	t.Logf("ops ledger objects (%d):\n%s", len(lines), strings.Join(lines, "\n"))
	if len(lines) < 20 {
		t.Fatalf("too few objects: %d", len(lines))
	}
}
