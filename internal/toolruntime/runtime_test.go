package toolruntime

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
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
	if _, e := r.Execute(context.Background(), FullAccess, s, "command.run", json.RawMessage(`{"argv":["go","version"]}`), false); e != nil {
		t.Fatal(e)
	}
	if _, e := r.Execute(context.Background(), FullAccess, s, "command.run", json.RawMessage(`{"argv":["cmd","/c","del","x"]}`), false); e == nil {
		t.Fatal("dangerous command ran")
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
	if err != nil || html.Artifact == nil || html.Artifact.Kind != "html" || html.Artifact.Path != "site/index.html" || html.Artifact.Content != "<h1>preview</h1>" {
		t.Fatalf("html result = %+v, err=%v", html, err)
	}
	text, err := r.Execute(context.Background(), AutoEdit, session, "workspace.write", json.RawMessage(`{"path":"notes.txt","content":"plain"}`), false)
	if err != nil || text.Artifact != nil {
		t.Fatalf("text result = %+v, err=%v", text, err)
	}
}
