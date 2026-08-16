// P3-B Hooks: user-configurable tool-call interceptors persisted at
// <tool-workspaces>/hooks-policy.json. Mirrors command-policy.json's
// fail-closed discipline: a present-but-invalid file refuses startup,
// an invalid Set document is rejected without touching live rules, and
// rule evaluation runs on an RLock snapshot for hot reload.
//
// Decisions (priority block > requireApproval > allow when several
// rules match one call):
//   - block: the call is refused before execution with the rule message.
//   - requireApproval: mutating tools gate through the approval flow
//     even in auto-edit/full-access modes (plan mode still disables
//     tools entirely; read-only tools are unaffected).
//   - allow: skips only the approval round-trip for the matched tool.
//     command.run keeps its whitelist; allow never widens it.
//
// Events: beforeToolCall enforces decisions; afterToolCall (and any
// matched beforeToolCall) append bounded audit rows surfaced through
// tools.hooksEvents.list.
package toolruntime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"
)

const (
	HookEventBefore = "beforeToolCall"
	HookEventAfter  = "afterToolCall"

	HookDecisionBlock           = "block"
	HookDecisionRequireApproval = "requireApproval"
	HookDecisionAllow           = "allow"

	hooksMaxEntries = 64
	hooksMaxMessage = 256
	hooksMaxID      = 64
)

var ErrHookBlocked = errors.New("hook blocked")

// hookableTools is the frozen matcher universe: hooks may only reference
// tools the runtime actually executes (fail-closed on anything else).
var hookableTools = map[string]bool{
	"workspace.list": true, "workspace.read": true, "workspace.write": true,
	"workspace.search": true, "workspace.edit": true, "todo.write": true,
	"command.run": true, "web.fetch": true, "web.search": true,
	"excel.gen": true, "excel.parse": true, "docx.gen": true, "pptx.gen": true, "pdf.gen": true,
}

var hookIDPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

// hookRule is one compiled rule: which tools it watches, on which event,
// and what to do.
type hookRule struct {
	id       string
	events   map[string]bool
	tools    map[string]bool
	decision string
	message  string
}

// hookPolicyDoc is the hooks-policy.json wire shape shared by load, get
// and set.
type hookPolicyDoc struct {
	Hooks []struct {
		ID       string   `json:"id"`
		Events   []string `json:"events"`
		Tools    []string `json:"tools"`
		Decision string   `json:"decision"`
		Message  string   `json:"message,omitempty"`
	} `json:"hooks"`
}

// buildHookRules validates one document and renders it into concrete
// rules without touching runtime state (build-then-swap keeps a rejected
// document from half-applying).
func buildHookRules(raw []byte) ([]hookRule, error) {
	var doc hookPolicyDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("hooks-policy.json: %w", err)
	}
	if len(doc.Hooks) > hooksMaxEntries {
		return nil, fmt.Errorf("hooks-policy.json: more than %d hooks", hooksMaxEntries)
	}
	rules := make([]hookRule, 0, len(doc.Hooks))
	seen := map[string]bool{}
	for _, h := range doc.Hooks {
		if !hookIDPattern.MatchString(h.ID) {
			return nil, fmt.Errorf("hooks-policy.json: invalid id %q (1-64 chars of A-Z a-z 0-9 . _ -)", h.ID)
		}
		if seen[h.ID] {
			return nil, fmt.Errorf("hooks-policy.json: duplicate id %q", h.ID)
		}
		seen[h.ID] = true
		if len(h.Events) == 0 || len(h.Tools) == 0 {
			return nil, fmt.Errorf("hooks-policy.json: hook %q needs at least one event and one tool", h.ID)
		}
		events := map[string]bool{}
		for _, ev := range h.Events {
			if ev != HookEventBefore && ev != HookEventAfter {
				return nil, fmt.Errorf("hooks-policy.json: hook %q unknown event %q", h.ID, ev)
			}
			events[ev] = true
		}
		tools := map[string]bool{}
		for _, t := range h.Tools {
			if !hookableTools[t] {
				return nil, fmt.Errorf("hooks-policy.json: hook %q unknown tool %q", h.ID, t)
			}
			tools[t] = true
		}
		switch h.Decision {
		case HookDecisionBlock, HookDecisionRequireApproval, HookDecisionAllow:
		default:
			return nil, fmt.Errorf("hooks-policy.json: hook %q invalid decision %q", h.ID, h.Decision)
		}
		if len(h.Message) > hooksMaxMessage {
			return nil, fmt.Errorf("hooks-policy.json: hook %q message exceeds %d chars", h.ID, hooksMaxMessage)
		}
		if h.Decision == HookDecisionBlock && strings.TrimSpace(h.Message) == "" {
			return nil, fmt.Errorf("hooks-policy.json: hook %q block decision requires a message", h.ID)
		}
		if h.Decision != HookDecisionBlock && h.Message != "" {
			return nil, fmt.Errorf("hooks-policy.json: hook %q message is only valid for block", h.ID)
		}
		rules = append(rules, hookRule{id: h.ID, events: events, tools: tools, decision: h.Decision, message: h.Message})
	}
	return rules, nil
}

