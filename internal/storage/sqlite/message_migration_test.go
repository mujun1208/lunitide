package sqlite

import (
	"strings"
	"testing"

	"github.com/lunitide/lunitide/migrations"
)

func TestMessageMigrationBackfillsExistingProjectsAndSessions(t *testing.T) {
	db := openRaw(t, t.TempDir()+"/v9.db")
	defer db.Close()
	for _, name := range []string{"0001_provider.sql", "0002_provider_production.sql", "0003_provider_app.sql", "0004_model_sync_claims.sql", "0005_electron_provider_metadata.sql", "0006_electron_credential_adoption.sql", "0007_project.sql", "0008_session.sql"} {
		body, err := migrations.Files.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = db.Exec(string(body)); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	const now = "2026-01-01T00:00:00Z"
	if _, err := db.Exec(`INSERT INTO projects VALUES
		('01ARZ3NDEKTSV4RRFFQ69G5FAV','One','active',?,?,1),
		('01ARZ3NDEKTSV4RRFFQ69G5FAW','Two','active',?,?,1);
		INSERT INTO sessions VALUES
		('01ARZ3NDEKTSV4RRFFQ69G5FAX','01ARZ3NDEKTSV4RRFFQ69G5FAV','First','active',?,?,1),
		('01ARZ3NDEKTSV4RRFFQ69G5FAY','01ARZ3NDEKTSV4RRFFQ69G5FAV','Second','active',?,?,1)`, now, now, now, now, now, now, now, now); err != nil {
		t.Fatal(err)
	}
	body, err := migrations.Files.ReadFile("0009_message.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(string(body)); err != nil {
		t.Fatal(err)
	}
	for table, want := range map[string]int{"message_project_usage": 2, "message_session_state": 2, "message_workspace_usage": 1} {
		var got int
		if err = db.QueryRow(`SELECT count(*) FROM ` + table).Scan(&got); err != nil || got != want {
			t.Fatalf("%s rows=%d want=%d err=%v", table, got, want, err)
		}
	}
	var bad int
	if err = db.QueryRow(`SELECT (SELECT count(*) FROM message_project_usage WHERE text_bytes<>0)+(SELECT count(*) FROM message_session_state WHERE last_sequence<>0 OR message_count<>0 OR text_bytes<>0)+(SELECT count(*) FROM message_workspace_usage WHERE singleton<>1 OR text_bytes<>0)`).Scan(&bad); err != nil || bad != 0 {
		t.Fatalf("incorrect backfill=%d err=%v", bad, err)
	}
}

func TestMessageUsageSchemaRejectsCountersAboveQuota(t *testing.T) {
	db := openRaw(t, t.TempDir()+"/quota-check.db")
	defer db.Close()
	for _, name := range []string{"0001_provider.sql", "0002_provider_production.sql", "0003_provider_app.sql", "0004_model_sync_claims.sql", "0005_electron_provider_metadata.sql", "0006_electron_credential_adoption.sql", "0007_project.sql", "0008_session.sql", "0009_message.sql"} {
		body, err := migrations.Files.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = db.Exec(string(body)); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	if _, err := db.Exec(`UPDATE message_workspace_usage SET text_bytes=268435457 WHERE singleton=1`); err == nil || !strings.Contains(err.Error(), "CHECK constraint failed") {
		t.Fatalf("expected workspace CHECK rejection, got %v", err)
	}
	const now = "2026-01-01T00:00:00Z"
	if _, err := db.Exec(`INSERT INTO projects VALUES('01ARZ3NDEKTSV4RRFFQ69G5FAV','One','active',?,?,1)`, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO message_project_usage(project_id,text_bytes) VALUES('01ARZ3NDEKTSV4RRFFQ69G5FAV',67108865)`); err == nil || !strings.Contains(err.Error(), "CHECK constraint failed") {
		t.Fatalf("expected project CHECK rejection, got %v", err)
	}
}
