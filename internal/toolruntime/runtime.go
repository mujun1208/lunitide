package toolruntime

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/lunitide/lunitide/internal/canonpath"
	"github.com/lunitide/lunitide/internal/ccapp"
	"github.com/lunitide/lunitide/internal/htmlapp"
	"github.com/lunitide/lunitide/internal/jsonutil"
	"github.com/lunitide/lunitide/internal/networkpolicy"
	"github.com/lunitide/lunitide/internal/officetools"
	"github.com/lunitide/lunitide/internal/webfetch"
	_ "modernc.org/sqlite"
)

type Mode string

const (
	Approval   Mode = "approval"
	AutoEdit   Mode = "auto-edit"
	Plan       Mode = "plan"
	FullAccess Mode = "full-access"
)
const maxFile = 1 << 20

var ErrApprovalRequired = errors.New("approval required")

type Runtime struct {
	root string
	db   *sql.DB
	now  func() time.Time
	// fetchWeb is the SSRF-pinned web transport injected by the host
	// (cmd/engine). nil keeps web.* tools unavailable (tests, offline).
	fetchWeb func(ctx context.Context, rawURL string) (networkpolicy.FetchResult, error)
	// fullAccessRoot resolves the user-selected workspace root (workspace-root.json,
	// chosen via the host workspace picker). In full-access mode file tools
	// read/write inside that root; every other mode stays sandboxed to
	// <root>/<session>. nil or a resolver failure falls back to the sandbox.
	fullAccessRoot func() (string, error)
	// sessionStorageRoot overrides the per-session sandbox parent when the
	// user configures a conversations directory in General settings.
	sessionStorageRoot func() (string, error)
	// rulesMu guards commandRules and fullDisk for hot reload
	// (SetCommandPolicyJSON swaps both; Execute copies under RLock).
	rulesMu      sync.RWMutex
	commandRules []commandRule
	// fullDisk is the user opt-in "full-disk full-access" switch persisted in
	// command-policy.json. When true, full-access mode accepts absolute paths
	// on any drive for file tools and runs commands without the allowlist.
	fullDisk      bool
	userRulesPath string
	// hooksMu guards hookRules for hot reload (SetHooksPolicyJSON).
	hooksMu        sync.RWMutex
	hookRules      []hookRule
	hooksRulesPath string
	// auditMu makes the lazy ensureAudit open exactly one SQLite handle
	// even under concurrent callers.
	auditMu sync.Mutex
	// P2-1 FTS workspace-search index: wsFTSReady flips true once the
	// trigram virtual table exists on this handle; wsIdxMu/wsRootMu
	// serialize per-root index refresh against concurrent searches.
	wsFTSReady bool
	wsIdxMu    sync.Mutex
	wsRootMu   map[string]*sync.Mutex
	// ccExec runs the cc.* computer-control tools through the ccapp
	// service (injected by the host; nil keeps them unavailable).
	ccExec func(ctx context.Context, session, tool string, args json.RawMessage, approved bool) (ccapp.Outcome, error)
	imSend func(ctx context.Context, kind, to, text string) (desktopApp, output string, err error)
}
type Result struct {
	Output     string    `json:"output"`
	Digest     string    `json:"digest"`
	Artifact   *Artifact `json:"artifact,omitempty"`
	VisionMIME string    `json:"-"`
	VisionData []byte    `json:"-"`
}

type Artifact struct {
	Kind    string `json:"kind"`
	Path    string `json:"path"`
	Content string `json:"content"`
}

func New(root string) (*Runtime, error) {
	if !filepath.IsAbs(root) {
		return nil, errors.New("runtime root must be absolute")
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		return nil, err
	}
	// Pinned in the operating system's own spelling, because every
	// containment check below compares a resolved child against it.
	real, err := canonpath.Canonical(root)
	if err != nil {
		return nil, err
	}
	r := &Runtime{root: filepath.Clean(real), now: func() time.Time { return time.Now().UTC() }}
	r.commandRules = builtinCommandRules()
	r.userRulesPath = filepath.Join(r.root, "command-policy.json")
	r.hooksRulesPath = filepath.Join(r.root, "hooks-policy.json")
	return r, nil
}

// commandRule is one allowlist entry: an argv prefix plus how many total
// argv items the invocation may carry and the deadline granted to it.
type commandRule struct {
	prefix   []string
	maxArgs  int
	deadline time.Duration
}

const (
	commandDeadlineDefault = 10 * time.Second
	commandDeadlineMin     = time.Second
	commandDeadlineMax     = 5 * time.Minute
	commandMaxArgv         = 16
	// toolProgressMaxChunks bounds how many incremental output chunks one
	// command.run invocation may push to the live stream (P1-2). Chunks
	// beyond the cap still land in the final combined result; only the
	// live feed stops, keeping the event pipe flood-safe.
	toolProgressMaxChunks = 40
)

// builtinCommandRules is the fixed read-only observation set. git runs only
// through --no-pager explicit flags (pagers/filters disabled both via the
// flag and the sanitized environment set in runCommand).
func builtinCommandRules() []commandRule {
	return []commandRule{
		{prefix: []string{"go", "version"}, maxArgs: 2, deadline: 10 * time.Second},
		{prefix: []string{"git", "--no-pager", "status"}, maxArgs: 4, deadline: 10 * time.Second},
		{prefix: []string{"git", "--no-pager", "log"}, maxArgs: 8, deadline: 10 * time.Second},
		{prefix: []string{"git", "--no-pager", "diff"}, maxArgs: 6, deadline: 10 * time.Second},
		{prefix: []string{"git", "--no-pager", "show"}, maxArgs: 6, deadline: 10 * time.Second},
		{prefix: []string{"git", "--no-pager", "branch"}, maxArgs: 4, deadline: 10 * time.Second},
	}
}

// loadUserCommandPolicy merges the optional user whitelist file
// (<root>/command-policy.json). A present-but-invalid file fails closed so
// the operator notices instead of running with a half-applied policy.
func (r *Runtime) loadUserCommandPolicy() error {
	raw, err := os.ReadFile(r.userRulesPath)
	if err != nil {
		if os.IsNotExist(err) {
			// Missing file: fail closed. Full-disk is an explicit Settings
			// opt-in, never the implicit default of a fresh data root.
			r.rulesMu.Lock()
			r.fullDisk = false
			r.rulesMu.Unlock()
			return nil
		}
		return err
	}
	userRules, err := buildUserRules(raw)
	if err != nil {
		return err
	}
	var doc userPolicyDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		return err
	}
	rules := builtinCommandRules()
	rules = append(rules, userRules...)
	r.rulesMu.Lock()
	r.commandRules = rules
	r.fullDisk = doc.FullAccess
	r.rulesMu.Unlock()
	return nil
}

// userPolicyDoc is the command-policy.json wire shape shared by load, get
// and set.
type userPolicyDoc struct {
	Commands []struct {
		Prefix    []string `json:"prefix"`
		MaxArgs   int      `json:"maxArgs,omitempty"`
		TimeoutMS int64    `json:"timeoutMs,omitempty"`
	} `json:"commands"`
	// FullAccess is the opt-in "full-disk full-access" switch: with it on,
	// full-access mode runs any command and accepts absolute paths on any
	// drive. Off keeps the whitelist plus workspace-root confinement.
	FullAccess bool `json:"fullAccess,omitempty"`
}

