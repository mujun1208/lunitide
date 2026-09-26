package producthub

import (
	"context"
	"strings"
	"testing"
)

func TestGraphKeepsEveryPluginSettingSkillAndMCP(t *testing.T) {
	resetProductSurface()
	svc := New(&MemoryPersist{})
	svc.SetProductVersion("9.1.0")
	g, err := svc.Graph(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var plugins []string
	for _, c := range LiveCatalog() {
		if strings.HasPrefix(c.StableKey, "feature.assets.plugin.") && !strings.HasSuffix(c.StableKey, ".enable") {
			plugins = append(plugins, c.StableKey)
			id := "plugin." + strings.TrimPrefix(c.StableKey, "feature.assets.plugin.")
			node, ok := graphNode(g, id)
			if !ok || node.Type != "Plugin" || node.Name != c.Name || node.Summary == "" {
				t.Fatalf("plugin %s missing from graph: %+v", id, node)
			}
		}
		if strings.HasPrefix(c.StableKey, "feature.foundation.settings.") && !graphHas(g, c.StableKey) {
			t.Fatalf("setting %s missing", c.StableKey)
		}
	}
	if len(plugins) < 23 {
		t.Fatalf("plugin catalog %d", len(plugins))
	}
	got := 0
	for _, n := range g.Nodes {
		if n.Type != "Plugin" {
			continue
		}
		got++
		if n.StableKey == "plugin.plugins" || n.Name == "启用插件" {
			t.Fatalf("plugin roster collapsed or included the enable verb: %+v", n)
		}
	}
	if got != len(plugins) {
		t.Fatalf("plugin nodes %d catalog %d", got, len(plugins))
	}
	for _, name := range []string{"安装技能", "调用技能"} {
		if !graphHasType(g, "Skill", name) {
			t.Fatalf("skill %s collapsed", name)
		}
	}
	for _, name := range []string{"连接 MCP", "调用 MCP 工具"} {
		if !graphHasType(g, "Mcp", name) {
			t.Fatalf("mcp %s collapsed", name)
		}
	}
}

func TestOpenRebuildsACollapsedPluginRoster(t *testing.T) {
	ctx := context.Background()
	store := &MemoryPersist{}
	svc := New(store)
	svc.SetProductVersion("9.1.0")
	collapsed := Edition{
		ProductVersion: "9.1.0",
		Features: []Card{
			{StableKey: "feature.assets.plugin.llm", Name: "插件：LLM", Domain: "assets", Module: "plugins", Summary: "在插件页启用或使用「LLM」。"},
			{StableKey: "feature.assets.plugin.git", Name: "插件：Git", Domain: "assets", Module: "plugins", Summary: "在插件页启用或使用「Git」。"},
		},
		Graph: Graph{Nodes: []GraphNode{{ID: "plugin.plugins", StableKey: "plugin.plugins", Type: "Plugin", Name: "插件：工作区"}}},
	}
	if err := store.ProductHubSaveEdition(ctx, collapsed); err != nil {
		t.Fatal(err)
	}
	g, err := svc.Graph(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if graphHas(g, "plugin.plugins") || !graphHas(g, "plugin.llm") || !graphHas(g, "plugin.git") {
		t.Fatal("same version kept the collapsed plugin roster")
	}
}

func TestExpertNodeKeepsDistinctStoredSections(t *testing.T) {
	resetProductSurface()
	svc := New(&MemoryPersist{})
	svc.SetProductVersion("9.1.0")
	g, err := svc.Graph(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var expert GraphNode
	for _, node := range g.Nodes {
		if node.Type == "Expert" && node.Name == "试用专家" {
			expert = node
		}
	}
	if expert.Summary == "" || expert.Principle == "" || expert.Logic == "" || expert.Tech == "" || expert.Analysis == "" {
		t.Fatalf("expert sections missing: %+v", expert)
	}
	if expert.Summary == expert.Principle || expert.Principle == expert.Logic || expert.Logic == expert.Tech || expert.Tech == expert.Analysis {
		t.Fatalf("expert sections repeat: %+v", expert)
	}
}

func graphNode(g Graph, key string) (GraphNode, bool) {
	for _, node := range g.Nodes {
		if node.StableKey == key {
			return node, true
		}
	}
	return GraphNode{}, false
}

func graphHasType(g Graph, typ, name string) bool {
	for _, node := range g.Nodes {
		if node.Type == typ && node.Name == name {
			return true
		}
	}
	return false
}
