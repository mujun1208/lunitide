// Package vcs: M5 T-5.2.3 Git allowlist. Only status/diff/add/restore/
// commit/branch run, always parameterised (argv array, no shell), with
// hooks, external diffs and credential helpers disabled and a scrubbed
// environment. Anything outside the allowlist answers GIT-001 together
// with the allowed list so the agent can self-correct.
package vcs

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strings"

	"github.com/lunitide/lunitide/internal/workspace"
)

// ErrNotAllowed is GIT-001: subcommand or argument outside the allowlist.
var ErrNotAllowed = errors.New("vcs: git operation outside allowlist (GIT-001)")

// AllowedSubcommands is the frozen M5 allowlist plus the P1-4 worktree
// isolation surface (add/list/remove only).
var AllowedSubcommands = []string{"add", "branch", "commit", "diff", "restore", "status", "worktree"}

// allowedFlags is the per-subcommand flag whitelist. A flag argument is
// accepted when its name part (before '=') matches exactly, or matches a
// whitelisted prefix ending in '*' (e.g. "-U*" accepts -U3).
var allowedFlags = map[string]map[string]bool{
	"status":  set("--short", "-s", "--porcelain", "--branch", "-b", "--untracked-files*", "-u*", "--"),
	"diff":    set("--stat", "--name-only", "--name-status", "--cached", "--staged", "-U*", "--unified*", "-m", "--"),
	"add":     set("-A", "--all", "-u", "--update", "-f", "--force", "-n", "--dry-run", "--"),
	"restore": set("--staged", "--worktree", "-s", "--source", "--", "-S", "--patch"),
	"commit":  set("-m*", "--message*", "--amend", "--no-edit", "--allow-empty", "--"),
	"branch":  set("-d", "--delete", "-D", "-m", "--move", "--list", "-a", "--all", "--show-current"),
}

func set(items ...string) map[string]bool {
	m := make(map[string]bool, len(items))
	for _, i := range items {
		m[i] = true
	}
	return m
}

// nonFlagPolicy says how bare arguments are validated per subcommand.
type nonFlagPolicy int

const (
	nonFlagNone nonFlagPolicy = iota // commit: everything rides on flags
	nonFlagPathspec                  // add/restore: workspace-relative pathspecs
	nonFlagRefOrPath                 // status/diff: read-only refs or pathspecs
	nonFlagBranchName                // branch: ref names
)

var nonFlag = map[string]nonFlagPolicy{
	"status":  nonFlagRefOrPath,
	"diff":    nonFlagRefOrPath,
	"add":     nonFlagPathspec,
	"restore": nonFlagPathspec,
	"commit":  nonFlagNone,
	"branch":  nonFlagBranchName,
}

// valueFlags are flags that consume the next argument as their value; the
// value then follows the flag's own validation instead of the bare-arg
// policy (commit -m <message>, restore -s <ref>).
var valueFlags = map[string]map[string]string{
	"commit":  {"-m": "message", "--message": "message"},
	"restore": {"-s": "ref", "--source": "ref"},
}

