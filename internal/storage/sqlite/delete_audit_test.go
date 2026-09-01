package sqlite

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/lunitide/lunitide/internal/domain/project"
	"github.com/lunitide/lunitide/internal/domain/session"
	"github.com/lunitide/lunitide/internal/projectapp"
	"github.com/lunitide/lunitide/internal/sessionapp"
)

func countAuditEvents(t *testing.T, store *Store, action, aggregateID string) int {
	t.Helper()
	var n int
	if err := store.db.QueryRowContext(context.Background(),
		`SELECT count(*) FROM audit_events WHERE action=? AND aggregate_id=?`,
		action, aggregateID).Scan(&n); err != nil {
		t.Fatalf("count audit_events: %v", err)
	}
	return n
}

// TestDeleteSessionWritesAudit pins the T2 fix: a hard delete of a real session
// records a session.deleted audit row in the same transaction. Before 0111 the
// action enum forbade it, so the delete ran with no trail at all.
func TestDeleteSessionWritesAudit(t *testing.T) {
	ctx := context.Background()
	store, err := OpenTemplated(ctx, filepath.Join(t.TempDir(), "delaudit.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	p, err := projectapp.New(store, store).Create(ctx, "proj-key", "test", nil, project.Project{Name: "P"})
	if err != nil {
		t.Fatal(err)
	}
	s, err := sessionapp.New(store, store).Create(ctx, "sess-key", "test", nil, session.Session{ProjectID: p.ID, Title: "S"})
	if err != nil {
		t.Fatal(err)
	}

	if err := store.DeleteSession(ctx, s.ID); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	if got := countAuditEvents(t, store, "session.deleted", s.ID); got != 1 {
		t.Fatalf("session.deleted audit rows = %d, want 1", got)
	}

	// The row is append-only: it must survive and remain the single record.
	if got := countAuditEvents(t, store, "session.deleted", s.ID); got != 1 {
		t.Fatalf("audit row not durable: %d", got)
	}
}

// TestDeleteMissingSessionWritesNoAudit ensures the idempotent no-op path does
// not fabricate an audit row for a session that never existed.
func TestDeleteMissingSessionWritesNoAudit(t *testing.T) {
	ctx := context.Background()
	store, err := OpenTemplated(ctx, filepath.Join(t.TempDir(), "delaudit-missing.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	missing := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	if err := store.DeleteSession(ctx, missing); err != nil {
		t.Fatalf("delete missing: %v", err)
	}
	if got := countAuditEvents(t, store, "session.deleted", missing); got != 0 {
		t.Fatalf("no-op delete wrote %d audit rows, want 0", got)
	}
}
