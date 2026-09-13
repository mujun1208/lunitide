package toolruntime

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestEffectiveRootPrefersProjectRoot(t *testing.T) {
	sandbox := t.TempDir()
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "marker.txt"), []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	rt, err := New(sandbox)
	if err != nil {
		t.Fatal(err)
	}
	rt.SetProjectRootResolver(func(session string) (string, error) {
		if session == "01ARZ3NDEKTSV4RRFFQ69G5FAV" {
			return project, nil
		}
		return "", nil
	})
	got, err := rt.effectiveRoot(Approval, "01ARZ3NDEKTSV4RRFFQ69G5FAV")
	if err != nil {
		t.Fatal(err)
	}
	if got != project {
		t.Fatalf("effectiveRoot = %q, want project %q", got, project)
	}
	personal, err := rt.effectiveRoot(Approval, "01ARZ3NDEKTSV4RRFFQ69G5FAB")
	if err != nil {
		t.Fatal(err)
	}
	if personal == project {
		t.Fatal("personal chat must not inherit the project root")
	}
}

func TestWorkspaceWriteLandsInProjectRoot(t *testing.T) {
	sandbox := t.TempDir()
	project := t.TempDir()
	rt, err := New(sandbox)
	if err != nil {
		t.Fatal(err)
	}
	rt.SetProjectRootResolver(func(session string) (string, error) {
		return project, nil
	})
	_, err = rt.Execute(context.Background(), FullAccess, "01ARZ3NDEKTSV4RRFFQ69G5FAV", "workspace.write", json.RawMessage(`{"path":"src/a.txt","content":"ok"}`), false)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(project, "src", "a.txt"))
	if err != nil || string(got) != "ok" {
		t.Fatalf("project file: %s %v", got, err)
	}
	if _, err = os.Stat(filepath.Join(sandbox, "src", "a.txt")); err == nil {
		t.Fatal("wrote into session sandbox")
	}
}
