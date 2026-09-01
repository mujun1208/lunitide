package app

import (
	"encoding/json"
	"strings"
	"testing"
)

// The tool runtime owns the approval gate, but the chat loop dispatches a few
// tool families itself — MCP, the settings-plane installers, browser.act — so
// those names never reach the gate. They ran unprompted in every mode, 手动审批
// included, which is the one mode whose entire promise is a prompt.
func TestUngatedEngineToolsRefusedInApprovalMode(t *testing.T) {
	for _, tc := range []struct{ name, args string }{
		{"mcp.install", `{"presetId":"p1"}`},
		{"plugin.install", `{"id":"x"}`},
		{"mcp.call", `{"tool":"send_email","arguments":{}}`},
		{"mcp_01ARZ3NDEKTSV4RRFFQ69G5FAW_send_email", `{}`},
		{"browser.act", `{"op":"click","ref":"e12"}`},
		{"browser.act", `{"op":"type","ref":"e12","text":"hi"}`},
		{"browser.act", `{"op":"press","key":"Enter"}`},
		{"browser.act", `{"op":"select","ref":"e12","value":"a"}`},
		{"browser.act", `{"op":"dialog","accept":true}`},
		{"browser.act", `not json`}, // Unreadable args fail closed.
	} {
		reason, deny := ungatedEngineToolDenied(executionModeApproval, false, tc.name, json.RawMessage(tc.args))
		if !deny {
			t.Fatalf("%s %s ran without a prompt in approval mode", tc.name, tc.args)
		}
		if !strings.HasPrefix(reason, "ok:false") {
			t.Fatalf("%s refusal must read as a failed tool call: %q", tc.name, reason)
		}
	}
}

func TestUngatedEngineToolsStayOpenWhenTheOperatorGrantedAccess(t *testing.T) {
	// auto-edit and full-access are the modes where the operator has already
	// said yes. Refusing there would just break working setups.
	for _, mode := range []executionMode{executionModeAutoEdit, executionModeFullAccess} {
		for _, tc := range []struct{ name, args string }{
			{"mcp.call", `{"tool":"send_email","arguments":{}}`},
			{"mcp_01ARZ3NDEKTSV4RRFFQ69G5FAW_send_email", `{}`},
			{"browser.act", `{"op":"click","ref":"e12"}`},
		} {
			if _, deny := ungatedEngineToolDenied(mode, false, tc.name, json.RawMessage(tc.args)); deny {
				t.Fatalf("%s must stay available in %q", tc.name, mode)
			}
		}
	}
}

func TestReadOnlyBranchesAreNotGated(t *testing.T) {
	// Listing what is installed, or reading a page, is how the model finds out
	// what it may do. Gating a lookup would put a prompt in front of looking.
	for _, tc := range []struct{ name, args string }{
		{"mcp.search", `{"query":"mail"}`},
		{"mcp.presets", `{}`},
		{"plugin.search", `{"query":"mail"}`},
		{"browser.act", `{"op":"snapshot"}`},
		{"browser.act", `{"op":"read","url":"https://example.com"}`},
		{"browser.act", `{"op":"navigate","url":"https://example.com"}`},
		{"browser.act", `{"op":"tabs"}`},
		{"workspace.read", `{"path":"a.txt"}`},
	} {
		if _, deny := ungatedEngineToolDenied(executionModeApproval, false, tc.name, json.RawMessage(tc.args)); deny {
			t.Fatalf("%s %s must not be gated", tc.name, tc.args)
		}
	}
}

func TestCompanionRefusesInstallAndBrowserActuation(t *testing.T) {
	// A voice turn runs at full access with no screen to approve on, so the
	// two families that add software to the machine or click inside a
	// signed-in browser have to be refused outright rather than gated.
	for _, tc := range []struct{ name, args string }{
		{"mcp.install", `{"presetId":"p1"}`},
		{"plugin.install", `{"id":"x"}`},
		{"browser.act", `{"op":"click","ref":"e12"}`},
	} {
		if _, deny := ungatedEngineToolDenied(executionModeFullAccess, true, tc.name, json.RawMessage(tc.args)); !deny {
			t.Fatalf("%s must not run from a voice turn", tc.name)
		}
	}
	// Reading through MCP is still how voice answers questions.
	if _, deny := ungatedEngineToolDenied(executionModeFullAccess, true, "mcp.call", json.RawMessage(`{"tool":"lookup"}`)); deny {
		t.Fatal("mcp.call must stay usable from a voice turn")
	}
}
