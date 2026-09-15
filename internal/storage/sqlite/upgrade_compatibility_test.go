package sqlite

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/lunitide/lunitide/migrations"
	"github.com/oklog/ulid/v2"
)

func TestOfficeDeliveryV2MigrationLF(t *testing.T) {
	body, err := migrations.Files.ReadFile("0159_office_delivery_v2.sql")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(body, []byte{'\r'}) {
		t.Fatal("0159_office_delivery_v2.sql must be LF")
	}
}

func TestUpgradeMigrationBackupCompatibility(t *testing.T) {
	ctx := context.Background()
	store, task := officeFixture(t)
	published := officePublish(t, store, task, ulid.Make().String(), "upgrade-freeze")

	rows, err := store.db.QueryContext(ctx, `SELECT version FROM schema_migrations ORDER BY rowid`)
	if err != nil {
		t.Fatal(err)
	}
	var applied []string
	for rows.Next() {
		var version string
		if err = rows.Scan(&version); err != nil {
			t.Fatal(err)
		}
		applied = append(applied, version)
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	_ = rows.Close()

	if err = RefuseUnknownUpgradeSchema(applied); err != nil {
		t.Fatalf("current journal must be known: %v", err)
	}
	if err = reservedModelOfficeCollision(applied); err != nil {
		t.Fatal(err)
	}
	if !containsName(applied, "0155_project_root_tree.sql") || !containsName(applied, "0156_project_factory.sql") {
		t.Fatalf("factory migrations missing: %v", applied[len(applied)-4:])
	}
	for _, name := range reservedModelOfficeMigrations {
		if !containsName(applied, name) {
			t.Fatalf("reserved pack missing: %s", name)
		}
	}
	if containsName(applied, "0155_model_native") || containsName(applied, "0155_model_native_v2.sql") {
		t.Fatal("stale 0155_model_native must stay refused")
	}
	if containsName(applied, "0156_execution_contract") || containsName(applied, "0156_execution_contract_v2.sql") {
		t.Fatal("stale 0156_execution_contract must stay refused")
	}
	if containsName(applied, "0157_office_delivery_v2.sql") {
		t.Fatal("stale 0157_office_delivery_v2 must not exist")
	}
	if !containsName(applied, "0158_execution_contract_v2.sql") {
		t.Fatal("0158_execution_contract_v2.sql must be applied")
	}
	if !containsName(applied, "0159_office_delivery_v2.sql") {
		t.Fatal("0159_office_delivery_v2.sql must be applied")
	}
	if err = RefuseUnknownUpgradeSchema([]string{"0160_future.sql"}); !errors.Is(err, ErrUnknownSchema) {
		t.Fatalf("legacy writer must refuse unknown schema, got %v", err)
	}

	sessionID := task.SessionID
	var title string
	if err = store.db.QueryRowContext(ctx, `SELECT title FROM sessions WHERE id=?`, sessionID).Scan(&title); err != nil {
		t.Fatal(err)
	}
	var officeCount int
	if err = store.db.QueryRowContext(ctx, `SELECT count(*) FROM office_version_metadata`).Scan(&officeCount); err != nil {
		t.Fatal(err)
	}
	if officeCount < 1 {
		t.Fatalf("office_version_metadata rows = %d", officeCount)
	}
	var contentDigest string
	if err = store.db.QueryRowContext(ctx, `SELECT sha256 FROM artifact_versions WHERE id=?`, published.ID).Scan(&contentDigest); err != nil {
		t.Fatal(err)
	}
	if _, err = store.db.ExecContext(ctx, `INSERT INTO message_session_state(session_id,last_sequence,message_count,text_bytes) VALUES(?,0,0,0)`, sessionID); err != nil {
		t.Fatal(err)
	}
	if _, err = store.db.ExecContext(ctx, `INSERT INTO message_project_usage(project_id,text_bytes) VALUES(?,0)`, rtProjectULID); err != nil {
		t.Fatal(err)
	}

	dir := filepath.Dir(store.path)
	backup := filepath.Join(dir, "snap.db")
	if err = store.CreateBackup(ctx, backup); err != nil {
		t.Fatal(err)
	}
	if _, err = store.db.ExecContext(ctx, `UPDATE sessions SET title='mutated-after-backup' WHERE id=?`, sessionID); err != nil {
		t.Fatal(err)
	}
	if err = store.RestoreBackup(ctx, backup); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenTemplated(ctx, store.path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })

	var restoredTitle string
	var restoredOffices int
	var restoredDigest string
	if err = reopened.db.QueryRowContext(ctx, `SELECT title FROM sessions WHERE id=?`, sessionID).Scan(&restoredTitle); err != nil {
		t.Fatal(err)
	}
	if err = reopened.db.QueryRowContext(ctx, `SELECT count(*) FROM office_version_metadata`).Scan(&restoredOffices); err != nil {
		t.Fatal(err)
	}
	if err = reopened.db.QueryRowContext(ctx, `SELECT sha256 FROM artifact_versions WHERE id=?`, published.ID).Scan(&restoredDigest); err != nil {
		t.Fatal(err)
	}
	if restoredTitle != title {
		t.Fatalf("restored session title = %q, want %q", restoredTitle, title)
	}
	if restoredOffices != officeCount {
		t.Fatalf("restored office count = %d, want %d", restoredOffices, officeCount)
	}
	if restoredDigest != contentDigest {
		t.Fatalf("restored content digest = %q, want %q", restoredDigest, contentDigest)
	}
}

func containsName(names []string, want string) bool {
	for _, name := range names {
		if name == want {
			return true
		}
	}
	return false
}
