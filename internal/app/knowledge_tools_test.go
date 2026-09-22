package app

import (
	"context"
	"strings"
	"testing"
)

func TestKnowledgeToolsReportMissingInsteadOfUnknown(t *testing.T) {
	e := NewEngine(nil, "test")
	for _, name := range []string{"kb.search", "kb.cite", "graph.expand"} {
		args := []byte(`{"query":"力矩","docId":"01ARZ3NDEKTSV4RRFFQ69G5FAV","locator":"{}","quote":"10 Nm"}`)
		out, err := e.executeKnowledgeTool(context.Background(), "sess", name, args)
		if err != nil {
			t.Fatalf("%s err = %v", name, err)
		}
		if !strings.Contains(out.Output, `"missing":true`) {
			t.Fatalf("%s = %s", name, out.Output)
		}
	}
	defs := knowledgeToolDefinitions()
	if len(defs) != 3 || defs[0].Name != "kb.search" || defs[1].Name != "kb.cite" || defs[2].Name != "graph.expand" {
		t.Fatalf("defs = %#v", defs)
	}
	seen := map[string]bool{}
	for _, d := range e.engineToolDefinitionsFor(executionModeApproval) {
		seen[d.Name] = true
	}
	for _, name := range []string{"kb.search", "kb.cite", "graph.expand"} {
		if !seen[name] {
			t.Fatalf("%s missing from engine tools", name)
		}
	}
}
