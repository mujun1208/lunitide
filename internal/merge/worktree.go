// Package merge implements the M6 slice-4 root-tree discipline (T-6.4.x):
// children live in isolated worktrees and hand their work back as patch
// manifests; a single Root Writer applies those patches to the final tree
// under a head CAS, and the final-tree test gate stands between the last
// merge and COMPLETE.
//
// worktree.go (T-6.4.1) owns the child-side filesystem story:
//
//   - one WorktreeLease per child: the worktree lives at a derived path
//     under the leases root, pinned to the baseHead the child patched
//     against; the path can never escape the leases root
//   - the main tree is never written by child machinery — every git call
//     this manager issues either reads, touches only the leased worktree
//     path, or is an explicit Root-Writer operation (ApplyToRoot)
//   - a child's changes leave the worktree only as a PatchManifest
//     (canonical diff bytes + SHA-256 digest), which is what a
//     MergeIntent references
//
// Git execution is a dedicated hardened channel: fixed command templates
// assembled in code (never from caller text), hooks/external diffs/
// credential helpers disabled and a scrubbed environment — the same
// hardening the M5 vcs package applies, but the command set is private to
// this package so the M5 agent-facing allowlist stays frozen.
package merge

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// ErrWorktreeEscaped is the path-guard verdict: a lease path resolved
// outside the leases root (WTR-001 path discipline).
var ErrWorktreeEscaped = errors.New("merge: worktree path escapes the leases root")

// ErrLeaseExists: one child, one lease — a live lease for the child id
// already exists.
var ErrLeaseExists = errors.New("merge: worktree lease already exists for child")

// ErrPatchDigestMismatch: the exported patch digest does not match the
// intent's pinned digest (the tree changed under the intent).
var ErrPatchDigestMismatch = errors.New("merge: patch digest mismatch")

// ErrWorktreeMissing: lease path does not exist (removed or never created).
var ErrWorktreeMissing = errors.New("merge: worktree missing")

// childIDRe is the child-identifier whitelist; it deliberately excludes
// path separators, '..' and friends so a child id can become exactly one
// path segment under the leases root.
var childIDRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// refRe accepts git commit-ish tokens (hex sha, branch@sha, refs/...).
var refRe = regexp.MustCompile(`^[A-Za-z0-9_./@~^+=-]{1,128}$`)

// markerPrefix stamps Root-Writer commits so crash recovery can tell an
// applied intent from a pending one by reading the HEAD commit subject.
const markerPrefix = "lunitide-merge:"

// WorktreeLease is the child-side isolation record. One child holds one
// lease; the lease root is the only writable area the child ever sees.
type WorktreeLease struct {
	ChildID   string
	PathRef   string // absolute worktree path (inside LeasesRoot)
	BaseHead  string // pinned head the child patched against
	BranchRef string // child branch name
	WorkerID  string
	ExpiresAt time.Time
}

// PatchManifest is the only shape in which child changes may cross the
// worktree boundary: canonical diff bytes plus their digest and size.
type PatchManifest struct {
	ChildID string
	Digest  string // hex SHA-256 of Bytes
	Size    int64
	Bytes   []byte
}

// WorktreeManager hands out isolated child worktrees under one leases
// root and exports their changes as patch manifests.
type WorktreeManager struct {
	Exec       *GitExec
	Repo       string // main tree path (read + Root-Writer operations only)
	LeasesRoot string // parent of every child worktree
}

// NewWorktreeManager validates the shape of the two roots.
func NewWorktreeManager(exec *GitExec, repo, leasesRoot string) (*WorktreeManager, error) {
	if exec == nil || repo == "" || leasesRoot == "" {
		return nil, errors.New("merge: worktree manager requires exec, repo and leases root")
	}
	absLeases, err := filepath.Abs(leasesRoot)
	if err != nil {
		return nil, err
	}
	absRepo, err := filepath.Abs(repo)
	if err != nil {
		return nil, err
	}
	return &WorktreeManager{Exec: exec, Repo: absRepo, LeasesRoot: absLeases}, nil
}

// leasePath derives the one path segment a child may occupy.
func (m *WorktreeManager) leasePath(childID string) (string, error) {
	if !childIDRe.MatchString(childID) {
		return "", fmt.Errorf("%w: child id %q invalid", ErrWorktreeEscaped, childID)
	}
	return filepath.Join(m.LeasesRoot, childID), nil
}

