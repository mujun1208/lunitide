package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/domain/queueinput"
	"github.com/lunitide/lunitide/internal/queueapp"
)

func TestQueuedInputConcurrentAdmissionAndConsumption(t *testing.T) {
	f := newMessageFixture(t, "queue-atomic")
	svc := queueapp.New(f.store)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	ids := make(chan string, 20)
	for range 20 {
		wg.Go(func() {
			m, err := svc.Enqueue(ctx, f.sessionID, "", "original", "", "same-key")
			if err != nil {
				t.Error(err)
				return
			}
			ids <- m.ID
		})
	}
	wg.Wait()
	close(ids)
	first := ""
	for id := range ids {
		if first != "" && first != id {
			t.Fatal("replayed request duplicated")
		}
		first = id
	}
	var audits int
	if err := f.store.db.QueryRow(`SELECT COUNT(*) FROM audit_events WHERE action='queue.input'`).Scan(&audits); err != nil || audits != 1 {
		t.Fatalf("duplicate audit: %d %v", audits, err)
	}
	if _, err := svc.Enqueue(ctx, f.sessionID, "", "changed", "", "same-key"); !errors.Is(err, queueapp.ErrRequestReused) {
		t.Fatalf("changed payload=%v", err)
	}
	accepted := make(chan string, 20)
	for i := range 20 {
		wg.Go(func() {
			m, err := svc.Enqueue(ctx, f.sessionID, "", fmt.Sprintf("supplement %d", i), "", fmt.Sprintf("key-%d", i))
			if err == nil {
				accepted <- m.ID
			} else if !errors.Is(err, queueapp.ErrQueueFull) {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	close(accepted)
	if len(accepted) != 4 {
		t.Fatalf("capacity bypass: %d new rows", len(accepted))
	}
	consumed := make(chan []queueinput.Message, 20)
	for range 20 {
		wg.Go(func() {
			rows, err := svc.Consume(ctx, f.sessionID)
			if err != nil {
				t.Error(err)
				return
			}
			consumed <- rows
		})
	}
	wg.Wait()
	close(consumed)
	seen := map[string]bool{}
	for rows := range consumed {
		for _, row := range rows {
			if seen[row.ID] {
				t.Fatal("same supplement consumed twice")
			}
			seen[row.ID] = true
		}
	}
	if len(seen) != 5 {
		t.Fatalf("lost supplements: %d", len(seen))
	}
	if _, err := svc.Enqueue(ctx, f.sessionID, "", "original", "", "same-key"); !errors.Is(err, queueapp.ErrRequestReused) {
		t.Fatalf("settled key reusable: %v", err)
	}
	for i := range 5 {
		if _, err := svc.Enqueue(ctx, f.sessionID, "", "next batch", "", fmt.Sprintf("next-%d", i)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := svc.Consume(ctx, f.sessionID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Enqueue(ctx, f.sessionID, "", "eleventh", "", "rate-limit"); !errors.Is(err, queueapp.ErrRateLimited) {
		t.Fatalf("rate limit bypass: %v", err)
	}
}

func TestQueuedInputAuditFailurePreservesPendingRows(t *testing.T) {
	f := newMessageFixture(t, "queue-audit")
	svc := queueapp.New(f.store)
	ctx := context.Background()
	if _, err := svc.Enqueue(ctx, f.sessionID, "", "keep me", "", "pending"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.db.Exec(`CREATE TRIGGER fail_queue_audit BEFORE INSERT ON audit_events WHEN NEW.action='queue.consume' BEGIN SELECT RAISE(ABORT,'injected failure'); END`); err != nil {
		t.Fatal(err)
	}
	if rows, err := svc.Consume(ctx, f.sessionID); err == nil || len(rows) != 0 {
		t.Fatalf("failed consume exposed items: %v %v", rows, err)
	}
	rows, err := svc.List(ctx, f.sessionID)
	if err != nil || len(rows) != 1 {
		t.Fatalf("pending input lost: %v %v", rows, err)
	}
}

func TestAuditedLegacyWriteUnwindsPanic(t *testing.T) {
	f := newMessageFixture(t, "audit-panic")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	func() {
		defer func() {
			if recover() == nil {
				t.Error("expected callback panic")
			}
		}()
		_ = f.store.execWithAudit(ctx, "queue.input", f.sessionID, "test", nil, func(tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, `UPDATE sessions SET title='uncommitted' WHERE id=?`, f.sessionID); err != nil {
				t.Fatal(err)
			}
			panic("fixture")
		})
	}()
	var title string
	if err := f.store.db.QueryRowContext(ctx, `SELECT title FROM sessions WHERE id=?`, f.sessionID).Scan(&title); err != nil || title == "uncommitted" {
		t.Fatalf("panic leaked writer/changes: %q %v", title, err)
	}
	if _, err := queueapp.New(f.store).Enqueue(ctx, f.sessionID, "", "still writable", "", "after-panic"); err != nil {
		t.Fatal(err)
	}
}
