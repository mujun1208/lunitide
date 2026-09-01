package app

import (
	"encoding/json"
	"strings"
	"testing"
)

// The people agent replies to a colleague with no human present. Even under its
// auto-edit authority it must not reach a third-party MCP server or actuate the
// signed-in browser. Discovery and read-only browsing stay open.
func TestUnattendedMcpDeniedBlocksReachOut(t *testing.T) {
	for _, name := range []string{
		"mcp.call",
		"mcp.install",
		"plugin.install",
		"mcp_01ARZ3NDEKTSV4RRFFQ69G5FAW_send_email",
	} {
		if _, deny := unattendedMcpDenied(name, json.RawMessage(`{}`)); !deny {
			t.Errorf("unattended turn must deny %q", name)
		}
	}
}

func TestUnattendedMcpDeniedAllowsDiscovery(t *testing.T) {
	if _, deny := unattendedMcpDenied("mcp.search", json.RawMessage(`{}`)); deny {
		t.Fatal("mcp.search is read-only discovery and must stay open")
	}
}

func TestUnattendedMcpDeniedBrowserActuationOnly(t *testing.T) {
	if _, deny := unattendedMcpDenied("browser.act", json.RawMessage(`{"op":"click"}`)); !deny {
		t.Fatal("actuating browser.act must be denied for an unattended colleague turn")
	}
	if _, deny := unattendedMcpDenied("browser.act", json.RawMessage(`{"op":"navigate"}`)); deny {
		t.Fatal("read-only browser.act (navigate) must stay open")
	}
	reason, _ := unattendedMcpDenied("browser.act", json.RawMessage(`{"op":"type"}`))
	if !strings.HasPrefix(reason, "ok:false") {
		t.Fatalf("denial must be an ok:false result, got %q", reason)
	}
}

// Ordinary workspace tools are untouched by this gate — the people agent still
// needs them under its own authority.
func TestUnattendedMcpDeniedIgnoresPlainTools(t *testing.T) {
	for _, name := range []string{"workspace.write", "workspace.read", "skill.invoke", "image.generate"} {
		if _, deny := unattendedMcpDenied(name, json.RawMessage(`{}`)); deny {
			t.Errorf("gate must not touch %q", name)
		}
	}
}
