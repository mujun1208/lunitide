// M5 T-5.0.3 Scope Seal contract tests.
//
// 引用 M5 开工裁决 07：M5 期间保持 Scope Seal——禁止 Child Run、九阶段编排
// 与 fanout，seal 是 M5 发布门禁的前置项（DR-20260814-01），仅 M6 允许解除。
// seal 由 DB 层强制：migrations/0041_m5_scope_seal.sql 重建 agent_plan_runs，
// 使 parent_run_id 列带内联 CHECK (parent_run_id IS NULL)（M4 的 agent_run 表
// 从未有过 parent_run_id，0030 注释明确 M6 才引入，故 0041 封的是
// agent_plan_runs 这张唯一带该列的表）。
//
// 注意：M4 遗留的 plan.run.spawn/join/tree 方法名仍留在 bridge schema 元数据
// 与 app 层 handler 中（0041 的 DR 注记"handler/wire removal ships in app
// code"），seal 的执行点在 DB 层。因此本文件的 unseal 前置断言锁定的是：
// M5 run.* wire 命名空间无 spawn/join 注册、0042 幂等/审计枚举无 child
// spawn 操作、且活库拒收任何 spawn 记录。

package run_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	storage "github.com/lunitide/lunitide/internal/storage/sqlite"
	"github.com/lunitide/lunitide/migrations"
)

const (
	sealProjectID  = "01ARZ3NDEKTSV4RRFFQ69G5FA0"
	sealPlanID     = "01ARZ3NDEKTSV4RRFFQ69G5FA1"
	sealNodeID     = "01ARZ3NDEKTSV4RRFFQ69G5FA2"
	sealRootRunID  = "01ARZ3NDEKTSV4RRFFQ69G5FA3"
	sealChildRunID = "01ARZ3NDEKTSV4RRFFQ69G5FA4"
	sealTodoID     = "01ARZ3NDEKTSV4RRFFQ69G5FA5"
)