// buildUserRules validates one whitelist document and renders it into
// concrete rules without touching runtime state (build-then-swap keeps a
// rejected document from half-applying).
func buildUserRules(raw []byte) ([]commandRule, error) {
	var doc userPolicyDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("command-policy.json: %w", err)
	}
	if len(doc.Commands) > 128 {
		return nil, errors.New("command-policy.json: more than 128 commands")
	}
	rules := make([]commandRule, 0, len(doc.Commands))
	for _, c := range doc.Commands {
		if len(c.Prefix) < 1 || len(c.Prefix) > 8 {
			return nil, errors.New("command-policy.json: prefix must have 1-8 items")
		}
		for _, item := range c.Prefix {
			if item == "" || strings.Contains(item, "..") || strings.HasPrefix(item, "/") || strings.HasPrefix(item, `\`) || len(item) > 2 && item[1] == ':' {
				return nil, errors.New("command-policy.json: invalid prefix item")
			}
		}
		maxArgs := c.MaxArgs
		if maxArgs <= 0 {
			maxArgs = len(c.Prefix)
		}
		if maxArgs > commandMaxArgv {
			maxArgs = commandMaxArgv
		}
		deadline := time.Duration(c.TimeoutMS) * time.Millisecond
		if deadline <= 0 {
			deadline = commandDeadlineDefault
		}
		if deadline > commandDeadlineMax {
			deadline = commandDeadlineMax
		}
		rules = append(rules, commandRule{prefix: c.Prefix, maxArgs: maxArgs, deadline: deadline})
	}
	return rules, nil
}

// CommandPolicyJSON answers the persisted user whitelist document
// ({"commands":[]} when the file does not exist yet).
func (r *Runtime) CommandPolicyJSON() ([]byte, error) {
	raw, err := os.ReadFile(r.userRulesPath)
	if err != nil {
		if os.IsNotExist(err) {
			return []byte(`{"commands":[]}`), nil
		}
		return nil, err
	}
	if !json.Valid(raw) {
		return nil, errors.New("command-policy.json: stored document is not valid JSON")
	}
	return raw, nil
}

// SetCommandPolicyJSON validates, atomically persists and hot-applies a
// new user whitelist. An invalid document is refused without touching the
// file or the live rules.
func (r *Runtime) SetCommandPolicyJSON(raw []byte) error {
	if len(raw) > 64<<10 {
		return errors.New("command-policy.json: document exceeds 64 KiB")
	}
	if !json.Valid(raw) {
		return errors.New("command-policy.json: document is not valid JSON")
	}
	userRules, err := buildUserRules(raw)
	if err != nil {
		return err
	}
	var doc userPolicyDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		return err
	}
	tmp := r.userRulesPath + ".tmp"
	if err := os.WriteFile(tmp, raw, 0600); err != nil {
		return err
	}
	// os.Rename on Windows refuses to replace an existing destination.
	_ = os.Remove(r.userRulesPath)
	if err := os.Rename(tmp, r.userRulesPath); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	rules := builtinCommandRules()
	rules = append(rules, userRules...)
	r.rulesMu.Lock()
	r.commandRules = rules
	r.fullDisk = doc.FullAccess
	r.rulesMu.Unlock()
	return nil
}

// FullDiskEnabled answers whether the user opted into full-disk full-access
// (command-policy.json "fullAccess": true).
func (r *Runtime) FullDiskEnabled() bool {
	r.rulesMu.RLock()
	defer r.rulesMu.RUnlock()
	return r.fullDisk
}

// SetWebFetcher installs the SSRF-pinned fetch transport for web.* tools.
func (r *Runtime) SetWebFetcher(f func(ctx context.Context, rawURL string) (networkpolicy.FetchResult, error)) {
	r.fetchWeb = f
}

// SetCcExecutor installs the computer-control executor backing the cc.*
// agent tools (ccapp.Service.ExecuteTool).
func (r *Runtime) SetCcExecutor(f func(ctx context.Context, session, tool string, args json.RawMessage, approved bool) (ccapp.Outcome, error)) {
	r.ccExec = f
}

func (r *Runtime) SetIMSend(f func(ctx context.Context, kind, to, text string) (desktopApp, output string, err error)) {
	r.imSend = f
}

// SetFullAccessRootResolver installs the user-workspace root resolver used by
// full-access mode. The resolver is consulted per call so a changed root
// selection takes effect immediately; failures fall back to the sandbox.
func (r *Runtime) SetFullAccessRootResolver(f func() (string, error)) {
	r.fullAccessRoot = f
}

func (r *Runtime) SetSessionStorageRoot(f func() (string, error)) { r.sessionStorageRoot = f }

func (r *Runtime) effectiveSessionsRoot() string {
	if r.sessionStorageRoot != nil {
		if root, err := r.sessionStorageRoot(); err == nil && root != "" {
			return root
		}
	}
	return r.root
}

func (r *Runtime) SessionFolder(session string) (string, error) { return r.sessionRoot(session) }

// effectiveRoot returns the directory file tools operate in for this call.
// Full-access rides the user-selected workspace root when one resolves;
// everything else (and any resolver failure) keeps the per-session sandbox.
func (r *Runtime) effectiveRoot(mode Mode, session string) (string, error) {
	if mode == FullAccess && r.fullAccessRoot != nil {
		if root, err := r.fullAccessRoot(); err == nil && root != "" {
			if info, statErr := os.Lstat(root); statErr == nil && info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
				return root, nil
			}
		}
	}
	return r.sessionPath(session)
}

// FullAccessRootHint answers the currently resolvable user workspace root.
// Used to tell the model where file tools actually operate.
func (r *Runtime) FullAccessRootHint() (string, bool) {
	if r.fullAccessRoot == nil {
		return "", false
	}
	root, err := r.fullAccessRoot()
	if err != nil || root == "" {
		return "", false
	}
	if info, statErr := os.Lstat(root); statErr != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", false
	}
	return root, true
}
func (r *Runtime) ensureAudit() error {
	r.auditMu.Lock()
	defer r.auditMu.Unlock()
	if r.db != nil {
		return nil
	}
	db, err := sql.Open("sqlite", filepath.Join(r.root, ".tool-runtime.sqlite"))
	if err != nil {
		return err
	}
	if _, err = db.Exec(`PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000; CREATE TABLE IF NOT EXISTS chat_tool_calls(
		id INTEGER PRIMARY KEY, session_id TEXT NOT NULL, run_id TEXT NOT NULL, call_id TEXT NOT NULL,
		tool_name TEXT NOT NULL, args_json BLOB NOT NULL, args_digest TEXT NOT NULL, execution_mode TEXT NOT NULL,
		workspace_digest TEXT NOT NULL, decision TEXT NOT NULL DEFAULT '', status TEXT NOT NULL,
		result_digest TEXT NOT NULL DEFAULT '', summary TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL,
		decided_at TEXT NOT NULL DEFAULT '', completed_at TEXT NOT NULL DEFAULT '', expires_at TEXT NOT NULL,
		UNIQUE(session_id, call_id, args_digest));
		CREATE TABLE IF NOT EXISTS chat_tool_approval_rules(
			id INTEGER PRIMARY KEY, session_id TEXT NOT NULL, tool_name TEXT NOT NULL,
			args_digest TEXT NOT NULL, scope TEXT NOT NULL, created_at TEXT NOT NULL,
			UNIQUE(session_id, tool_name, args_digest));
		CREATE TABLE IF NOT EXISTS chat_tool_hook_events(
		id INTEGER PRIMARY KEY, session_id TEXT NOT NULL, tool_name TEXT NOT NULL,
		hook_id TEXT NOT NULL, event TEXT NOT NULL, decision TEXT NOT NULL DEFAULT '',
		args_digest TEXT NOT NULL DEFAULT '', result_digest TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL);
	CREATE INDEX IF NOT EXISTS ix_chat_tool_calls_status ON chat_tool_calls(status, expires_at);
	CREATE INDEX IF NOT EXISTS ix_tool_approval_rules_match ON chat_tool_approval_rules(tool_name, args_digest, scope);
	CREATE INDEX IF NOT EXISTS ix_chat_tool_hook_events_session ON chat_tool_hook_events(session_id, id);`); err != nil {
		db.Close()
		return err
	}
	r.db = db
	return nil
}
func Open(root string) (*Runtime, error) {
	r, err := New(root)
	if err != nil {
		return nil, err
	}
	if err = r.ensureAudit(); err != nil {
		return nil, err
	}
	if err = r.loadUserCommandPolicy(); err != nil {
		_ = r.Close()
		return nil, err
	}
	if err = r.loadUserHooksPolicy(); err != nil {
		_ = r.Close()
		return nil, err
	}
	return r, nil
}
func (r *Runtime) Close() error {
	if r.db == nil {
		return nil
	}
	return r.db.Close()
}
func Digest(name string, args json.RawMessage) string {
	args = jsonutil.Repair(args)
	var v any
	if json.Unmarshal(args, &v) != nil {
		return ""
	}
	canonical, _ := json.Marshal(v)
	h := sha256.Sum256(append(append([]byte(name), 0), canonical...))
	return hex.EncodeToString(h[:])
}
func (r *Runtime) sessionPath(session string) (string, error) {
	if len(session) != 26 || strings.ContainsAny(session, "/\\") {
		return "", errors.New("invalid session")
	}
	return filepath.Join(r.effectiveSessionsRoot(), session), nil
}
func (r *Runtime) sessionRoot(session string) (string, error) {
	p, err := r.sessionPath(session)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(p, 0700); err != nil {
		return "", err
	}
	return p, nil
}

type Pending struct {
	SessionID, RunID, CallID, ToolName, ArgsDigest, Mode, WorkspaceDigest string
	ExpiresAt                                                             time.Time
}

var ErrPendingConsumed = errors.New("pending action already consumed")
var ErrWorkspaceChanged = errors.New("workspace changed since approval request")

func (r *Runtime) workspaceDigest(mode Mode, session string) (string, error) {
	// Full-access over a user-selected root skips the tree walk: the root
	// can be huge and the digest only guards pending approvals against
	// workspace swaps, so hashing the resolved root path is enough.
	if mode == FullAccess && r.fullAccessRoot != nil {
		if root, err := r.fullAccessRoot(); err == nil && root != "" {
			h := sha256.New()
			h.Write([]byte("full-access:" + filepath.Clean(root)))
			return hex.EncodeToString(h.Sum(nil)), nil
		}
	}
	root, err := r.sessionPath(session)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	if _, err = os.Stat(root); os.IsNotExist(err) {
		return hex.EncodeToString(h.Sum(nil)), nil
	} else if err != nil {
		return "", err
	}
	var paths []string
	err = filepath.Walk(root, func(path string, info os.FileInfo, e error) error {
		if e != nil {
			return e
		}
		if path == root || info.IsDir() || strings.HasPrefix(info.Name(), ".tool-runtime.sqlite") {
			return nil
		}
		rel, e := filepath.Rel(root, path)
		if e != nil {
			return e
		}
		paths = append(paths, rel)
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(paths)
	for _, rel := range paths {
		b, e := os.ReadFile(filepath.Join(root, rel))
		if e != nil {
			return "", e
		}
		fmt.Fprintf(h, "%s\x00%d\x00", filepath.ToSlash(rel), len(b))
		h.Write(b)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
func canonicalArgs(args json.RawMessage) ([]byte, error) {
	var v any
	if err := json.Unmarshal(args, &v); err != nil {
		return nil, err
	}
	return json.Marshal(v)
}

func (r *Runtime) Prepare(ctx context.Context, runID, session, callID, name string, args json.RawMessage, mode Mode, ttl time.Duration) (Pending, error) {
	if err := r.ensureAudit(); err != nil {
		return Pending{}, err
	}
	if ttl <= 0 || ttl > 30*time.Minute {
		ttl = 10 * time.Minute
	}
	canonical, err := canonicalArgs(args)
	if err != nil {
		return Pending{}, err
	}
	digest := Digest(name, canonical)
	if digest == "" {
		return Pending{}, errors.New("invalid tool arguments")
	}
	if _, err = r.Execute(ctx, mode, session, name, canonical, false); !errors.Is(err, ErrApprovalRequired) {
		if err == nil {
			return Pending{}, errors.New("tool does not require approval")
		}
		return Pending{}, err
	}
	wd, err := r.workspaceDigest(mode, session)
	if err != nil {
		return Pending{}, err
	}
	now := r.now()
	exp := now.Add(ttl)
	_, err = r.db.ExecContext(ctx, `INSERT INTO chat_tool_calls(session_id,run_id,call_id,tool_name,args_json,args_digest,execution_mode,workspace_digest,status,created_at,expires_at) VALUES(?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(session_id,call_id,args_digest) DO NOTHING`, session, runID, callID, name, canonical, digest, string(mode), wd, "pending", now.Format(time.RFC3339Nano), exp.Format(time.RFC3339Nano))
	if err != nil {
		return Pending{}, err
	}
	var p Pending
	var expires string
	err = r.db.QueryRowContext(ctx, `SELECT session_id,run_id,call_id,tool_name,args_digest,execution_mode,workspace_digest,expires_at FROM chat_tool_calls WHERE session_id=? AND call_id=? AND args_digest=? AND status='pending'`, session, callID, digest).Scan(&p.SessionID, &p.RunID, &p.CallID, &p.ToolName, &p.ArgsDigest, &p.Mode, &p.WorkspaceDigest, &expires)
	if err != nil {
		return Pending{}, ErrPendingConsumed
	}
	p.ExpiresAt, _ = time.Parse(time.RFC3339Nano, expires)
	return p, nil
}

func (r *Runtime) Decide(ctx context.Context, session, callID, digest string, approve bool) (Result, error) {
	return r.DecideScoped(ctx, session, callID, digest, approve, ApprovalScopeOnce)
}

// Approval remember scopes (P1-5). "session" auto-approves the exact
// (tool, canonical args) pair for the rest of one session; "always"
// persists the rule across sessions. Matching stays exact-digest, so a
// remembered rule never widens to argument variants that were not
// approved.
const (
	ApprovalScopeOnce    = "once"
	ApprovalScopeSession = "session"
	ApprovalScopeAlways  = "always"
)

// ApprovalScopeValid reports whether scope is one of the frozen values.
func ApprovalScopeValid(scope string) bool {
	return scope == ApprovalScopeOnce || scope == ApprovalScopeSession || scope == ApprovalScopeAlways
}

// DecideScoped is Decide with a remember scope. Approving with
// session/always records an exact (tool, args digest) auto-approve rule;
// rejecting never records anything.
func (r *Runtime) DecideScoped(ctx context.Context, session, callID, digest string, approve bool, scope string) (Result, error) {
	if !ApprovalScopeValid(scope) {
		return Result{}, errors.New("invalid approval scope")
	}
	out, err := r.decide(ctx, session, callID, digest, approve)
	if err != nil {
		return out, err
	}
	if approve && scope != ApprovalScopeOnce {
		var name, raw string
		if e := r.db.QueryRowContext(ctx, `SELECT tool_name,args_json FROM chat_tool_calls WHERE session_id=? AND call_id=? AND args_digest=?`, session, callID, digest).Scan(&name, &raw); e == nil && name != userAskTool {
			canonical, ce := canonicalArgs(json.RawMessage(raw))
			if ce == nil {
				if d := Digest(name, canonical); d != "" {
					_ = r.rememberApproval(ctx, session, name, d, scope)
				}
			}
		}
	}
	return out, nil
}

// rememberApproval upserts one auto-approve rule. Re-approving the same
// pair at a different scope upgrades it (session → always).
func (r *Runtime) rememberApproval(ctx context.Context, session, name, digest, scope string) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO chat_tool_approval_rules(session_id,tool_name,args_digest,scope,created_at) VALUES(?,?,?,?,?)
		ON CONFLICT(session_id,tool_name,args_digest) DO UPDATE SET scope=excluded.scope`, session, name, digest, scope, r.now().Format(time.RFC3339Nano))
	return err
}

// approvalRemembered reports whether an exact (tool, canonical args)
// auto-approve rule exists: a session rule bound to this session, or an
// always rule from any session.
func (r *Runtime) approvalRemembered(ctx context.Context, session, name, digest string) bool {
	if r.db == nil {
		return false
	}
	var one int
	err := r.db.QueryRowContext(ctx, `SELECT 1 FROM chat_tool_approval_rules WHERE tool_name=? AND args_digest=? AND (scope=? OR (scope=? AND session_id=?)) LIMIT 1`, name, digest, ApprovalScopeAlways, ApprovalScopeSession, session).Scan(&one)
	return err == nil
}

func (r *Runtime) decide(ctx context.Context, session, callID, digest string, approve bool) (Result, error) {
	if err := r.ensureAudit(); err != nil {
		return Result{}, err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Result{}, err
	}
	defer tx.Rollback()
	var name, raw, mode, wd, expires string
	err = tx.QueryRowContext(ctx, `SELECT tool_name,args_json,execution_mode,workspace_digest,expires_at FROM chat_tool_calls WHERE session_id=? AND call_id=? AND args_digest=? AND status='pending'`, session, callID, digest).Scan(&name, &raw, &mode, &wd, &expires)
	if err != nil {
		return Result{}, ErrPendingConsumed
	}
	exp, _ := time.Parse(time.RFC3339Nano, expires)
	now := r.now()
	if !now.Before(exp) {
		_, _ = tx.ExecContext(ctx, `UPDATE chat_tool_calls SET status='failed',summary='approval expired',completed_at=? WHERE session_id=? AND call_id=? AND args_digest=? AND status='pending'`, now.Format(time.RFC3339Nano), session, callID, digest)
		_ = tx.Commit()
		return Result{}, errors.New("approval expired")
	}
	decision, status := "rejected", "rejected"
	if approve {
		decision, status = "approved", "approved"
	}
	res, err := tx.ExecContext(ctx, `UPDATE chat_tool_calls SET decision=?,status=?,decided_at=? WHERE session_id=? AND call_id=? AND args_digest=? AND status='pending'`, decision, status, now.Format(time.RFC3339Nano), session, callID, digest)
	if err != nil {
		return Result{}, err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return Result{}, ErrPendingConsumed
	}
	if err = tx.Commit(); err != nil {
		return Result{}, err
	}
	if !approve {
		return result("rejected by user"), nil
	}
	current, e := r.workspaceDigest(Mode(mode), session)
	if e != nil {
		return r.finishDecision(ctx, session, callID, digest, Result{}, e)
	}
	if current != wd {
		return r.finishDecision(ctx, session, callID, digest, Result{}, ErrWorkspaceChanged)
	}
	out, e := r.Execute(ctx, Mode(mode), session, name, json.RawMessage(raw), true)
	return r.finishDecision(ctx, session, callID, digest, out, e)
}
func (r *Runtime) finishDecision(ctx context.Context, session, callID, digest string, out Result, runErr error) (Result, error) {
	status := "executed"
	summary := out.Output
	if runErr != nil {
		status = "failed"
		summary = runErr.Error()
	}
	if len(summary) > 4096 {
		summary = summary[:4096]
	}
	_, err := r.db.ExecContext(ctx, `UPDATE chat_tool_calls SET status=?,result_digest=?,summary=?,completed_at=? WHERE session_id=? AND call_id=? AND args_digest=? AND status='approved'`, status, out.Digest, summary, r.now().Format(time.RFC3339Nano), session, callID, digest)
	if err != nil {
		return Result{}, err
	}
	return out, runErr
}
func (r *Runtime) path(mode Mode, session, rel string, write, unconfined bool) (string, error) {
	// Full-disk opt-in lifts the confinement for user conversations: absolute
	// paths on any drive resolve to themselves (still cleaned and
	// length-bounded), so the model can touch Desktop, other drives and any
	// user-writable location. Subagent paths pass unconfined=false and stay
	// confined to the workspace root.
	if unconfined && r.FullDiskEnabled() && rel != "" && (filepath.IsAbs(rel) || filepath.VolumeName(rel) != "") {
		clean := filepath.Clean(rel)
		if len(clean) > 4096 || strings.ContainsRune(clean, 0) {
			return "", errors.New("invalid path")
		}
		if write {
			if err := os.MkdirAll(filepath.Dir(clean), 0700); err != nil {
				return "", err
			}
		}
		return clean, nil
	}
	if rel == "" || filepath.IsAbs(rel) || filepath.VolumeName(rel) != "" {
		return "", errors.New("relative path required")
	}
	clean := filepath.Clean(rel)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
		return "", errors.New("path traversal")
	}
	root, err := r.effectiveRoot(mode, session)
	if err != nil {
		return "", err
	}
	if write {
		if err = os.MkdirAll(root, 0700); err != nil {
			return "", err
		}
	} else if info, statErr := os.Stat(root); statErr != nil {
		return "", statErr
	} else if !info.IsDir() {
		return "", errors.New("session workspace is not a directory")
	}
	p := filepath.Join(root, clean)
	// The session root itself (path ".") is a valid read target: resolve
	// symlinks and confirm it stays a directory. It is never a writable
	// target (replacing the workspace root would destroy the sandbox).
	if clean == "." {
		if write {
			return "", errors.New("workspace root is not writable")
		}
		real, err := canonpath.Canonical(p)
		if err != nil {
			return "", err
		}
		return real, nil
	}
	parent := filepath.Dir(p)
	if write {
		if err = os.MkdirAll(parent, 0700); err != nil {
			return "", err
		}
	}
	realParent, err := canonpath.Canonical(parent)
	if err != nil {
		return "", err
	}
	relCheck, err := filepath.Rel(root, realParent)
	if err != nil || relCheck == ".." || strings.HasPrefix(relCheck, ".."+string(os.PathSeparator)) {
		return "", errors.New("path escape")
	}
	if !write {
		real, err := canonpath.Canonical(p)
		if err != nil {
			return "", err
		}
		relCheck, err = filepath.Rel(root, real)
		if err != nil || relCheck == ".." || strings.HasPrefix(relCheck, ".."+string(os.PathSeparator)) {
			return "", errors.New("symlink escape")
		}
		p = real
	}
	return p, nil
}

// Execute runs one tool call for the given conversation mode. Subagent and
// delegation paths must stay on this entry point: it never lifts the
// command allowlist or the path confinement, whatever the persisted policy
// says.
func (r *Runtime) Execute(ctx context.Context, mode Mode, session, name string, args json.RawMessage, approved bool) (out Result, err error) {
	return r.execute(ctx, mode, session, name, args, approved, false, nil)
}

// ExecuteStreaming runs one tool with an optional progress sink receiving
// bounded incremental output chunks while the tool runs (P1-2). Only
// command.run emits progress today; other tools simply complete as usual.
// progress may be called from background goroutines but is serialized by
// the runtime, so a non-concurrent-safe sink is fine.
func (r *Runtime) ExecuteStreaming(ctx context.Context, mode Mode, session, name string, args json.RawMessage, approved bool, progress func(chunk string)) (out Result, err error) {
	return r.execute(ctx, mode, session, name, args, approved, false, progress)
}

// ExecuteUnconfined is the user-conversation-only entry point that honors
// the full-disk opt-in: commands skip the allowlist and file tools accept
// absolute paths on any drive. It is reserved for chat tool calls made in
// full-access mode while command-policy.json has "fullAccess": true.
func (r *Runtime) ExecuteUnconfined(ctx context.Context, session, name string, args json.RawMessage, approved bool) (out Result, err error) {
	return r.execute(ctx, FullAccess, session, name, args, approved, true, nil)
}

// ExecuteUnconfinedStreaming is the full-disk variant of ExecuteStreaming.
func (r *Runtime) ExecuteUnconfinedStreaming(ctx context.Context, session, name string, args json.RawMessage, approved bool, progress func(chunk string)) (out Result, err error) {
	return r.execute(ctx, FullAccess, session, name, args, approved, true, progress)
}

// ccToolChangesMachine folds computer control into the mutating gate. The
// wrapper tools (desktop.type, media.play) were already listed, but a direct
// cc.* or computer.act call reached the desktop through ccapp alone, and ccapp
// only pauses for high/critical risk — a click is medium.
func ccToolChangesMachine(name string, args json.RawMessage) bool {
	if name == ccapp.ToolComputerAct {
		mapped, _, err := ccapp.MapComputerAct(args)
		if err != nil {
			// Fail closed: ccapp will refuse an unmappable payload anyway, and
			// assuming "harmless" is the wrong way to be wrong here.
			return true
		}
		return ccapp.ToolChangesMachine(mapped)
	}
	return ccapp.ToolChangesMachine(name)
}

func (r *Runtime) execute(ctx context.Context, mode Mode, session, name string, args json.RawMessage, approved, unconfined bool, progress func(chunk string)) (out Result, err error) {
	switch mode {
	case Approval, AutoEdit, Plan, FullAccess:
	default:
		return Result{}, errors.New("invalid execution mode")
	}
	if mode == Plan {
		return Result{}, errors.New("tools disabled in plan mode")
	}
	args = jsonutil.Repair(args)
	// P3-B hooks: evaluate beforeToolCall rules first (block > gate >
	// grant priority, fail-closed). A block refuses before anything else;
	// every matched rule leaves one audit row whatever the outcome.
	hooks := r.evaluateHooks(name)
	defer func() { r.recordHookEvents(ctx, session, name, Digest(name, args), out.Digest, hooks) }()
	if hooks.blockMessage != "" {
		return Result{}, fmt.Errorf("%w: %s", ErrHookBlocked, hooks.blockMessage)
	}
	if hooks.grantApproval && !approved && name != userAskTool {
		approved = true
	}
	mutating := name == "workspace.write" || name == "workspace.edit" || name == "command.run" || name == "desktop.open" || name == "desktop.type" || name == "media.play" || name == "im.send" || officeGenTools[name] || ccToolChangesMachine(name, args)
	if mutating && !approved && (hooks.forceApproval || mode == Approval || (name == "command.run" && mode == AutoEdit)) {
		// Remembered exact approvals (P1-5) satisfy the gate without a new
		// round-trip; unmatched or argument-variant calls still gate.
		if canonical, ce := canonicalArgs(args); ce == nil {
			if d := Digest(name, canonical); d != "" && r.approvalRemembered(ctx, session, name, d) {
				approved = true
			}
		}
		if !approved {
			return Result{}, ErrApprovalRequired
		}
	}
	switch name {
	case "workspace.list":
		var a struct {
			Path string `json:"path"`
		}
		if strict(args, &a) != nil {
			return Result{}, errors.New("invalid arguments")
		}
		if a.Path == "" {
			a.Path = "."
		}
		p, e := r.path(mode, session, a.Path, false, unconfined)
		if e != nil {
			return Result{}, e
		}
		entries, e := os.ReadDir(p)
		if e != nil {
			return Result{}, e
		}
		names := make([]string, 0, len(entries))
		for _, x := range entries {
			n := x.Name()
			if x.IsDir() {
				n += "/"
			}
			names = append(names, n)
		}
		sort.Strings(names)
		return result(strings.Join(names, "\n")), nil
	case "workspace.read":
		var a struct {
			Path string `json:"path"`
		}
		if strict(args, &a) != nil || a.Path == "" {
			return Result{}, errors.New("invalid arguments")
		}
		p, e := r.path(mode, session, a.Path, false, unconfined)
		if e != nil {
			return Result{}, e
		}
		f, e := os.Open(p)
		if e != nil {
			return Result{}, e
		}
		defer f.Close()
		b, e := io.ReadAll(io.LimitReader(f, maxFile+1))
		if e != nil || len(b) > maxFile {
			return Result{}, errors.New("file exceeds limit")
		}
		return result(string(b)), nil
	case "workspace.write":
		var a struct {
			Path    string `json:"path"`
			Content string `json:"content"`
		}
		if strict(args, &a) != nil || a.Path == "" || len(a.Content) > maxFile {
			return Result{}, errors.New("invalid arguments")
		}
		p, e := r.path(mode, session, a.Path, true, unconfined)
		if e != nil {
			return Result{}, e
		}
		tmp, e := os.CreateTemp(filepath.Dir(p), ".write-*")
		if e != nil {
			return Result{}, e
		}
		tn := tmp.Name()
		defer os.Remove(tn)
		if e = tmp.Chmod(0600); e == nil {
			_, e = tmp.WriteString(a.Content)
		}
		if e == nil {
			e = tmp.Sync()
		}
		ce := tmp.Close()
		if e == nil {
			e = ce
		}
		if e == nil {
			e = os.Rename(tn, p)
		}
		if e != nil {
			return Result{}, e
		}
		written := result("wrote " + a.Path)
		ext := strings.ToLower(filepath.Ext(a.Path))
		if ext == ".html" || ext == ".htm" {
			written.Artifact = &Artifact{Kind: "html", Path: htmlArtifactPath(a.Path, false), Content: a.Content}
		}
		return written, nil
	case "workspace.search":
		var a struct {
			Query string `json:"query"`
			Path  string `json:"path"`
			Regex bool   `json:"regex"`
			Max   int    `json:"max"`
		}
		if strict(args, &a) != nil || a.Query == "" || len(a.Query) > 512 {
			return Result{}, errors.New("invalid arguments")
		}
		if a.Path == "" {
			a.Path = "."
		}
		max := a.Max
		if max <= 0 {
			max = 50
		}
		if max > 200 {
			max = 200
		}
		hits, e := r.searchWorkspace(mode, session, a.Path, a.Query, a.Regex, max, unconfined)
		if e != nil {
			return Result{}, e
		}
		return result(strings.Join(hits, "\n")), nil
	case "workspace.edit":
		files, e := parseWorkspaceEditArgs(args)
		if e != nil {
			return Result{}, e
		}
		type pendingEdit struct {
			rel     string
			abs     string
			updated string
			count   int
		}
		pending := make([]pendingEdit, 0, len(files))
		total := 0
		for _, f := range files {
			p, pe := r.path(mode, session, f.Path, false, unconfined)
			if pe != nil {
				return Result{}, pe
			}
			b, re := os.ReadFile(p)
			if re != nil || len(b) > maxFile {
				return Result{}, errors.New("file missing or exceeds limit")
			}
			updated, count, ae := applyWorkspaceHunks(string(b), f.Hunks)
			if ae != nil {
				if len(files) > 1 {
					return Result{}, fmt.Errorf("%s: %v", f.Path, ae)
				}
				return Result{}, ae
			}
			if len(updated) > maxFile {
				return Result{}, errors.New("edited file exceeds limit")
			}
			pending = append(pending, pendingEdit{rel: f.Path, abs: p, updated: updated, count: count})
			total += count
		}
		for _, item := range pending {
			if we := writeFileReplace(item.abs, item.updated); we != nil {
				return Result{}, we
			}
		}
		if len(pending) == 1 {
			return result(fmt.Sprintf("edited %s (%d replacement(s))", pending[0].rel, pending[0].count)), nil
		}
		names := make([]string, 0, len(pending))
		for _, item := range pending {
			names = append(names, item.rel)
		}
		return result(fmt.Sprintf("edited %d files (%d replacement(s)): %s", len(pending), total, strings.Join(names, ", "))), nil
	case "todo.write":
		var a struct {
			Todos []struct {
				Content  string `json:"content"`
				Status   string `json:"status"`
				Priority string `json:"priority"`
			} `json:"todos"`
		}
		if strict(args, &a) != nil {
			return Result{}, errors.New("invalid arguments")
		}
		rendered, e := r.writeTodos(session, a.Todos)
		if e != nil {
			return Result{}, e
		}
		return result(rendered), nil
	case userAskTool:
		return executeUserAsk(args, approved)
	case "command.run":
		if mode != FullAccess && !(approved && (mode == Approval || mode == AutoEdit)) {
			return Result{}, errors.New("command denied")
		}
		var a struct {
			Argv []string `json:"argv"`
		}
		if strict(args, &a) != nil || len(a.Argv) == 0 || len(a.Argv) > commandMaxArgv {
			return Result{}, errors.New("command denied")
		}
		// Checked before every mode branch below, because those branches are
		// all reachable without a human in the loop: a companion voice turn
		// upgrades itself to full access, and full-disk lifts the allowlist
		// entirely. This floor has no opt-out.
		if reason := hardlineRefusal(a.Argv); reason != "" {
			return Result{}, commandFailure("refused, this cannot be undone (" + reason + "). Run it yourself in a terminal if you really mean it.")
		}
		// Full-disk opt-in lifts the whitelist for user conversations that
		// came in through ExecuteUnconfined: any argv runs with the max
		// deadline; every other path keeps matching the built-in plus user
		// allowlist.
		var deadline time.Duration
		if unconfined && r.FullDiskEnabled() {
			deadline = commandDeadlineMax
		} else {
			r.rulesMu.RLock()
			rules := r.commandRules
			r.rulesMu.RUnlock()
			rule, ok := matchCommandRule(rules, a.Argv)
			if !ok {
				return Result{}, errors.New("command denied")
			}
			deadline = rule.deadline
		}
		root, e := r.effectiveRoot(mode, session)
		if e != nil {
			return Result{}, e
		}
		if e = os.MkdirAll(root, 0700); e != nil {
			return Result{}, e
		}
		if dir, ok := extractMkdirPath(a.Argv); ok {
			dir = expandWindowsEnv(dir)
			if dir == "" {
				return Result{}, commandFailure("empty directory path")
			}
			if !filepath.IsAbs(dir) {
				dir = filepath.Join(root, dir)
			}
			if e = os.MkdirAll(dir, 0755); e != nil {
				return Result{}, commandFailure(e.Error())
			}
			return result(formatCommandOutput(true, "created directory: "+dir)), nil
		}
		argv, cleanup, wrapErr := prepareCommandArgv(a.Argv)
		if wrapErr != nil {
			return Result{}, commandFailure(wrapErr.Error())
		}
		defer cleanup()
		cctx, cancel := context.WithTimeout(ctx, deadline)
		defer cancel()
		cmd := exec.CommandContext(cctx, argv[0], argv[1:]...)
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "GIT_PAGER=cat", "PAGER=cat", "TERM=dumb", "GIT_OPTIONAL_LOCKS=0", "PYTHONIOENCODING=utf-8", "PYTHONUTF8=1")
		// P1-2: with a progress sink the pipes are read live so long
		// running commands stream bounded stdout/stderr chunks to the
		// caller instead of black-boxing until exit. The final result
		// keeps the legacy combined-output shape (64 KiB cap, error text
		// carried in the failure message); line scanning only normalizes
		// CRLF tails away.
		if progress != nil {
			stdoutPipe, e := cmd.StdoutPipe()
			if e != nil {
				return Result{}, commandFailure(e.Error())
			}
			stderrPipe, e := cmd.StderrPipe()
			if e != nil {
				return Result{}, commandFailure(e.Error())
			}
			if e = cmd.Start(); e != nil {
				return Result{}, commandFailure(e.Error())
			}
			var mu sync.Mutex
			var combined []byte
			emitted := 0
			scan := func(r io.Reader, done chan<- struct{}) {
				sc := bufio.NewScanner(r)
				sc.Buffer(make([]byte, 0, 64*1024), 256*1024)
				for sc.Scan() {
					line := decodeCommandOutput(sc.Bytes())
					mu.Lock()
					if len(combined) < 64<<10 {
						combined = append(combined, line...)
						combined = append(combined, '\n')
					}
					emit := emitted < toolProgressMaxChunks
					if emit {
						emitted++
						// Hold the lock across progress so stdout/stderr
						// scanners cannot interleave send() and race the
						// chat stream sequence cursor.
						progress(truncateRunes(line, 400))
					}
					mu.Unlock()
				}
				done <- struct{}{}
			}
			doneOut, doneErr := make(chan struct{}), make(chan struct{})
			go scan(stdoutPipe, doneOut)
			go scan(stderrPipe, doneErr)
			<-doneOut
			<-doneErr
			waitErr := cmd.Wait()
			out := combined
			if len(out) > 64<<10 {
				out = out[:64<<10]
			}
			text := decodeCommandOutput(out)
			if waitErr != nil {
				return Result{}, commandFailure(text)
			}
			return result(formatCommandOutput(true, text)), nil
		}
		out, e := cmd.CombinedOutput()
		if len(out) > 64<<10 {
			out = out[:64<<10]
		}
		text := decodeCommandOutput(out)
		if e != nil {
			return Result{}, commandFailure(text)
		}
		return result(formatCommandOutput(true, text)), nil
	case "web.fetch":
		var a struct {
			URL string `json:"url"`
		}
		if strict(args, &a) != nil || a.URL == "" || len(a.URL) > 2048 {
			return Result{}, errors.New("invalid arguments")
		}
		if r.fetchWeb == nil {
			return Result{}, errors.New("web tools unavailable")
		}
		page, e := r.fetchWeb(ctx, a.URL)
		if e != nil {
			return Result{}, e
		}
		extracted, ok := webfetch.ExtractText(page.ContentType, page.Body, webfetch.MaxTextBytes)
		if !ok {
			return Result{}, fmt.Errorf("unsupported content type: %s", page.ContentType)
		}
		var b strings.Builder
		if extracted.Title != "" {
			b.WriteString("title: " + extracted.Title + "\n")
		}
		b.WriteString("url: " + page.FinalURL + "\n")
		if extracted.Truncated || page.Truncated {
			b.WriteString("note: content truncated\n")
		}
		b.WriteString("\n" + extracted.Text)
		out := result(truncateRunes(b.String(), 12000))
		preview := extracted.Text
		if len(preview) > 24<<10 {
			preview = preview[:24<<10]
		}
		title := extracted.Title
		// Path must end in .html — the desktop host strips any other
		// artifact (including https:// URLs) before it reaches the
		// renderer, which left the browser tab on an empty placeholder.
		out.Artifact = &Artifact{Kind: "html", Path: "fetch.html", Content: webfetch.RenderExtractHTML(title, page.FinalURL, preview)}
		return out, nil
	case "web.search":
		var a struct {
			Query string `json:"query"`
			Max   int    `json:"max"`
		}
		if strict(args, &a) != nil || strings.TrimSpace(a.Query) == "" || len(a.Query) > 512 {
			return Result{}, errors.New("invalid arguments")
		}
		if r.fetchWeb == nil {
			return Result{}, errors.New("web tools unavailable")
		}
		max := a.Max
		if max <= 0 {
			max = 5
		}
		if max > 10 {
			max = 10
		}
		results, source, pageURL, e := r.searchWeb(ctx, a.Query, max)
		if e != nil {
			return Result{}, e
		}
		if pageURL == "" {
			pageURL = webfetch.BingCNSearchURL(a.Query)
		}
		var b strings.Builder
		b.WriteString("query: " + a.Query + "\n")
		if source != "" && source != "none" {
			b.WriteString("source: " + source + "\n")
		}
		b.WriteString("results_url: " + pageURL + "\n")
		if len(results) == 0 {
			b.WriteString("no results\n")
		}
		for i, hit := range results {
			fmt.Fprintf(&b, "\n%d. %s\n   %s\n", i+1, hit.Title, hit.URL)
			if hit.Snippet != "" {
				b.WriteString("   " + hit.Snippet + "\n")
			}
		}
		out := result(b.String())
		out.Artifact = &Artifact{Kind: "html", Path: "search.html", Content: webfetch.RenderSearchHTML(a.Query, results)}
		return out, nil
	case "excel.gen":
		var a struct {
			Path    string                  `json:"path"`
			Desktop bool                    `json:"desktop"`
			Sheets  []officetools.SheetSpec `json:"sheets"`
		}
		if strict(args, &a) != nil || (a.Path == "" && !a.Desktop) {
			return Result{}, errors.New("invalid arguments")
		}
		if !a.Desktop && strings.ToLower(filepath.Ext(a.Path)) != ".xlsx" {
			return Result{}, errors.New("excel.gen path must end with .xlsx")
		}
		outPath, e := r.desktopWritePath(a.Path, "workbook.xlsx", ".xlsx", a.Desktop, unconfined)
		if e != nil {
			return Result{}, e
		}
		data, e := officetools.GenXLSX(a.Sheets)
		if e != nil {
			return Result{}, e
		}
		return r.finishOfficeGen(mode, session, outPath, data, len(a.Sheets), unconfined, a.Desktop, "workbook.xlsx")
	case "excel.parse":
		var a struct {
			Path string `json:"path"`
		}
		if strict(args, &a) != nil || a.Path == "" {
			return Result{}, errors.New("invalid arguments")
		}
		p, e := r.path(mode, session, a.Path, false, unconfined)
		if e != nil {
			return Result{}, e
		}
		b, e := os.ReadFile(p)
		if e != nil || len(b) > maxGeneratedBytes {
			return Result{}, errors.New("file missing or exceeds limit")
		}
		summary, e := officetools.ParseXLSX(b)
		if e != nil {
			return Result{}, e
		}
		return result(summary), nil
	case "docx.gen":
		var a struct {
			Path     string                  `json:"path"`
			Desktop  bool                    `json:"desktop"`
			Title    string                  `json:"title"`
			Subtitle string                  `json:"subtitle"`
			Author   string                  `json:"author"`
			Kind     string                  `json:"kind"`
			Blocks   []officetools.DocxBlock `json:"blocks"`
		}
		if strict(args, &a) != nil || (a.Path == "" && !a.Desktop) {
			return Result{}, errors.New("invalid arguments")
		}
		if !a.Desktop && strings.ToLower(filepath.Ext(a.Path)) != ".docx" {
			return Result{}, errors.New("docx.gen path must end with .docx")
		}
		outPath, e := r.desktopWritePath(a.Path, "document.docx", ".docx", a.Desktop, unconfined)
		if e != nil {
			return Result{}, e
		}
		data, e := officetools.GenDocxDoc(officetools.DocxDoc{
			Title: a.Title, Subtitle: a.Subtitle, Author: a.Author, Kind: a.Kind, Blocks: a.Blocks,
		})
		if e != nil {
			return Result{}, e
		}
		return r.finishOfficeGen(mode, session, outPath, data, len(a.Blocks), unconfined, a.Desktop, "document.docx")
	case "pptx.gen":
		var a struct {
			Path    string                  `json:"path"`
			Desktop bool                    `json:"desktop"`
			Title   string                  `json:"title"`
			Slides  []officetools.SlideSpec `json:"slides"`
		}
		if strict(args, &a) != nil || (a.Path == "" && !a.Desktop) {
			return Result{}, errors.New("invalid arguments")
		}
		if !a.Desktop && strings.ToLower(filepath.Ext(a.Path)) != ".pptx" {
			return Result{}, errors.New("pptx.gen path must end with .pptx")
		}
		outPath, e := r.desktopWritePath(a.Path, "deck.pptx", ".pptx", a.Desktop, unconfined)
		if e != nil {
			return Result{}, e
		}
		data, e := officetools.GenPptx(a.Title, a.Slides)
		if e != nil {
			return Result{}, e
		}
		return r.finishOfficeGen(mode, session, outPath, data, len(a.Slides), unconfined, a.Desktop, "deck.pptx")
	case "html.gen":
		var a struct {
			Path     string `json:"path"`
			Title    string `json:"title"`
			Template string `json:"template"`
			Desktop  bool   `json:"desktop"`
		}
		if strict(args, &a) != nil {
			return Result{}, errors.New("invalid arguments")
		}
		if strings.TrimSpace(a.Template) == "" {
			a.Template = "penalty-shootout"
		}
		page, e := htmlapp.Render(a.Template, a.Title)
		if e != nil {
			return Result{}, e
		}
		if !a.Desktop && strings.TrimSpace(a.Path) == "" {
			switch a.Template {
			case "timer":
				a.Path = "timer.html"
			case "checklist":
				a.Path = "checklist.html"
			default:
				a.Path = "penalty-shootout.html"
			}
		}
		fallbackHTML := "世界杯点球大战.html"
		if a.Template == "timer" {
			fallbackHTML = "计时器.html"
		} else if a.Template == "checklist" {
			fallbackHTML = "清单.html"
		}
		outPath, de := r.desktopWritePath(a.Path, fallbackHTML, ".html", a.Desktop, unconfined)
		if de != nil {
			return Result{}, de
		}
		if ext := strings.ToLower(filepath.Ext(outPath)); ext != ".html" && ext != ".htm" {
			return Result{}, errors.New("html.gen path must end with .html")
		}
		written, e := r.writeGenerated(mode, session, outPath, []byte(page), -1, unconfined)
		if e != nil {
			return Result{}, e
		}
		written.Artifact = &Artifact{Kind: "html", Path: htmlArtifactPath(outPath, a.Desktop), Content: page}
		return written, nil
	case "desktop.open":
		var a struct {
			Name string `json:"name"`
		}
		if strict(args, &a) != nil || strings.TrimSpace(a.Name) == "" {
			return Result{}, errors.New("invalid arguments")
		}
		if err := requireDesktopAction(approved); err != nil {
			return Result{}, err
		}
		path, others, e := pickLaunchTarget(a.Name)
		if e != nil {
			if strings.Contains(e.Error(), "无法执行") {
				return Result{}, e
			}
			return Result{}, fmt.Errorf("无法执行：%v", e)
		}
		if path == "" {
			return Result{}, fmt.Errorf("无法执行：桌面上有多份匹配「%s」：%s。请说出完整文件名", strings.TrimSpace(a.Name), strings.Join(others, "、"))
		}
		if e = openWithDefaultApp(path); e != nil {
			return Result{}, fmt.Errorf("无法执行：打不开（%v）", e)
		}
		return result("opened " + path), nil
	case "desktop.type":
		invoke := func(ctx context.Context, session, tool string, args json.RawMessage, approved bool) (Result, error) {
			return r.runCcTool(ctx, mode, session, tool, args, approved, unconfined)
		}
		return executeDesktopType(ctx, invoke, session, args, approved, unconfined)
	case "media.play":
		invoke := func(ctx context.Context, session, tool string, args json.RawMessage, approved bool) (Result, error) {
			return r.runCcTool(ctx, mode, session, tool, args, approved, unconfined)
		}
		return executeMediaPlayWithCC(ctx, invoke, session, args, unconfined, approved)
	case "im.send":
		return r.executeIMSend(ctx, session, args, approved, unconfined)
	case "pdf.gen":
		var a struct {
			Path    string `json:"path"`
			Desktop bool   `json:"desktop"`
			Title   string `json:"title"`
			Body    string `json:"body"`
		}
		if strict(args, &a) != nil || (a.Path == "" && !a.Desktop) {
			return Result{}, errors.New("invalid arguments")
		}
		if !a.Desktop && strings.ToLower(filepath.Ext(a.Path)) != ".pdf" {
			return Result{}, errors.New("pdf.gen path must end with .pdf")
		}
		outPath, e := r.desktopWritePath(a.Path, "report.pdf", ".pdf", a.Desktop, unconfined)
		if e != nil {
			return Result{}, e
		}
		data, e := officetools.GenPDF(a.Title, a.Body)
		if e != nil {
			return Result{}, e
		}
		return r.finishOfficeGen(mode, session, outPath, data, -1, unconfined, a.Desktop, "report.pdf")
	case "cc.mouse_move", "cc.mouse_click", "cc.keyboard_type",
		"cc.keyboard_shortcut", "cc.screen_capture", "cc.get_active_window":
		return r.runCcTool(ctx, mode, session, name, args, approved, unconfined)
	default:
		if ccapp.IsCcTool(name) {
			return r.runCcTool(ctx, mode, session, name, args, approved, unconfined)
		}
		return Result{}, errors.New("unknown tool")
	}
}

