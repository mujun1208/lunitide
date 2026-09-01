package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/gateway"
)

func TestSubagentCannotOutrankTheTurnThatSpawnedIt(t *testing.T) {
	// Every subagent tool call ran as toolruntime.FullAccess whatever the
	// parent turn's mode was, so delegating from an approval-mode turn was a
	// way to get a write executed with nobody approving it.
	e := newSubagentChatEngine(t)
	profile, _, ok := resolveSubagentProfile(subTestPolicy(), "explore")
	if !ok {
		t.Fatal("explore profile missing")
	}
	// The profile allows the write here, so the inherited mode is the only
	// thing left that can stop it.
	allowed := map[string]bool{"workspace.write": true}
	calls := []gateway.ToolCall{
		{ID: "w", Name: "workspace.write", Arguments: json.RawMessage(`{"path":"escalated.txt","content":"x"}`)},
	}

	gated := e.runSubagentToolCalls(context.Background(), subTestSession, profile, allowed, calls, executionModeApproval)
	if len(gated) != 1 {
		t.Fatalf("messages = %d, want 1", len(gated))
	}
	if !strings.Contains(gated[0].Content, "approval required") {
		t.Fatalf("write ran under an approval-mode parent: %q", gated[0].Content)
	}

	// A full-access parent keeps working: the point is inheritance, not a
	// blanket ban on delegated writes.
	granted := e.runSubagentToolCalls(context.Background(), subTestSession, profile, allowed, calls, executionModeFullAccess)
	if len(granted) != 1 {
		t.Fatalf("messages = %d, want 1", len(granted))
	}
	if strings.Contains(granted[0].Content, "approval required") {
		t.Fatalf("write was gated under a full-access parent: %q", granted[0].Content)
	}
}

func TestSubagentUnsetParentModeFailsClosed(t *testing.T) {
	// A caller that forgets to pass the mode must land on the strict end, not
	// on "invalid execution mode" and not on full access.
	e := newSubagentChatEngine(t)
	profile, _, ok := resolveSubagentProfile(subTestPolicy(), "explore")
	if !ok {
		t.Fatal("explore profile missing")
	}
	allowed := map[string]bool{"workspace.write": true}
	calls := []gateway.ToolCall{
		{ID: "w", Name: "workspace.write", Arguments: json.RawMessage(`{"path":"unset.txt","content":"x"}`)},
	}
	msgs := e.runSubagentToolCalls(context.Background(), subTestSession, profile, allowed, calls, executionMode(""))
	if len(msgs) != 1 {
		t.Fatalf("messages = %d, want 1", len(msgs))
	}
	if !strings.Contains(msgs[0].Content, "approval required") {
		t.Fatalf("unset parent mode did not fail closed: %q", msgs[0].Content)
	}
}

func TestPeopleAgentDeniesShellCommands(t *testing.T) {
	// The people agent answers inbound colleague and IM traffic with nobody
	// watching, so an external message must not be able to reach a shell.
	if peopleAgentAllowedTool("command.run") {
		t.Fatal("command.run must not be reachable from an inbound colleague message")
	}
	for _, d := range peopleAgentToolDefinitions(engineToolDefinitions()) {
		if d.Name == "command.run" {
			t.Fatal("command.run is still offered to the people agent")
		}
	}
}

func TestPeopleAgentNeverRunsFullAccess(t *testing.T) {
	// full-access is also what unlocks unconfined whole-disk tool execution
	// (fullDiskChat), so an unattended turn asking for it hands an inbound
	// message the entire filesystem.
	if mode := peopleAgentExecutionMode(); mode == executionModeFullAccess {
		t.Fatalf("people agent mode = %q; an unattended turn must not be full-access", mode)
	}
}
