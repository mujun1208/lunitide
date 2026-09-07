package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/lunitide/lunitide/internal/toolruntime"
)

func TestChatApprovalLostReplyReturnsSameDecision(t *testing.T) {
	e := newArtifactEngine(t)
	ctx := context.Background()
	p, err := e.tools.Prepare(ctx, "run", artifactSession, "decision", "user.ask", json.RawMessage(`{"questions":[{"prompt":"部署方式","options":[{"label":"容器"},{"label":"虚拟机"}]}]}`), toolruntime.Approval, 0)
	if err != nil {
		t.Fatal(err)
	}
	args := map[string]any{"sessionId": artifactSession, "callId": p.CallID, "argsDigest": p.ArgsDigest, "approved": true}
	raw, _ := json.Marshal(args)
	first := handleChatToolApprove(e, ctx, artifactRequest(string(raw)))
	second := handleChatToolApprove(e, ctx, artifactRequest(string(raw)))
	if !first.OK || !second.OK {
		t.Fatalf("first=%+v second=%+v", first, second)
	}
	a, _ := json.Marshal(first.Payload)
	b, _ := json.Marshal(second.Payload)
	if string(a) != string(b) {
		t.Fatalf("replay changed result: %s / %s", a, b)
	}
}

func TestChatApprovalWorkspaceChangeIsNotMisreportedAsConsumed(t *testing.T) {
	e := newArtifactEngine(t)
	ctx := context.Background()
	dir, err := e.tools.SessionFolder(artifactSession)
	if err != nil {
		t.Fatal(err)
	}
	p, err := e.tools.Prepare(ctx, "run", artifactSession, "write", "workspace.write", json.RawMessage(`{"path":"note.txt","content":"x"}`), toolruntime.Approval, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "changed.txt"), []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(map[string]any{"sessionId": artifactSession, "callId": p.CallID, "argsDigest": p.ArgsDigest, "approved": true})
	resp := handleChatToolApprove(e, ctx, artifactRequest(string(raw)))
	if resp.OK || resp.Error == nil || resp.Error.Code != "TOOL_WORKSPACE_CHANGED" {
		t.Fatalf("workspace error: %+v", resp)
	}
	retry := handleChatToolApprove(e, ctx, artifactRequest(string(raw)))
	if retry.OK || retry.Error == nil || retry.Error.Code != "TOOL_EXECUTION_FAILED" {
		t.Fatalf("failed replay: %+v", retry)
	}
}
