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

func TestCallAttemptEfficiencySchemaDump(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, name := range []string{"0149_continuity_foundation.sql", "0150_call_attempt_efficiency.sql"} {
		body, readErr := migrations.Files.ReadFile(name)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if _, err := db.ExecContext(context.Background(), string(body)); err != nil {
			t.Fatal(err)
		}
	}
	var sqlText string
	if err := db.QueryRowContext(context.Background(), `SELECT sql FROM sqlite_schema WHERE name='model_call_attempts'`).Scan(&sqlText); err != nil {
		t.Fatal(err)
	}
	t.Logf("table:model_call_attempts = %q", sqlText)
}

func TestContinuityFoundationSchemaDump(t *testing.T) {
	body, err := migrations.Files.ReadFile("0149_continuity_foundation.sql")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(body, []byte{'\r'}) {
		t.Fatal("0149_continuity_foundation.sql must be LF; CRLF changes the checksum")
	}
	sum := sha256.Sum256(body)
	t.Logf("0149 sha256 %x", sum)
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.ExecContext(context.Background(), string(body)); err != nil {
		t.Fatal(err)
	}
	rows, err := db.QueryContext(context.Background(), `SELECT type,name,coalesce(sql,'') FROM sqlite_schema WHERE name IN ('tool_operations','ix_tool_operations_session','ix_tool_operations_external','model_call_attempts','ix_model_call_attempts_task') ORDER BY type,name`)
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
	t.Logf("schema objects:\n%s", strings.Join(lines, "\n"))
}

func TestProtocolMessageGroupsSchemaDump(t *testing.T) {
	body, err := migrations.Files.ReadFile("0151_protocol_message_groups.sql")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(body, []byte{'\r'}) {
		t.Fatal("0151_protocol_message_groups.sql must be LF; CRLF changes the checksum")
	}
	sum := sha256.Sum256(body)
	t.Logf("0151 sha256 %x", sum)
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.ExecContext(context.Background(), string(body)); err != nil {
		t.Fatal(err)
	}
	rows, err := db.QueryContext(context.Background(), `SELECT type,name,coalesce(sql,'') FROM sqlite_schema WHERE name IN ('protocol_message_groups','ix_protocol_message_groups_session','protocol_private') ORDER BY type,name`)
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
		lines = append(lines, fmt.Sprintf("%s:%s = %q", typ, name, sqlText))
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	t.Logf("schema objects:\n%s", strings.Join(lines, "\n"))
}
