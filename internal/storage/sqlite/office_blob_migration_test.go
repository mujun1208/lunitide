package sqlite

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	domain "github.com/lunitide/lunitide/internal/domain/officestudio"
	"github.com/lunitide/lunitide/migrations"
	"github.com/oklog/ulid/v2"
)

func TestOfficeBlobMigrationKeepsLegacyVersionsAndPreviewReferences(t *testing.T) {
	s, task := officeFixture(t)
	ctx := context.Background()
	v := officeVersion(task.ID, ulid.Make().String())
	v.ContentRef = strings.Repeat("a", 64)
	v.SHA256 = v.ContentRef
	v, err := s.PublishOfficeVersion(ctx, domain.PublishRequest{Version: v, IdempotencyKey: "legacy"})
	if err != nil {
		t.Fatal(err)
	}
	preview := strings.Repeat("b", 64)
	qa, err := s.AddOfficeValidation(ctx, domain.Validation{VersionID: v.ID, SHA256: v.SHA256, Validator: "legacy-renderer", Evidence: json.RawMessage(`{"pdfRef":"` + preview + `"}`)})
	if err != nil {
		t.Fatal(err)
	}
	// This fixture owns a temporary SQLite database. Remove only S11 objects
	// to reproduce the pre-0147 metadata layout, then run the real migration.
	if _, err = s.db.Exec(`DROP TABLE office_storage_sweep_items; DROP TABLE office_storage_sweeps; DROP TABLE office_blob_events; DROP TABLE office_blob_references; DROP TABLE office_blob_leases; DROP TABLE office_blobs; DROP TABLE office_storage_control;`); err != nil {
		t.Fatal(err)
	}
	sql, err := migrations.Files.ReadFile("0147_office_blob_storage.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec(string(sql)); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetOfficeVersion(ctx, v.ID)
	if err != nil || got.ContentRef != v.ContentRef || got.SHA256 != v.SHA256 || got.VersionNo != v.VersionNo {
		t.Fatalf("legacy version changed: %+v %v", got, err)
	}
	for _, entry := range []struct{ kind, id, ref string }{{"version", v.ID, v.ContentRef}, {"validation", qa.ID, preview}} {
		var ref string
		var managed int
		if err = s.db.QueryRow(`SELECT r.digest,b.managed FROM office_blob_references r JOIN office_blobs b ON b.digest=r.digest WHERE r.kind=? AND r.reference_id=?`, entry.kind, entry.id).Scan(&ref, &managed); err != nil || ref != entry.ref || managed != 0 {
			t.Fatalf("legacy reference unprotected: %+v %s %d %v", entry, ref, managed, err)
		}
	}
	if _, err = s.db.Exec(`UPDATE artifact_versions SET sha256=? WHERE id=?`, strings.Repeat("c", 64), v.ID); err == nil {
		t.Fatal("immutable history trigger removed")
	}
	rows, err := s.db.Query(`PRAGMA foreign_key_check`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if rows.Next() {
		t.Fatal("migration broke foreign key")
	}
}
