package merge

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// gitAvailable reports whether a usable git is on PATH.
func gitAvailable() bool {
	path, err := exec.LookPath("git")
	return err == nil && path != ""
}

// newGitRepo creates a real repository with one initial commit and
// returns (repoPath, baseHead).
func newGitRepo(t *testing.T) (string, string) {
	t.Helper()
	if !gitAvailable() {
		t.Skip("git not available on PATH")
	}
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) string {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL="+os.DevNull)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return string(out)
	}
	run("init", "-q")
	run("config", "user.email", "t@lunitide.local")
	run("config", "user.name", "T")
	if err := os.WriteFile(filepath.Join(repo, "hello.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "-A")
	run("commit", "-q", "-m", "base")
	head := strings.TrimSpace(run("rev-parse", "HEAD"))
	return repo, head
}

func TestWorktreeIsolation(t *testing.T) {
	repo, base := newGitRepo(t)
	ctx := context.Background()
	exec_ := NewGitExec("")
	leases := filepath.Join(t.TempDir(), "worktrees")
	mgr, err := NewWorktreeManager(exec_, repo, leases)
	if err != nil {
		t.Fatal(err)
	}

	lease, err := mgr.CreateLease(ctx, "child-1", base, "worker-a", time.Hour)
	if err != nil {
		t.Fatalf("CreateLease: %v", err)
	}
	if lease.PathRef == "" || lease.BranchRef != "lunitide/child/child-1" {
		t.Fatalf("lease malformed: %+v", lease)
	}
	// lease path is under the leases root (containment holds by construction)
	if err := mgr.ValidateLeasePath(lease.PathRef); err != nil {
		t.Fatalf("own path rejected: %v", err)
	}

	// child commits inside its worktree
	if err := os.WriteFile(filepath.Join(lease.PathRef, "feature.txt"), []byte("child work\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git := exec.Command("git", "add", "-A")
	git.Dir = lease.PathRef
	git.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL="+os.DevNull)
	if out, err := git.CombinedOutput(); err != nil {
		t.Fatalf("child add: %v\n%s", err, out)
	}
	git = exec.Command("git", "commit", "-q", "-m", "child change")
	git.Dir = lease.PathRef
	git.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL="+os.DevNull)
	if out, err := git.CombinedOutput(); err != nil {
		t.Fatalf("child commit: %v\n%s", err, out)
	}

	// MAIN TREE WRITE EVENTS = 0: main-tree HEAD unchanged, no feature.txt
	mainHead, err := exec_.Head(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}
	if mainHead != base {
		t.Fatalf("main tree head moved: %s != %s", mainHead, base)
	}
	if _, err := os.Stat(filepath.Join(repo, "feature.txt")); !os.IsNotExist(err) {
		t.Fatalf("child file leaked into main tree")
	}

	// patch manifest: digest stable and bytes carry the change
	manifest, err := mgr.ExportPatch(ctx, lease)
	if err != nil {
		t.Fatalf("ExportPatch: %v", err)
	}
	if !strings.Contains(string(manifest.Bytes), "feature.txt") {
		t.Fatalf("patch missing the child change:\n%s", manifest.Bytes)
	}
	if err := VerifyPatch(manifest, manifest.Digest); err != nil {
		t.Fatalf("VerifyPatch: %v", err)
	}
	if err := VerifyPatch(manifest, "0"+manifest.Digest[1:]); err == nil {
		t.Fatalf("VerifyPatch accepted a wrong pin")
	}

	// one child one lease: a second live lease is refused
	if _, err := mgr.CreateLease(ctx, "child-1", base, "worker-b", time.Hour); err == nil {
		t.Fatalf("second lease for the same child accepted")
	}

	// remove then re-create is allowed
	if err := mgr.RemoveLease(ctx, "child-1"); err != nil {
		t.Fatalf("RemoveLease: %v", err)
	}
	if _, err := mgr.CreateLease(ctx, "child-1", base, "worker-a", time.Hour); err != nil {
		t.Fatalf("re-create after remove: %v", err)
	}
}

func TestWorktreePathDiscipline(t *testing.T) {
	repo, _ := newGitRepo(t)
	exec_ := NewGitExec("")
	leases := filepath.Join(t.TempDir(), "wt")
	mgr, err := NewWorktreeManager(exec_, repo, leases)
	if err != nil {
		t.Fatal(err)
	}
	// child ids with separators / traversal never become a lease
	for _, bad := range []string{"../escape", "a/b", `a\b`, ".hidden", "", strings.Repeat("x", 65)} {
		if _, err := mgr.CreateLease(context.Background(), bad, "HEAD", "w", time.Minute); err == nil {
			t.Fatalf("child id %q accepted", bad)
		}
	}
	// paths outside the leases root are refused
	outside := filepath.Join(filepath.Dir(leases), "elsewhere")
	if err := mgr.ValidateLeasePath(outside); err == nil {
		t.Fatalf("outside path accepted")
	}
	if err := mgr.ValidateLeasePath(filepath.Join(leases, "ok-child")); err != nil {
		t.Fatalf("inside path refused: %v", err)
	}
}

func TestWorktreeRootApplyMarker(t *testing.T) {
	repo, base := newGitRepo(t)
	ctx := context.Background()
	exec_ := NewGitExec("")
	mgr, err := NewWorktreeManager(exec_, repo, filepath.Join(t.TempDir(), "wt"))
	if err != nil {
		t.Fatal(err)
	}
	// patch: append a line to hello.txt (canonical unified diff)
	patch := []byte("--- a/hello.txt\n+++ b/hello.txt\n@@ -1 +1,2 @@\n base\n+root-writer\n")
	newHead, err := mgr.ApplyToRoot(ctx, "01ARZ3NDEKTSV4RRFFQ69G5FA9", patch, base)
	if err != nil {
		t.Fatalf("ApplyToRoot: %v", err)
	}
	if newHead == base {
		t.Fatalf("head did not advance")
	}
	// marker is readable back — this is what crash recovery keys on
	id, err := mgr.HeadIntent(ctx, repo)
	if err != nil || id != "01ARZ3NDEKTSV4RRFFQ69G5FA9" {
		t.Fatalf("HeadIntent = %q err=%v", id, err)
	}
	// the effect landed in the final tree
	data, err := os.ReadFile(filepath.Join(repo, "hello.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "root-writer") {
		t.Fatalf("patch content missing: %q", data)
	}
}
