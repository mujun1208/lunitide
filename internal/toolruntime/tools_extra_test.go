package toolruntime

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func seedWorkspace(t *testing.T, r *Runtime, files map[string]string) {
	t.Helper()
	session := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	root, err := r.sessionRoot(session)
	if err != nil {
		t.Fatal(err)
	}
	for rel, content := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
}

// C2-1: workspace.search answers literal and regex hits with path:line
// prefixes, skips binary files and stays read-only.
func TestWorkspaceSearchLiteralAndRegex(t *testing.T) {
	r := openHooksRuntime(t)
	seedWorkspace(t, r, map[string]string{
		"notes/todo.md": "alpha\nbeta target\ngamma",
		"src/a.txt":     "target one\ntarget two",
		"blob.bin":      "target\x00binary",
	})
	ctx := context.Background()
	session := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	out, err := r.Execute(ctx, Approval, session, "workspace.search", json.RawMessage(`{"query":"target"}`), false)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"notes/todo.md:2: beta target", "src/a.txt:1: target one", "src/a.txt:2: target two"} {
		if !strings.Contains(out.Output, want) {
			t.Fatalf("output missing %q:\n%s", want, out.Output)
		}
	}
	if strings.Contains(out.Output, "blob.bin") {
		t.Fatal("binary file must be skipped")
	}
	out, err = r.Execute(ctx, Approval, session, "workspace.search", json.RawMessage(`{"query":"tar[a-z]et t","regex":true}`), false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.Output, "src/a.txt:2: target two") || strings.Contains(out.Output, "todo.md") {
		t.Fatalf("regex hits wrong:\n%s", out.Output)
	}
	// Traversal stays contained.
	if _, err = r.Execute(ctx, Approval, session, "workspace.search", json.RawMessage(`{"query":"x","path":"../escape"}`), false); err == nil {
		t.Fatal("path traversal must be refused")
	}
	// Bad regex is a clean error.
	if _, err = r.Execute(ctx, Approval, session, "workspace.search", json.RawMessage(`{"query":"[","regex":true}`), false); err == nil {
		t.Fatal("invalid regex must be refused")
	}
}

