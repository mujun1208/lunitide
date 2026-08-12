package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/domain/message"
	"github.com/lunitide/lunitide/internal/domain/project"
	"github.com/lunitide/lunitide/internal/domain/session"
	"github.com/lunitide/lunitide/internal/messageapp"
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
	if _, err := store.db.ExecContext(ctx,
		`INSERT INTO compaction_activations(session_id,checkpoint_id,revision,updated_at) VALUES(?,?,1,?)`,
		sessionID, cpID, now); err != nil {
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

func TestDeleteSessionDecrementsMessageUsageCounters(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "delete-message-usage.db")
	store, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}

	p, err := projectapp.New(store, store).Create(ctx, "usage-project", "test", nil, project.Project{Name: "Usage Project"})
	if err != nil {
		t.Fatal(err)
	}
	sessions := sessionapp.New(store, store)
	deleted, err := sessions.Create(ctx, "usage-deleted", "test", nil, session.Session{ProjectID: p.ID, Title: "Deleted"})
	if err != nil {
		t.Fatal(err)
	}
	retained, err := sessions.Create(ctx, "usage-retained", "test", nil, session.Session{ProjectID: p.ID, Title: "Retained"})
	if err != nil {
		t.Fatal(err)
	}
	messages, err := messageapp.New(store, store, []byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = messages.Append(ctx, "usage-message-deleted", "test", nil, message.Message{SessionID: deleted.ID, Text: "deleted text"}); err != nil {
		t.Fatal(err)
	}
	if _, err = messages.Append(ctx, "usage-message-retained", "test", nil, message.Message{SessionID: retained.ID, Text: "retained"}); err != nil {
		t.Fatal(err)
	}

	if err = store.DeleteSession(ctx, deleted.ID); err != nil {
		t.Fatal(err)
	}
	for table, query := range map[string]string{
		"project":   `SELECT text_bytes FROM message_project_usage WHERE project_id=?`,
		"workspace": `SELECT text_bytes FROM message_workspace_usage WHERE singleton=1`,
	} {
		var got int64
		args := []any{}
		if table == "project" {
			args = append(args, p.ID)
		}
		if err = store.db.QueryRowContext(ctx, query, args...).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != int64(len("retained")) {
			t.Fatalf("%s usage=%d want=%d", table, got, len("retained"))
		}
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("reopen after session delete: %v", err)
	}
	reopened.Close()
}

func TestDeleteProjectDecrementsWorkspaceMessageUsage(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "delete-project-message-usage.db")
	store, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}

	projects := projectapp.New(store, store)
	deletedProject, err := projects.Create(ctx, "usage-project-deleted", "test", nil, project.Project{Name: "Deleted Project"})
	if err != nil {
		t.Fatal(err)
	}
	retainedProject, err := projects.Create(ctx, "usage-project-retained", "test", nil, project.Project{Name: "Retained Project"})
	if err != nil {
		t.Fatal(err)
	}
	sessions := sessionapp.New(store, store)
	deletedSession, err := sessions.Create(ctx, "usage-session-deleted-project", "test", nil, session.Session{ProjectID: deletedProject.ID, Title: "Deleted"})
	if err != nil {
		t.Fatal(err)
	}
	retainedSession, err := sessions.Create(ctx, "usage-session-retained-project", "test", nil, session.Session{ProjectID: retainedProject.ID, Title: "Retained"})
	if err != nil {
		t.Fatal(err)
	}
	messages, err := messageapp.New(store, store, []byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = messages.Append(ctx, "usage-message-deleted-project", "test", nil, message.Message{SessionID: deletedSession.ID, Text: "deleted project text"}); err != nil {
		t.Fatal(err)
	}
	if _, err = messages.Append(ctx, "usage-message-retained-project", "test", nil, message.Message{SessionID: retainedSession.ID, Text: "retained project text"}); err != nil {
		t.Fatal(err)
	}

	if err = store.DeleteProject(ctx, deletedProject.ID); err != nil {
		t.Fatalf("DeleteProject failed: %v", err)
	}
	var got int64
	if err = store.db.QueryRowContext(ctx, `SELECT text_bytes FROM message_workspace_usage WHERE singleton=1`).Scan(&got); err != nil {
		t.Fatal(err)
	}
	want := int64(len("retained project text"))
	if got != want {
		t.Fatalf("workspace usage=%d want=%d", got, want)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("reopen after project delete: %v", err)
	}
	defer reopened.Close()
	if err = reopened.db.QueryRowContext(ctx, `SELECT text_bytes FROM message_workspace_usage WHERE singleton=1`).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("workspace usage after reopen=%d want=%d", got, want)
	}
}

