package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/datasourceapp"
)

func writeFixture(t *testing.T) (*Store, *datasourceapp.Service, string, *atomic.Int32) {
	t.Helper()
	store, err := OpenTemplated(context.Background(), filepath.Join(t.TempDir(), "write.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	svc := datasourceapp.New(store)
	secrets := datasourceapp.NewMemorySecrets()
	svc.SetSecrets(secrets.Put, secrets.Get)
	row, err := svc.Create(context.Background(), datasourceapp.CreateInput{Name: "fixture", Kind: "postgres", DSN: "postgres://fixture:private@127.0.0.1/test?sslmode=disable"})
	if err != nil {
		t.Fatal(err)
	}
	if err = store.SetConnectionVerified(context.Background(), row.ID, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	var count atomic.Int32
	svc.SetWriteQuerier(func(context.Context, string, string, string, []any, int) ([]string, [][]any, bool, error) {
		count.Add(1)
		return []string{"rows_affected"}, [][]any{{1}}, false, nil
	})
	return store, svc, row.ID, &count
}

func TestDatasourceWriteConcurrentConfirmationAndLostACK(t *testing.T) {
	store, svc, id, count := writeFixture(t)
	ctx := context.Background()
	op, err := svc.PrepareWrite(ctx, id, "UPDATE stock SET qty=2 WHERE id=1", "write-request")
	if err != nil {
		t.Fatal(err)
	}
	same, err := svc.PrepareWrite(ctx, id, op.SQL, "write-request")
	if err != nil || same.ID != op.ID {
		t.Fatalf("prepare replay: %+v %v", same, err)
	}
	if _, err = svc.PrepareWrite(ctx, id, "DELETE FROM stock", "write-request"); !errors.Is(err, datasourceapp.ErrWriteConflict) {
		t.Fatalf("different intent: %v", err)
	}
	if _, err = svc.CommitWrite(ctx, op.ID, strings.Repeat("b", 64)); !errors.Is(err, datasourceapp.ErrWriteConflict) {
		t.Fatalf("changed confirmation: %v", err)
	}
	var wg sync.WaitGroup
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := svc.CommitWrite(ctx, op.ID, op.Digest)
			if err != nil || got.State != "completed" {
				t.Errorf("commit replay: %+v %v", got, err)
			}
		}()
	}
	wg.Wait()
	if count.Load() != 1 {
		t.Fatalf("executed %d times", count.Load())
	}
	var audits int
	if err = store.db.QueryRow(`SELECT count(*) FROM audit_events WHERE aggregate_id=?`, op.ID).Scan(&audits); err != nil || audits != 3 {
		t.Fatalf("prepared/intent/receipt audit: %d %v", audits, err)
	}
	// A fresh service reconstructs the persisted result; no in-memory receipt.
	restarted := datasourceapp.New(store)
	got, err := restarted.CommitWrite(ctx, op.ID, op.Digest)
	if err != nil || got.Result == nil || count.Load() != 1 {
		t.Fatalf("receipt after restart: %+v %v", got, err)
	}
}

func TestDatasourceWriteFailureAndRestartNeverReplayUnknownEffect(t *testing.T) {
	store, svc, id, count := writeFixture(t)
	ctx := context.Background()
	op, err := svc.PrepareWrite(ctx, id, "DELETE FROM stock WHERE id=1", "unknown")
	if err != nil {
		t.Fatal(err)
	}
	svc.SetWriteQuerier(func(context.Context, string, string, string, []any, int) ([]string, [][]any, bool, error) {
		count.Add(1)
		return nil, nil, false, errors.New("connection lost after commit")
	})
	if _, err = svc.CommitWrite(ctx, op.ID, op.Digest); !errors.Is(err, datasourceapp.ErrWriteUnknown) {
		t.Fatal(err)
	}
	if _, err = svc.CommitWrite(ctx, op.ID, op.Digest); !errors.Is(err, datasourceapp.ErrWriteUnknown) || count.Load() != 1 {
		t.Fatalf("unknown replayed: %v %d", err, count.Load())
	}
	next, err := svc.PrepareWrite(ctx, id, "INSERT INTO stock(id) VALUES(4)", "crash")
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := store.TransitionDatasourceWrite(ctx, next.ID, "prepared", "executing", nil); err != nil || !ok {
		t.Fatalf("intent: %v %v", ok, err)
	}
	if err = store.RecoverDatasourceWrites(ctx); err != nil {
		t.Fatal(err)
	}
	got, err := svc.GetWrite(ctx, next.ID)
	if err != nil || got.State != "unknown" {
		t.Fatalf("recovery: %+v %v", got, err)
	}
	if _, err = svc.CommitWrite(ctx, next.ID, next.Digest); !errors.Is(err, datasourceapp.ErrWriteUnknown) || count.Load() != 1 {
		t.Fatalf("crashed intent replayed: %v", err)
	}
}

func TestDatasourceWriteAuditsBeforeEffectAndSurvivesCallerCancel(t *testing.T) {
	store, svc, id, count := writeFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	op, err := svc.PrepareWrite(ctx, id, "DELETE FROM stock WHERE id=1", "cancelled-ack")
	if err != nil {
		t.Fatal(err)
	}
	svc.SetWriteQuerier(func(context.Context, string, string, string, []any, int) ([]string, [][]any, bool, error) {
		saved, err := store.GetDatasourceWrite(context.Background(), op.ID)
		if err != nil || saved.State != "executing" {
			t.Fatalf("effect before durable intent: %+v %v", saved, err)
		}
		count.Add(1)
		cancel()
		return []string{"rows_affected"}, [][]any{{1}}, false, nil
	})
	got, err := svc.CommitWrite(ctx, op.ID, op.Digest)
	if err != nil || got.State != "completed" || count.Load() != 1 {
		t.Fatalf("receipt lost on caller cancellation: %+v %v", got, err)
	}
}

func TestDatasourceWriteDeniesAfterDisableAndFailedIntent(t *testing.T) {
	store, svc, id, count := writeFixture(t)
	ctx := context.Background()
	op, err := svc.PrepareWrite(ctx, id, "UPDATE stock SET qty=0", "disable")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.db.Exec(`CREATE TRIGGER fixture_deny_intent BEFORE UPDATE ON datasource_write_operations BEGIN SELECT RAISE(ABORT,'disk write fault'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.CommitWrite(ctx, op.ID, op.Digest); err == nil || count.Load() != 0 {
		t.Fatalf("effect without durable intent: %v", err)
	}
	if _, err = store.db.Exec(`DROP TRIGGER fixture_deny_intent`); err != nil {
		t.Fatal(err)
	}
	if err = svc.Disable(ctx, id); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.CommitWrite(ctx, op.ID, op.Digest); !errors.Is(err, datasourceapp.ErrDisabled) || count.Load() != 0 {
		t.Fatalf("disabled write: %v", err)
	}
}

func TestDatasourceWriteRefusesRemoteAndRotatedCredentials(t *testing.T) {
	store, svc, id, count := writeFixture(t)
	ctx := context.Background()
	op, err := svc.PrepareWrite(ctx, id, "DELETE FROM stock WHERE id=1", "rotate")
	if err != nil {
		t.Fatal(err)
	}
	svc.SetSecrets(nil, func(string) (string, error) { return "postgres://fixture:changed@127.0.0.1/test?sslmode=disable", nil })
	if _, err = svc.CommitWrite(ctx, op.ID, op.Digest); !errors.Is(err, datasourceapp.ErrWriteConflict) {
		t.Fatalf("rotated confirmation: %v", err)
	}
	svc.SetSecrets(nil, func(string) (string, error) {
		return "postgres://fixture:private@127.0.0.1/test?host=192.0.2.10&sslmode=disable", nil
	})
	if _, err = svc.PrepareWrite(ctx, id, op.SQL, "remote"); !errors.Is(err, datasourceapp.ErrStatementDenied) {
		t.Fatalf("effective remote write: %v", err)
	}
	if count.Load() != 0 {
		t.Fatal("write executed")
	}
	items, err := store.ListDatasourceWrites(ctx, id)
	if err != nil || len(items) != 1 {
		t.Fatalf("history: %d %v", len(items), err)
	}
}
