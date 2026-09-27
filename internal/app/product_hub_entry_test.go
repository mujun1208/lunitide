package app

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/producthub"
)

func TestEntryRequeriesOnlyWhenTheProductVersionChanges(t *testing.T) {
	restore := producthub.SetTestLiveRun(func(context.Context) ([]producthub.TaskResult, string) {
		t.Fatal("opening the hub ran a fresh probe")
		return nil, ""
	})
	t.Cleanup(restore)

	svc := producthub.New(&producthub.MemoryPersist{})
	svc.SetProductVersion("9.1.0")
	engine := &Engine{}
	engine.SetProductHub(svc)
	ctx := context.Background()
	if _, err := svc.Overview(ctx); err != nil {
		t.Fatal(err)
	}
	first, err := svc.Latest(ctx)
	if err != nil || first == nil || first.EditionID == "" || first.CardCount == 0 || len(first.Graph.Nodes) == 0 {
		t.Fatalf("first assembly %+v err=%v", first, err)
	}
	token, err := svc.Unlock(ctx, producthub.AdminUsername, "1234567890")
	if err != nil {
		t.Fatal(err)
	}
	openHub := func() bridge.Response {
		raw, _ := json.Marshal(map[string]string{"sessionToken": token})
		return handleProductHub(engine, ctx, bridge.Request{Method: "productHub.diagnostics", Payload: raw})
	}
	if resp := openHub(); !resp.OK {
		t.Fatalf("open %+v", resp)
	}
	again, err := svc.Latest(ctx)
	if err != nil || again.EditionID != first.EditionID || again.ProductVersion != "9.1.0" {
		t.Fatalf("same version stored %s %s, want %s 9.1.0", again.EditionID, again.ProductVersion, first.EditionID)
	}
	if again.CardCount != first.CardCount || len(again.Graph.Nodes) != len(first.Graph.Nodes) {
		t.Fatalf("same version changed the assembly cards %d→%d nodes %d→%d", first.CardCount, again.CardCount, len(first.Graph.Nodes), len(again.Graph.Nodes))
	}

	svc.SetProductVersion("9.2.0")
	if resp := openHub(); !resp.OK {
		t.Fatalf("version open %+v", resp)
	}
	next, err := svc.Latest(ctx)
	if err != nil || next.EditionID == first.EditionID || next.ProductVersion != "9.2.0" {
		t.Fatalf("new version stored %+v", next)
	}
	if next.CardCount == 0 || len(next.Graph.Nodes) == 0 {
		t.Fatal("new version stored an empty assembly")
	}
	ov, err := svc.Overview(ctx)
	if err != nil || ov.EditionID != next.EditionID || ov.CardCount != next.CardCount {
		t.Fatalf("overview %+v err=%v stored %s cards %d", ov, err, next.EditionID, next.CardCount)
	}
	graph, err := svc.Graph(ctx)
	if err != nil || len(graph.Nodes) != len(next.Graph.Nodes) {
		t.Fatalf("graph nodes %d err=%v", len(graph.Nodes), err)
	}
}
