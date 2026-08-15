package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"sort"
	"testing"

	"github.com/lunitide/lunitide/migrations"
	_ "modernc.org/sqlite"
)

// TestM6S5CDumpSchema applies every embedded migration on a scratch database
// and prints the stored sqlite_schema DDL for the 0053 objects. Temporary
// helper used while registering migration 0053 in expectedSchemaSQL.
func TestM6S5CDumpSchema(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "dump.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, "PRAGMA foreign_keys=OFF"); err != nil {
		t.Fatal(err)
	}
	entries, err := migrations.Files.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".sql" {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for _, n := range names {
		b, _ := migrations.Files.ReadFile(n)
		if _, err := db.ExecContext(ctx, string(b)); err != nil {
			t.Fatalf("apply %s: %v", n, err)
		}
	}
	rows, err := db.QueryContext(ctx, `SELECT type,name,coalesce(sql,'') FROM sqlite_schema WHERE name IN (
		'm5_workspace_conversion','audit_events','idempotency_records','m6_credential_ref','m6_integration','m6_api_operation','m6_field_mapping','m6_health_sample','m6_call_log','m6_skill','m6_skill_version','m6_skill_dependency','m6_skill_install','m6_skill_trigger','m6_import_candidate','m6_complexity_decision','m6_child_manifest','m6_result_bundle','m6_synthesis_record','m6_region_policy','m6_cloudrunner','m6_worker_lease','m6_remote_receipt','m6_reconcile_decision','ix_m5_conversion_run','ix_m5_conversion_source','ix_audit_aggregate_created','ix_idempotency_expires','ix_m6_integration_state','ix_m6_health_sample_integration','ix_m6_call_log_integration','ix_m6_call_log_operation','ix_m6_skill_install_workspace','ix_m6_skill_trigger_session','ix_m6_import_candidate_state','ix_m6_complexity_decision_session','ix_m6_synthesis_record_root','ix_m6_cloudrunner_state','ix_m6_worker_lease_state','ix_m6_remote_receipt_pending','ix_m6_reconcile_decision_task','trg_m6_hs_ro_u','trg_m6_hs_ro_d','trg_m6_cl_ro_u','trg_m6_cl_ro_d') ORDER BY type,name`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var typ, name, sqlText string
		if err := rows.Scan(&typ, &name, &sqlText); err != nil {
			t.Fatal(err)
		}
		fmt.Printf("DUMP|%s|%s|%q\n", typ, name, sqlText)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
}