// ValidateLeasePath enforces the lease containment invariant: the resolved
// path must sit strictly inside the leases root.
func (m *WorktreeManager) ValidateLeasePath(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrWorktreeEscaped, err)
	}
	rel, err := filepath.Rel(m.LeasesRoot, abs)
	if err != nil || rel == "." || rel == "" || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return fmt.Errorf("%w: %s", ErrWorktreeEscaped, path)
	}
	return nil
}

// branchFor derives the child branch name (also one safe token).
func branchFor(childID string) string { return "lunitide/child/" + childID }

// Head reads the current HEAD of a tree (main repo or worktree).
func (g *GitExec) Head(ctx context.Context, dir string) (string, error) {
	out, err := g.runOk(ctx, dir, []string{"rev-parse", "HEAD"})
	if err != nil {
		return "", err
	}
	h := strings.TrimSpace(out.Stdout)
	if h == "" {
		return "", errors.New("merge: empty HEAD")
	}
	return h, nil
}

// CreateLease provisions one isolated worktree for the child, pinned at
// baseHead. Refuses a second live lease for the same child.
func (m *WorktreeManager) CreateLease(ctx context.Context, childID, baseHead, workerID string, ttl time.Duration) (WorktreeLease, error) {
	path, err := m.leasePath(childID)
	if err != nil {
		return WorktreeLease{}, err
	}
	if err := m.ValidateLeasePath(path); err != nil {
		return WorktreeLease{}, err
	}
	if !refRe.MatchString(baseHead) {
		return WorktreeLease{}, fmt.Errorf("%w: base head %q invalid", ErrWorktreeEscaped, baseHead)
	}
	if _, statErr := os.Stat(path); statErr == nil {
		return WorktreeLease{}, fmt.Errorf("%w: %s", ErrLeaseExists, childID)
	} else if !os.IsNotExist(statErr) {
		return WorktreeLease{}, statErr
	}
	// git worktree add <path> -B <branch> <baseHead> — the only command
	// that creates filesystem state outside the main tree's .git dir, and
	// it lands strictly under the leases root. -B (not -b) resets a branch
	// left behind by a removed lease: re-creation must pin the new base,
	// never fail on the stale branch name.
	if _, err := m.Exec.runOk(ctx, m.Repo, []string{"worktree", "add", path, "-B", branchFor(childID), baseHead}); err != nil {
		return WorktreeLease{}, err
	}
	return WorktreeLease{
		ChildID: childID, PathRef: path, BaseHead: baseHead,
		BranchRef: branchFor(childID), WorkerID: workerID,
		ExpiresAt: time.Now().UTC().Add(ttl),
	}, nil
}

// Lease re-reads the lease state for a child (existence + containment).
func (m *WorktreeManager) Lease(childID string) (WorktreeLease, error) {
	path, err := m.leasePath(childID)
	if err != nil {
		return WorktreeLease{}, err
	}
	if err := m.ValidateLeasePath(path); err != nil {
		return WorktreeLease{}, err
	}
	if _, statErr := os.Stat(path); statErr != nil {
		return WorktreeLease{}, fmt.Errorf("%w: %s", ErrWorktreeMissing, childID)
	}
	return WorktreeLease{ChildID: childID, PathRef: path, BranchRef: branchFor(childID)}, nil
}

// ExportPatch freezes the child's committed changes as a PatchManifest:
// one canonical binary diff from the pinned base to the child HEAD. This
// is the only outbound channel for child work.
func (m *WorktreeManager) ExportPatch(ctx context.Context, lease WorktreeLease) (PatchManifest, error) {
	if err := m.ValidateLeasePath(lease.PathRef); err != nil {
		return PatchManifest{}, err
	}
	if !refRe.MatchString(lease.BaseHead) {
		return PatchManifest{}, fmt.Errorf("%w: base head %q invalid", ErrWorktreeEscaped, lease.BaseHead)
	}
	// --no-ext-diff is load-bearing: an external diff driver set anywhere
	// (even as an empty value, which git counts as "set") would make git
	// spawn a command per file; this flag guarantees the in-process diff
	// machinery regardless of any config the tree carries.
	out, err := m.Exec.runOk(ctx, lease.PathRef, []string{"diff", "--no-ext-diff", "--binary", lease.BaseHead + "..HEAD"})
	if err != nil {
		return PatchManifest{}, err
	}
	sum := sha256.Sum256([]byte(out.Stdout))
	return PatchManifest{
		ChildID: lease.ChildID,
		Digest:  hex.EncodeToString(sum[:]),
		Size:    int64(len(out.Stdout)),
		Bytes:   []byte(out.Stdout),
	}, nil
}