// C2-2: workspace.edit replaces one anchored occurrence, refuses
// ambiguous anchors without replaceAll and rides the approval class.
func TestWorkspaceEditAnchoredReplace(t *testing.T) {
	r := openHooksRuntime(t)
	seedWorkspace(t, r, map[string]string{"a.txt": "one\ntwo\nthree\n"})
	ctx := context.Background()
	session := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	// Approval mode gates the mutating edit.
	if _, err := r.Execute(ctx, Approval, session, "workspace.edit", json.RawMessage(`{"path":"a.txt","oldText":"two","newText":"TWO"}`), false); !errors.Is(err, ErrApprovalRequired) {
		t.Fatalf("want approval gate, got %v", err)
	}
	out, err := r.Execute(ctx, Approval, session, "workspace.edit", json.RawMessage(`{"path":"a.txt","oldText":"two","newText":"TWO"}`), true)
	if err != nil || !strings.Contains(out.Output, "1 replacement") {
		t.Fatalf("anchored edit failed: %v %q", err, out.Output)
	}
	b, _ := os.ReadFile(filepath.Join(r.WorkspaceRoot(), session, "a.txt"))
	if string(b) != "one\nTWO\nthree\n" {
		t.Fatalf("edited file = %q", b)
	}
	// Ambiguous anchor without replaceAll refuses.
	seedWorkspace(t, r, map[string]string{"b.txt": "x x x\n"})
	if _, err = r.Execute(ctx, FullAccess, session, "workspace.edit", json.RawMessage(`{"path":"b.txt","oldText":"x","newText":"y"}`), true); err == nil || !strings.Contains(err.Error(), "3 times") {
		t.Fatalf("ambiguous anchor error = %v", err)
	}
	if _, err = r.Execute(ctx, FullAccess, session, "workspace.edit", json.RawMessage(`{"path":"b.txt","oldText":"x","newText":"y","replaceAll":true}`), true); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(filepath.Join(r.WorkspaceRoot(), session, "b.txt"))
	if string(b) != "y y y\n" {
		t.Fatalf("replaceAll result = %q", b)
	}
	// Missing anchor is a clean error.
	if _, err = r.Execute(ctx, FullAccess, session, "workspace.edit", json.RawMessage(`{"path":"a.txt","oldText":"nope","newText":"x"}`), true); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("missing anchor error = %v", err)
	}
	seedWorkspace(t, r, map[string]string{"c.txt": "alpha\nbeta\ngamma\n"})
	out, err = r.Execute(ctx, FullAccess, session, "workspace.edit", json.RawMessage(`{"path":"c.txt","edits":[{"oldText":"alpha","newText":"ALPHA"},{"oldText":"gamma","newText":"GAMMA"}]}`), true)
	if err != nil || !strings.Contains(out.Output, "2 replacement") {
		t.Fatalf("multi-hunk edit: %v %q", err, out.Output)
	}
	b, _ = os.ReadFile(filepath.Join(r.WorkspaceRoot(), session, "c.txt"))
	if string(b) != "ALPHA\nbeta\nGAMMA\n" {
		t.Fatalf("multi-hunk file = %q", b)
	}
	if _, err = r.Execute(ctx, FullAccess, session, "workspace.edit", json.RawMessage(`{"path":"c.txt","edits":[{"oldText":"ALPHA","newText":"ok"},{"oldText":"missing","newText":"x"}]}`), true); err == nil || !strings.Contains(err.Error(), "hunk 2") {
		t.Fatalf("fail-closed multi-hunk error = %v", err)
	}
	b, _ = os.ReadFile(filepath.Join(r.WorkspaceRoot(), session, "c.txt"))
	if string(b) != "ALPHA\nbeta\nGAMMA\n" {
		t.Fatalf("failed multi-hunk must not write: %q", b)
	}
}

// C2-3: todo.write validates, persists outside the workspace digest and
// answers the rendered checklist; a single in_progress is enforced.
func TestTodoWritePersistsAndValidates(t *testing.T) {
	r := openHooksRuntime(t)
	ctx := context.Background()
	session := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	out, err := r.Execute(ctx, Approval, session, "todo.write", json.RawMessage(`{"todos":[{"content":"步骤一","status":"completed"},{"content":"步骤二","status":"in_progress","priority":"high"},{"content":"步骤三"}]}`), false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.Output, "3 todo(s) stored") || !strings.Contains(out.Output, "[x] (completed|medium) 步骤一") || !strings.Contains(out.Output, "[ ] (in_progress|high) 步骤二") {
		t.Fatalf("rendered = %s", out.Output)
	}
	raw, err := os.ReadFile(filepath.Join(r.WorkspaceRoot(), ".todos", session+".json"))
	if err != nil || !json.Valid(raw) {
		t.Fatalf("persisted todos = %s err=%v", raw, err)
	}
	// Two in_progress items refuse.
	if _, err = r.Execute(ctx, Approval, session, "todo.write", json.RawMessage(`{"todos":[{"content":"a","status":"in_progress"},{"content":"b","status":"in_progress"}]}`), false); err == nil || !strings.Contains(err.Error(), "one todo") {
		t.Fatalf("double in_progress error = %v", err)
	}
	// Bad status refuses.
	if _, err = r.Execute(ctx, Approval, session, "todo.write", json.RawMessage(`{"todos":[{"content":"a","status":"done"}]}`), false); err == nil {
		t.Fatal("bad status accepted")
	}
	// Over-budget list refuses.
	var big strings.Builder
	big.WriteString(`{"todos":[`)
	for i := 0; i < 51; i++ {
		if i > 0 {
			big.WriteString(",")
		}
		big.WriteString(`{"content":"t"}`)
	}
	big.WriteString("]}")
	if _, err = r.Execute(ctx, Approval, session, "todo.write", json.RawMessage(big.String()), false); err == nil {
		t.Fatal("over-budget todo list accepted")
	}
}
