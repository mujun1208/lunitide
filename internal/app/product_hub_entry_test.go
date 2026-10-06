package app

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/domain/provider"
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

type providerRepositoryWithModelSlots struct{ providerRepositoryStub }

func (providerRepositoryWithModelSlots) List(context.Context, provider.Filter) ([]provider.Provider, error) {
	now := time.Now().UTC()
	return []provider.Provider{{
		ID: "glm", Name: "智谱 GLM", Protocol: provider.ProtocolOpenAICompatible,
		BaseURL: "https://open.bigmodel.cn/api/paas/v4",
		Models: []provider.Model{
			{ModelID: "glm-5.3", DisplayName: "GLM-5.3", Kind: provider.KindLLM, IsDefault: true, ContextWindow: 131072},
			{ModelID: "glm-4.5v", DisplayName: "GLM-4.5V", Kind: provider.KindVision, KindDefault: true, ContextWindow: 131072},
		},
		Status: provider.StatusEnabled, CredentialState: provider.CredentialConfigured,
		CreatedAt: now, UpdatedAt: now, Version: 1,
	}}, nil
}

func TestSyncModelSlotsMirrorsProviderConfig(t *testing.T) {
	defer producthub.SetModelSlots(nil)
	e := NewEngine(providerRepositoryWithModelSlots{}, "test")
	syncModelSlots(e, context.Background())
	slots := producthub.RegisteredModelSlots()
	if len(slots) != 2 {
		t.Fatalf("slots got %d want 2: %+v", len(slots), slots)
	}
	var glm53, glm45v producthub.ModelSlot
	for _, s := range slots {
		switch s.ModelID {
		case "glm-5.3":
			glm53 = s
		case "glm-4.5v":
			glm45v = s
		}
	}
	if glm53.ModelID == "" || !glm53.IsDefault || glm53.Kind != "llm" || glm53.ContextWindow != 131072 {
		t.Fatalf("glm-5.3 槽位映射错误: %+v", glm53)
	}
	if glm53.Status != "enabled" || glm53.CredentialState != "configured" || glm53.Protocol != "openai_compatible" || glm53.ProviderName != "智谱 GLM" {
		t.Fatalf("glm-5.3 供应商状态映射错误: %+v", glm53)
	}
	if glm45v.ModelID == "" || glm45v.Kind != "vision" || !glm45v.KindDefault {
		t.Fatalf("glm-4.5v 槽位映射错误: %+v", glm45v)
	}
	// providers 为空的引擎不注入也不清空：guest/旧入口保持现状。
	syncModelSlots(&Engine{}, context.Background())
	if got := producthub.RegisteredModelSlots(); len(got) != 2 {
		t.Fatalf("空引擎不应改动已注入槽位, got %d", len(got))
	}
}
