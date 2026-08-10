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
	"github.com/lunitide/lunitide/internal/projectapp"
)

func TestProjectCapacityBoundaryAndConcurrentAtomicity(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "capacity.db")
	a, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	serviceA, serviceB := projectapp.New(a, a), projectapp.New(b, b)
	for i := 0; i < 99; i++ {
		name := fmt.Sprintf("Project %03d", i)
		if _, err = serviceA.Create(ctx, fmt.Sprintf("key-%03d", i), "test", struct{ Name string }{name}, project.Project{Name: name}); err != nil {
			t.Fatal(err)
		}
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for i, service := range []*projectapp.Service{serviceA, serviceB} {
		wg.Add(1)
		go func(i int, service *projectapp.Service) {
			defer wg.Done()
			<-start
			name := fmt.Sprintf("Boundary %d", i)
			_, createErr := service.Create(ctx, fmt.Sprintf("boundary-%d", i), "test", struct{ Name string }{name}, project.Project{Name: name})
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
		} else if errors.Is(result, projectapp.ErrProjectCapacityReached) {
			capacity++
		} else {
			t.Fatalf("unexpected create error: %v", result)
		}
	}
	if wins != 1 || capacity != 1 {
		t.Fatalf("wins=%d capacity=%d", wins, capacity)
	}
	items, err := a.ListProjects(ctx, project.Filter{})
	if err != nil || len(items) != 100 {
		t.Fatalf("projects=%d err=%v", len(items), err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err = a.db.Exec(`INSERT INTO projects(id,name,status,created_at,updated_at,version) VALUES(?,?,?,?,?,1)`, "01ARZ3NDEKTSV4RRFFQ69G5FAV", "Externally inserted", "active", now, now); err != nil {
		t.Fatal(err)
	}
	if _, err = a.ListProjects(ctx, project.Filter{}); err == nil || !strings.Contains(err.Error(), "list exceeds capacity") {
		t.Fatalf("over-capacity list accepted: %v", err)
	}
}

func TestOpenRejectsCorruptProjectRows(t *testing.T) {
	for _, tc := range []struct {
		name   string
		column string
		value  string
	}{
		{"doubled whitespace", "name", "Alpha  Beta"},
		{"unicode whitespace", "name", "Alpha\u00a0Beta"},
		{"malformed created", "created_at", "not-a-time"},
		{"malformed updated", "updated_at", "not-a-time"},
		{"backward timestamps", "updated_at", "2024-12-31T23:59:59Z"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			path := filepath.Join(t.TempDir(), "corrupt-project.db")
			store, err := Open(ctx, path)
			if err != nil {
				t.Fatal(err)
			}
			service := projectapp.New(store, store)
			created, err := service.Create(ctx, "seed-key", "test", struct{ Name string }{"Alpha Beta"}, project.Project{Name: "Alpha Beta"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = store.db.Exec(`UPDATE projects SET `+tc.column+`=? WHERE id=?`, tc.value, created.ID); err != nil {
				t.Fatal(err)
			}
			store.Close()
			if _, err = Open(ctx, path); err == nil || !strings.Contains(err.Error(), "project data invariant violation") {
				t.Fatalf("corruption accepted: %v", err)
			}
		})
	}
}
