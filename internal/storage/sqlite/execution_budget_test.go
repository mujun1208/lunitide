package sqlite

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"

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
}