// RemoveLease tears a child worktree down. The main tree is untouched.
func (m *WorktreeManager) RemoveLease(ctx context.Context, childID string) error {
	path, err := m.leasePath(childID)
	if err != nil {
		return err
	}
	if err := m.ValidateLeasePath(path); err != nil {
		return err
	}
	if _, statErr := os.Stat(path); os.IsNotExist(statErr) {
		return nil
	}
	_, err = m.Exec.runOk(ctx, m.Repo, []string{"worktree", "remove", "--force", path})
	return err
}

// ListLeases returns the child ids currently holding a worktree.
func (m *WorktreeManager) ListLeases(ctx context.Context) ([]string, error) {
	out, err := m.Exec.runOk(ctx, m.Repo, []string{"worktree", "list", "--porcelain"})
	if err != nil {
		return nil, err
	}
	var children []string
	for _, line := range strings.Split(out.Stdout, "\n") {
		line = strings.TrimRight(line, "\r")
		if !strings.HasPrefix(line, "worktree ") {
			continue
		}
		path := strings.TrimSpace(strings.TrimPrefix(line, "worktree "))
		rel, err := filepath.Rel(m.LeasesRoot, path)
		if err != nil || rel == "." || rel == "" || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
			continue // main tree or foreign path — not a child lease
		}
		if childIDRe.MatchString(rel) {
			children = append(children, rel)
		}
	}
	return children, nil
}

// VerifyPatch re-derives the manifest digest over the given bytes and
// pins it against the digest an intent carries.
func VerifyPatch(manifest PatchManifest, pinnedDigest string) error {
	sum := sha256.Sum256(manifest.Bytes)
	got := hex.EncodeToString(sum[:])
	if got != pinnedDigest || manifest.Digest != pinnedDigest {
		return fmt.Errorf("%w: manifest %s pinned %s", ErrPatchDigestMismatch, got, pinnedDigest)
	}
	return nil
}

// ApplyToRoot is the Root-Writer side operation (called by the writer
// walk, never by child machinery): apply one patch to the main tree and
// commit it under a marker carrying the intent id. `git apply --check`
// first makes the whole operation idempotent; the marker in the commit
// subject is what crash recovery reads back.
func (m *WorktreeManager) ApplyToRoot(ctx context.Context, intentID string, patch []byte, expectedHead string) (newHead string, err error) {
	if !childIDRe.MatchString(intentID) && !isULIDLike(intentID) {
		return "", fmt.Errorf("merge: intent id %q invalid", intentID)
	}
	// Bail out when the patch is already in (recovery re-run): apply
	// --check failing with "already applied" style output means the
	// effect landed; the caller reconciles via HeadIntent instead of
	// double-applying.
	check, err := m.Exec.runStdin(ctx, m.Repo, []string{"apply", "--check", "-"}, patch)
	if err != nil {
		return "", err
	}
	if check.ExitCode != 0 {
		if strings.Contains(check.Stderr, "already applied") {
			return m.Exec.Head(ctx, m.Repo)
		}
		return "", fmt.Errorf("merge: patch does not apply: %s", strings.TrimSpace(check.Stderr))
	}
	if applied, err := m.Exec.runStdin(ctx, m.Repo, []string{"apply", "-"}, patch); err != nil {
		return "", err
	} else if applied.ExitCode != 0 {
		return "", fmt.Errorf("merge: patch apply failed: %s", strings.TrimSpace(applied.Stderr))
	}
	msg := markerPrefix + intentID
	if _, err := m.Exec.runOk(ctx, m.Repo, []string{"commit", "-m", msg, "--allow-empty"}); err != nil {
		return "", err
	}
	return m.Exec.Head(ctx, m.Repo)
}

