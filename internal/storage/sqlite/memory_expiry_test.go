package sqlite

import (
	"context"
	"fmt"
	"github.com/lunitide/lunitide/internal/memoryapp"
	"github.com/oklog/ulid/v2"
	"path/filepath"
	"testing"
	"time"
)

type expiryTestClock struct{ at time.Time }

func (c expiryTestClock) Now() time.Time { return c.at }
func TestMemoryExpiryFiltersBeforeLimitAndPurgesEveryBatch(t *testing.T) {
	ctx := context.Background()
	store, err := OpenTemplated(ctx, filepath.Join(t.TempDir(), "expiry.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	project, other := ulid.Make().String(), ulid.Make().String()
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for i, id := range []string{project, other} {
		if _, err = tx.ExecContext(ctx, `INSERT INTO projects(id,name,project_code,created_at,updated_at) VALUES(?,?,?,?,?)`, id, "fixture", fmt.Sprintf("ITM%05d", i+1), formatTime(now), formatTime(now)); err != nil {
			t.Fatal(err)
		}
	}
	seed := func(id string, expires time.Time, confidence float64) {
		t.Helper()
		if _, err = tx.ExecContext(ctx, `INSERT INTO memories(id,project_id,layer,scope,key,content,confidence,access_count,expires_at,created_at,updated_at) VALUES(?,?,'semantic','project','expiry','expiry fixture',?,0,?,?,?)`, ulid.Make().String(), id, confidence, formatTime(expires), formatTime(now), formatTime(now)); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 700; i++ {
		seed(project, now, .99)
	}
	seed(project, now.Add(time.Nanosecond), .2)
	seed(project, now.Add(time.Hour), .1)
	seed(other, now, .99)
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	rows, err := store.ListActiveMemoriesByProject(ctx, project, "", now, 100)
	if err != nil || len(rows) != 2 {
		t.Fatalf("expired rows consumed list budget %d %v", len(rows), err)
	}
	rows, err = store.SearchActiveMemoriesFTS(ctx, project, "expiry", now, 1)
	if err != nil || len(rows) != 1 || !rows[0].ExpiresAt.Equal(now.Add(time.Nanosecond)) {
		t.Fatalf("FTS expiry ordering %+v %v", rows, err)
	}
	service := memoryapp.New(store, store)
	service.SetClock(expiryTestClock{now})
	n, err := service.PurgeExpired(ctx, project)
	if err != nil || n != 700 {
		t.Fatalf("partial purge %d %v", n, err)
	}
	var remaining int
	if err = store.db.QueryRowContext(ctx, `SELECT count(*) FROM memories WHERE project_id=?`, other).Scan(&remaining); err != nil || remaining != 1 {
		t.Fatalf("cross-project purge %d %v", remaining, err)
	}
	if n, err = service.PurgeExpired(ctx, project); err != nil || n != 0 {
		t.Fatalf("idempotent purge %d %v", n, err)
	}
}
func TestMemoryExpiryDeleteFailureRollsBackBatch(t *testing.T) {
	ctx := context.Background()
	store, err := OpenTemplated(ctx, filepath.Join(t.TempDir(), "expiry-fail.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	id := ulid.Make().String()
	at := "2026-01-01T00:00:00Z"
	if _, err = store.db.ExecContext(ctx, `INSERT INTO projects(id,name,project_code,created_at,updated_at) VALUES(?,'fixture','ITM00001',?,?)`, id, at, at); err != nil {
		t.Fatal(err)
	}
	if _, err = store.db.ExecContext(ctx, `INSERT INTO memories(id,project_id,layer,scope,key,content,confidence,access_count,expires_at,created_at,updated_at) VALUES(?,?,'semantic','project','x','fixture',1,0,?,?,?)`, ulid.Make().String(), id, at, at, at); err != nil {
		t.Fatal(err)
	}
	if _, err = store.db.ExecContext(ctx, `CREATE TRIGGER test_memory_delete_failure BEFORE DELETE ON memories BEGIN SELECT RAISE(ABORT,'fixture delete failure'); END`); err != nil {
		t.Fatal(err)
	}
	n, err := memoryapp.New(store, store).PurgeExpired(ctx, id)
	if err == nil || n != 0 {
		t.Fatalf("write failure swallowed %d %v", n, err)
	}
	var remaining int
	if err = store.db.QueryRowContext(ctx, `SELECT count(*) FROM memories WHERE project_id=?`, id).Scan(&remaining); err != nil || remaining != 1 {
		t.Fatalf("failed batch removed data %d %v", remaining, err)
	}
}
