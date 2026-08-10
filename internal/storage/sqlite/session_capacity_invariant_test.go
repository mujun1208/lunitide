package sqlite

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/domain/project"
	"github.com/lunitide/lunitide/internal/domain/session"
	"github.com/lunitide/lunitide/internal/projectapp"
	"github.com/lunitide/lunitide/internal/sessionapp"
)

func createSessionProject(t *testing.T, store *Store, key, name string) string {
	t.Helper()
	p, err := projectapp.New(store, store).Create(context.Background(), key, "test", struct{ Name string }{name}, project.Project{Name: name})
	if err != nil {
		t.Fatal(err)
	}
	return p.ID
}

func createSession(t *testing.T, service *sessionapp.Service, parent string, i int) {
	t.Helper()
	title := fmt.Sprintf("Session %03d", i)
	_, err := service.Create(context.Background(), fmt.Sprintf("session-key-%s-%03d", parent, i), "test", struct{ ProjectID, Title string }{parent, title}, session.Session{ProjectID: parent, Title: title})
	if err != nil {
		t.Fatal(err)
	}
}

func TestSessionCapacityListInvariantAndParentIsolation(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "sessions.db")
	store, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	first := createSessionProject(t, store, "project-first", "First")
	second := createSessionProject(t, store, "project-second", "Second")
	service := sessionapp.New(store, store)
	for i := 0; i < 100; i++ {
		createSession(t, service, first, i)
	}
	createSession(t, service, second, 1000)
	items, err := store.ListSessions(ctx, session.Filter{ProjectID: first})
	if err != nil || len(items) != 100 {
		t.Fatalf("first sessions=%d err=%v", len(items), err)
	}
	isolated, err := store.ListSessions(ctx, session.Filter{ProjectID: second})
	if err != nil || len(isolated) != 1 || isolated[0].ProjectID != second {
		t.Fatalf("isolated=%#v err=%v", isolated, err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = store.db.Exec(`INSERT INTO sessions(id,project_id,title,status,created_at,updated_at,version) VALUES(?,?,?,?,?,?,1)`, "01ARZ3NDEKTSV4RRFFQ69G5FAV", first, "Sentinel 101", "active", now, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.ListSessions(ctx, session.Filter{ProjectID: first}); err == nil || !strings.Contains(err.Error(), "list exceeds capacity") {
		t.Fatalf("101-session invariant accepted: %v", err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = Open(ctx, path); err == nil || !strings.Contains(err.Error(), "session data invariant violation") {
		t.Fatalf("startup accepted 101 sessions: %v", err)
	}
}

func TestSessionConcurrentBoundaryAcrossStoreConnections(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "boundary.db")
	a, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	parent := createSessionProject(t, a, "parent", "Parent")
	b, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	serviceA, serviceB := sessionapp.New(a, a), sessionapp.New(b, b)
	for i := 0; i < 99; i++ {
		createSession(t, serviceA, parent, i)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for i, service := range []*sessionapp.Service{serviceA, serviceB} {
		wg.Add(1)
		go func(i int, service *sessionapp.Service) {
			defer wg.Done()
			<-start
			title := fmt.Sprintf("Boundary %d", i)
			_, createErr := service.Create(ctx, fmt.Sprintf("boundary-%d", i), "test", struct{ ProjectID, Title string }{parent, title}, session.Session{ProjectID: parent, Title: title})
			results <- createErr
		}(i, service)
	}
	close(start)
	wg.Wait()
	close(results)
	wins, capacity := 0, 0
	for result := range results {
		if result == nil {
			wins++
		} else if errors.Is(result, sessionapp.ErrSessionCapacityReached) {
			capacity++
		} else {
			t.Fatalf("unexpected error: %v", result)
		}
	}
	if wins != 1 || capacity != 1 {
		t.Fatalf("wins=%d capacity=%d", wins, capacity)
	}
}

func TestOpenRejectsOrphanSessionInsertedWithForeignKeysDisabled(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "orphan.db")
	store, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	db := openRaw(t, path)
	if _, err = db.Exec(`PRAGMA foreign_keys=OFF`); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = db.Exec(`INSERT INTO sessions(id,project_id,title,status,created_at,updated_at,version) VALUES(?,?,?,?,?,?,1)`, "01ARZ3NDEKTSV4RRFFQ69G5FAV", "01ARZ3NDEKTSV4RRFFQ69G5FAW", "Orphan", "active", now, now)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = Open(ctx, path); err == nil || !strings.Contains(err.Error(), "foreign key violation") {
		t.Fatalf("orphan accepted: %v", err)
	}
}
