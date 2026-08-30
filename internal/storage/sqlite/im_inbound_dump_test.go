// IM inbound dump: apply 0098 then 0102 and print sqlite_schema for expectedSchemaSQL.
package sqlite

import (
	"bytes"
	"context"
	"database/sql"
	"strings"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/lunitide/lunitide/migrations"
)

func TestIMInboundSchemaDump(t *testing.T) {
	body, err := migrations.Files.ReadFile("0102_im_inbound.sql")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(body, []byte{'\r'}) {
		t.Fatal("0102 must be LF; CRLF changes the checksum and sqlite_schema text")
	}
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	apply := func(name string) {
		t.Helper()
		sqlBody, err := migrations.Files.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if _, err := db.ExecContext(ctx, string(sqlBody)); err != nil {
			t.Fatalf("apply %s: %v", name, err)
		}
	}
	apply("0098_im_channels.sql")
	if _, err := db.ExecContext(ctx, `INSERT INTO im_channels(kind, enabled, webhook_url, updated_at)
		VALUES('feishu', 1, 'https://open.feishu.cn/open-apis/bot/v2/hook/aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee', '2026-08-30T00:00:00Z')`); err != nil {
		t.Fatalf("seed 0098: %v", err)
	}
	apply("0102_im_inbound.sql")
	var enabled int
	var allow string
	if err := db.QueryRowContext(ctx, `SELECT enabled, inbound_allowlist FROM im_channels WHERE kind='feishu'`).Scan(&enabled, &allow); err != nil || enabled != 1 || allow != "" {
		t.Fatalf("row survived 0102: enabled=%d allow=%q err=%v", enabled, allow, err)
	}
	var sqlText string
	if err := db.QueryRowContext(ctx, `SELECT sql FROM sqlite_schema WHERE type='table' AND name='im_channels'`).Scan(&sqlText); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sqlText, "inbound_enabled") || !strings.Contains(sqlText, "inbound_app_secret") {
		t.Fatalf("im_channels ddl missing inbound columns: %s", sqlText)
	}
	t.Logf("table:im_channels %q", sqlText)
}
