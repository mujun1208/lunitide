package producthub

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// Opening the hub must recount the catalog compiled into this engine.
// A saved snapshot with one stale card and a one-node graph must not be
// what 功能全景, 知识图谱, or the exported report render.
func TestOpenRecountsCompiledCatalogNotTheSavedSnapshot(t *testing.T) {
	ctx := context.Background()
	mem := &MemoryPersist{}
	if err := mem.ProductHubSaveEdition(ctx, Edition{
		EditionID:   "old-snapshot",
		GeneratedAt: "2026-09-21T00:00:00Z",
		CardCount:   1,
		Features:    []Card{{StableKey: "feature.stale.only", Name: "过期卡", Domain: "foundation", Module: "diagnostics"}},
		Graph:       Graph{Nodes: []GraphNode{{ID: "only", StableKey: "only", Type: "Feature"}}},
	}); err != nil {
		t.Fatal(err)
	}
	svc := New(mem)

	overview, err := svc.Overview(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if overview.CardCount <= 1 {
		t.Fatalf("功能全景 still shows the saved snapshot, cardCount=%d", overview.CardCount)
	}
	graph, err := svc.Graph(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(graph.Nodes) <= 1 {
		t.Fatalf("知识图谱 still shows the saved snapshot, nodes=%d", len(graph.Nodes))
	}
	var pageCard bool
	features := 0
	for _, node := range graph.Nodes {
		if node.Type == "Feature" {
			features++
		}
		if strings.Contains(node.StableKey, ".page.") {
			pageCard = true
		}
	}
	if !pageCard {
		t.Fatal("recounted graph has no page card from the compiled catalog")
	}

	md, _, err := svc.Export(ctx, "md")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(md, "下次生成会自动出现") {
		t.Fatal("report still says a new verb appears on the next generate")
	}
	if strings.Contains(md, "每次输入密码进入时，按本机引擎里的活源目录重算") {
		t.Fatal("report still says every login recounts the catalog")
	}
	if !strings.Contains(md, "版本没变") || !strings.Contains(md, "复查通过才改为") {
		t.Fatal("report does not state the version read and the recheck rule")
	}
	if !strings.Contains(md, fmt.Sprintf("功能卡：%d", features)) {
		t.Fatalf("report feature count is not the recounted feature count %d", features)
	}
	if !strings.Contains(md, fmt.Sprintf("节点 %d，边 %d", len(graph.Nodes), len(graph.Edges))) {
		t.Fatal("report graph summary is not the recounted graph")
	}
	if strings.Contains(md, "功能卡：1 ") || strings.Contains(md, "节点 1，边 0") {
		t.Fatal("report still prints the saved snapshot")
	}
}