// runCcTool executes one computer-control tool through the injected ccapp
// service. Plan mode never reaches here (tools are globally disabled); the
// ccapp confirmation gate maps onto the standard approval flow so
// high/critical operations pause for a manual decision.
func (r *Runtime) runCcTool(ctx context.Context, mode Mode, session, name string, args json.RawMessage, approved, unconfined bool) (Result, error) {
	if r.ccExec == nil {
		return Result{}, errors.New("computer control unavailable (M10-CC-010)")
	}
	outcome, err := r.ccExec(ctx, session, name, args, approved)
	if err != nil {
		if errors.Is(err, ccapp.ErrCcConfirmRequired) {
			return Result{}, ErrApprovalRequired
		}
		return Result{}, fmt.Errorf("%s: %v", ccapp.Code(err), err)
	}
	res := result(outcome.Summary)
	if len(outcome.CapturePNG) > 0 {
		rel := fmt.Sprintf("screen-capture-%s.png", r.now().UTC().Format("20060102T150405.000000000"))
		p, e := r.path(mode, session, rel, true, unconfined)
		if e != nil {
			return Result{}, e
		}
		if e = os.WriteFile(p, outcome.CapturePNG, 0600); e != nil {
			return Result{}, e
		}
		res.Artifact = &Artifact{Kind: "image", Path: rel}
		res.Output = fmt.Sprintf("%s (saved %s)", outcome.Summary, rel)
		if data, mime, ve := ccapp.PrepareVisionImage(outcome.CapturePNG); ve == nil && len(data) > 0 {
			res.VisionMIME = mime
			res.VisionData = data
		}
	}
	return res, nil
}

