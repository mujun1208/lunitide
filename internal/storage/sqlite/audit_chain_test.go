package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/lunitide/lunitide/internal/audit"
	"github.com/lunitide/lunitide/internal/domain/project"
	"github.com/lunitide/lunitide/internal/domain/session"
	"github.com/lunitide/lunitide/internal/projectapp"
	"github.com/lunitide/lunitide/internal/sessionapp"
)

// seedAuditChain drives real use cases through the different audit write paths
// (project/session creates via the UoW, a session delete via appendAuditTx) so
// the resulting audit_events chain spans every entry point that was unified in
// W3. It returns the store for further inspection.
func seedAuditChain(t *testing.T, name string) *Store {
	t.Helper()
	ctx := context.Background()
	store, err := OpenTemplated(ctx, filepath.Join(t.TempDir(), name))
	if err != nil {
		t.Fatal(err)
	}
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
	return store
}

func chainLen(t *testing.T, store *Store) int {
	t.Helper()
	var n int
	if err := store.db.QueryRowContext(context.Background(),
		`SELECT count(*) FROM audit_events WHERE seq IS NOT NULL`).Scan(&n); err != nil {
		t.Fatalf("count chained rows: %v", err)
	}
	return n
}

// TestAuditChainVerifiesAcrossWrites proves the chain is well-formed after
// audit rows land through every unified write path.
func TestAuditChainVerifiesAcrossWrites(t *testing.T) {
	ctx := context.Background()
	store := seedAuditChain(t, "auditchain.db")
	defer store.Close()

	if got := chainLen(t, store); got < 3 {
		t.Fatalf("expected at least 3 chained audit rows, got %d", got)
	}
	if err := store.VerifyAuditChain(ctx); err != nil {
		t.Fatalf("VerifyAuditChain on an intact chain: %v", err)
	}
}

// TestAuditChainDetectsFieldTampering drops the append-only triggers to
// simulate a raw-handle edit and confirms an in-place metadata change breaks
// the recomputed event hash.
func TestAuditChainDetectsFieldTampering(t *testing.T) {
	ctx := context.Background()
	store := seedAuditChain(t, "audittamper.db")
	defer store.Close()

	if err := store.VerifyAuditChain(ctx); err != nil {
		t.Fatalf("precondition intact chain: %v", err)
	}
	if _, err := store.db.ExecContext(ctx, `DROP TRIGGER trg_audit_append_only`); err != nil {
		t.Fatalf("drop update trigger: %v", err)
	}
	if _, err := store.db.ExecContext(ctx,
		`UPDATE audit_events SET metadata_json='{"tampered":true}' WHERE seq=1`); err != nil {
		t.Fatalf("tamper row: %v", err)
	}
	if err := store.VerifyAuditChain(ctx); !errors.Is(err, audit.ErrChainBroken) {
		t.Fatalf("VerifyAuditChain after tamper = %v, want ErrChainBroken", err)
	}
}

// TestAuditChainDetectsDeletion confirms removing a row (seq gap) is caught.
func TestAuditChainDetectsDeletion(t *testing.T) {
	ctx := context.Background()
	store := seedAuditChain(t, "auditdelete.db")
	defer store.Close()

	if _, err := store.db.ExecContext(ctx, `DROP TRIGGER trg_audit_nodelete`); err != nil {
		t.Fatalf("drop delete trigger: %v", err)
	}
	if _, err := store.db.ExecContext(ctx, `DELETE FROM audit_events WHERE seq=1`); err != nil {
		t.Fatalf("delete row: %v", err)
	}
	if err := store.VerifyAuditChain(ctx); !errors.Is(err, audit.ErrChainBroken) {
		t.Fatalf("VerifyAuditChain after deletion = %v, want ErrChainBroken", err)
	}
}
