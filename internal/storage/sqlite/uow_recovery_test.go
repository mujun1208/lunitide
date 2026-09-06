package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestUnitOfWorkUnwindsEveryUncommittedExit(t *testing.T) {
	for _, exit := range []string{"panic", "error", "cancel"} {
		t.Run(exit, func(t *testing.T) {
			s, err := OpenTemplated(context.Background(), filepath.Join(t.TempDir(), "test.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			if _, err := s.db.Exec(`CREATE TABLE uow_fixture(value TEXT)`); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			cause := errors.New("callback failed")
			var recovered any
			func() {
				defer func() { recovered = recover() }()
				err = s.do(ctx, func(tx *txAdapter) error {
					if _, e := tx.q.ExecContext(ctx, `INSERT INTO uow_fixture VALUES('uncommitted')`); e != nil {
						return e
					}
					switch exit {
					case "panic":
						panic(cause)
					case "cancel":
						cancel()
						return nil // COMMIT must fail with the cancelled context.
					default:
						return cause
					}
				})
			}()
			if exit == "panic" && recovered != cause {
				t.Fatalf("panic changed: %v", recovered)
			}
			if exit == "error" && !errors.Is(err, cause) {
				t.Fatalf("callback cause lost: %v", err)
			}
			if exit == "cancel" && !errors.Is(err, context.Canceled) {
				t.Fatalf("expected cancelled commit: %v", err)
			}
			var count int
			if err := s.db.QueryRow(`SELECT count(*) FROM uow_fixture`).Scan(&count); err != nil || count != 0 {
				t.Fatalf("uncommitted row survived: count=%d err=%v", count, err)
			}
			if err := s.do(context.Background(), func(tx *txAdapter) error {
				_, e := tx.q.ExecContext(context.Background(), `INSERT INTO uow_fixture VALUES('committed')`)
				return e
			}); err != nil {
				t.Fatalf("next transaction poisoned: %v", err)
			}
			if err := s.db.QueryRow(`SELECT count(*) FROM uow_fixture`).Scan(&count); err != nil || count != 1 {
				t.Fatalf("next transaction not committed: count=%d err=%v", count, err)
			}
		})
	}
}

func TestUnitOfWorkClosesStoreWhenRollbackCannotBeConfirmed(t *testing.T) {
	s, err := OpenTemplated(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	cause := errors.New("injected failure")
	err = s.do(context.Background(), func(tx *txAdapter) error {
		// Simulate the connection losing its transaction before cleanup.
		if _, err := tx.q.ExecContext(context.Background(), `ROLLBACK`); err != nil {
			return err
		}
		return cause
	})
	if !errors.Is(err, cause) || !strings.Contains(err.Error(), "storage closed") {
		t.Fatalf("rollback failure must retain original cause: %v", err)
	}
	if err := s.db.Ping(); err == nil {
		t.Fatal("unhealthy connection pool remained available")
	}
}