func TestDeleteProjectWorkspaceUsageInvariantFailsClosed(t *testing.T) {
	for _, tc := range []struct {
		name    string
		corrupt string
	}{
		{name: "underflow", corrupt: `UPDATE message_workspace_usage SET text_bytes=0 WHERE singleton=1`},
		{name: "missing singleton", corrupt: `DELETE FROM message_workspace_usage WHERE singleton=1`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			store, err := Open(ctx, filepath.Join(t.TempDir(), "delete-project-invariant.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			p, err := projectapp.New(store, store).Create(ctx, "invariant-project", "test", nil, project.Project{Name: "Invariant Project"})
			if err != nil {
				t.Fatal(err)
			}
			s, err := sessionapp.New(store, store).Create(ctx, "invariant-session", "test", nil, session.Session{ProjectID: p.ID, Title: "Session"})
			if err != nil {
				t.Fatal(err)
			}
			messages, err := messageapp.New(store, store, []byte("0123456789abcdef0123456789abcdef"))
			if err != nil {
				t.Fatal(err)
			}
			if _, err = messages.Append(ctx, "invariant-message", "test", nil, message.Message{SessionID: s.ID, Text: "accounted text"}); err != nil {
				t.Fatal(err)
			}
			if _, err = store.db.ExecContext(ctx, tc.corrupt); err != nil {
				t.Fatal(err)
			}

			if err = store.DeleteProject(ctx, p.ID); err == nil {
				t.Fatal("DeleteProject succeeded with invalid workspace usage")
			}
			var count int
			if err = store.db.QueryRowContext(ctx, `SELECT count(*) FROM projects WHERE id=?`, p.ID).Scan(&count); err != nil {
				t.Fatal(err)
			}
			if count != 1 {
				t.Fatalf("project count=%d want=1 after failed delete", count)
			}
			if err = store.db.QueryRowContext(ctx, `SELECT count(*) FROM messages WHERE session_id=?`, s.ID).Scan(&count); err != nil {
				t.Fatal(err)
			}
			if count != 1 {
				t.Fatalf("message count=%d want=1 after failed delete", count)
			}
		})
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
	now := time.Now().UTC().Format(time.RFC3339Nano)
	msgID := ulid.Make().String()
	if _, err := store.db.ExecContext(ctx, `INSERT INTO messages(id,session_id,role,sequence,created_at) VALUES(?,?,'user',1,?)`, msgID, sessionID, now); err != nil {
		t.Fatal(err)
	}
	cpID := ulid.Make().String()
	if _, err := store.db.ExecContext(ctx,
		`INSERT INTO compaction_checkpoints(id,session_id,version,source_start_id,source_end_id,source_start_seq,source_end_seq,source_digest,trigger,status,created_at,completed_at)
		 VALUES(?,?,1,?,?,1,1,?,'manual','succeeded',?,?)`,
		cpID, sessionID, msgID, msgID, "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789", now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, `INSERT INTO compaction_activations(session_id,checkpoint_id,revision,updated_at) VALUES(?,?,1,?)`, sessionID, cpID, now); err != nil {
		t.Fatal(err)
	}

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
