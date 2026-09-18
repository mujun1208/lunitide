package app

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSettingsPlaneToolsHiddenWithoutServices(t *testing.T) {
	e := NewEngineWithGateway(chatAttachmentProvider{}, "test", streamTestLease{})
	if defs := e.settingsPlaneToolDefinitions(); len(defs) != 0 {
		t.Fatalf("expected no settings-plane tools without services, got %#v", defs)
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
