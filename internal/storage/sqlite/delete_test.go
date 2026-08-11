package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/domain/project"
	"github.com/lunitide/lunitide/internal/domain/session"
	"github.com/lunitide/lunitide/internal/projectapp"
	"github.com/lunitide/lunitide/internal/sessionapp"
	"github.com/oklog/ulid/v2"
)

// TestDeleteSessionRecordsTombstones verifies that DeleteSession records
// tombstones for the session, its compaction checkpoints, and handoff
// capsules before physically removing them. Tombstones enable fail-closed
// readability checks for derived objects (ADR-005 §6).
//
// This test also verifies that the FK delete order is correct: checkpoints
// and capsules (which reference messages and sessions via ON DELETE RESTRICT)
// must be deleted BEFORE messages and sessions.
func TestDeleteSessionRecordsTombstones(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "tombstone.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	// Create project + session via app services.
	p, err := projectapp.New(store, store).Create(ctx, "proj-key", "test", nil, project.Project{Name: "Tombstone Project"})
	if err != nil {
		t.Fatal(err)
	}
	svc := sessionapp.New(store, store)
	s, err := svc.Create(ctx, "sess-key", "test", nil, session.Session{ProjectID: p.ID, Title: "Session"})
	if err != nil {
		t.Fatal(err)
	}
	sessionID := s.ID

	// Insert a message (required as FK target for checkpoint).
	now := time.Now().UTC().Format(time.RFC3339Nano)
	msgID := ulid.Make().String()
	if _, err := store.db.ExecContext(ctx,
		`INSERT INTO messages(id,session_id,role,sequence,created_at) VALUES(?,?, 'user', 1, ?)`,
		msgID, sessionID, now); err != nil {
		t.Fatal(err)
	}

	// Insert a compaction checkpoint referencing the message.
	cpID := ulid.Make().String()
	if _, err := store.db.ExecContext(ctx,
		`INSERT INTO compaction_checkpoints(id,session_id,version,source_start_id,source_end_id,source_start_seq,source_end_seq,source_digest,summary_schema_version,trigger,status,provider,model,summary_json,human_summary,created_at,completed_at)
		 VALUES(?,?,1,?,?,1,1,?,'1.0','manual','succeeded','test','test-model','{}','test summary',?,?)`,
		cpID, sessionID, msgID, msgID, "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789", now, now); err != nil {
		t.Fatal(err)
	}

	// Insert a handoff capsule referencing the checkpoint.
	capID := ulid.Make().String()
	if _, err := store.db.ExecContext(ctx,
		`INSERT INTO handoff_capsules(id,source_session_id,checkpoint_id,active_tasks_json,recent_message_ids,digest,status,created_at)
		 VALUES(?,?,?,'[]','[]',?,'active',?)`,
		capID, sessionID, cpID, "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789", now); err != nil {
		t.Fatal(err)
	}

	// Delete the session. This must record tombstones AND respect FK order
	// (checkpoints and capsules deleted before messages, not after).
	if err := store.DeleteSession(ctx, sessionID); err != nil {
		t.Fatalf("DeleteSession failed: %v", err)
	}

	// Verify tombstones were recorded.
	for _, tc := range []struct{ ownerType, ownerID, label string }{
		{"session", sessionID, "session"},
		{"checkpoint", cpID, "checkpoint"},
		{"capsule", capID, "capsule"},
	} {
		has, err := store.HasTombstone(ctx, tc.ownerType, tc.ownerID)
		if err != nil {
			t.Fatalf("HasTombstone %s: %v", tc.label, err)
		}
		if !has {
			t.Errorf("tombstone not recorded for %s %s", tc.label, tc.ownerID)
		}
	}

	// Verify the session row is physically gone.
	var count int
	if err := store.db.QueryRowContext(ctx, `SELECT count(*) FROM sessions WHERE id=?`, sessionID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Errorf("session row still exists after delete")
	}
}

// TestDeleteSessionIdempotent verifies that deleting a non-existent session
// is a no-op (idempotent) and does not error.
func TestDeleteSessionIdempotent(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "idempotent.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	nonExistent := ulid.Make().String()

	// First delete: no-op, no error.
	if err := store.DeleteSession(ctx, nonExistent); err != nil {
		t.Fatalf("first delete of non-existent session: %v", err)
	}
	// Second delete: still no-op, no error.
	if err := store.DeleteSession(ctx, nonExistent); err != nil {
		t.Fatalf("second delete of non-existent session: %v", err)
	}
}

// TestDeleteProjectRecordsTombstones verifies that DeleteProject records
// tombstones for all sessions under the project.
func TestDeleteProjectRecordsTombstones(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "proj-tombstone.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	p, err := projectapp.New(store, store).Create(ctx, "proj-key-2", "test", nil, project.Project{Name: "Project To Delete"})
	if err != nil {
		t.Fatal(err)
	}
	svc := sessionapp.New(store, store)
	s, err := svc.Create(ctx, "sess-key-2", "test", nil, session.Session{ProjectID: p.ID, Title: "Session"})
	if err != nil {
		t.Fatal(err)
	}
	sessionID := s.ID

	if err := store.DeleteProject(ctx, p.ID); err != nil {
		t.Fatalf("DeleteProject failed: %v", err)
	}

	// Session tombstone should exist.
	has, err := store.HasTombstone(ctx, "session", sessionID)
	if err != nil {
		t.Fatalf("HasTombstone: %v", err)
	}
	if !has {
		t.Error("session tombstone not recorded after project delete")
	}

	// Project row should be gone.
	var count int
	if err := store.db.QueryRowContext(ctx, `SELECT count(*) FROM projects WHERE id=?`, p.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Error("project row still exists after delete")
	}
}

// TestRecordTombstoneIdempotent verifies that recording the same tombstone
// twice is a no-op (INSERT OR IGNORE).
func TestRecordTombstoneIdempotent(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "tombstone-idempotent.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ownerID := ulid.Make().String()
	if err := store.RecordTombstone(ctx, "session", ownerID); err != nil {
		t.Fatalf("first RecordTombstone: %v", err)
	}
	if err := store.RecordTombstone(ctx, "session", ownerID); err != nil {
		t.Fatalf("second RecordTombstone: %v", err)
	}
	has, err := store.HasTombstone(ctx, "session", ownerID)
	if err != nil {
		t.Fatal(err)
	}
	if !has {
		t.Error("tombstone not found after duplicate record")
	}
}
