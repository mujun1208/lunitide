package app

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/lunitide/lunitide/internal/domain/provider"
	"github.com/lunitide/lunitide/internal/gateway"
	"github.com/lunitide/lunitide/internal/storage/sqlite"
	"github.com/oklog/ulid/v2"
)

type memoryRoleStore struct {
	mu   sync.Mutex
	rows []sqlite.CapabilityRoleBinding
}

func (s *memoryRoleStore) ListCapabilityRoles(context.Context) ([]sqlite.CapabilityRoleBinding, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]sqlite.CapabilityRoleBinding, len(s.rows))
	copy(out, s.rows)
	return out, nil
}

func (s *memoryRoleStore) ReplaceCapabilityRoles(_ context.Context, rows []sqlite.CapabilityRoleBinding) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rows = append([]sqlite.CapabilityRoleBinding{}, rows...)
	return nil
}

type roleCatalog struct{ providerRepositoryStub }

func (roleCatalog) List(context.Context, provider.Filter) ([]provider.Provider, error) {
	return []provider.Provider{{
		ID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", Name: "Demo", Protocol: provider.ProtocolOpenAICompatible,
		BaseURL: "https://example.com", CredentialRef: "cred",
		CredentialState: provider.CredentialConfigured, Status: provider.StatusEnabled,
		Models: []provider.Model{
			{ModelID: "chat-l", DisplayName: "chat", IsDefault: true, Kind: provider.KindLLM},
			{ModelID: "flash-l", DisplayName: "flash", Kind: provider.KindLLM},
			{ModelID: "gui-m", DisplayName: "gui", Kind: provider.KindGUI},
		},
	}}, nil
}

func emptyRolePayload() string {
	roles := []map[string]any{
		{"role": "chat"}, {"role": "flash"}, {"role": "vision"},
		{"role": "embed"}, {"role": "judge"}, {"role": "gui"},
	}
	raw, _ := json.Marshal(map[string]any{"roles": roles})
	return string(raw)
}

func TestCapabilityRolesGetEmptyReturnsSixAutos(t *testing.T) {
	e := NewEngine(roleCatalog{}, "test")
	e.SetCapabilityRoleStore(&memoryRoleStore{})
	resp := e.Handle(context.Background(), validRequest("capability.roles.get", `{}`))
	if !resp.OK {
		t.Fatalf("D-P2 %#v", resp.Error)
	}
	var out struct {
		Roles []capabilityRoleDTO `json:"roles"`
	}
	raw, _ := json.Marshal(resp.Payload)
	if json.Unmarshal(raw, &out) != nil || len(out.Roles) != 6 {
		t.Fatalf("D-P2 payload=%s", raw)
	}
	want := []string{"chat", "flash", "vision", "embed", "judge", "gui"}
	for i, role := range want {
		if out.Roles[i].Role != role || out.Roles[i].ModelID != "" {
			t.Fatalf("D-P2 row %d = %+v", i, out.Roles[i])
		}
	}
}

func TestCapabilityRolesSetRejectsJudgeEqChat(t *testing.T) {
	e := NewEngine(roleCatalog{}, "test")
	e.SetCapabilityRoleStore(&memoryRoleStore{})
	payload := map[string]any{"roles": []map[string]any{
		{"role": "chat", "providerId": "01ARZ3NDEKTSV4RRFFQ69G5FAV", "modelId": "chat-l"},
		{"role": "flash"}, {"role": "vision"}, {"role": "embed"},
		{"role": "judge", "providerId": "01ARZ3NDEKTSV4RRFFQ69G5FAV", "modelId": "chat-l"},
		{"role": "gui"},
	}}
	raw, _ := json.Marshal(payload)
	req := validRequest("capability.roles.set", string(raw))
	req.IdempotencyKey = ulid.Make().String()
	resp := e.Handle(context.Background(), req)
	if resp.OK || resp.Error == nil || resp.Error.Code != "CAPABILITY_ROLE_JUDGE_EQ_CHAT" {
		t.Fatalf("D-P1 %#v", resp)
	}
}