// scopeSealDB opens a scratch database through the production Open path (all
// embedded migrations applied), closes the store, and returns a raw *sql.DB
// for seal-level SQL: INSERT rejection checks and sqlite_master drift scans.
// The sqlite driver is registered transitively via the storage package import.
func scopeSealDB(t *testing.T) *sql.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "m5-scope-seal.db")
	store, err := storage.OpenTemplated(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// seedSealPlan inserts the minimal project→plan→node chain required by
// agent_plan_runs foreign keys (mirrors the sqlite orchestration test setup).
func seedSealPlan(t *testing.T, db *sql.DB) {
	t.Helper()
	steps := []struct {
		query string
		args  []any
	}{
		{`INSERT INTO projects(id,name,project_code,created_at,updated_at) VALUES(?,'seal-project','ITM00007','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`, []any{sealProjectID}},
		{`INSERT INTO message_project_usage(project_id,text_bytes) VALUES(?,0)`, []any{sealProjectID}},
		{`INSERT INTO plans(id,project_id,name,version,status,created_at,updated_at) VALUES(?,?,'seal-plan',1,'draft','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`, []any{sealPlanID, sealProjectID}},
		{`INSERT INTO plan_nodes(id,plan_id,name,status,risk_level,sequence,created_at,updated_at) VALUES(?,?,'seal-node','pending','low',1,'2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`, []any{sealNodeID, sealPlanID}},
	}
	for i, step := range steps {
		if _, err := db.Exec(step.query, step.args...); err != nil {
			t.Fatalf("seed step %d: %v", i, err)
		}
	}
}

const sealRunInsert = `INSERT INTO agent_plan_runs(id,parent_run_id,plan_id,node_id,role,todo_id,todo_title,todo_description,todo_metadata_json,status,depth,failure,created_at,updated_at,version) VALUES(?,?,?,?,?,?,?,'sealed root','{}','queued',0,'','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z',1)`

func assertCheckViolation(t *testing.T, err error) {
	t.Helper()
	msg := strings.ToLower(err.Error())
	if !strings.Contains(msg, "constraint") || !strings.Contains(msg, "check") {
		t.Fatalf("expected a CHECK constraint violation, got: %v", err)
	}
}

// TestScopeSealSealsChildCreation: the 0041 inline CHECK makes every write of
// a non-NULL parent_run_id structurally impossible, while a Root Run
// (parent NULL) inserts cleanly.
func TestScopeSealSealsChildCreation(t *testing.T) {
	db := scopeSealDB(t)
	seedSealPlan(t, db)

	if _, err := db.Exec(sealRunInsert, sealRootRunID, nil, sealPlanID, sealNodeID, "planner", sealTodoID, "root run"); err != nil {
		t.Fatalf("root run (parent NULL) must insert: %v", err)
	}
	var roots int
	if err := db.QueryRow(`SELECT count(*) FROM agent_plan_runs WHERE id=? AND parent_run_id IS NULL`, sealRootRunID).Scan(&roots); err != nil || roots != 1 {
		t.Fatalf("root run not persisted: count=%d err=%v", roots, err)
	}

	if _, err := db.Exec(sealRunInsert, sealChildRunID, sealRootRunID, sealPlanID, sealNodeID, "worker", sealTodoID, "child run"); err == nil {
		t.Fatal("child run INSERT accepted: scope seal CHECK did not fire")
	} else {
		assertCheckViolation(t, err)
	}

	if _, err := db.Exec(`UPDATE agent_plan_runs SET parent_run_id=? WHERE id=?`, sealRootRunID, sealRootRunID); err == nil {
		t.Fatal("parent backfill via UPDATE accepted: scope seal CHECK did not fire")
	} else {
		assertCheckViolation(t, err)
	}
}

// TestScopeSealUnsealReadiness: before M6 may lift the seal, nothing may have
// re-registered child orchestration: the 0041 CHECK text stays, the 0042
// idempotency/audit enums carry no spawn/join operations, the bridge schema's
// M5 run.* namespace has no spawn/join method, and a live database refuses to
// durable-record a child spawn even if a caller bypasses the app layer.
func TestScopeSealUnsealReadiness(t *testing.T) {
	m41, err := migrations.Files.ReadFile("0041_m5_scope_seal.sql")
	if err != nil {
		t.Fatal(err)
	}
	if norm := normalizeDDL(string(m41)); !strings.Contains(norm, "check (parent_run_id is null)") {
		t.Fatal("0041 lost the scope seal CHECK text")
	}

	m42, err := migrations.Files.ReadFile("0042_m5_runtime.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{"plan.run.spawn", "plan.run.join", "plan.run.tree", "agent.run.spawn", "run.spawn", "run.join"} {
		if strings.Contains(string(m42), token) {
			t.Fatalf("0042 durable wire registers child-orchestration token %q", token)
		}
	}
	if !strings.Contains(string(m42), "run.send") { // positive control: right file, live enum
		t.Fatal("0042 sanity failed: run.send missing from wire operations")
	}

	src, err := os.ReadFile(filepath.Join("..", "bridge", "schema_generated.go"))
	if err != nil {
		t.Fatal(err)
	}
	// The M5 run.* namespace is the sealed surface; plan.run.* metadata rows
	// are M4 legacy pending the app-side wire removal (0041 DR-20260814-01).
	for _, token := range []string{`"run.spawn"`, `"run.join"`} {
		if strings.Contains(string(src), token) {
			t.Fatalf("bridge schema registers %s in the M5 run namespace", token)
		}
	}
	if !strings.Contains(string(src), `"run.send"`) { // positive control
		t.Fatal("bridge schema sanity failed: run.send missing")
	}

	db := scopeSealDB(t)
	if _, err := db.Exec(`INSERT INTO idempotency_records(operation,idempotency_key,request_digest,response_json,created_at,expires_at) VALUES('plan.run.spawn','seal-key',?,'{}','2026-01-01T00:00:00Z','2026-01-01T00:00:01Z')`, strings.Repeat("0", 64)); err == nil {
		t.Fatal("idempotency layer accepted a plan.run.spawn record")
	} else {
		assertCheckViolation(t, err)
	}
	if _, err := db.Exec(`INSERT INTO audit_events(id,action,aggregate_id,actor,metadata_json,created_at) VALUES('seal-audit-1','agent.run.spawned','agg','tester','{}','2026-01-01T00:00:00Z')`); err == nil {
		t.Fatal("audit layer accepted an agent.run.spawned record")
	} else {
		assertCheckViolation(t, err)
	}
	if _, err := db.Exec(`INSERT INTO idempotency_records(operation,idempotency_key,request_digest,response_json,created_at,expires_at) VALUES('run.send','seal-key',?,'{}','2026-01-01T00:00:00Z','2026-01-01T00:00:01Z')`, strings.Repeat("0", 64)); err != nil {
		t.Fatalf("root-run operation run.send must still record: %v", err)
	}
}

// scanScopeSealDrift walks sqlite_master plus PRAGMA table_info to detect any
// schema shape that resurrects parent_run_id orchestration:
//   - agent_plan_runs may carry the column only together with the sealed
//     CHECK text (verified against its CREATE statement);
//   - any other user table carrying parent_run_id is drift;
//   - agent_plan_runs disappearing from the schema is drift.
func scanScopeSealDrift(db *sql.DB) error {
	rows, err := db.Query(`SELECT name, COALESCE(sql,'') FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'`)
	if err != nil {
		return err
	}
	defer rows.Close()
	type userTable struct{ name, ddl string }
	var tables []userTable
	for rows.Next() {
		var tb userTable
		if err := rows.Scan(&tb.name, &tb.ddl); err != nil {
			return err
		}
		tables = append(tables, tb)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	sealedSeen := false
	for _, tb := range tables {
		hasParent, err := tableHasColumn(db, tb.name, "parent_run_id")
		if err != nil {
			return err
		}
		switch {
		case tb.name == "agent_plan_runs":
			sealedSeen = true
			if hasParent && !strings.Contains(normalizeDDL(tb.ddl), "check (parent_run_id is null)") {
				return fmt.Errorf("scope seal drift: agent_plan_runs lost its parent_run_id IS NULL CHECK")
			}
		case hasParent:
			return fmt.Errorf("scope seal drift: table %s declares parent_run_id; only agent_plan_runs may carry it (sealed NULL)", tb.name)
		}
	}
	if !sealedSeen {
		return fmt.Errorf("scope seal drift: agent_plan_runs table missing from schema")
	}
	return nil
}

func tableHasColumn(db *sql.DB, table, column string) (bool, error) {
	rows, err := db.Query(fmt.Sprintf(`PRAGMA table_info('%s')`, strings.ReplaceAll(table, "'", "''")))
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, colType string
		var notNull, pk int
		var dflt any
		if err := rows.Scan(&cid, &name, &colType, &notNull, &dflt, &pk); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}

func normalizeDDL(ddl string) string {
	return strings.Join(strings.Fields(strings.ToLower(ddl)), " ")
}

// TestScopeSealDriftDetection proves the scanner catches deliberate sabotage:
// a clean migrated schema reports zero drift, a rogue orchestration table is
// flagged, and a rebuilt agent_plan_runs without the CHECK is flagged.
func TestScopeSealDriftDetection(t *testing.T) {
	t.Run("clean schema reports zero drift", func(t *testing.T) {
		db := scopeSealDB(t)
		if err := scanScopeSealDrift(db); err != nil {
			t.Fatalf("clean schema flagged: %v", err)
		}
	})

	t.Run("rogue orchestration table is caught", func(t *testing.T) {
		db := scopeSealDB(t)
		if _, err := db.Exec(`CREATE TABLE rogue_orchestration (id TEXT PRIMARY KEY, parent_run_id TEXT, payload TEXT)`); err != nil {
			t.Fatal(err)
		}
		err := scanScopeSealDrift(db)
		if err == nil || !strings.Contains(err.Error(), "rogue_orchestration") {
			t.Fatalf("rogue table not caught: %v", err)
		}
	})

	t.Run("tampered seal constraint is caught", func(t *testing.T) {
		db := scopeSealDB(t)
		// ALTER TABLE cannot drop constraints, so sabotage mirrors 0041's
		// rebuild dance: rename, recreate without the CHECK, drop the old one.
		if _, err := db.Exec(`ALTER TABLE agent_plan_runs RENAME TO agent_plan_runs_seal_old`); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`CREATE TABLE agent_plan_runs (
			id TEXT PRIMARY KEY,
			parent_run_id TEXT,
			plan_id TEXT NOT NULL,
			node_id TEXT NOT NULL,
			role TEXT NOT NULL,
			todo_id TEXT NOT NULL,
			todo_title TEXT NOT NULL,
			todo_description TEXT NOT NULL DEFAULT '',
			todo_metadata_json TEXT NOT NULL DEFAULT '{}',
			status TEXT NOT NULL,
			depth INTEGER NOT NULL,
			failure TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			terminal_at TEXT,
			version INTEGER NOT NULL
		)`); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`DROP TABLE agent_plan_runs_seal_old`); err != nil {
			t.Fatal(err)
		}
		err := scanScopeSealDrift(db)
		if err == nil || !strings.Contains(err.Error(), "lost") {
			t.Fatalf("tampered constraint not caught: %v", err)
		}
	})
}
