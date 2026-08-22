package toolruntime

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestPolicyAndRealSideEffects(t *testing.T) {
	r, _ := New(t.TempDir())
	s := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	write := json.RawMessage(`{"path":"a.txt","content":"one"}`)
	if _, e := r.Execute(context.Background(), Plan, s, "workspace.write", write, false); e == nil {
		t.Fatal("plan wrote")
	}
	if _, e := r.Execute(context.Background(), Approval, s, "workspace.write", write, false); !errors.Is(e, ErrApprovalRequired) {
		t.Fatalf("approval=%v", e)
	}
	if _, e := r.Execute(context.Background(), Approval, s, "workspace.write", write, true); e != nil {
		t.Fatal(e)
	}
	if _, e := r.Execute(context.Background(), Approval, s, "workspace.write", write, true); e != nil {
		t.Fatal(e)
	}
	b, _ := os.ReadFile(filepath.Join(r.root, s, "a.txt"))
	if string(b) != "one" {
		t.Fatal(string(b))
	}
	if _, e := r.Execute(context.Background(), AutoEdit, s, "workspace.write", json.RawMessage(`{"path":"b.txt","content":"two"}`), false); e != nil {
		t.Fatal(e)
	}
	if _, e := r.Execute(context.Background(), AutoEdit, s, "command.run", json.RawMessage(`{"argv":["go","version"]}`), false); !errors.Is(e, ErrApprovalRequired) {
		t.Fatal(e)
	}
	if _, e := r.Execute(context.Background(), AutoEdit, s, "command.run", json.RawMessage(`{"argv":["go","version"]}`), true); e != nil {
		t.Fatalf("approved auto-edit command: %v", e)
	}
	if _, e := r.Execute(context.Background(), FullAccess, s, "command.run", json.RawMessage(`{"argv":["go","version"]}`), false); e != nil {
		t.Fatal(e)
	}
	if _, e := r.Execute(context.Background(), FullAccess, s, "command.run", json.RawMessage(`{"argv":["cmd","/c","del","x"]}`), false); e == nil {
		t.Fatal("dangerous command ran")
	}
}

func TestInvalidExecutionModeFailsClosed(t *testing.T) {
	r, _ := New(t.TempDir())
	s := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	for _, name := range []string{"workspace.list", "workspace.write", "command.run"} {
		args := json.RawMessage(`{"path":"."}`)
		if name == "workspace.write" {
			args = json.RawMessage(`{"path":"blocked.txt","content":"blocked"}`)
		} else if name == "command.run" {
			args = json.RawMessage(`{"argv":["go","version"]}`)
		}
		if _, err := r.Execute(context.Background(), Mode("invalid"), s, name, args, true); err == nil {
			t.Fatalf("invalid mode executed %s", name)
		}
	}
	if _, err := os.Stat(filepath.Join(r.root, s, "blocked.txt")); !os.IsNotExist(err) {
		t.Fatalf("invalid mode mutated workspace: %v", err)
	}
}

func TestReadOnlyToolsDoNotCreateRequestedPaths(t *testing.T) {
	r, _ := New(t.TempDir())
	s := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	for _, name := range []string{"workspace.list", "workspace.read"} {
		if _, err := r.Execute(context.Background(), Approval, s, name, json.RawMessage(`{"path":"missing/nested"}`), false); err == nil {
			t.Fatalf("%s unexpectedly succeeded", name)
		}
		if _, err := os.Stat(filepath.Join(r.root, s)); !os.IsNotExist(err) {
			t.Fatalf("%s created a read-only workspace: %v", name, err)
		}
	}
}

func TestContainment(t *testing.T) {
	r, _ := New(t.TempDir())
	s := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	for _, p := range []string{"../x", filepath.Join(t.TempDir(), "x")} {
		a, _ := json.Marshal(map[string]string{"path": p, "content": "x"})
		if _, e := r.Execute(context.Background(), AutoEdit, s, "workspace.write", a, false); e == nil {
			t.Fatalf("accepted %q", p)
		}
	}
	if runtime.GOOS != "windows" {
		outside := t.TempDir()
		root, _ := r.sessionRoot(s)
		if os.Symlink(outside, filepath.Join(root, "link")) == nil {
			a := json.RawMessage(`{"path":"link/x","content":"x"}`)
			if _, e := r.Execute(context.Background(), AutoEdit, s, "workspace.write", a, false); e == nil {
				t.Fatal("symlink escaped")
			}
		}
	}
}

func TestDurableApprovalCASAndNoPreApprovalSideEffect(t *testing.T) {
	root := t.TempDir()
	r, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	ctx := context.Background()
	s := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	args := json.RawMessage(`{"content":"once","path":"approved.txt"}`)
	digest := Digest("workspace.write", args)
	if _, err = r.Prepare(ctx, "run-1", s, "call-1", "workspace.write", args, Approval, 0); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(root, s, "approved.txt")); !os.IsNotExist(err) {
		t.Fatalf("side effect before approval: %v", err)
	}
	if _, err = r.Decide(ctx, s, "call-1", digest, true); err != nil {
		t.Fatal(err)
	}
	if _, err = r.Decide(ctx, s, "call-1", digest, true); !errors.Is(err, ErrPendingConsumed) {
		t.Fatalf("replay=%v", err)
	}
	b, err := os.ReadFile(filepath.Join(root, s, "approved.txt"))
	if err != nil || string(b) != "once" {
		t.Fatalf("file=%q err=%v", b, err)
	}
}

