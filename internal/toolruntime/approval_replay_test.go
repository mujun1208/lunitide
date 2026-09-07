package toolruntime

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestApprovalReplayAfterRestartDoesNotRepeatWrite(t *testing.T) {
	root := t.TempDir()
	r, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	const session = "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	args := json.RawMessage(`{"path":"approved.html","content":"<h1>original</h1>"}`)
	pending, err := r.Prepare(ctx, "run", session, "write-1", "workspace.write", args, Approval, 0)
	if err != nil {
		t.Fatal(err)
	}
	want, err := r.Decide(ctx, session, pending.CallID, pending.ArgsDigest, true)
	if err != nil {
		t.Fatal(err)
	}
	if want.Artifact == nil {
		t.Fatal("generated artifact missing")
	}
	path, err := r.ResolveSessionArtifact(session, "approved.html")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, []byte("user edited later"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = r.Close(); err != nil {
		t.Fatal(err)
	}
	r, err = Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	got, err := r.ReplayDecision(ctx, session, pending.CallID, pending.ArgsDigest, true)
	if err != nil || got.Digest != want.Digest || got.Artifact == nil || *got.Artifact != *want.Artifact {
		t.Fatalf("replay=%+v err=%v", got, err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "user edited later" {
		t.Fatalf("replay executed again: %q %v", data, err)
	}
	for _, wrong := range []struct {
		session, digest string
		approved        bool
	}{{session, pending.ArgsDigest, false}, {"other-session", pending.ArgsDigest, true}, {session, "other-digest", true}} {
		if _, err = r.ReplayDecision(ctx, wrong.session, pending.CallID, wrong.digest, wrong.approved); !errors.Is(err, ErrPendingConsumed) {
			t.Fatalf("different decision accepted: %v", err)
		}
	}
}

func TestApprovalReplayReportsUnfinishedAndFailedWithoutExecuting(t *testing.T) {
	r, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	ctx := context.Background()
	const session = "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	p, err := r.Prepare(ctx, "run", session, "write", "workspace.write", json.RawMessage(`{"path":"never.txt","content":"x"}`), Approval, 0)
	if err != nil {
		t.Fatal(err)
	}
	for status, want := range map[string]error{"approved": ErrDecisionInProgress, "failed": ErrDecisionFailed} {
		if _, err = r.db.ExecContext(ctx, `UPDATE chat_tool_calls SET decision='approved',status=? WHERE session_id=? AND call_id=?`, status, session, p.CallID); err != nil {
			t.Fatal(err)
		}
		if _, err = r.ReplayDecision(ctx, session, p.CallID, p.ArgsDigest, true); !errors.Is(err, want) {
			t.Fatalf("status=%s got=%v", status, err)
		}
	}
	if _, err = os.Stat(filepath.Join(r.root, session, "never.txt")); !os.IsNotExist(err) {
		t.Fatalf("replay ran pending action: %v", err)
	}
}

func TestFinishDecisionCommitsAfterClientCancellation(t *testing.T) {
	r, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	const session = "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	p, err := r.Prepare(context.Background(), "run", session, "ask", "user.ask", json.RawMessage(`{"questions":[{"prompt":"部署方式","options":[{"label":"容器"},{"label":"虚拟机"}]}]}`), Approval, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = r.db.Exec(`UPDATE chat_tool_calls SET decision='approved',status='approved' WHERE session_id=? AND call_id=?`, session, p.CallID); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	want := result("confirmed")
	if _, err = r.finishDecision(ctx, session, p.CallID, p.ArgsDigest, want, nil); err != nil {
		t.Fatal(err)
	}
	got, err := r.ReplayDecision(context.Background(), session, p.CallID, p.ArgsDigest, true)
	if err != nil || got.Digest != want.Digest {
		t.Fatalf("cancel lost result: %+v %v", got, err)
	}
}
