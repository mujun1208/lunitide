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

// TestM7DumpEvidenceSchema applies every embedded migration on a scratch
// database and prints the stored sqlite_schema DDL for the 0052 evidence
// objects. Temporary helper used while registering migration 0052 in
// expectedSchemaSQL.
func TestM7DumpEvidenceSchema(t *testing.T) {
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
		'reviews','trace_edges','stale_marks','stale_resolutions','gate_evaluations','checkpoints',
		'dev_tasks','test_runs','scan_runs','artifact_derivations','reproduction_manifests','evaluation_baselines',
		'ix_reviews_subject','idx_te_down','idx_te_up','ix_sm_subject','ix_sr_mark','ix_ge_run','ix_dt_run',
		'ix_tr_task','ix_scan_task','ix_ad_artifact','ix_rm_artifact','ix_eb_scope',
		'trg_evd_reviews_ro_u','trg_evd_reviews_ro_d','trg_evd_te_ro_u','trg_evd_te_ro_d',
		'trg_evd_sm_ro_u','trg_evd_sm_ro_d','trg_evd_sr_ro_u','trg_evd_sr_ro_d',
		'trg_evd_ge_ro_u','trg_evd_ge_ro_d','trg_evd_ck_ro_u','trg_evd_ck_ro_d',
		'trg_evd_tr_ro_u','trg_evd_tr_ro_d','trg_evd_sc_ro_u','trg_evd_sc_ro_d',
		'trg_evd_ad_ro_u','trg_evd_ad_ro_d','trg_evd_rm_ro_u','trg_evd_rm_ro_d',
		'trg_evd_eb_ro_u','trg_evd_eb_ro_d') ORDER BY type,name`)
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
