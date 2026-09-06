package datasourceapp

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	_ "modernc.org/sqlite"
	"net"
	"strings"
	"testing"
)

func TestSQLResultByteAndColumnBudgets(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, body := range []string{strings.Repeat("x", MaxResultCellBytes+1), strings.Repeat("\x00", 100_000)} {
		rows, err := db.Query(`SELECT ? AS body`, body)
		if err != nil {
			t.Fatal(err)
		}
		_, got, truncated, err := scanRows(rows, 1000)
		rows.Close()
		if err != nil || !truncated || len(got) != 0 {
			t.Fatalf("oversized or JSON-escaped cell escaped cap: %d %v %v", len(got), truncated, err)
		}
	}
	rows, err := db.Query(`WITH RECURSIVE n(x) AS (SELECT 1 UNION ALL SELECT x+1 FROM n WHERE x<1000) SELECT x, ? FROM n`, strings.Repeat("字", 1000))
	if err != nil {
		t.Fatal(err)
	}
	cols, got, truncated, err := scanRows(rows, 1000)
	rows.Close()
	encoded, _ := json.Marshal(map[string]any{"columns": cols, "rows": got})
	if err != nil || !truncated || len(got) == 0 || len(encoded) > MaxResultBytes {
		t.Fatalf("result len=%d rows=%d truncated=%v err=%v", len(encoded), len(got), truncated, err)
	}
	rows, err = db.Query("SELECT " + strings.Repeat("1,", MaxResultColumns) + "1")
	if err != nil {
		t.Fatal(err)
	}
	_, _, _, err = scanRows(rows, 10)
	rows.Close()
	if !errors.Is(err, ErrResultBudget) {
		t.Fatalf("wide result: %v", err)
	}
}

type budgetReadConn struct {
	net.Conn
	reader io.Reader
}

func (c budgetReadConn) Read(p []byte) (int, error) { return c.reader.Read(p) }
func TestSQLSocketBudgetStopsDriverInput(t *testing.T) {
	c := &sqlBudgetConn{Conn: budgetReadConn{reader: bytes.NewReader(make([]byte, 100))}, remaining: 32}
	got, err := io.ReadAll(c)
	if len(got) != 32 || !errors.Is(err, ErrResultBudget) {
		t.Fatalf("read=%d err=%v", len(got), err)
	}
}
