package m5_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/agentrunapp"
	"github.com/lunitide/lunitide/internal/bridge/m5"
	"github.com/lunitide/lunitide/internal/domain/agentrun"
	"github.com/lunitide/lunitide/internal/domain/project"
	"github.com/lunitide/lunitide/internal/domain/session"
	"github.com/lunitide/lunitide/internal/projectapp"
	"github.com/lunitide/lunitide/internal/sessionapp"
	"github.com/lunitide/lunitide/internal/workspace"
	storage "github.com/lunitide/lunitide/internal/storage/sqlite"
)

func fsHarness(t *testing.T) (*m5.FsService, string, string) {
	t.Helper()
	ctx := context.Background()
	store, err := storage.Open(ctx, filepath.Join(t.TempDir(), "m5-fs.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	p, err := projectapp.New(store, store).Create(ctx, "m5-fs-p", "test", map[string]string{"name": "fs"}, project.Project{Name: "fs"})
	if err != nil {
		t.Fatal(err)
	}
	sess, err := sessionapp.New(store, store).Create(ctx, "m5-fs-s", "test", map[string]string{"projectId": p.ID}, session.Session{ProjectID: p.ID, Title: "fs"})
	if err != nil {
		t.Fatal(err)
	}
	run, err := agentrunapp.New(store.AgentRuntimeRepository()).Start(ctx, "m5-fs-r", "test", map[string]string{"sessionId": sess.ID}, sess.ID, agentrun.Budget{MaxModelTurns: 2, MaxToolCalls: 2, MaxTokens: 100, MaxCostMicros: 100, MaxWallClockSeconds: 30, MaxOutputBytes: 1024, MaxRetries: 1, MaxNoProgress: 1})
	if err != nil {
		t.Fatal(err)
	}
	wsRoot := filepath.Join(t.TempDir(), "ws")
	adhoc := workspace.NewAdHocService(store.AgentRuntimeRepository())
	w, err := adhoc.Create(ctx, run.ID, wsRoot, "FS")
	if err != nil {
		t.Fatal(err)
	}
	// Seed a small tree.
	seed := map[string]string{
		"README.md":      "# hello\nworld\n",
		"src/main.go":    "package main\nfunc main() {}\n",
		"src/util.go":    "package main\nfunc util() {}\n",
		"notes/TODO.txt": "TODO grep me\n",
	}
	for rel, body := range seed {
		if err := os.MkdirAll(filepath.Join(wsRoot, filepath.Dir(rel)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(wsRoot, rel), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return m5.NewFsService(store.AgentRuntimeRepository()), w.ID, wsRoot
}

func TestFsOpsReadTreeGrep(t *testing.T) {
	svc, wsID, _ := fsHarness(t)
	ctx := context.Background()

	got, err := svc.Op(ctx, m5.FsInput{WorkspaceID: wsID, Op: "read", Path: "README.md"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Content != "# hello\nworld\n" || got.Truncated {
		t.Fatalf("read mismatch: %+v", got)
	}

	// maxBytes truncation marks the response.
	got, err = svc.Op(ctx, m5.FsInput{WorkspaceID: wsID, Op: "read", Path: "README.md", MaxBytes: 5})
	if err != nil {
		t.Fatal(err)
	}
	if got.Content != "# hel" || !got.Truncated {
		t.Fatalf("truncated read mismatch: %+v", got)
	}

	// Tree lists the subtree rooted at src.
	got, err = svc.Op(ctx, m5.FsInput{WorkspaceID: wsID, Op: "tree", Path: "src"})
	if err != nil {
		t.Fatal(err)
	}
	paths := map[string]bool{}
	for _, e := range got.Entries {
		paths[e.Path] = true
	}
	if !paths["src"] || !paths["src/main.go"] || !paths["src/util.go"] || got.Truncated {
		t.Fatalf("tree mismatch: %+v", got.Entries)
	}

	// Grep finds matches with file+line.
	got, err = svc.Op(ctx, m5.FsInput{WorkspaceID: wsID, Op: "grep", Path: ".", Pattern: "grep me"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Matches) != 1 || got.Matches[0].Path != "notes/TODO.txt" || got.Matches[0].Line != 1 {
		t.Fatalf("grep mismatch: %+v", got.Matches)
	}

	// Escape attempts answer WS-002 through the path safety core.
	if _, err := svc.Op(ctx, m5.FsInput{WorkspaceID: wsID, Op: "read", Path: "../escape.txt"}); err == nil || !strings.Contains(err.Error(), "WS-002") {
		t.Fatalf("escape must fail WS-002, got %v", err)
	}
	if _, err := svc.Op(ctx, m5.FsInput{WorkspaceID: wsID, Op: "bogus", Path: "README.md"}); err == nil {
		t.Fatal("bogus op must fail")
	}
	if _, err := svc.Op(ctx, m5.FsInput{WorkspaceID: wsID, Op: "grep", Path: ".", Pattern: ""}); err == nil {
		t.Fatal("empty pattern must fail")
	}
}

func TestFsReadBinarySkipAndCaps(t *testing.T) {
	svc, wsID, wsRoot := fsHarness(t)
	ctx := context.Background()
	// Binary file with NULs is skipped by grep but readable raw.
	if err := os.WriteFile(filepath.Join(wsRoot, "bin.dat"), []byte{0x01, 0x00, 0x02, 'g', 'r', 'e', 'p', ' ', 'm', 'e'}, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := svc.Op(ctx, m5.FsInput{WorkspaceID: wsID, Op: "grep", Path: "bin.dat", Pattern: "grep me"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Matches) != 0 {
		t.Fatalf("binary must be skipped, got %+v", got.Matches)
	}
	// MaxBytes is clamped to the 8 MiB cap, negative resets to default 1 MiB.
	got, err = svc.Op(ctx, m5.FsInput{WorkspaceID: wsID, Op: "read", Path: "bin.dat", MaxBytes: -1})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Content) != 10 || got.Truncated {
		t.Fatalf("default-limit read mismatch: %+v", got)
	}
}
