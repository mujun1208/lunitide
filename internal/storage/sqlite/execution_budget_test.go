package sqlite

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/agentrunapp"
	"github.com/lunitide/lunitide/migrations"
)

func TestExecutionContractV2MigrationLF(t *testing.T) {
	body, err := migrations.Files.ReadFile("0158_execution_contract_v2.sql")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(body, []byte{'\r'}) {
		t.Fatal("0158_execution_contract_v2.sql must be LF")
	}
}

func TestExecutionBudgetConcurrentSchemaPresent(t *testing.T) {
	ctx := context.Background()
	store, err := OpenTemplated(ctx, filepath.Join(t.TempDir(), "execution-contract.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	for _, table := range []string{"execution_task_contracts", "execution_run_bindings", "execution_step_outcomes"} {
		var name string
		if err = store.db.QueryRow(`SELECT name FROM sqlite_schema WHERE type='table' AND name=?`, table).Scan(&name); err != nil {
			t.Fatalf("0158 must create %s: %v", table, err)
		}
	}
	var statusSQL string
	if err = store.db.QueryRow(`SELECT sql FROM sqlite_schema WHERE name='run_usage_reservation'`).Scan(&statusSQL); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains([]byte(statusSQL), []byte("isolated")) {
		t.Fatalf("run_usage_reservation CHECK must allow isolated: %s", statusSQL)
	}
	if !bytes.Contains([]byte(statusSQL), []byte("overrun")) || !bytes.Contains([]byte(statusSQL), []byte("receipt_conflict")) {
		t.Fatalf("run_usage_reservation CHECK must allow settlement overrun: %s", statusSQL)
	}
}

func TestEnsureAccountingSessionKeepsProjectUsageInvariant(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "accounting.db")
	store, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.AgentRuntimeRepository().TransactExecutionBudget(ctx, func(tx agentrunapp.ExecutionBudgetTx) error {
		_, err := tx.EnsureAccountingSession()
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(ctx, path)
	if err != nil {
		t.Fatalf("reopen after accounting session: %v", err)
	}
	t.Cleanup(func() { closeTestStore(store) })
	var projects, usage int
	if err = store.db.QueryRow(`SELECT count(*) FROM projects`).Scan(&projects); err != nil {
		t.Fatal(err)
	}
	if err = store.db.QueryRow(`SELECT count(*) FROM message_project_usage`).Scan(&usage); err != nil {
		t.Fatal(err)
	}
	if projects != usage {
		t.Fatalf("projects=%d usage=%d", projects, usage)
	}
}

func TestOpenBackfillsMissingAccountingProjectUsage(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "leftover.db")
	store, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err = store.db.Exec(`INSERT INTO projects(id,name,project_code,created_at,updated_at) VALUES(?,?,?,?,?)`,
		"01ARZ3NDEKTSV4RRFFQ69G5FAV", "execution-accounting", "ITM89001", now, now); err != nil {
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(ctx, path)
	if err != nil {
		t.Fatalf("reopen leftover accounting project: %v", err)
	}
	t.Cleanup(func() { closeTestStore(store) })
	var bytes int
	if err = store.db.QueryRow(`SELECT text_bytes FROM message_project_usage WHERE project_id=?`, "01ARZ3NDEKTSV4RRFFQ69G5FAV").Scan(&bytes); err != nil {
		t.Fatal(err)
	}
	if bytes != 0 {
		t.Fatalf("backfill text_bytes=%d", bytes)
	}
}
