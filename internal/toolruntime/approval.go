package toolruntime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/lunitide/lunitide/internal/jsonutil"
)

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
