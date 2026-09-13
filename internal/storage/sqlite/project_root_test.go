package sqlite

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/lunitide/lunitide/internal/domain/project"
	"github.com/lunitide/lunitide/internal/projectapp"
	"github.com/lunitide/lunitide/internal/projectroot"
)

func TestCreateProjectBindsRootAndRejectsDuplicate(t *testing.T) {
	ctx := context.Background()
	store, err := OpenTemplated(ctx, filepath.Join(t.TempDir(), "root-bind.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	svc := projectapp.New(store, store)
	root := t.TempDir()
	first, err := svc.Create(ctx, "root-one", "test", map[string]string{"n": "one"}, project.Project{
		Name: "Mall", Type: project.TypeImplementation, RootPath: root,
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.RootPath == "" || first.TreeStatus != project.TreeNone || first.DefaultExecutor != project.ExecutorLunitide {
		t.Fatalf("dto fields: %+v", first)
	}
	lock, err := projectroot.ReadLock(root)
	if err != nil || lock.ProjectID != first.ID {
		t.Fatalf("lock: %+v %v", lock, err)
	}
	if _, err := os.Stat(filepath.Join(root, ".lunitide", "project.json")); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetProject(ctx, first.ID)
	if err != nil || got.RootPath != first.RootPath {
		t.Fatalf("get: %+v %v", got, err)
	}
	_, err = svc.Create(ctx, "root-two", "test", map[string]string{"n": "two"}, project.Project{
		Name: "Other", Type: project.TypeImplementation, RootPath: root,
	})
	if err == nil || !projectapp.IsRootBusy(err) {
		t.Fatalf("duplicate root: %v", err)
	}
}

func TestCreateProjectEmptyRootSkipsLock(t *testing.T) {
	ctx := context.Background()
	store, err := OpenTemplated(ctx, filepath.Join(t.TempDir(), "root-empty.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	svc := projectapp.New(store, store)
	created, err := svc.Create(ctx, "chat-empty", "test", map[string]string{"n": "chat"}, project.Project{Name: "Chat"})
	if err != nil {
		t.Fatal(err)
	}
	if created.RootPath != "" {
		t.Fatalf("chat got root %q", created.RootPath)
	}
}
