package sqlite

import (
	"context"
	"database/sql"
	"testing"
)

func TestAuditedWriteRejectsUnserializableMetadataAtomically(t *testing.T) {
	f := newMessageFixture(t, "audit-metadata")
	ctx := context.Background()
	var before string
	if err := f.store.db.QueryRow(`SELECT title FROM sessions WHERE id=?`, f.sessionID).Scan(&before); err != nil {
		t.Fatal(err)
	}
	err := f.store.execWithAudit(ctx, "queue.input", f.sessionID, "test", map[string]any{"invalid": make(chan int)}, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE sessions SET title='must rollback' WHERE id=?`, f.sessionID)
		return err
	})
	if err == nil {
		t.Fatal("invalid audit silently became empty metadata")
	}
	var after string
	if err := f.store.db.QueryRow(`SELECT title FROM sessions WHERE id=?`, f.sessionID).Scan(&after); err != nil || after != before {
		t.Fatalf("partial audited write: %q %v", after, err)
	}
}
