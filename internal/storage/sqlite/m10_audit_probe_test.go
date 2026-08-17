package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

// Wave-2 audit actions (memory.purge / queue.input / skill.category_set ...)
// must exist in the audit_events CHECK enum, otherwise execWithAudit rolls
// the whole business write back at runtime.
func TestM10AuditActionsAccepted(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "m10-audit.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	actions := []string{
		"memory.settings.update", "memory.fact.flag", "memory.fact.unflag",
		"memory.growth.enroll", "memory.growth.decide", "memory.purge",
		"queue.input", "queue.withdraw", "queue.consume",
		"skill.category_set", "skill.category_seeded",
	}
	for _, action := range actions {
		id, err := store.newULID(time.Now().UTC())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.db.ExecContext(ctx,
			`INSERT INTO audit_events(id,action,aggregate_id,actor,metadata_json,created_at) VALUES(?,?,?,?,?,?)`,
			id, action, "aggregate-1", "renderer", "{}", formatTime(time.Now().UTC())); err != nil {
			t.Errorf("audit action %q rejected by CHECK constraint: %v", action, err)
		}
	}
}
