package app

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSettingsPlaneToolsHiddenWithoutServices(t *testing.T) {
	e := NewEngineWithGateway(chatAttachmentProvider{}, "test", streamTestLease{})
	var names []string
	for _, d := range e.settingsPlaneToolDefinitions() {
		names = append(names, d.Name)
	}
	// capability.discover is self-contained (static builtin keywords + preset
	// catalog, nil-safe on MCP/skill services) and must survive even when the
	// settings-plane services are absent; everything service-bound stays hidden.
	if len(names) != 1 || names[0] != "capability.discover" {
		t.Fatalf("expected only capability.discover without services, got %v", names)
	}
}

func TestInvokeMcpPresetsListsCuratedCatalog(t *testing.T) {
	e := NewEngineWithGateway(chatAttachmentProvider{}, "test", streamTestLease{})
	out, err := e.invokeMcpPresets()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"presetId":"playwright"`) || !strings.Contains(out, `"presetId":"filesystem"`) {
		t.Fatalf("preset catalog missing expected servers: %s", out)
	}
	var parsed struct {
		Items []struct {
			PresetID        string `json:"presetId"`
			NeedsArgs       bool   `json:"needsArgs"`
			NeedsCredential bool   `json:"needsCredential"`
			ArgDefault      string `json:"argDefault"`
		} `json:"items"`
	}
	if json.Unmarshal([]byte(out), &parsed) != nil || len(parsed.Items) == 0 {
		t.Fatalf("preset catalog is not JSON items: %s", out)
	}
	var filesystem bool
	for _, item := range parsed.Items {
		if item.NeedsCredential {
			t.Fatalf("chat MCP catalog still lists a credential preset: %+v", item)
		}
		if item.PresetID == "tushare" || item.PresetID == "juhe-query" || item.PresetID == "gdrive" {
			t.Fatalf("paid/fill-in preset %s must stay off chat install", item.PresetID)
		}
		if item.PresetID == "filesystem" {
			filesystem = true
			if !item.NeedsArgs || item.ArgDefault == "" || strings.Contains(item.ArgDefault, `\`) {
				t.Fatalf("filesystem must ship a sandbox argDefault, got %+v", item)
			}
		}
	}
	if !filesystem {
		t.Fatal("filesystem missing from chat MCP catalog")
	}
}

func TestInvokeMcpInstallRequiresService(t *testing.T) {
	e := NewEngineWithGateway(chatAttachmentProvider{}, "test", streamTestLease{})
	if _, err := e.invokeMcpInstallPreset(t.Context(), []byte(`{"presetId":"playwright"}`)); err == nil {
		t.Fatal("expected MCP service unavailable")
	}
}

// capability-self-bootstrap P3: the approval card must name what gets
// installed and where its data goes, and the installer hook must fail closed
// without the MCP service.
func TestMcpInstallApprovalSummaryNamesPresetAndDataFlow(t *testing.T) {
	s := mcpInstallApprovalSummary([]byte(`{"presetId":"fetch"}`))
	for _, want := range []string{"Fetch", "uvx", "无需密钥", "第三方服务端"} {
		if !strings.Contains(s, want) {
			t.Fatalf("summary %q missing %q", s, want)
		}
	}
	if mcpInstallApprovalSummary([]byte(`{"presetId":"no-such-preset"}`)) != "" {
		t.Fatal("unknown preset falls back to the generic summary")
	}
	if mcpInstallApprovalSummary([]byte(`not json`)) != "" {
		t.Fatal("unreadable args fall back to the generic summary")
	}
	if got := approvalRequiredSummary("mcp.install", []byte(`{"presetId":"fetch"}`)); !strings.Contains(got, "Fetch") {
		t.Fatalf("approvalRequiredSummary must use the preset card: %q", got)
	}
}

func TestInstallMcpPresetViaRuntimeRequiresService(t *testing.T) {
	e := NewEngineWithGateway(chatAttachmentProvider{}, "test", streamTestLease{})
	if _, err := e.installMcpPresetViaRuntime(t.Context(), "sess", []byte(`{"presetId":"playwright"}`)); err == nil {
		t.Fatal("expected MCP service unavailable")
	}
}
