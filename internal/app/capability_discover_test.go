package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/llmadapter"
)

func TestCapabilityScore(t *testing.T) {
	if got := capabilityScore("web.search", "use web.search now"); got != 100 {
		t.Fatalf("containment score=%d", got)
	}
	// A Chinese need bridges to a Chinese description via bigrams.
	if got := capabilityScore("搜索公司公开信息", "无密钥网页搜索"); got < capabilityMatchThreshold {
		t.Fatalf("bigram score=%d", got)
	}
	// Concept groups bridge cross-language gaps.
	if got := capabilityScore("明天天气怎样", "weather forecast"); got < 20 {
		t.Fatalf("concept score=%d", got)
	}
	// Stopword bigrams carry no evidence.
	if got := capabilityScore("帮我查一下这个", "随便一段文字"); got >= capabilityMatchThreshold {
		t.Fatalf("stopword score=%d", got)
	}
	if got := capabilityScore("量子引力波占卜", "无密钥网页搜索"); got >= capabilityMatchThreshold {
		t.Fatalf("no-match score=%d", got)
	}
}

func TestIsGatewayTool(t *testing.T) {
	for _, name := range []string{"mcp_01j5xxxxxxxxxxxxxxxxxx_geocode", "mcp.search", "mcp.call", "capability.discover", "mcp.presets", "mcp.install"} {
		if !isGatewayTool(name) {
			t.Errorf("%s must be a gateway tool", name)
		}
	}
	for _, name := range []string{"web.search", "workspace.read", "", "mcp", "mcpfoo"} {
		if isGatewayTool(name) {
			t.Errorf("%s must not be a gateway tool", name)
		}
	}
}

func TestDiscoverCapabilitiesBuiltinMatch(t *testing.T) {
	e := &Engine{}
	out, err := e.discoverCapabilities(context.Background(), []byte(`{"need":"搜索某公司的公开信息"}`))
	if err != nil {
		t.Fatal(err)
	}
	var res struct {
		Need     string                 `json:"need"`
		Builtin  []discoveredCapability `json:"builtin"`
		Guidance string                 `json:"guidance"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("bad json: %v", err)
	}
	found := false
	for _, c := range res.Builtin {
		if c.Name == "web.search" {
			found = true
		}
	}
	if !found {
		t.Fatalf("web.search missing from builtin: %s", out)
	}
	if res.Guidance == "" {
		t.Fatal("guidance empty")
	}
}

func TestDiscoverCapabilitiesPresetMatch(t *testing.T) {
	e := &Engine{}
	out, err := e.discoverCapabilities(context.Background(), []byte(`{"need":"抓取网页转成 Markdown"}`))
	if err != nil {
		t.Fatal(err)
	}
	var res struct {
		InstallablePresets []discoveredCapability `json:"installablePresets"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("bad json: %v", err)
	}
	found := false
	for _, c := range res.InstallablePresets {
		if c.Name == "fetch" {
			found = true
		}
	}
	if !found {
		t.Fatalf("fetch preset missing: %s", out)
	}
}

func TestDiscoverCapabilitiesNoMatch(t *testing.T) {
	e := &Engine{}
	out, err := e.discoverCapabilities(context.Background(), []byte(`{"need":"量子引力波占卜"}`))
	if err != nil {
		t.Fatal(err)
	}
	var res struct {
		ConnectedMcp       []discoveredCapability `json:"connectedMcp"`
		Builtin            []discoveredCapability `json:"builtin"`
		Skills             []discoveredCapability `json:"skills"`
		InstallablePresets []discoveredCapability `json:"installablePresets"`
		Guidance           string                 `json:"guidance"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("bad json: %v", err)
	}
	if len(res.ConnectedMcp)+len(res.Builtin)+len(res.Skills)+len(res.InstallablePresets) != 0 {
		t.Fatalf("expected no matches: %s", out)
	}
	if !strings.Contains(res.Guidance, "没有找到匹配能力") {
		t.Fatalf("guidance=%q", res.Guidance)
	}
}

func TestDiscoverCapabilitiesValidatesInput(t *testing.T) {
	e := &Engine{}
	if _, err := e.discoverCapabilities(context.Background(), []byte(`{}`)); err == nil {
		t.Fatal("empty need must error")
	}
	if _, err := e.discoverCapabilities(context.Background(), []byte(`{"need":"  "}`)); err == nil {
		t.Fatal("blank need must error")
	}
}

// capability-self-bootstrap P4: a preset the user approved before must tell
// the model to reconnect, not to install a second copy.
func TestDiscoverAnnotatesPreviouslyInstalledPreset(t *testing.T) {
	e := NewEngineWithGateway(chatAttachmentProvider{}, "test", streamTestLease{})
	e.mcpPresetByEP.Store("ep-1", "fetch")
	out, err := e.discoverCapabilities(context.Background(), []byte(`{"need":"抓取网页转成 Markdown"}`))
	if err != nil {
		t.Fatal(err)
	}
	var res struct {
		InstallablePresets []discoveredCapability `json:"installablePresets"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("bad json: %v", err)
	}
	found := false
	for _, p := range res.InstallablePresets {
		if p.Name != "fetch" {
			continue
		}
		found = true
		if !strings.Contains(p.How, "已安装") || !strings.Contains(p.How, "重新连接") {
			t.Fatalf("How must prefer reconnect over reinstall: %q", p.How)
		}
	}
	if !found {
		t.Fatalf("fetch preset missing: %s", out)
	}
}

func TestSettingsPlaneToolDefinitionsIncludeDiscover(t *testing.T) {
	e := &Engine{}
	found := false
	for _, d := range e.settingsPlaneToolDefinitions() {
		if d.Name == "capability.discover" {
			found = true
		}
	}
	if !found {
		t.Fatal("capability.discover definition missing with zero services wired")
	}
}

func TestPickTaskToolsKeepsGatewayTools(t *testing.T) {
	catalog := []llmadapter.ToolDefinition{
		{Name: "web.search"}, {Name: "capability.discover"}, {Name: "mcp.install"},
	}
	out := pickTaskTools(nil, catalog, map[string]bool{})
	hasDiscover := false
	for _, d := range out {
		if d.Name == "capability.discover" {
			hasDiscover = true
		}
	}
	if !hasDiscover {
		t.Fatalf("capability.discover must survive lane trimming: %+v", out)
	}
}
