// Meeting notes dump: apply 0094 and print sqlite_schema for expectedSchemaSQL.
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

func TestMeetingsSchemaDump(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	body, err := migrations.Files.ReadFile("0094_meetings.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(context.Background(), string(body)); err != nil {
		t.Fatalf("apply 0094: %v", err)
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
	t.Logf("meetings schema objects (%d):\n%s", count, strings.Join(lines, "\n"))
	if count != 6 {
		t.Fatalf("meetings schema object count = %d, want 6", count)
	}
}
