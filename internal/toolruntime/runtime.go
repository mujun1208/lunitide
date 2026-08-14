package toolruntime

import (
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
	"sort"
	"strings"
	"time"

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
}
type Result struct {
	Output   string    `json:"output"`
	Digest   string    `json:"digest"`
	Artifact *Artifact `json:"artifact,omitempty"`
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
	real, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	return &Runtime{root: filepath.Clean(real), now: func() time.Time { return time.Now().UTC() }}, nil
}
func (r *Runtime) ensureAudit() error {
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
		CREATE INDEX IF NOT EXISTS ix_chat_tool_calls_status ON chat_tool_calls(status, expires_at);`); err != nil {
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
	return r, nil
}
func (r *Runtime) Close() error {
	if r.db == nil {
		return nil
	}
	return r.db.Close()
}
func Digest(name string, args json.RawMessage) string {
	var v any
	if json.Unmarshal(args, &v) != nil {
		return ""
	}
	canonical, _ := json.Marshal(v)
	h := sha256.Sum256(append(append([]byte(name), 0), canonical...))
	return hex.EncodeToString(h[:])
}
func (r *Runtime) sessionRoot(session string) (string, error) {
	if len(session) != 26 || strings.ContainsAny(session, "/\\") {
		return "", errors.New("invalid session")
	}
	p := filepath.Join(r.root, session)
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

func (r *Runtime) workspaceDigest(session string) (string, error) {
	root, err := r.sessionRoot(session)
	if err != nil {
		return "", err
	}
	h := sha256.New()
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
	wd, err := r.workspaceDigest(session)
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
	current, e := r.workspaceDigest(session)
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
func (r *Runtime) path(session, rel string, write bool) (string, error) {
	if rel == "" || filepath.IsAbs(rel) || filepath.VolumeName(rel) != "" {
		return "", errors.New("relative path required")
	}
	clean := filepath.Clean(rel)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
		return "", errors.New("path traversal")
	}
	root, err := r.sessionRoot(session)
	if err != nil {
		return "", err
	}
	p := filepath.Join(root, clean)
	parent := p
	if write {
		parent = filepath.Dir(p)
	}
	if err = os.MkdirAll(parent, 0700); err != nil {
		return "", err
	}
	realParent, err := filepath.EvalSymlinks(parent)
	if err != nil {
		return "", err
	}
	relCheck, err := filepath.Rel(root, realParent)
	if err != nil || relCheck == ".." || strings.HasPrefix(relCheck, ".."+string(os.PathSeparator)) {
		return "", errors.New("path escape")
	}
	if !write {
		real, err := filepath.EvalSymlinks(p)
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
func (r *Runtime) Execute(ctx context.Context, mode Mode, session, name string, args json.RawMessage, approved bool) (Result, error) {
	switch mode {
	case Approval, AutoEdit, Plan, FullAccess:
	default:
		return Result{}, errors.New("invalid execution mode")
	}
	if mode == Plan {
		return Result{}, errors.New("tools disabled in plan mode")
	}
	mutating := name == "workspace.write" || name == "command.run"
	if mutating && !approved && ((mode == Approval) || (name == "command.run" && mode == AutoEdit)) {
		return Result{}, ErrApprovalRequired
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
		p, e := r.path(session, a.Path, false)
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
		p, e := r.path(session, a.Path, false)
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
		p, e := r.path(session, a.Path, true)
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
			written.Artifact = &Artifact{Kind: "html", Path: filepath.ToSlash(a.Path), Content: a.Content}
		}
		return written, nil
	case "command.run":
		if mode != FullAccess && !(approved && (mode == Approval || mode == AutoEdit)) {
			return Result{}, errors.New("command denied")
		}
		var a struct {
			Argv []string `json:"argv"`
		}
		if strict(args, &a) != nil || !allowed(a.Argv) {
			return Result{}, errors.New("command denied")
		}
		root, e := r.sessionRoot(session)
		if e != nil {
			return Result{}, e
		}
		cctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		cmd := exec.CommandContext(cctx, a.Argv[0], a.Argv[1:]...)
		cmd.Dir = root
		out, e := cmd.CombinedOutput()
		if len(out) > 64<<10 {
			out = out[:64<<10]
		}
		if e != nil {
			return Result{}, fmt.Errorf("command failed: %s", out)
		}
		return result(string(out)), nil
	default:
		return Result{}, errors.New("unknown tool")
	}
}
func allowed(a []string) bool {
	// Keep the command surface to a fixed, informational invocation that does
	// not evaluate repository configuration, hooks, aliases, or user input.
	// In particular, git is intentionally excluded because filters/pagers may
	// create descendants that CommandContext cannot reliably contain on Windows.
	return len(a) == 2 && a[0] == "go" && a[1] == "version"
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