// officeGenTools are the P2-1 generators: they mutate the session
// workspace, so they ride the workspace.write approval class.
var officeGenTools = map[string]bool{
	"excel.gen": true, "docx.gen": true, "pptx.gen": true, "pdf.gen": true, "html.gen": true,
}

// htmlArtifactPath is the renderer-safe preview name. Host sanitizer
// rejects file:// URLs, backslashes and non-.html suffixes, so desktop
// writes still preview as desktop/basename.html.
func htmlArtifactPath(requested string, desktop bool) string {
	base := filepath.Base(filepath.Clean(strings.TrimSpace(requested)))
	base = strings.ReplaceAll(base, `\`, "")
	lower := strings.ToLower(base)
	if base == "" || base == "." || strings.Contains(base, "..") {
		base = "preview.html"
	} else if !strings.HasSuffix(lower, ".html") && !strings.HasSuffix(lower, ".htm") {
		base = "preview.html"
	}
	if desktop {
		return "desktop/" + base
	}
	return base
}

// ResolveSessionArtifact maps a renderer-safe artifact path to a confined
// on-disk target under the session folder or user Desktop (desktop/ prefix).
func (r *Runtime) ResolveSessionArtifact(sessionID, relPath string) (string, error) {
	relPath = strings.TrimSpace(relPath)
	if relPath == "" {
		return r.SessionFolder(sessionID)
	}
	clean := filepath.Clean(filepath.FromSlash(relPath))
	clean = strings.ReplaceAll(clean, `\`, "/")
	if strings.HasPrefix(clean, "desktop/") {
		base := strings.TrimPrefix(clean, "desktop/")
		if base == "" || base == "." {
			return userDesktopDir()
		}
		dir, err := userDesktopDir()
		if err != nil {
			return "", err
		}
		return r.containedRead(dir, base)
	}
	dir, err := r.SessionFolder(sessionID)
	if err != nil {
		return "", err
	}
	return r.containedRead(dir, clean)
}

// ReadWorkspaceFile reads up to max bytes of one contained session
// workspace file (binary-safe). Host-side preview surfaces (bridge
// workspace.officePreview) use it; the model-facing read path stays
// workspace.read.
func (r *Runtime) ReadWorkspaceFile(session, relPath string, max int64) ([]byte, error) {
	if max <= 0 || max > maxGeneratedBytes {
		max = maxGeneratedBytes
	}
	// Host-side previews do not know the execution mode that produced the
	// artifact, so try the session sandbox first and fall back to the
	// user-selected full-access root (read-only, same containment checks).
	p, err := r.path(Approval, session, relPath, false, false)
	if err != nil {
		return nil, err
	}
	if _, statErr := os.Stat(p); statErr != nil && r.fullAccessRoot != nil {
		if root, rootErr := r.fullAccessRoot(); rootErr == nil && root != "" {
			if alt, altErr := r.containedRead(root, relPath); altErr == nil {
				p = alt
			}
		}
	}
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, max))
}

// WorkspaceRoot answers the runtime root (host-side export surfaces).
func (r *Runtime) WorkspaceRoot() string { return r.root }

// containedRead resolves relPath inside an arbitrary root with the same
// traversal and symlink-escape discipline as path(); read-only.
func (r *Runtime) containedRead(root, relPath string) (string, error) {
	if relPath == "" || filepath.IsAbs(relPath) || filepath.VolumeName(relPath) != "" {
		return "", errors.New("relative path required")
	}
	clean := filepath.Clean(relPath)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
		return "", errors.New("path traversal")
	}
	// r.root is canonicalized once in New, but this root arrives from a
	// caller and has not been. Comparing a resolved child against an
	// unresolved root reports an escape for every file under a short-name
	// or redirected directory, so canonicalize both and compare like with
	// like.
	root, err := canonpath.Canonical(root)
	if err != nil {
		return "", err
	}
	p := filepath.Join(root, clean)
	real, err := canonpath.Canonical(p)
	if err != nil {
		return "", err
	}
	relCheck, err := filepath.Rel(root, real)
	if err != nil || relCheck == ".." || strings.HasPrefix(relCheck, ".."+string(os.PathSeparator)) {
		return "", errors.New("symlink escape")
	}
	return real, nil
}

// maxGeneratedBytes bounds both generated and parsed Office payloads.
const maxGeneratedBytes = 8 << 20

// finishOfficeGen writes generated office bytes and, for desktop=true,
// rewrites the artifact path to desktop/basename so the renderer can
// display a card (absolute C:\ paths are rejected as invalid stream events).
func (r *Runtime) finishOfficeGen(mode Mode, session, outPath string, data []byte, count int, unconfined, desktop bool, fallback string) (Result, error) {
	written, err := r.writeGenerated(mode, session, outPath, data, count, unconfined)
	if err != nil {
		return Result{}, err
	}
	if desktop && written.Artifact != nil {
		written.Artifact.Path = desktopPreviewPath(outPath, true, fallback)
	}
	return written, nil
}

// writeGenerated persists generated bytes into the session workspace with
// the same atomic temp+rename discipline as workspace.write.
func (r *Runtime) writeGenerated(mode Mode, session, relPath string, data []byte, count int, unconfined bool) (Result, error) {
	if len(data) > maxGeneratedBytes {
		return Result{}, errors.New("generated file exceeds limit")
	}
	p, e := r.path(mode, session, relPath, true, unconfined)
	if e != nil {
		return Result{}, e
	}
	tmp, e := os.CreateTemp(filepath.Dir(p), ".gen-*")
	if e != nil {
		return Result{}, e
	}
	tn := tmp.Name()
	defer os.Remove(tn)
	if e = tmp.Chmod(0600); e == nil {
		_, e = tmp.Write(data)
	}
	if e == nil {
		e = tmp.Sync()
	}
	ce := tmp.Close()
	if e == nil {
		e = ce
	}
	if e == nil {
		_ = os.Remove(p)
		e = os.Rename(tn, p)
	}
	if e != nil {
		return Result{}, e
	}
	out := result(fmt.Sprintf("generated %s (%d bytes)", relPath, len(data)))
	if count >= 0 {
		out.Output = fmt.Sprintf("generated %s (%d bytes, %d sections)", relPath, len(data), count)
	}
	// P2-2: surface the generated file as an artifact card. Office kinds
	// carry no inline content (binary payloads); preview goes through the
	// host-side workspace.artifact.preview method.
	switch strings.ToLower(filepath.Ext(relPath)) {
	case ".xlsx", ".docx", ".pptx", ".pdf":
		out.Artifact = &Artifact{Kind: strings.TrimPrefix(strings.ToLower(filepath.Ext(relPath)), "."), Path: filepath.ToSlash(relPath)}
	}
	return out, nil
}

// matchCommandRule finds the first allowlist entry whose prefix matches the
// argv head and whose total length stays within maxArgs. Longer argv lists
// are denied even when the prefix matches, so one entry cannot become a
// wildcard.
func matchCommandRule(rules []commandRule, a []string) (commandRule, bool) {
	if len(a) < 1 || len(a) > commandMaxArgv {
		return commandRule{}, false
	}
	for _, rule := range rules {
		if len(a) > rule.maxArgs || len(a) < len(rule.prefix) {
			continue
		}
		match := true
		for i := range rule.prefix {
			if a[i] != rule.prefix[i] {
				match = false
				break
			}
		}
		if match {
			return rule, true
		}
	}
	return commandRule{}, false
}

// truncateRunes bounds tool output on a rune boundary.
func truncateRunes(s string, limit int) string {
	if limit <= 0 || len(s) <= limit {
		return s
	}
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	return string(runes[:limit])
}

// searchWorkspace walks one contained session workspace subtree and
// answers "path:line: text" hits for a literal or regex query. Binary
// files (NUL byte in the first 8 KiB) and oversized files are skipped;
// the walk stops as soon as max hits accumulate.
func (r *Runtime) searchWorkspace(mode Mode, session, relPath, query string, regex bool, max int, unconfined bool) ([]string, error) {
	root, err := r.path(mode, session, relPath, false, unconfined)
	if err != nil {
		return nil, err
	}
	var re *regexp.Regexp
	if regex {
		re, err = regexp.Compile(query)
		if err != nil {
			return nil, fmt.Errorf("invalid regex: %w", err)
		}
	}
	// P2-1 fast path: literal queries of 3+ runes ride the trigram index;
	// any index-side miss or error falls back to the linear scan below.
	if re == nil && wsFTSEligible(query, false) {
		if fts, ok, ferr := r.searchFTS(root, query, max); ferr == nil && ok {
			return fts, nil
		}
	}
	return searchLinear(root, re, query, max)
}

// searchLinear is the original scan: walk the subtree, read each
// candidate file and match line by line (regex or literal substring).
func searchLinear(root string, re *regexp.Regexp, query string, max int) ([]string, error) {
	var hits []string
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, e error) error {
		if e != nil || d.IsDir() || len(hits) >= max {
			if e != nil {
				return e
			}
			return nil
		}
		rel, e2 := filepath.Rel(root, p)
		if e2 != nil {
			return e2
		}
		if rel == "." {
			return nil
		}
		b, e2 := os.ReadFile(p)
		if e2 != nil || len(b) > maxFile {
			return nil
		}
		probe := b
		if len(probe) > 8192 {
			probe = probe[:8192]
		}
		if bytes.IndexByte(probe, 0) >= 0 {
			return nil
		}
		for i, line := range strings.Split(string(b), "\n") {
			if len(hits) >= max {
				return nil
			}
			if len(line) > 400 {
				line = truncateRunes(line, 400)
			}
			matched := false
			if re != nil {
				matched = re.MatchString(line)
			} else {
				matched = strings.Contains(line, query)
			}
			if matched {
				hits = append(hits, fmt.Sprintf("%s:%d: %s", filepath.ToSlash(rel), i+1, strings.TrimRight(line, "\r")))
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(hits) == 0 {
		return []string{"no matches"}, nil
	}
	return hits, nil
}

// todoItem is one checklist entry persisted per session.
type todoItem struct {
	Content  string `json:"content"`
	Status   string `json:"status"`
	Priority string `json:"priority"`
}

const (
	todoMax        = 50
	todoMaxContent = 500
)

// writeTodos validates and atomically persists one full checklist for
// the session (stored outside the session workspace so it never disturbs
// the approval workspace digest) and answers the rendered checklist.
func (r *Runtime) writeTodos(session string, todos []struct {
	Content  string `json:"content"`
	Status   string `json:"status"`
	Priority string `json:"priority"`
}) (string, error) {
	if len(todos) > todoMax {
		return "", errors.New("too many todos (max 50)")
	}
	items := make([]todoItem, 0, len(todos))
	inProgress := 0
	for _, t := range todos {
		content := strings.TrimSpace(t.Content)
		if content == "" || len(content) > todoMaxContent {
			return "", errors.New("todo content must be 1-500 chars")
		}
		status := t.Status
		if status == "" {
			status = "pending"
		}
		if status != "pending" && status != "in_progress" && status != "completed" {
			return "", errors.New("todo status must be pending|in_progress|completed")
		}
		if status == "in_progress" {
			inProgress++
		}
		priority := t.Priority
		if priority == "" {
			priority = "medium"
		}
		if priority != "high" && priority != "medium" && priority != "low" {
			return "", errors.New("todo priority must be high|medium|low")
		}
		items = append(items, todoItem{Content: content, Status: status, Priority: priority})
	}
	if inProgress > 1 {
		return "", errors.New("only one todo may be in_progress at a time")
	}
	dir := filepath.Join(r.root, ".todos")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	raw, err := json.Marshal(items)
	if err != nil {
		return "", err
	}
	target := filepath.Join(dir, session+".json")
	tmp := target + ".tmp"
	if err := os.WriteFile(tmp, raw, 0600); err != nil {
		return "", err
	}
	_ = os.Remove(target)
	if err := os.Rename(tmp, target); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d todo(s) stored", len(items))
	for i, t := range items {
		mark := " "
		if t.Status == "completed" {
			mark = "x"
		}
		fmt.Fprintf(&b, "\n%d. [%s] (%s|%s) %s", i+1, mark, t.Status, t.Priority, t.Content)
	}
	return b.String(), nil
}

type editHunk struct {
	OldText    string
	NewText    string
	ReplaceAll bool
}

type workspaceEditFile struct {
	Path  string
	Hunks []editHunk
}

type workspaceEditHunkJSON struct {
	OldText    string `json:"oldText"`
	NewText    string `json:"newText"`
	ReplaceAll bool   `json:"replaceAll"`
}

func requireDesktopAction(approved bool) error {
	if approved {
		return nil
	}
	return errors.New("desktop action requires full-access or user approval")
}

func hunksFromJSON(oldText, newText string, replaceAll bool, edits []workspaceEditHunkJSON) ([]editHunk, error) {
	if len(edits) > 0 {
		if len(edits) > 20 {
			return nil, errors.New("invalid arguments")
		}
		hunks := make([]editHunk, 0, len(edits))
		for _, h := range edits {
			hunks = append(hunks, editHunk{OldText: h.OldText, NewText: h.NewText, ReplaceAll: h.ReplaceAll})
		}
		return hunks, nil
	}
	return []editHunk{{OldText: oldText, NewText: newText, ReplaceAll: replaceAll}}, nil
}

func parseWorkspaceEditArgs(args json.RawMessage) ([]workspaceEditFile, error) {
	var a struct {
		Path       string                  `json:"path"`
		OldText    string                  `json:"oldText"`
		NewText    string                  `json:"newText"`
		ReplaceAll bool                    `json:"replaceAll"`
		Edits      []workspaceEditHunkJSON `json:"edits"`
		Files      []struct {
			Path       string                  `json:"path"`
			OldText    string                  `json:"oldText"`
			NewText    string                  `json:"newText"`
			ReplaceAll bool                    `json:"replaceAll"`
			Edits      []workspaceEditHunkJSON `json:"edits"`
		} `json:"files"`
	}
	if strict(args, &a) != nil {
		return nil, errors.New("invalid arguments")
	}
	if len(a.Files) > 0 {
		if len(a.Files) > 8 {
			return nil, errors.New("invalid arguments")
		}
		out := make([]workspaceEditFile, 0, len(a.Files))
		seen := map[string]bool{}
		for _, f := range a.Files {
			if strings.TrimSpace(f.Path) == "" {
				return nil, errors.New("invalid arguments")
			}
			if seen[f.Path] {
				return nil, errors.New("invalid arguments")
			}
			seen[f.Path] = true
			hunks, err := hunksFromJSON(f.OldText, f.NewText, f.ReplaceAll, f.Edits)
			if err != nil {
				return nil, err
			}
			out = append(out, workspaceEditFile{Path: f.Path, Hunks: hunks})
		}
		return out, nil
	}
	if strings.TrimSpace(a.Path) == "" {
		return nil, errors.New("invalid arguments")
	}
	hunks, err := hunksFromJSON(a.OldText, a.NewText, a.ReplaceAll, a.Edits)
	if err != nil {
		return nil, err
	}
	return []workspaceEditFile{{Path: a.Path, Hunks: hunks}}, nil
}

func writeFileReplace(path, content string) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".edit-*")
	if err != nil {
		return err
	}
	tn := tmp.Name()
	defer os.Remove(tn)
	if err = tmp.Chmod(0600); err == nil {
		_, err = tmp.WriteString(content)
	}
	if err == nil {
		err = tmp.Sync()
	}
	ce := tmp.Close()
	if err == nil {
		err = ce
	}
	if err == nil {
		_ = os.Remove(path)
		err = os.Rename(tn, path)
	}
	return err
}

func applyWorkspaceHunks(content string, hunks []editHunk) (string, int, error) {
	if len(hunks) == 0 {
		return "", 0, errors.New("invalid arguments")
	}
	updated := content
	total := 0
	for i, h := range hunks {
		if h.OldText == "" || len(h.OldText) > maxFile || len(h.NewText) > maxFile {
			return "", 0, errors.New("invalid arguments")
		}
		count := strings.Count(updated, h.OldText)
		if count == 0 {
			return "", 0, fmt.Errorf("oldText not found in file (hunk %d)", i+1)
		}
		if count > 1 && !h.ReplaceAll {
			return "", 0, fmt.Errorf("oldText found %d times; set replaceAll=true or narrow the anchor", count)
		}
		if h.ReplaceAll {
			updated = strings.ReplaceAll(updated, h.OldText, h.NewText)
			total += count
		} else {
			updated = strings.Replace(updated, h.OldText, h.NewText, 1)
			total++
		}
	}
	return updated, total, nil
}

func strict(b []byte, v any) error {
	d := json.NewDecoder(strings.NewReader(string(b)))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return err
	}
	if d.Decode(&struct{}{}) != io.EOF {
		return errors.New("trailing json")
	}
	return nil
}
func result(s string) Result {
	h := sha256.Sum256([]byte(s))
	return Result{Output: s, Digest: hex.EncodeToString(h[:])}
}

const searchAttemptTimeout = 8 * time.Second

func (r *Runtime) searchWeb(ctx context.Context, query string, max int) ([]webfetch.SearchResult, string, string, error) {
	attempts := []struct {
		url    string
		source string
	}{
		{webfetch.SearchURL(query), "duckduckgo"},
		{webfetch.BingCNSearchURL(query), "bing"},
		{webfetch.BingSearchURL(query), "bing"},
	}
	var lastErr error
	var lastHits []webfetch.SearchResult
	var lastSrc, lastURL string
	for _, attempt := range attempts {
		c, cancel := context.WithTimeout(ctx, searchAttemptTimeout)
		page, err := r.fetchWeb(c, attempt.url)
		cancel()
		if err != nil {
			lastErr = err
			continue
		}
		var hits []webfetch.SearchResult
		if attempt.source == "duckduckgo" {
			hits = webfetch.ParseSearchResults(string(page.Body), max)
		} else {
			hits = webfetch.ParseBingResults(string(page.Body), max)
		}
		pageURL := attempt.url
		if page.FinalURL != "" {
			pageURL = page.FinalURL
		}
		lastHits, lastSrc, lastURL = hits, attempt.source, pageURL
		if len(hits) > 0 {
			return hits, attempt.source, pageURL, nil
		}
	}
	if lastSrc != "" {
		return lastHits, lastSrc, lastURL, nil
	}
	if lastErr != nil {
		return nil, "", "", lastErr
	}
	return nil, "none", webfetch.BingCNSearchURL(query), nil
}
