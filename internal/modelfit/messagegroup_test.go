package modelfit

import (
	"encoding/json"
	"testing"
)

func TestTaskOutcomeForcedSummarySequence(t *testing.T) {
	group := MessageGroup{
		Assistant: ProtocolMessage{
			Role: "assistant",
			ToolCalls: []ProtocolToolCall{
				{ID: "c1", Name: "workspace.read", Arguments: json.RawMessage(`{"path":"a.md"}`)},
				{ID: "c2", Name: "workspace.read", Arguments: json.RawMessage(`{"path":"b.md"}`)},
			},
		},
		Tools: []ProtocolMessage{
			{Role: "tool", ToolCallID: "c1", Content: "ok-a"},
			{Role: "tool", ToolCallID: "c2", Content: "ok-b"},
		},
	}
	if !MessageGroupComplete(group) {
		t.Fatal("closed tool group required")
	}
	plan := PlanForcedSummary([]MessageGroup{group}, "after_tools")
	if !plan.AppendSummary || plan.ReappendToolAssistant || plan.ExecuteTools {
		t.Fatalf("closed group must summarize once without replaying tools: %+v", plan)
	}
	if len(plan.ToolCallIDs) != 2 || plan.ToolCallIDs[0] != "c1" || plan.ToolCallIDs[1] != "c2" {
		t.Fatalf("server must see complete call IDs once: %v", plan.ToolCallIDs)
	}
	for _, event := range []string{"cancel", "restart", "lost_commit_ack"} {
		again := PlanForcedSummary([]MessageGroup{group}, event)
		if again.ExecuteTools || again.ReappendToolAssistant {
			t.Fatalf("%s must not re-execute tools: %+v", event, again)
		}
		if len(again.ToolCallIDs) != 2 {
			t.Fatalf("%s lost call IDs: %v", event, again.ToolCallIDs)
		}
	}
}

func TestMessageGroupCompleteRequiresPairedToolCalls(t *testing.T) {
	g := MessageGroup{
		Assistant: ProtocolMessage{
			Role: "assistant",
			ToolCalls: []ProtocolToolCall{
				{ID: "c1", Name: "workspace.read", Arguments: json.RawMessage(`{"path":"a.md"}`)},
			},
		},
		Tools: []ProtocolMessage{
			{Role: "tool", ToolCallID: "c1", Content: "ok"},
		},
	}
	if !MessageGroupComplete(g) {
		t.Fatal("paired complete group")
	}
	g.Tools = nil
	if MessageGroupComplete(g) {
		t.Fatal("missing tool result must not be complete")
	}
	g.Tools = []ProtocolMessage{{Role: "tool", Content: "ok"}}
	if MessageGroupComplete(g) {
		t.Fatal("empty tool_call_id must not be complete")
	}
}

func TestIncompleteStreamArgsAreNotComplete(t *testing.T) {
	g := MessageGroup{
		Assistant: ProtocolMessage{
			Role: "assistant",
			ToolCalls: []ProtocolToolCall{
				{ID: "c1", Name: "workspace.write", Arguments: json.RawMessage(`{"path":`)},
			},
		},
		Tools: []ProtocolMessage{{Role: "tool", ToolCallID: "c1", Content: "no"}},
	}
	if MessageGroupComplete(g) {
		t.Fatal("truncated JSON args must not be complete")
	}
	if CanExecuteToolCalls(g.Assistant.ToolCalls) {
		t.Fatal("incomplete stream args must not execute")
	}
	g.Assistant.ToolCalls[0].Arguments = json.RawMessage(`{"path":"a.md"}`)
	if !CanExecuteToolCalls(g.Assistant.ToolCalls) {
		t.Fatal("valid JSON args may execute")
	}
}

func TestExtractMessageGroupsKeepsLivePairing(t *testing.T) {
	groups := ExtractMessageGroups([]ProtocolMessage{
		{Role: "user", Content: "写周报"},
		{Role: "assistant", Content: "先读文件", ToolCalls: []ProtocolToolCall{
			{ID: "c1", Name: "workspace.read", Arguments: json.RawMessage(`{"path":"a.md"}`)},
		}},
		{Role: "tool", ToolCallID: "c1", Content: "ok"},
		{Role: "assistant", Content: "做好了"},
	})
	if len(groups) != 1 || !groups[0].Complete || groups[0].Assistant.ToolCalls[0].ID != "c1" || groups[0].Tools[0].Content != "ok" {
		t.Fatalf("live pairing lost: %#v", groups)
	}
	orphan := ExtractMessageGroups([]ProtocolMessage{
		{Role: "assistant", ToolCalls: []ProtocolToolCall{{ID: "", Name: "x", Arguments: json.RawMessage(`{}`)}}},
		{Role: "tool", ToolCallID: "", Content: "x"},
	})
	if len(orphan) != 1 && len(orphan) != 0 {
		t.Fatalf("unexpected orphan extract: %#v", orphan)
	}
	if len(orphan) == 1 && orphan[0].Complete {
		t.Fatal("orphan empty ids must not be complete")
	}
}
