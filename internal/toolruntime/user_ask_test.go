package toolruntime

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
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
}
