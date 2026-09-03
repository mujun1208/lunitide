package datasourceapp

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

// TestLiveMySQLDriver exercises the production MySQL Pinger/Querier against a
// real server. It is skipped unless LUNITIDE_MYSQL_DSN is set, e.g.:
//
//	root:secret@tcp(127.0.0.1:3306)/?allowPublicKeyRetrieval=true&tls=false
//
// The test is strictly read-only: it pings, reads VERSION(), lists schemas via
// the metadata allowlist SQL, and verifies the row cap on a live cursor. It
// never creates or mutates anything on the target server.
func TestLiveMySQLDriver(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("LUNITIDE_MYSQL_DSN"))
	if dsn == "" {
		t.Skip("set LUNITIDE_MYSQL_DSN to run the live MySQL integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := SQLPinger(ctx, "mysql", dsn); err != nil {
		t.Fatalf("ping (read-only SELECT 1): %v", err)
	}

	cols, rows, _, err := SQLQuerier(ctx, "mysql", dsn, "SELECT VERSION()", nil, 10)
	if err != nil {
		t.Fatalf("version query: %v", err)
	}
	if len(cols) != 1 || len(rows) != 1 {
		t.Fatalf("version shape cols=%v rows=%v", cols, rows)
	}
	t.Logf("server VERSION() = %v", rows[0][0])

	sqlText, args, err := browseSQL("mysql", "schema", "", "")
	if err != nil {
		t.Fatal(err)
	}
	scols, srows, _, err := SQLQuerier(ctx, "mysql", dsn, sqlText, args, 200)
	if err != nil {
		t.Fatalf("schema browse: %v", err)
	}
	if len(srows) == 0 {
		t.Fatal("expected at least one schema from information_schema.schemata")
	}
	t.Logf("schema browse cols=%v count=%d first=%v", scols, len(srows), srows[0])

	// A live cursor with more rows than maxRows must report truncated=true and
	// stop at exactly maxRows — the DoS guard the service depends on.
	_, capped, truncated, err := SQLQuerier(ctx, "mysql", dsn,
		"SELECT table_name FROM information_schema.tables", nil, 1)
	if err != nil {
		t.Fatalf("row-cap query: %v", err)
	}
	if len(capped) != 1 || !truncated {
		t.Fatalf("row cap not enforced: rows=%d truncated=%v", len(capped), truncated)
	}
}

// TestLiveMySQLServiceEndToEnd drives the full production Service chain against
// a real server: Create (DSN only in the secret ref) → Probe (marks verified)
// → Browse (metadata allowlist) → Query (read) → write is denied. Skipped
// unless LUNITIDE_MYSQL_DSN is set. Strictly read-only against the target: no
// write querier is wired here, so every statement (even a local one) falls back
// to the read-only path — this asserts that guarantee. Production wires
// SQLWriteQuerier, which permits writes on a local connection only.
func TestLiveMySQLServiceEndToEnd(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("LUNITIDE_MYSQL_DSN"))
	if dsn == "" {
		t.Skip("set LUNITIDE_MYSQL_DSN to run the live MySQL service test")
	}
	svc, _, secrets := newTestService()
	svc.SetPinger(SQLPinger)
	svc.SetQuerier(SQLQuerier)
	ctx := context.Background()

	conn, err := svc.Create(ctx, CreateInput{Name: "本地 MySQL 只读", Kind: "mysql", DSN: dsn})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if conn.ReadonlyVerified {
		t.Fatal("a fresh connection must not be pre-verified")
	}
	// The raw DSN must live only behind the secret ref, never on the row.
	if len(secrets) != 1 {
		t.Fatalf("expected exactly one stored secret, got %d", len(secrets))
	}

	if err := svc.Probe(ctx, conn.ID); err != nil {
		t.Fatalf("probe: %v", err)
	}
	list, err := svc.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	verified := false
	for _, c := range list {
		if c.ID == conn.ID && c.ReadonlyVerified {
			verified = true
		}
	}
	if !verified {
		t.Fatal("probe must mark the connection read-only verified")
	}

	items, err := svc.Browse(ctx, BrowseInput{ConnectionID: conn.ID, Scope: "schema"})
	if err != nil {
		t.Fatalf("browse schema: %v", err)
	}
	if len(items) == 0 {
		t.Fatal("expected at least one schema from the live server")
	}
	t.Logf("browse schema count=%d first=%s", len(items), items[0].Name)

	res, err := svc.Query(ctx, QueryInput{ConnectionID: conn.ID, SQL: "SELECT 1 AS n", MaxRows: 10})
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if res.RowCount != 1 || len(res.Columns) != 1 {
		t.Fatalf("select shape = %+v", res)
	}

	// A write must be denied at the service allowlist, before it can reach the
	// server. mysql.user is never touched.
	if _, err := svc.Query(ctx, QueryInput{ConnectionID: conn.ID, SQL: "DELETE FROM mysql.user"}); !errors.Is(err, ErrStatementDenied) {
		t.Fatalf("write must be denied, got %v", err)
	}
}
