package vcs_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/vcs"
)

func gitRunner(t *testing.T) (*vcs.Runner, string) {
	t.Helper()
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Skipf("git not installed: %v", err)
	}
	base := t.TempDir()
	repo := filepath.Join(base, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	run(t, repo, nil, "init", "-q")
	runner := vcs.NewRunner(gitPath, filepath.Join(base, "empty-hooks"))
	if err := runner.EnsureEmptyHooksDir(); err != nil {
		t.Fatal(err)
	}
	return runner, repo
}

func run(t *testing.T, dir string, env []string, args ...string) string {
	t.Helper()
	gitPath, _ := exec.LookPath("git")
	cmd := exec.Command(gitPath, args...)
	cmd.Dir = dir
	if env != nil {
		cmd.Env = env
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("setup git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func TestGitAllowlistValidateArgv(t *testing.T) {
	allowed := [][]string{
		{"status"}, {"status", "--porcelain"}, {"status", "--short", "--branch"},
		{"diff"}, {"diff", "--stat"}, {"diff", "--cached"}, {"diff", "-U3"}, {"diff", "HEAD"},
		{"add", "."}, {"add", "-A", "src/main.go"}, {"restore", "--staged", "a.txt"}, {"restore", "a.txt"},
		{"commit", "-m", "fix: thing"}, {"commit", "--amend", "--no-edit"},
		{"branch"}, {"branch", "feature-x"}, {"branch", "-d", "feature/x-2"},
	}
	for _, argv := range allowed {
		if err := vcs.ValidateArgv(argv); err != nil {
			t.Errorf("argv %v must pass, got %v", argv, err)
		}
	}
	denied := [][]string{
		// remote / repo-level escapes
		{"push"}, {"pull"}, {"fetch"}, {"clone", "https://evil"}, {"config", "user.name", "x"},
		{"log"}, {"checkout", "main"}, {"reset", "--hard"}, {"stash"}, {"worktree", "add", ".."},
		{"show"}, {"tag"}, {"rebase"}, {"merge"}, {"cherry-pick"}, {"submodule"},
		// hook / exec style flags on allowed subcommands
		{"commit", "--exec=calc.exe", "-m", "x"}, {"diff", "--ext-diff"}, {"diff", "--textconv"},
		{"status", "--no-optional-locks", "--exec=x"},
		// pathspec escapes
		{"add", "../outside.txt"}, {"add", "a/../../b"}, {"restore", `..\..\x`},
		{"add", "C:\\abs"}, {"add", "file:stream"},
		// branch name injection
		{"branch", "-d", "../evil"}, {"branch", "evil..range"}, {"branch", "-m", "-x"}, {"branch", "b.lock"},
		// bare args where none are allowed
		{"commit", "message.txt"},
		// empty / stdin
		{}, {"status", "-"},
	}
	for _, argv := range denied {
		err := vcs.ValidateArgv(argv)
		if err == nil {
			t.Errorf("argv %v must be denied", argv)
			continue
		}
		if !errors.Is(err, vcs.ErrNotAllowed) {
			t.Errorf("argv %v must answer GIT-001, got %v", argv, err)
		}
	}
}

func TestGitAllowlistRunsAndBlocksHooks(t *testing.T) {
	runner, repo := gitRunner(t)
	ctx := context.Background()
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Poison every hook: if any executes it writes a marker file.
	marker := filepath.Join(repo, "HOOK-RAN")
	hookBody := "#!/bin/sh\necho pwned > " + marker + "\n"
	for _, hook := range []string{"pre-commit", "commit-msg", "post-commit", "pre-add", "post-checkout", "prepare-commit-msg"} {
		p := filepath.Join(repo, ".git", "hooks", hook)
		if err := os.WriteFile(p, []byte(hookBody), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if res, err := runner.Run(ctx, repo, []string{"add", "a.txt"}, nil); err != nil || res.ExitCode != 0 {
		t.Fatalf("add failed: %v %+v", err, res)
	}
	if res, err := runner.Run(ctx, repo, []string{"commit", "-m", "test: m5 allowlist"}, nil); err != nil || res.ExitCode != 0 {
		t.Fatalf("commit failed: %v %+v", err, res)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("hook executed despite core.hooksPath pinning")
	}
	res, err := runner.Run(ctx, repo, []string{"status", "--porcelain"}, nil)
	if err != nil || res.ExitCode != 0 {
		t.Fatalf("status failed: %v %+v", err, res)
	}
	if strings.Contains(res.Stdout, "a.txt") {
		t.Fatalf("status after commit should be clean, got %q", res.Stdout)
	}
	diff, err := runner.Run(ctx, repo, []string{"diff", "--stat"}, nil)
	if err != nil || diff.ExitCode != 0 {
		t.Fatalf("diff failed: %v %+v", err, diff)
	}
}

func TestGitAllowlistGIT001CarriesAllowedList(t *testing.T) {
	runner, repo := gitRunner(t)
	_, err := runner.Run(context.Background(), repo, []string{"push", "origin", "main"}, nil)
	if err == nil || !strings.Contains(err.Error(), "GIT-001") {
		t.Fatalf("push must answer GIT-001, got %v", err)
	}
	if !strings.Contains(err.Error(), "status") || !strings.Contains(err.Error(), "commit") {
		t.Fatalf("GIT-001 must embed the allowed list, got %v", err)
	}
}

func TestGitAllowlistEnvScrubbed(t *testing.T) {
	runner, repo := gitRunner(t)
	// Poison the environment: GIT_INDEX_FILE would redirect writes.
	t.Setenv("GIT_INDEX_FILE", filepath.Join(t.TempDir(), "evil-index"))
	t.Setenv("GIT_DIR", filepath.Join(t.TempDir(), "evil-git"))
	res, err := runner.Run(context.Background(), repo, []string{"status", "--porcelain"}, nil)
	if err != nil || res.ExitCode != 0 {
		t.Fatalf("status with poisoned env failed: %v %+v", err, res)
	}
	if strings.Contains(res.Stderr, "evil-git") || strings.Contains(res.Stderr, "evil-index") {
		t.Fatalf("env injection leaked: %q", res.Stderr)
	}
}