// TestApprovalRememberSessionAndAlwaysScopes covers P1-5: approving with
// scope=session auto-approves the exact pair within the session (but not
// other sessions), scope=always carries across sessions, argument
// variants never inherit a remembered approval, and rejecting records
// nothing.
func TestApprovalRememberSessionAndAlwaysScopes(t *testing.T) {
	root := t.TempDir()
	r, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	ctx := context.Background()
	s1 := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	s2 := "01ARZ3NDEKTSV4RRFFQ69G5FAW"
	args := json.RawMessage(`{"content":"v1","path":"a.txt"}`)
	digest := Digest("workspace.write", args)

	// scope=session: the same pair in s1 skips the gate, s2 still gates.
	if _, err = r.Prepare(ctx, "run-1", s1, "call-1", "workspace.write", args, Approval, 0); err != nil {
		t.Fatal(err)
	}
	if _, err = r.DecideScoped(ctx, s1, "call-1", digest, true, ApprovalScopeSession); err != nil {
		t.Fatal(err)
	}
	if _, err = r.Execute(ctx, Approval, s1, "workspace.write", args, false); err != nil {
		t.Fatalf("session-remembered call still gated: %v", err)
	}
	if _, err = r.Execute(ctx, Approval, s1, "workspace.write", json.RawMessage(`{"content":"v2","path":"a.txt"}`), false); !errors.Is(err, ErrApprovalRequired) {
		t.Fatalf("argument variant inherited approval: %v", err)
	}
	if _, err = r.Execute(ctx, Approval, s2, "workspace.write", args, false); !errors.Is(err, ErrApprovalRequired) {
		t.Fatalf("session rule leaked across sessions: %v", err)
	}

	// scope=always: the pair passes in a different session too.
	if _, err = r.Prepare(ctx, "run-2", s2, "call-2", "workspace.write", args, Approval, 0); err != nil {
		t.Fatal(err)
	}
	if _, err = r.DecideScoped(ctx, s2, "call-2", digest, true, ApprovalScopeAlways); err != nil {
		t.Fatal(err)
	}
	if _, err = r.Execute(ctx, Approval, s1, "workspace.write", args, false); err != nil {
		t.Fatalf("always rule not honored in original session: %v", err)
	}
	third := "01ARZ3NDEKTSV4RRFFQ69G5FAX"
	if _, err = r.Execute(ctx, Approval, third, "workspace.write", args, false); err != nil {
		t.Fatalf("always rule not honored in third session: %v", err)
	}

	// Rejection records nothing: the next identical call still gates.
	rejectArgs := json.RawMessage(`{"content":"no","path":"b.txt"}`)
	rejectDigest := Digest("workspace.write", rejectArgs)
	if _, err = r.Prepare(ctx, "run-3", s1, "call-3", "workspace.write", rejectArgs, Approval, 0); err != nil {
		t.Fatal(err)
	}
	if _, err = r.DecideScoped(ctx, s1, "call-3", rejectDigest, false, ApprovalScopeAlways); err != nil {
		t.Fatal(err)
	}
	if _, err = r.Execute(ctx, Approval, s1, "workspace.write", rejectArgs, false); !errors.Is(err, ErrApprovalRequired) {
		t.Fatalf("rejected pair became auto-approved: %v", err)
	}

	// Invalid scope is refused before any state change.
	if _, err = r.DecideScoped(ctx, s1, "x", digest, true, "forever"); err == nil {
		t.Fatal("invalid scope accepted")
	}
}

func TestApprovalFailsClosedWhenWorkspaceChanges(t *testing.T) {
	root := t.TempDir()
	r, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	ctx := context.Background()
	s := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	args := json.RawMessage(`{"path":"target.txt","content":"new"}`)
	digest := Digest("workspace.write", args)
	if _, err = r.Prepare(ctx, "run", s, "call", "workspace.write", args, Approval, 0); err != nil {
		t.Fatal(err)
	}
	if _, err = r.Execute(ctx, AutoEdit, s, "workspace.write", json.RawMessage(`{"path":"other.txt","content":"changed"}`), false); err != nil {
		t.Fatal(err)
	}
	if _, err = r.Decide(ctx, s, "call", digest, true); !errors.Is(err, ErrWorkspaceChanged) {
		t.Fatalf("got %v", err)
	}
	if _, err = os.Stat(filepath.Join(root, s, "target.txt")); !os.IsNotExist(err) {
		t.Fatal("target mutated")
	}
}

func TestWorkspaceWriteReturnsPreviewMetadataOnlyForHTML(t *testing.T) {
	r, _ := New(t.TempDir())
	session := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	html, err := r.Execute(context.Background(), AutoEdit, session, "workspace.write", json.RawMessage(`{"path":"site/index.html","content":"<h1>preview</h1>"}`), false)
	if err != nil || html.Artifact == nil || html.Artifact.Kind != "html" || !strings.Contains(html.Artifact.Path, "index.html") || html.Artifact.Content != "<h1>preview</h1>" {
		t.Fatalf("html result = %+v, err=%v", html, err)
	}
	text, err := r.Execute(context.Background(), AutoEdit, session, "workspace.write", json.RawMessage(`{"path":"notes.txt","content":"plain"}`), false)
	if err != nil || text.Artifact != nil {
		t.Fatalf("text result = %+v, err=%v", text, err)
	}
}
