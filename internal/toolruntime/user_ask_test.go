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

func TestUserAskRequiresApprovalThenReturnsDecision(t *testing.T) {
	r, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	session := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	args := json.RawMessage(`{"title":"需求边界","questions":[{"id":"deploy","prompt":"部署方式","options":[{"id":"k8s","label":"容器化"},{"id":"vm","label":"虚拟机"}]}]}`)
	if _, err = r.Execute(context.Background(), FullAccess, session, "user.ask", args, false); !errors.Is(err, ErrApprovalRequired) {
		t.Fatalf("unapproved user.ask must gate, got %v", err)
	}
	out, err := r.Execute(context.Background(), FullAccess, session, "user.ask", args, true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.Output, "用户已提交决策") {
		t.Fatalf("approved output = %q", out.Output)
	}
	if _, err = r.Execute(context.Background(), Approval, session, "user.ask", json.RawMessage(`{"questions":[]}`), false); err == nil {
		t.Fatal("empty questions must be invalid")
	}
	packed := UserAskApprovalSummary(args)
	if !strings.Contains(packed, `"title":"需求边界"`) || !strings.Contains(packed, `"questions"`) {
		t.Fatalf("approval summary = %q", packed)
	}
	if err := validateUserAsk(json.RawMessage(`{"reason":"login","questions":[{"prompt":"登录","options":[{"label":"我登好了"},{"label":"取消"}]}]}`)); err != nil {
		t.Fatalf("reason=login must be valid: %v", err)
	}
	if err := validateUserAsk(json.RawMessage(`{"reason":"nope","questions":[{"prompt":"登录","options":[{"label":"我登好了"},{"label":"取消"}]}]}`)); err == nil {
		t.Fatal("unknown reason must fail")
	}
}

func TestUserAskDecisionSurvivesUnrelatedWorkspaceChange(t *testing.T) {
	r, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	ctx := context.Background()
	session := "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	args := json.RawMessage(`{"questions":[{"prompt":"部署方式","options":[{"label":"容器化"},{"label":"虚拟机"}]}]}`)
	pending, err := r.Prepare(ctx, "run", session, "ask-1", "user.ask", args, Approval, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	root, err := r.sessionPath(session)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, "new-note.txt"), []byte("unrelated edit"), 0600); err != nil {
		t.Fatal(err)
	}
	out, err := r.Decide(ctx, session, pending.CallID, pending.ArgsDigest, true)
	if err != nil || !strings.Contains(out.Output, "用户已提交决策") {
		t.Fatalf("decision blocked by unrelated files: %v", err)
	}
}
