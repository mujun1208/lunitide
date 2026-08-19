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
			PresetID string `json:"presetId"`
		} `json:"items"`
	}
	if json.Unmarshal([]byte(out), &parsed) != nil || len(parsed.Items) == 0 {
		t.Fatalf("preset catalog is not JSON items: %s", out)
	}
}

func TestInvokeMcpInstallRequiresService(t *testing.T) {
	e := NewEngineWithGateway(chatAttachmentProvider{}, "test", streamTestLease{})
	if _, err := e.invokeMcpInstallPreset(t.Context(), []byte(`{"presetId":"playwright"}`)); err == nil {
		t.Fatal("expected MCP service unavailable")
	}
}
