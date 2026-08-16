package toolruntime

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func openHooksRuntime(t *testing.T) *Runtime {
	t.Helper()
	r, err := Open(filepath.Join(t.TempDir(), "tool-workspaces"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	return r
}

func setHooks(t *testing.T, r *Runtime, doc string) {
	t.Helper()
	if err := r.SetHooksPolicyJSON([]byte(doc)); err != nil {
		t.Fatalf("set hooks: %v", err)
	}
}

// P3-B-1: block refuses with the rule message, before any other gate.
func TestHookBlockRefusesBeforeExecution(t *testing.T) {
	r := openHooksRuntime(t)
	setHooks(t, r, `{"hooks":[{"id":"no-write","events":["beforeToolCall"],"tools":["workspace.write"],"decision":"block","message":"写入已禁用"}]}`)
	_, err := r.Execute(context.Background(), FullAccess, "01ARZ3NDEKTSV4RRFFQ69G5FAV", "workspace.write", json.RawMessage(`{"path":"a.txt","content":"x"}`), true)
	if !errors.Is(err, ErrHookBlocked) || !strings.Contains(err.Error(), "写入已禁用") {
		t.Fatalf("want ErrHookBlocked with message, got %v", err)
	}
	// The refused call leaves an audit row.
	events, err := r.ListHookEvents(context.Background(), 10)
	if err != nil || len(events) != 1 {
		t.Fatalf("events = %v err=%v", events, err)
	}
	if events[0].HookID != "no-write" || events[0].Decision != HookDecisionBlock {
		t.Fatalf("event = %+v", events[0])
	}
}

// P3-B-2: requireApproval forces the gate even in full-access mode; the
// approved retry then executes.
func TestHookRequireApprovalForcesGate(t *testing.T) {
	r := openHooksRuntime(t)
	setHooks(t, r, `{"hooks":[{"id":"gate-pdf","events":["beforeToolCall","afterToolCall"],"tools":["pdf.gen"],"decision":"requireApproval"}]}`)
	session := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	args := json.RawMessage(`{"path":"out.pdf","title":"t","body":"b"}`)
	_, err := r.Execute(context.Background(), FullAccess, session, "pdf.gen", args, false)
	if !errors.Is(err, ErrApprovalRequired) {
		t.Fatalf("want ErrApprovalRequired in full-access, got %v", err)
	}
	out, err := r.Execute(context.Background(), FullAccess, session, "pdf.gen", args, true)
	if err != nil || !strings.Contains(out.Output, "out.pdf") {
		t.Fatalf("approved run failed: %v %q", err, out.Output)
	}
	// rows: gated Execute (before) + Prepare's internal Execute (before)
	// + approved run (before) + approved run (after) = 4.
	events, _ := r.ListHookEvents(context.Background(), 10)
	if len(events) != 4 {
		t.Fatalf("want 4 hook rows (gate, prepare-check, before, after), got %d", len(events))
	}
}

// P3-B-3: allow skips the approval round-trip; command.run keeps its
// whitelist (allow never widens argv admission).
func TestHookAllowGrantsApprovalButNotWhitelist(t *testing.T) {
	r := openHooksRuntime(t)
	setHooks(t, r, `{"hooks":[{"id":"fast-git","events":["beforeToolCall"],"tools":["command.run"],"decision":"allow"}]}`)
	session := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	// Whitelisted argv executes without the approval round-trip.
	_, err := r.Execute(context.Background(), Approval, session, "command.run", json.RawMessage(`{"argv":["go","version"]}`), false)
	if err != nil {
		t.Fatalf("allow should admit whitelisted argv: %v", err)
	}
	// Non-whitelisted argv still denied.
	_, err = r.Execute(context.Background(), Approval, session, "command.run", json.RawMessage(`{"argv":["curl","example.com"]}`), false)
	if err == nil || !strings.Contains(err.Error(), "denied") {
		t.Fatalf("want denial for non-whitelisted argv, got %v", err)
	}
}

// P3-B-4: validation is fail-closed — unknown tools, unknown events,
// bad decisions, duplicate ids, block without message, message on
// non-block are all refused, and the live rules stay untouched.
func TestHooksPolicyValidationFailClosed(t *testing.T) {
	r := openHooksRuntime(t)
	bad := []string{
		`{"hooks":[{"id":"x","events":["beforeToolCall"],"tools":["rm -rf"],"decision":"block","message":"m"}]}`,
		`{"hooks":[{"id":"x","events":["onFire"],"tools":["workspace.read"],"decision":"block","message":"m"}]}`,
		`{"hooks":[{"id":"x","events":["beforeToolCall"],"tools":["workspace.read"],"decision":"maybe"}]}`,
		`{"hooks":[{"id":"x","events":["beforeToolCall"],"tools":["workspace.read"],"decision":"block"}]}`,
		`{"hooks":[{"id":"x","events":["beforeToolCall"],"tools":["workspace.read"],"decision":"allow","message":"m"}]}`,
		`{"hooks":[{"id":"x","events":["beforeToolCall"],"tools":["workspace.read"],"decision":"allow"},{"id":"x","events":["afterToolCall"],"tools":["workspace.read"],"decision":"allow"}]}`,
		`{"hooks":[{"id":"bad id!","events":["beforeToolCall"],"tools":["workspace.read"],"decision":"allow"}]}`,
	}
	for _, doc := range bad {
		if err := r.SetHooksPolicyJSON([]byte(doc)); err == nil {
			t.Fatalf("invalid doc accepted: %s", doc)
		}
	}
	if _, err := os.Stat(r.hooksRulesPath); !os.IsNotExist(err) {
		t.Fatal("rejected doc must not touch the persisted file")
	}
	// A present-but-invalid stored file fails Open (fail-closed startup).
	root := filepath.Join(t.TempDir(), "tool-workspaces")
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "hooks-policy.json"), []byte(`{"hooks":[{"id":"x","events":["nope"],"tools":["a"],"decision":"b"}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(root); err == nil {
		t.Fatal("Open must refuse an invalid persisted hooks document")
	}
}

// P3-B-5: get/set round-trip and hot apply without restart; priority is
// block > requireApproval > allow across rules.
func TestHooksRoundTripHotApplyAndPriority(t *testing.T) {
	r := openHooksRuntime(t)
	doc := `{"hooks":[{"id":"a","events":["beforeToolCall"],"tools":["workspace.write","docx.gen"],"decision":"allow"},{"id":"b","events":["beforeToolCall"],"tools":["docx.gen"],"decision":"block","message":"no docx"}]}`
	setHooks(t, r, doc)
	got, err := r.HooksPolicyJSON()
	if err != nil || !json.Valid(got) || strings.TrimSpace(string(got)) == "" {
		t.Fatalf("get = %s err=%v", got, err)
	}
	var parsed hookPolicyDoc
	if err := json.Unmarshal(got, &parsed); err != nil || len(parsed.Hooks) != 2 {
		t.Fatalf("round-trip = %+v err=%v", parsed, err)
	}
	session := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	// docx.gen matches both rules; block wins over allow.
	_, err = r.Execute(context.Background(), AutoEdit, session, "docx.gen", json.RawMessage(`{"path":"a.docx"}`), true)
	if !errors.Is(err, ErrHookBlocked) {
		t.Fatalf("block must outrank allow, got %v", err)
	}
	// workspace.write only matches allow: auto-edit proceeds without gate.
	_, err = r.Execute(context.Background(), Approval, session, "workspace.write", json.RawMessage(`{"path":"b.txt","content":"x"}`), false)
	if err != nil {
		t.Fatalf("allow should satisfy approval mode gate: %v", err)
	}
}

// P3-B-6: remembered approvals (P1-5) still satisfy a requireApproval
// hook — the hook forces the gate, the gate honors the memory.
func TestHookRequireApprovalHonorsRememberedApprovals(t *testing.T) {
	r := openHooksRuntime(t)
	setHooks(t, r, `{"hooks":[{"id":"g","events":["beforeToolCall"],"tools":["workspace.write"],"decision":"requireApproval"}]}`)
	ctx := context.Background()
	session := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	args := json.RawMessage(`{"path":"c.txt","content":"z"}`)
	if _, err := r.Execute(ctx, FullAccess, session, "workspace.write", args, false); !errors.Is(err, ErrApprovalRequired) {
		t.Fatalf("want gate, got %v", err)
	}
	p, err := r.Prepare(ctx, "run-1", session, "call-1", "workspace.write", args, FullAccess, 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = r.DecideScoped(ctx, session, p.CallID, p.ArgsDigest, true, ApprovalScopeAlways); err != nil {
		t.Fatal(err)
	}
	// Same (tool, args) now sails through the hook gate via memory.
	if _, err = r.Execute(ctx, FullAccess, session, "workspace.write", args, false); err != nil {
		t.Fatalf("remembered approval should satisfy hook gate: %v", err)
	}
}