// HeadIntent reads the intent id stamped into the HEAD commit of a tree,
// or "" when HEAD carries no marker. Crash recovery compares this against
// the applying intent to decide merged-vs-retry.
func (m *WorktreeManager) HeadIntent(ctx context.Context, dir string) (string, error) {
	out, err := m.Exec.runOk(ctx, dir, []string{"log", "-1", "--format=%s"})
	if err != nil {
		return "", err
	}
	subject := strings.TrimRight(strings.TrimSpace(out.Stdout), "\r")
	if !strings.HasPrefix(subject, markerPrefix) {
		return "", nil
	}
	id := strings.TrimPrefix(subject, markerPrefix)
	if !isULIDLike(id) && !childIDRe.MatchString(id) {
		return "", nil
	}
	return id, nil
}

func isULIDLike(s string) bool {
	if len(s) != 26 {
		return false
	}
	for _, r := range s {
		if !((r >= '0' && r <= '9') || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z')) {
			return false
		}
	}
	return true
}

// GitExec is the hardened git channel private to this package. Commands
// are fixed templates from code; hooks, external diffs and credential
// helpers are disabled and the environment is scrubbed on every call.
type GitExec struct {
	GitPath string
	// EmptyHooksDir is an existing empty directory pinned as
	// core.hooksPath so repository/user hooks can never run; created on
	// demand under the system temp dir.
	EmptyHooksDir string
	// EnvAllowlist carries only these variables plus forced overrides.
	EnvAllowlist []string
}

// NewGitExec builds the channel; gitPath falls back to "git".
func NewGitExec(gitPath string) *GitExec {
	if gitPath == "" {
		gitPath = "git"
	}
	return &GitExec{
		GitPath: gitPath,
		EnvAllowlist: []string{"PATH", "SystemRoot", "windir", "TEMP", "TMP",
			"USERPROFILE", "HOME", "LANG", "LC_ALL", "HOMEDRIVE", "HOMEPATH",
			"LOCALAPPDATA", "APPDATA", "ProgramFiles", "SystemDrive"},
	}
}

// EnsureEmptyHooksDir idempotently provisions the pinned empty hooks dir.
func (g *GitExec) EnsureEmptyHooksDir() error {
	if g.EmptyHooksDir == "" {
		dir, err := os.MkdirTemp("", "lunitide-hooks-")
		if err != nil {
			return err
		}
		g.EmptyHooksDir = dir
	}
	return os.MkdirAll(g.EmptyHooksDir, 0o755)
}

// GitOutput is one completed invocation (a non-zero exit is a result, not
// a transport error).
type GitOutput struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// runOk is run for commands whose only good outcome is exit 0: a non-zero
// exit is surfaced as an error (with stderr) instead of a silent empty
// result. Commands that treat non-zero exits as data (apply --check)
// keep using run/runStdin and inspect ExitCode themselves.
func (g *GitExec) runOk(ctx context.Context, dir string, args []string) (GitOutput, error) {
	out, err := g.runStdin(ctx, dir, args, nil)
	if err != nil {
		return out, err
	}
	if out.ExitCode != 0 {
		return out, fmt.Errorf("git %s: exit %d: %s", args[0], out.ExitCode, strings.TrimSpace(out.Stderr))
	}
	return out, nil
}

func (g *GitExec) runStdin(ctx context.Context, dir string, args []string, stdin []byte) (GitOutput, error) {
	if err := g.EnsureEmptyHooksDir(); err != nil {
		return GitOutput{}, err
	}
	full := append([]string{
		"-c", "core.hooksPath=" + g.EmptyHooksDir,
		"-c", "credential.helper=",
		"-c", "core.askPass=",
		"-c", "filter.disabled.required=true",
		"-c", "user.name=Lunitide Root Writer",
		"-c", "user.email=root-writer@lunitide.local",
		"-c", "commit.gpgsign=false",
	}, args...)
	cmd := exec.CommandContext(ctx, g.GitPath, full...)
	cmd.Dir = dir
	cmd.Env = g.scrubbedEnv()
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	err := cmd.Run()
	code := 0
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			code = ee.ExitCode()
			err = nil
		}
	}
	return GitOutput{Stdout: out.String(), Stderr: errBuf.String(), ExitCode: code}, err
}

func (g *GitExec) scrubbedEnv() []string {
	var env []string
	for _, key := range g.EnvAllowlist {
		if v, ok := os.LookupEnv(key); ok {
			env = append(env, key+"="+v)
		}
	}
	env = append(env,
		"GIT_TERMINAL_PROMPT=0",
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL="+os.DevNull,
		"GIT_ALLOW_PROTOCOL=file",
		"GIT_ASKPASS=",
		"SSH_ASKPASS=",
	)
	return env
}
