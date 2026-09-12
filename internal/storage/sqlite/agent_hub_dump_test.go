package sqlite

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/lunitide/lunitide/internal/agenthub"
	"github.com/lunitide/lunitide/migrations"
)

func TestAgentHubSchemaDump(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	body, err := migrations.Files.ReadFile("0153_agent_hub.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(context.Background(), string(body)); err != nil {
		t.Fatal(err)
	}
	rows, err := db.QueryContext(context.Background(), `SELECT type,name,coalesce(sql,'') FROM sqlite_schema WHERE name NOT LIKE 'sqlite_autoindex_%' ORDER BY type,name`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var lines []string
	count := 0
	for rows.Next() {
		var typ, name, sqlText string
		if err = rows.Scan(&typ, &name, &sqlText); err != nil {
			t.Fatal(err)
		}
		count++
		lines = append(lines, fmt.Sprintf("%q: %q,", typ+":"+name, sqlText))
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	sort.Strings(lines)
	t.Logf("agent hub schema objects (%d):\n%s", count, strings.Join(lines, "\n"))
	if count != 7 {
		t.Fatalf("agent hub schema object count = %d, want 7 (3 tables + 3 indexes + sqlite_sequence)", count)
	}
}

func TestAgentHubThreadsSchemaDump(t *testing.T) {
	body, err := migrations.Files.ReadFile("0154_agent_hub_threads.sql")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(body, []byte{'\r'}) {
		t.Fatal("0154_agent_hub_threads.sql must be LF; CRLF changes the checksum and sqlite_schema text")
	}
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.ExecContext(context.Background(), string(body)); err != nil {
		t.Fatal(err)
	}
	rows, err := db.QueryContext(context.Background(), `SELECT type,name,coalesce(sql,'') FROM sqlite_schema WHERE name NOT LIKE 'sqlite_autoindex_%' ORDER BY type,name`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var lines []string
	count := 0
	foundThreads := false
	for rows.Next() {
		var typ, name, sqlText string
		if err = rows.Scan(&typ, &name, &sqlText); err != nil {
			t.Fatal(err)
		}
		count++
		lines = append(lines, fmt.Sprintf("%q: %q,", typ+":"+name, sqlText))
		if typ+":"+name == "table:agent_hub_threads" {
			foundThreads = true
		}
		if strings.Contains(sqlText, "REFERENCES sessions") {
			t.Fatalf("%s references sessions", typ+":"+name)
		}
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	sort.Strings(lines)
	t.Logf("agent hub threads schema objects (%d):\n%s", count, strings.Join(lines, "\n"))
	if !foundThreads {
		t.Fatal("missing table:agent_hub_threads")
	}
	if count != 8 {
		t.Fatalf("agent hub threads schema object count = %d, want 8 (5 tables + 2 indexes + sqlite_sequence)", count)
	}
	store, err := OpenTemplated(context.Background(), filepath.Join(t.TempDir(), "agent-hub-threads.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
}

func TestAgentHubMigrationOpens(t *testing.T) {
	store, err := OpenTemplated(context.Background(), filepath.Join(t.TempDir(), "agent-hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	_, err = store.db.Exec(`INSERT INTO agent_hub_tasks(id,agent,prompt,work_dir,status,created_at,idempotency_key)
        VALUES('01ARZ3NDEKTSV4RRFFQ69G5FAE','codex','hi',?, 'queued', datetime('now'), 'k1')`, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
}

func TestAgentHubWriteDoesNotTouchTokenLedger(t *testing.T) {
	store, err := OpenTemplated(context.Background(), filepath.Join(t.TempDir(), "agent-hub-ledger.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var before int
	if err = store.db.QueryRow(`SELECT COUNT(*) FROM token_ledger`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if err = store.InsertTask(agenthub.TaskRecord{
		ID: "01ARZ3NDEKTSV4RRFFQ69G5FAF", Agent: "codex", Prompt: "hi", WorkDir: t.TempDir(),
		Status: "success", CreatedAt: "2026-01-01T00:00:00Z", IdempotencyKey: "ledger-guard",
	}); err != nil {
		t.Fatal(err)
	}
	var after int
	if err = store.db.QueryRow(`SELECT COUNT(*) FROM token_ledger`).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatalf("token_ledger changed %d -> %d", before, after)
	}
}