var refOrPathRe = regexp.MustCompile(`^[A-Za-z0-9_./@~^+=-]+$`)
var branchNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]*$`)

// ValidateArgv validates a full git argv (after the git binary) against the
// allowlist without executing anything. The error carries the allowed list.
func ValidateArgv(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("%w: empty argv; allowed: %s", ErrNotAllowed, strings.Join(AllowedSubcommands, ","))
	}
	sub := strings.ToLower(args[0])
	// P1-4: worktree has its own positional grammar (action then path/ref),
	// so it bypasses the generic flag/positional machinery.
	if sub == "worktree" {
		return validateWorktreeArgs(args[1:])
	}
	flags, ok := allowedFlags[sub]
	if !ok {
		return fmt.Errorf("%w: subcommand %q not allowed; allowed: %s", ErrNotAllowed, args[0], strings.Join(AllowedSubcommands, ","))
	}
	policy := nonFlag[sub]
	valueKinds := valueFlags[sub]
	expectKind := ""
	for _, arg := range args[1:] {
		if arg == "" {
			return fmt.Errorf("%w: empty argument", ErrNotAllowed)
		}
		if expectKind != "" {
			// This argument is the value of the previous flag.
			if err := validateFlagValue(expectKind, arg); err != nil {
				return err
			}
			expectKind = ""
			continue
		}
		if strings.HasPrefix(arg, "-") {
			if arg == "-" {
				return fmt.Errorf("%w: stdin arg not allowed", ErrNotAllowed)
			}
			name := arg
			hasValue := false
			if i := strings.IndexByte(arg, '='); i >= 0 {
				name = arg[:i]
				hasValue = true
			}
			if !flagAllowed(flags, name) {
				return fmt.Errorf("%w: flag %q not allowed for %s", ErrNotAllowed, arg, sub)
			}
			if kind, takes := valueKinds[name]; takes && !hasValue {
				expectKind = kind
			}
			continue
		}
		switch policy {
		case nonFlagNone:
			return fmt.Errorf("%w: %s takes no bare argument %q", ErrNotAllowed, sub, arg)
		case nonFlagPathspec:
			// "." is the conventional whole-directory pathspec.
			if arg != "." {
				if err := workspace.ValidateRelPath(arg); err != nil {
					return fmt.Errorf("%w: pathspec %q escapes workspace", ErrNotAllowed, arg)
				}
			}
		case nonFlagRefOrPath:
			if !refOrPathRe.MatchString(arg) || strings.Contains(arg, "..") || strings.HasPrefix(arg, "/") {
				return fmt.Errorf("%w: ref/pathspec %q rejected", ErrNotAllowed, arg)
			}
		case nonFlagBranchName:
			if !branchNameRe.MatchString(arg) || strings.Contains(arg, "..") || strings.HasSuffix(arg, ".lock") || strings.HasSuffix(arg, "/") || strings.HasSuffix(arg, ".") {
				return fmt.Errorf("%w: branch name %q rejected", ErrNotAllowed, arg)
			}
		}
	}
	if expectKind != "" {
		return fmt.Errorf("%w: flag value missing", ErrNotAllowed)
	}
	return nil
}

// validateFlagValue checks the consumed value of a value-flag.
func validateFlagValue(kind, val string) error {
	switch kind {
	case "message":
		if val == "" || len(val) > 4096 || strings.ContainsRune(val, 0) {
			return fmt.Errorf("%w: commit message invalid", ErrNotAllowed)
		}
	case "ref":
		if !refOrPathRe.MatchString(val) || strings.Contains(val, "..") || strings.HasPrefix(val, "/") {
			return fmt.Errorf("%w: ref %q rejected", ErrNotAllowed, val)
		}
	}
	return nil
}

// validateWorktreeArgs validates the P1-4 worktree isolation surface:
// worktree add <workspace-rel-path> [ref] [-b <branch>] [--detach],
// worktree list [--porcelain] [path],
// worktree remove [--force] <workspace-rel-path>.
// Worktree paths stay workspace-relative (no .., no absolute, no "."), so
// an isolated working tree can never be planted outside the controlled
// workspace.
func validateWorktreeArgs(args []string) error {
	bad := func(reason string) error {
		return fmt.Errorf("%w: worktree %s; allowed: worktree add <path> [ref] [-b <branch>] [--detach] | list [--porcelain] | remove [--force] <path>", ErrNotAllowed, reason)
	}
	if len(args) == 0 {
		return bad("requires add|list|remove")
	}
	action := strings.ToLower(args[0])
	if action != "add" && action != "list" && action != "remove" {
		return bad(fmt.Sprintf("action %q not allowed", args[0]))
	}
	okPath := func(p string) bool {
		return p != "." && !strings.HasPrefix(p, "/") && !strings.Contains(p, "\\") && !strings.Contains(p, "..") && workspace.ValidateRelPath(p) == nil
	}
	okRef := func(r string) bool {
		return refOrPathRe.MatchString(r) && !strings.Contains(r, "..") && !strings.HasPrefix(r, "/")
	}
	okBranch := func(b string) bool {
		return branchNameRe.MatchString(b) && !strings.Contains(b, "..") && !strings.HasSuffix(b, ".lock") && !strings.HasSuffix(b, "/") && !strings.HasSuffix(b, ".")
	}
	var positionals []string
	expectBranch := false
	for _, arg := range args[1:] {
		if arg == "" {
			return bad("empty argument")
		}
		if expectBranch {
			// -b value: a new branch name for the isolated worktree.
			if !okBranch(arg) {
				return bad(fmt.Sprintf("branch name %q rejected", arg))
			}
			expectBranch = false
			continue
		}
		if strings.HasPrefix(arg, "-") && arg != "-" {
			name := arg
			hasValue := false
			if i := strings.IndexByte(arg, '='); i >= 0 {
				name = arg[:i]
				hasValue = true
			}
			switch {
			case name == "--detach" && action == "add":
			case name == "--porcelain" && action == "list":
			case name == "--force" && action == "remove":
			case name == "-b" && action == "add":
				if !hasValue {
					expectBranch = true
					continue
				}
				branch := strings.TrimPrefix(arg, "-b=")
				if !okBranch(branch) {
					return bad(fmt.Sprintf("branch name %q rejected", branch))
				}
			default:
				return bad(fmt.Sprintf("flag %q not allowed for worktree %s", arg, action))
			}
			continue
		}
		positionals = append(positionals, arg)
	}
	if expectBranch {
		return bad("branch name missing")
	}
	switch action {
	case "add":
		if len(positionals) < 1 || len(positionals) > 2 {
			return bad("add takes one workspace-relative path and an optional ref")
		}
		if !okPath(positionals[0]) {
			return bad(fmt.Sprintf("worktree path %q escapes workspace", positionals[0]))
		}
		if len(positionals) == 2 && !okRef(positionals[1]) {
			return bad(fmt.Sprintf("ref %q rejected", positionals[1]))
		}
	case "list":
		if len(positionals) > 1 {
			return bad("list takes at most one path")
		}
		if len(positionals) == 1 && !okPath(positionals[0]) {
			return bad(fmt.Sprintf("path %q escapes workspace", positionals[0]))
		}
	case "remove":
		if len(positionals) != 1 {
			return bad("remove takes exactly one path")
		}
		if !okPath(positionals[0]) {
			return bad(fmt.Sprintf("worktree path %q escapes workspace", positionals[0]))
		}
	}
	return nil
}

func flagAllowed(flags map[string]bool, name string) bool {
	if flags[name] {
		return true
	}
	for pattern := range flags {
		if strings.HasSuffix(pattern, "*") && strings.HasPrefix(name, strings.TrimSuffix(pattern, "*")) {
			return true
		}
	}
	return false
}

// GitResult is one completed git invocation.
type GitResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// Runner executes allowlisted git commands inside one repository root.
type Runner struct {
	GitPath string
	// EmptyHooksDir is an existing empty directory pinned as core.hooksPath
	// so repository/user hooks can never run. Created on demand.
	EmptyHooksDir string
	// Env carries only these variables plus the forced git overrides.
	EnvAllowlist []string
}

func NewRunner(gitPath, emptyHooksDir string) *Runner {
	return &Runner{
		GitPath:       gitPath,
		EmptyHooksDir: emptyHooksDir,
		EnvAllowlist:  []string{"PATH", "SystemRoot", "windir", "TEMP", "TMP", "USERPROFILE", "HOME", "LANG", "LC_ALL", "HOMEDRIVE", "HOMEPATH", "LOCALAPPDATA", "APPDATA", "ProgramFiles", "SystemDrive"},
	}
}

// Run validates then executes git args in repoDir. Remote-touching or hook
// executing configurations are overridden; the environment is rebuilt from
// the allowlist so GIT_DIR/GIT_INDEX_FILE style injections cannot redirect
// the repository.
func (r *Runner) Run(ctx context.Context, repoDir string, args []string, stdin []byte) (GitResult, error) {
	if err := ValidateArgv(args); err != nil {
		return GitResult{}, err
	}
	full := append([]string{
		"-c", "core.hooksPath=" + r.EmptyHooksDir,
		"-c", "diff.external=",
		"-c", "credential.helper=",
		"-c", "core.askPass=",
		"-c", "filter.disabled.required=true",
		"-c", "user.name=Lunitide Agent",
		"-c", "user.email=agent@lunitide.local",
		"-c", "commit.gpgsign=false",
	}, args...)
	cmd := exec.CommandContext(ctx, r.GitPath, full...)
	cmd.Dir = repoDir
	cmd.Env = r.scrubbedEnv()
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
			err = nil // git's own non-zero exit is a valid result
		}
	}
	return GitResult{Stdout: out.String(), Stderr: errBuf.String(), ExitCode: code}, err
}

// scrubbedEnv rebuilds the process environment from the allowlist and then
// forces the git hardening overrides on top.
func (r *Runner) scrubbedEnv() []string {
	var env []string
	for _, key := range r.EnvAllowlist {
		if v, ok := os.LookupEnv(key); ok {
			env = append(env, key+"="+v)
		}
	}
	nullDev := os.DevNull
	env = append(env,
		"GIT_TERMINAL_PROMPT=0",
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL="+nullDev,
		"GIT_ALLOW_PROTOCOL=file",
		"GIT_ASKPASS=",
		"SSH_ASKPASS=",
		"GIT_REDIRECT_STDERR=0",
	)
	return env
}

// EnsureEmptyHooksDir creates (idempotently) the pinned empty hooks dir.
func (r *Runner) EnsureEmptyHooksDir() error {
	if r.EmptyHooksDir == "" {
		return errors.New("vcs: empty hooks dir not configured")
	}
	return os.MkdirAll(r.EmptyHooksDir, 0o755)
}

// SortFlags exposes the union of allowed flags for diagnostics (GIT-001
// responses embed the allowed list).
func AllowedFlagsFor(sub string) []string {
	flags := allowedFlags[sub]
	out := make([]string, 0, len(flags))
	for f := range flags {
		out = append(out, f)
	}
	sort.Strings(out)
	return out
}