func TestCapabilityRolesSetRejectsKindMismatch(t *testing.T) {
	e := NewEngine(roleCatalog{}, "test")
	e.SetCapabilityRoleStore(&memoryRoleStore{})
	payload := map[string]any{"roles": []map[string]any{
		{"role": "chat"}, {"role": "flash"}, {"role": "vision"}, {"role": "embed"}, {"role": "judge"},
		{"role": "gui", "providerId": "01ARZ3NDEKTSV4RRFFQ69G5FAV", "modelId": "chat-l"},
	}}
	raw, _ := json.Marshal(payload)
	req := validRequest("capability.roles.set", string(raw))
	req.IdempotencyKey = ulid.Make().String()
	resp := e.Handle(context.Background(), req)
	if resp.OK || resp.Error == nil || resp.Error.Code != "CAPABILITY_ROLE_KIND_MISMATCH" {
		t.Fatalf("kind mismatch %#v", resp)
	}
}

func TestCapabilityRolesSetPersistsAndGetMerges(t *testing.T) {
	store := &memoryRoleStore{}
	e := NewEngine(roleCatalog{}, "test")
	e.SetCapabilityRoleStore(store)
	req := validRequest("capability.roles.set", emptyRolePayload())
	req.IdempotencyKey = ulid.Make().String()
	if resp := e.Handle(context.Background(), req); !resp.OK {
		t.Fatalf("set empty %#v", resp.Error)
	}
	if got, _ := store.ListCapabilityRoles(context.Background()); len(got) != 6 {
		t.Fatalf("stored=%d", len(got))
	}
}

type flashClassifyAdapter struct {
	models []string
}

func (a *flashClassifyAdapter) Complete(_ context.Context, _ []byte, req gateway.Request) (gateway.Response, error) {
	a.models = append(a.models, req.Model)
	return gateway.Response{Message: gateway.Message{Content: `{"route":"R1","allow":{"video.understand":true}}`}}, nil
}
func (flashClassifyAdapter) TestConnection(context.Context, []byte, gateway.Request) error {
	return nil
}
func (flashClassifyAdapter) Discover(context.Context, []byte) (gateway.Discovery, error) {
	return gateway.Discovery{}, nil
}
func (flashClassifyAdapter) Stream(context.Context, []byte, gateway.Request, func(gateway.Delta) error) (gateway.Response, error) {
	return gateway.Response{}, nil
}

func TestFlashClassifySkippedWhenRoleEmpty(t *testing.T) {
	adapter := &flashClassifyAdapter{}
	e := NewEngineWithGateway(roleCatalog{}, "test", streamTestLease{})
	e.SetCapabilityRoleStore(&memoryRoleStore{})
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (gateway.Adapter, error) {
		return adapter, nil
	})
	_, _, used := e.tryFlashClassify(context.Background(), "随便问问")
	if used || len(adapter.models) != 0 {
		t.Fatalf("D-H3b used=%v models=%v", used, adapter.models)
	}
}

func TestFlashClassifyUsesBoundModel(t *testing.T) {
	adapter := &flashClassifyAdapter{}
	store := &memoryRoleStore{rows: []sqlite.CapabilityRoleBinding{
		{Role: "flash", ProviderID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", ModelID: "flash-l"},
	}}
	e := NewEngineWithGateway(roleCatalog{}, "test", streamTestLease{})
	e.SetCapabilityRoleStore(store)
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (gateway.Adapter, error) {
		return adapter, nil
	})
	route, allow, used := e.tryFlashClassify(context.Background(), "解读这段视频")
	if !used || route != RouteR1 || allow["video.understand"] != true {
		t.Fatalf("D-H3 route=%s allow=%v used=%v", route, allow, used)
	}
	if len(adapter.models) != 1 || adapter.models[0] != "flash-l" {
		t.Fatalf("D-H3 models=%v", adapter.models)
	}
}

