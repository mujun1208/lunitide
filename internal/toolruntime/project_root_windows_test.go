//go:build windows

package toolruntime

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/canonpath"
	"golang.org/x/sys/windows"
)

// Hosted Windows runners hand t.TempDir out as an 8.3 alias
// (C:\Users\RUNNER~1\...). workspace.write used to Canonical the child
// and Rel it against the unresolved project root, which looks like an
// escape for every file inside the project.
func TestWorkspaceWriteAcceptsShortNameProjectRoot(t *testing.T) {
	sandbox := t.TempDir()
	long := filepath.Join(t.TempDir(), "a-directory-name-well-past-eight-characters")
	if err := os.MkdirAll(long, 0o755); err != nil {
		t.Fatal(err)
	}
	short, err := shortPath(long)
	if err != nil {
		t.Skipf("8.3 short names are disabled on this volume: %v", err)
	}
	if strings.EqualFold(short, long) {
		t.Skip("volume produced no distinct short name")
	}
	rt, err := New(sandbox)
	if err != nil {
		t.Fatal(err)
	}
	rt.SetProjectRootResolver(func(string) (string, error) { return short, nil })
	_, err = rt.Execute(context.Background(), FullAccess, "01ARZ3NDEKTSV4RRFFQ69G5FAV", "workspace.write", json.RawMessage(`{"path":"src/a.txt","content":"ok"}`), false)
	if err != nil {
		t.Fatal(err)
	}
	wantRoot, err := canonpath.Canonical(long)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(wantRoot, "src", "a.txt"))
	if err != nil || string(got) != "ok" {
		t.Fatalf("project file: %s %v", got, err)
	}
}

func shortPath(p string) (string, error) {
	ptr, err := windows.UTF16PtrFromString(p)
	if err != nil {
		return "", err
	}
	buf := make([]uint16, windows.MAX_PATH)
	n, err := windows.GetShortPathName(ptr, &buf[0], uint32(len(buf)))
	if err != nil {
		return "", err
	}
	return windows.UTF16ToString(buf[:n]), nil
}
