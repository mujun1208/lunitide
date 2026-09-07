package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/llmadapter"
	"github.com/lunitide/lunitide/internal/toolruntime"
)

func TestSubagentReadCapsEnforcedInDefinitionsAndExecution(t *testing.T) {
	e := newSubagentChatEngine(t)
	profile, _, _ := resolveSubagentProfile(subTestPolicy(), "general-purpose")
	have := toolNameSet(subagentEngineToolDefinitions(profile))
	for _, name := range []string{"docx.gen", "excel.gen", "pptx.gen", "pdf.gen", "workspace.write", "im.send", "computer.act"} {
		if have[name] {
			t.Fatalf("read-only profile offered %s", name)
		}
	}
	// Even an overbroad cached tool map cannot bypass the profile snapshot.
	calls := []llmadapter.ToolCall{{ID: "write", Name: "workspace.write", Arguments: json.RawMessage(`{"path":"forbidden.txt","content":"no"}`)}, {ID: "docx", Name: "docx.gen", Arguments: json.RawMessage(`{"path":"forbidden.docx","title":"no","blocks":[{"text":"no"}]}`)}}
	msgs := e.runSubagentToolCalls(context.Background(), subTestSession, profile, map[string]bool{"workspace.write": true, "docx.gen": true}, calls, executionModeFullAccess)
	for _, msg := range msgs {
		if !strings.Contains(msg.Content, "not allowed") {
			t.Fatalf("read-only side effect: %s", msg.Content)
		}
	}
	if _, err := e.tools.Execute(context.Background(), toolruntime.FullAccess, subTestSession, "workspace.read", json.RawMessage(`{"path":"forbidden.txt"}`), false); err == nil {
		t.Fatal("disallowed write created a file")
	}
	custom := subagentProfileDef{ID: "custom", ReadCaps: []string{"web.search"}}
	customTools := toolNameSet(subagentEngineToolDefinitions(custom))
	if len(customTools) != 1 || !customTools["web.search"] {
		t.Fatalf("custom ReadCaps not applied: %v", customTools)
	}
}

func TestSubagentExplicitExpertWriteStillWorksAndParentApprovalStillApplies(t *testing.T) {
	e := newSubagentChatEngine(t)
	profile, _, _ := resolveSubagentProfile(subTestPolicy(), "writer")
	profile = applyExpertSpawnCaps(profile, []string{"workspace.write", "excel.gen", "computer.act"})
	allowed := toolNameSet(subagentEngineToolDefinitions(profile))
	if !allowed["workspace.write"] || !allowed["excel.gen"] || allowed["computer.act"] {
		t.Fatalf("explicit write tools=%v", allowed)
	}
	calls := []llmadapter.ToolCall{{ID: "write", Name: "workspace.write", Arguments: json.RawMessage(`{"path":"expert.txt","content":"approved expert content"}`)}}
	gated := e.runSubagentToolCalls(context.Background(), subTestSession, profile, allowed, calls, executionModeApproval)
	if !strings.Contains(gated[0].Content, "approval required") {
		t.Fatalf("write bypassed parent approval: %s", gated[0].Content)
	}
	written := e.runSubagentToolCalls(context.Background(), subTestSession, profile, allowed, calls, executionModeFullAccess)
	if strings.Contains(written[0].Content, "refused") {
		t.Fatalf("explicit write refused: %s", written[0].Content)
	}
	read, err := e.tools.Execute(context.Background(), toolruntime.FullAccess, subTestSession, "workspace.read", json.RawMessage(`{"path":"expert.txt"}`), false)
	if err != nil || !strings.Contains(read.Output, "approved expert content") {
		t.Fatalf("authorized file absent: %s %v", read.Output, err)
	}
}

func TestSubagentReadOnlyBrowserAndCommandArguments(t *testing.T) {
	profile := subagentProfileDef{ReadCaps: fullSubagentReadCaps()}
	for _, tc := range []struct {
		name, args string
		allow      bool
	}{
		{"browser.act", `{"op":"snapshot"}`, true},
		{"browser.act", `{"op":"click","selector":"ref1"}`, false},
		{"browser.act", `{"op":"tabs","tab":"close"}`, false},
		{"command.run", `{"argv":["go","version"]}`, true},
		{"command.run", `{"argv":["git","--no-pager","diff"]}`, true},
		{"command.run", `{"argv":["git","--no-pager","diff","--output=changed.txt"]}`, false},
		{"command.run", `{"argv":["git","--no-pager","commit","-am","unauthorized"]}`, false},
		{"command.run", `{"argv":["git","--no-pager","branch","new-branch"]}`, false},
	} {
		if got := subagentCallAllowed(profile, llmadapter.ToolCall{Name: tc.name, Arguments: json.RawMessage(tc.args)}); got != tc.allow {
			t.Fatalf("%s %s = %v", tc.name, tc.args, got)
		}
	}
	profile.WriteTools = []string{"command.run", "browser.act"}
	for _, call := range []llmadapter.ToolCall{{Name: "command.run", Arguments: json.RawMessage(`{"argv":["git","--no-pager","commit","-am","authorized"]}`)}, {Name: "browser.act", Arguments: json.RawMessage(`{"op":"click","selector":"ref1"}`)}} {
		if !subagentCallAllowed(profile, call) {
			t.Fatalf("explicit tool capability ignored: %s", call.Name)
		}
	}
	e := newSubagentChatEngine(t)
	call := llmadapter.ToolCall{Name: "browser.act", Arguments: json.RawMessage(`{"op":"click","selector":"ref1"}`)}
	if got := e.runSubagentTool(context.Background(), subTestSession, call, executionModeApproval); !strings.Contains(got, "手动审批") {
		t.Fatalf("explicit browser capability bypassed parent approval: %s", got)
	}
}