func TestJudgeModelIDUsesBoundRole(t *testing.T) {
	e := NewEngine(roleCatalog{}, "test")
	e.SetCapabilityRoleStore(&memoryRoleStore{rows: []sqlite.CapabilityRoleBinding{
		{Role: "judge", ProviderID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", ModelID: "flash-l"},
	}})
	if got := e.judgeModelID(context.Background(), "chat-l"); got != "flash-l" {
		t.Fatalf("judge=%s", got)
	}
}

func TestCompleteJudgeUsesBoundModel(t *testing.T) {
	adapter := &flashClassifyAdapter{}
	e := NewEngineWithGateway(roleCatalog{}, "test", streamTestLease{})
	e.SetCapabilityRoleStore(&memoryRoleStore{rows: []sqlite.CapabilityRoleBinding{
		{Role: "judge", ProviderID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", ModelID: "flash-l"},
	}})
	e.SetAdapterFactoryForTest(func(context.Context, provider.Provider) (gateway.Adapter, error) {
		return adapter, nil
	})
	resp, err := e.completeJudge(context.Background(), adapter, []byte("k"), "chat-l", gateway.Request{
		Messages: []gateway.Message{{Role: gateway.RoleUser, Content: "verify"}},
	})
	if err != nil || resp.Message.Content == "" {
		t.Fatalf("completeJudge err=%v resp=%+v", err, resp)
	}
	if len(adapter.models) != 1 || adapter.models[0] != "flash-l" {
		t.Fatalf("judge model=%v", adapter.models)
	}
}

func TestJudgeModelIDIgnoresUnallowedEqChat(t *testing.T) {
	e := NewEngine(roleCatalog{}, "test")
	e.SetCapabilityRoleStore(&memoryRoleStore{rows: []sqlite.CapabilityRoleBinding{
		{Role: "judge", ProviderID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", ModelID: "chat-l"},
	}})
	if got := e.judgeModelID(context.Background(), "chat-l"); got == "chat-l" {
		t.Fatalf("judge=session without allow must fall back to heuristic, got %s", got)
	}
	e.SetCapabilityRoleStore(&memoryRoleStore{rows: []sqlite.CapabilityRoleBinding{
		{Role: "judge", ProviderID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", ModelID: "chat-l", AllowJudgeEqChat: true},
	}})
	if got := e.judgeModelID(context.Background(), "chat-l"); got != "chat-l" {
		t.Fatalf("allowed judge=chat must keep binding, got %s", got)
	}
}

func TestPreferBoundCatalogFiltersRole(t *testing.T) {
	e := NewEngine(roleCatalog{}, "test")
	e.SetCapabilityRoleStore(&memoryRoleStore{rows: []sqlite.CapabilityRoleBinding{
		{Role: "vision", ProviderID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", ModelID: "flash-l"},
		{Role: "gui", ProviderID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", ModelID: "gui-m"},
	}})
	items, _ := roleCatalog{}.List(context.Background(), provider.Filter{})
	mixed := []provider.CatalogEntry{
		{Provider: items[0], Model: items[0].Models[0]},
		{Provider: items[0], Model: items[0].Models[1]},
	}
	vision := e.preferBoundCatalog(context.Background(), "vision", mixed)
	if len(vision) != 1 || vision[0].Model.ModelID != "flash-l" {
		t.Fatalf("vision catalog=%#v", vision)
	}
	gui := e.preferBoundCatalog(context.Background(), "gui", provider.CatalogForKind(items, provider.KindGUI))
	if len(gui) != 1 || gui[0].Model.ModelID != "gui-m" {
		t.Fatalf("gui catalog=%#v", gui)
	}
	empty := NewEngine(roleCatalog{}, "test")
	empty.SetCapabilityRoleStore(&memoryRoleStore{})
	if got := empty.preferBoundCatalog(context.Background(), "gui", provider.CatalogForKind(items, provider.KindGUI)); len(got) != 1 {
		t.Fatalf("empty gui role must keep catalog, got %d", len(got))
	}
}
