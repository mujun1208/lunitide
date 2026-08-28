// Meeting notes dump: apply 0094 then 0095 and print sqlite_schema for expectedSchemaSQL.
package sqlite

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/lunitide/lunitide/migrations"
)

func TestMeetingsSchemaDump(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	apply := func(name string) {
		t.Helper()
		body, err := migrations.Files.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if _, err := db.ExecContext(ctx, string(body)); err != nil {
			t.Fatalf("apply %s: %v", name, err)
		}
	}
	apply("0094_meetings.sql")
	if _, err := db.ExecContext(ctx, `INSERT INTO meetings(meeting_id,title,status,audio_source,started_at,created_at,updated_at)
		VALUES('01ARZ3NDEKTSV4RRFFQ69G5FAV','周会','transcribed','microphone','2026-08-27T03:00:00Z','2026-08-27T03:00:00Z','2026-08-27T03:00:00Z')`); err != nil {
		t.Fatalf("seed 0094: %v", err)
	}
	apply("0095_meetings_system_audio.sql")
	body0095, err := migrations.Files.ReadFile("0095_meetings_system_audio.sql")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(body0095, []byte{'\r'}) {
		t.Fatal("0095 must be LF; CRLF changes the checksum and sqlite_schema text")
	}
	var source string
	if err := db.QueryRowContext(ctx, `SELECT audio_source FROM meetings WHERE meeting_id='01ARZ3NDEKTSV4RRFFQ69G5FAV'`).Scan(&source); err != nil || source != "microphone" {
		t.Fatalf("row survived 0095: source=%q err=%v", source, err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO meetings(meeting_id,title,status,audio_source,started_at,created_at,updated_at)
		VALUES('01ARZ3NDEKTSV4RRFFQ69G5FAW','混录','recording','microphone_and_system','2026-08-27T04:00:00Z','2026-08-27T04:00:00Z','2026-08-27T04:00:00Z')`); err != nil {
		t.Fatalf("mix source after 0095: %v", err)
	}
	rows, err := db.QueryContext(ctx, `SELECT type,name,coalesce(sql,'') FROM sqlite_schema WHERE name NOT LIKE 'sqlite_autoindex_%' ORDER BY type,name`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var lines []string
	count := 0
	meetingsSQL := ""
	for rows.Next() {
		var typ, name, sqlText string
		if err := rows.Scan(&typ, &name, &sqlText); err != nil {
			t.Fatal(err)
		}
		count++
		lines = append(lines, fmt.Sprintf("%q: %q,", typ+":"+name, sqlText))
		if typ == "table" && name == "meetings" {
			meetingsSQL = sqlText
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	sort.Strings(lines)
	t.Logf("meetings schema objects (%d):\n%s", count, strings.Join(lines, "\n"))
	if count != 6 {
		t.Fatalf("meetings schema object count = %d, want 6", count)
	}
	if !strings.Contains(meetingsSQL, "microphone_and_system") {
		t.Fatalf("meetings ddl missing mix source: %s", meetingsSQL)
	}
}