// loadUserHooksPolicy merges the optional hooks document at startup. A
// present-but-invalid file fails closed (Open reports the error).
func (r *Runtime) loadUserHooksPolicy() error {
	raw, err := os.ReadFile(r.hooksRulesPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	rules, err := buildHookRules(raw)
	if err != nil {
		return err
	}
	r.hooksMu.Lock()
	r.hookRules = rules
	r.hooksMu.Unlock()
	return nil
}

// HooksPolicyJSON answers the persisted document ({"hooks":[]} when the
// file does not exist yet).
func (r *Runtime) HooksPolicyJSON() ([]byte, error) {
	raw, err := os.ReadFile(r.hooksRulesPath)
	if err != nil {
		if os.IsNotExist(err) {
			return []byte(`{"hooks":[]}`), nil
		}
		return nil, err
	}
	if !json.Valid(raw) {
		return nil, errors.New("hooks-policy.json: stored document is not valid JSON")
	}
	return raw, nil
}

// SetHooksPolicyJSON validates, atomically persists and hot-applies a
// new hooks document. An invalid document is refused without touching
// the file or the live rules.
func (r *Runtime) SetHooksPolicyJSON(raw []byte) error {
	if len(raw) > 64<<10 {
		return errors.New("hooks-policy.json: document exceeds 64 KiB")
	}
	if !json.Valid(raw) {
		return errors.New("hooks-policy.json: document is not valid JSON")
	}
	rules, err := buildHookRules(raw)
	if err != nil {
		return err
	}
	tmp := r.hooksRulesPath + ".tmp"
	if err := os.WriteFile(tmp, raw, 0600); err != nil {
		return err
	}
	// os.Rename on Windows refuses to replace an existing destination.
	_ = os.Remove(r.hooksRulesPath)
	if err := os.Rename(tmp, r.hooksRulesPath); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	r.hooksMu.Lock()
	r.hookRules = rules
	r.hooksMu.Unlock()
	return nil
}

// hookDecision is the aggregated outcome of every beforeToolCall rule
// matching one call (most restrictive wins).
type hookDecision struct {
	matched        bool
	blockMessage   string
	forceApproval  bool
	grantApproval  bool
	matchedHookIDs []string
}

// evaluateHooks scans the live snapshot for rules watching name on the
// before event. Priority: block > requireApproval > allow.
func (r *Runtime) evaluateHooks(name string) hookDecision {
	r.hooksMu.RLock()
	rules := r.hookRules
	r.hooksMu.RUnlock()
	var d hookDecision
	for _, rule := range rules {
		if !rule.tools[name] || !rule.events[HookEventBefore] {
			continue
		}
		d.matched = true
		d.matchedHookIDs = append(d.matchedHookIDs, rule.id)
		switch rule.decision {
		case HookDecisionBlock:
			d.blockMessage = rule.message
		case HookDecisionRequireApproval:
			d.forceApproval = true
		case HookDecisionAllow:
			d.grantApproval = true
		}
	}
	if d.blockMessage != "" {
		d.forceApproval, d.grantApproval = false, false
	}
	return d
}

// afterHookIDs lists rules watching name on the after event.
func (r *Runtime) afterHookIDs(name string) []string {
	r.hooksMu.RLock()
	rules := r.hookRules
	r.hooksMu.RUnlock()
	var ids []string
	for _, rule := range rules {
		if rule.tools[name] && rule.events[HookEventAfter] {
			ids = append(ids, rule.id)
		}
	}
	return ids
}

// recordHookEvents appends bounded audit rows for the hook matches of
// one executed call. Failures never break the tool result.
func (r *Runtime) recordHookEvents(ctx context.Context, session, name, argsDigest, resultDigest string, before hookDecision) {
	if r.db == nil {
		return
	}
	now := r.now().Format(time.RFC3339Nano)
	type row struct{ id, event, decision string }
	var rows []row
	for _, id := range before.matchedHookIDs {
		decision := HookDecisionAllow
		if before.blockMessage != "" {
			decision = HookDecisionBlock
		} else if before.forceApproval {
			decision = HookDecisionRequireApproval
		}
		rows = append(rows, row{id, HookEventBefore, decision})
	}
	for _, id := range r.afterHookIDs(name) {
		rows = append(rows, row{id, HookEventAfter, ""})
	}
	for _, x := range rows {
		_, _ = r.db.ExecContext(ctx, `INSERT INTO chat_tool_hook_events(session_id,tool_name,hook_id,event,decision,args_digest,result_digest,created_at) VALUES(?,?,?,?,?,?,?,?)`,
			session, name, x.id, x.event, x.decision, argsDigest, resultDigest, now)
	}
}

// HookEvent is one persisted audit row.
type HookEvent struct {
	SessionID    string `json:"sessionId"`
	ToolName     string `json:"toolName"`
	HookID       string `json:"hookId"`
	Event        string `json:"event"`
	Decision     string `json:"decision"`
	ArgsDigest   string `json:"argsDigest"`
	ResultDigest string `json:"resultDigest"`
	CreatedAt    string `json:"createdAt"`
}

// ListHookEvents answers the most recent audit rows (newest first).
func (r *Runtime) ListHookEvents(ctx context.Context, limit int) ([]HookEvent, error) {
	if r.db == nil {
		if err := r.ensureAudit(); err != nil {
			return nil, err
		}
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := r.db.QueryContext(ctx, `SELECT session_id,tool_name,hook_id,event,decision,args_digest,result_digest,created_at FROM chat_tool_hook_events ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []HookEvent
	for rows.Next() {
		var e HookEvent
		if err := rows.Scan(&e.SessionID, &e.ToolName, &e.HookID, &e.Event, &e.Decision, &e.ArgsDigest, &e.ResultDigest, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
