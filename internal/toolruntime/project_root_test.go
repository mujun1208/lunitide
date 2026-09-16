package toolruntime

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/lunitide/lunitide/internal/canonpath"
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
	// Hosted Windows runners spell t.TempDir as RUNNER~1; effectiveRoot
	// pins the OS long name so later Rel checks compare like with like.
	want, err := canonpath.Canonical(project)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("effectiveRoot = %q, want canonical project %q (resolver %q)", got, want, project)
	}
	personal, err := rt.effectiveRoot(Approval, "01ARZ3NDEKTSV4RRFFQ69G5FAB")
	if err != nil {
		t.Fatal(err)
	}
	if sameExistingFile(personal, project) {
		t.Fatal("personal chat must not inherit the project root")
	}
}

func sameExistingFile(a, b string) bool {
	ga, err := os.Stat(a)
	if err != nil {
		return false
	}
	wa, err := os.Stat(b)
	return err == nil && os.SameFile(ga, wa)
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
