package app

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/lunitide/lunitide/internal/domain/m8core"
	"github.com/lunitide/lunitide/internal/m7app"
	"github.com/lunitide/lunitide/internal/m8app"
	"github.com/lunitide/lunitide/internal/mcapp"
	"github.com/lunitide/lunitide/internal/skillapp"
	storage "github.com/lunitide/lunitide/internal/storage/sqlite"
)

func newSkillExpertBindEngine(t *testing.T) *Engine {
	t.Helper()
	store, err := storage.OpenTemplated(context.Background(), filepath.Join(t.TempDir(), "skill-expert-bind.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	e := NewEngine(nil, "test")
	e.skills = skillapp.New(store, store)
	svc := m8app.NewExpertService(store.AgentRuntimeRepository(), "local-user", &m8app.MemoryPersonaStore{})
	svc.SetSkillStore(store)
	e.SetM8ExpertService(svc)
	e.SetM7RuntimeServices(nil, nil, m7app.NewMcpRuntimeService(store.AgentRuntimeRepository()))
	e.SetMcMarketService(mcapp.New(store.AgentRuntimeRepository()))
	return e
}

func createCatalogPPT(t *testing.T, e *Engine, requestID string) string {
	t.Helper()
	res, err := e.m8expert.Create(context.Background(), m8app.CreateInput{
		Source: m8core.ExpertSourceLocal,
		Frontmatter: m8core.Frontmatter{
			Name: "PPT专家", Division: m8core.DivisionProduct,
			Description: "演示", Semver: "1.0.0",
		},
		SixSection: m8core.SixSection{
			Identity: "i", Mission: "m", Rules: "r", Workflow: "w",
			DeliverableTemplate: "d", SuccessMetrics: "s",
		},
		RequestID:      requestID,
		CatalogItemID:  "ppt-expert",
		CreationOrigin: m8core.ExpertOriginCatalog,
	})
	if err != nil {
		t.Fatal(err)
	}
	return res.ExpertID
}

func TestSkillInstallSameVersionSucceeds(t *testing.T) {
	e := newSkillExpertBindEngine(t)
	ctx := context.Background()
	first := handleSkillInstall(e, ctx, lifecyclePayload(t, map[string]any{"templateId": "meeting-minutes"}))
	if !first.OK {
		t.Fatalf("first install: %+v", first.Error)
	}
	second := handleSkillInstall(e, ctx, lifecyclePayload(t, map[string]any{"templateId": "meeting-minutes"}))
	if !second.OK {
		t.Fatalf("same-version install should succeed: %+v", second.Error)
	}
	var a, b struct {
		SkillID string `json:"skillId"`
	}
	if err := json.Unmarshal(mustJSON(first.Payload), &a); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(mustJSON(second.Payload), &b); err != nil {
		t.Fatal(err)
	}
	if a.SkillID == "" || a.SkillID != b.SkillID {
		t.Fatalf("same-version skill ids = %q / %q", a.SkillID, b.SkillID)
	}
}

func TestSkillInstallAttachesDeclaredExpertKeys(t *testing.T) {
	e := newSkillExpertBindEngine(t)
	ctx := context.Background()
	pptID := createCatalogPPT(t, e, "req-bind-ppt")
	custom, err := e.m8expert.Create(ctx, m8app.CreateInput{
		Source: m8core.ExpertSourceLocal,
		Frontmatter: m8core.Frontmatter{
			Name: "短剧专家", Division: m8core.DivisionProduct,
			Description: "短剧", Semver: "1.0.0",
		},
		SixSection: m8core.SixSection{
			Identity: "i", Mission: "m", Rules: "r", Workflow: "w",
			DeliverableTemplate: "d", SuccessMetrics: "s",
		},
		RequestID: "req-bind-custom",
	})
	if err != nil {
		t.Fatal(err)
	}
	installed := handleSkillInstall(e, ctx, lifecyclePayload(t, map[string]any{"templateId": "slide-builder"}))
	if !installed.OK {
		t.Fatalf("install: %+v", installed.Error)
	}
	pptKeys, err := e.m8expert.ListBoundSkills(ctx, pptID)
	if err != nil {
		t.Fatal(err)
	}
	if !containsBoundKey(pptKeys, "slide-builder") {
		t.Fatalf("catalog expert missing installed skill: %#v", pptKeys)
	}
	customKeys, err := e.m8expert.ListBoundSkills(ctx, custom.ExpertID)
	if err != nil {
		t.Fatal(err)
	}
	if containsBoundKey(customKeys, "slide-builder") {
		t.Fatalf("custom expert received guessed skill: %#v", customKeys)
	}
}

func TestSkillDeleteDetachesExpertKeys(t *testing.T) {
	e := newSkillExpertBindEngine(t)
	ctx := context.Background()
	pptID := createCatalogPPT(t, e, "req-del-ppt")
	installed := handleSkillInstall(e, ctx, lifecyclePayload(t, map[string]any{"templateId": "slide-builder"}))
	if !installed.OK {
		t.Fatalf("install: %+v", installed.Error)
	}
	var installedPayload struct {
		SkillID string `json:"skillId"`
	}
	if err := json.Unmarshal(mustJSON(installed.Payload), &installedPayload); err != nil {
		t.Fatal(err)
	}
	sk, err := e.skills.Get(ctx, installedPayload.SkillID)
	if err != nil {
		t.Fatal(err)
	}
	deleted := handleSkillDelete(e, ctx, lifecyclePayload(t, map[string]any{"id": sk.ID, "expectedVersion": sk.Rev}))
	if !deleted.OK {
		t.Fatalf("delete published: %+v", deleted.Error)
	}
	keys, err := e.m8expert.ListBoundSkills(ctx, pptID)
	if err != nil {
		t.Fatal(err)
	}
	if containsBoundKey(keys, "slide-builder") {
		t.Fatalf("delete left expert binding: %#v", keys)
	}
	floored, err := e.m8expert.ReplaceBoundSkills(ctx, pptID, []string{})
	if err != nil {
		t.Fatal(err)
	}
	if containsBoundKey(floored, "slide-builder") {
		t.Fatalf("missing skill still floored: %#v", floored)
	}
}

func TestMcpAddAndUninstallSyncsDeclaredExpertKeys(t *testing.T) {
	e := newSkillExpertBindEngine(t)
	ctx := context.Background()
	pptID := createCatalogPPT(t, e, "req-mcp-ppt")
	custom, err := e.m8expert.Create(ctx, m8app.CreateInput{
		Source: m8core.ExpertSourceLocal,
		Frontmatter: m8core.Frontmatter{
			Name: "短剧专家", Division: m8core.DivisionProduct,
			Description: "短剧", Semver: "1.0.0",
		},
		SixSection: m8core.SixSection{
			Identity: "i", Mission: "m", Rules: "r", Workflow: "w",
			DeliverableTemplate: "d", SuccessMetrics: "s",
		},
		RequestID: "req-mcp-custom",
	})
	if err != nil {
		t.Fatal(err)
	}
	addReq := lifecyclePayload(t, map[string]any{
		"origin": "manual", "transport": "stdio", "command": "npx",
		"args": []string{"-y", "@playwright/mcp"}, "riskConfirmed": true,
		"requestId": "req-mcp-add", "actor": "tester",
	})
	addReq.IdempotencyKey = "mcp-add-playwright"
	added := handleMcpAdd(e, ctx, addReq)
	if !added.OK {
		t.Fatalf("mcp.add: %+v", added.Error)
	}
	var addedPayload struct {
		EndpointID string `json:"endpointId"`
	}
	if err := json.Unmarshal(mustJSON(added.Payload), &addedPayload); err != nil {
		t.Fatal(err)
	}
	pptKeys, err := e.m8expert.ListBoundSkills(ctx, pptID)
	if err != nil {
		t.Fatal(err)
	}
	if !containsBoundKey(pptKeys, "mcp:playwright") {
		t.Fatalf("catalog expert missing installed MCP: %#v", pptKeys)
	}
	customKeys, err := e.m8expert.ListBoundSkills(ctx, custom.ExpertID)
	if err != nil {
		t.Fatal(err)
	}
	if containsBoundKey(customKeys, "mcp:playwright") {
		t.Fatalf("custom expert received guessed MCP: %#v", customKeys)
	}
	token, _, err := e.mcmarket.IssueConfirmToken(ctx, mcapp.ConfirmMethodUninstall, addedPayload.EndpointID, "")
	if err != nil {
		t.Fatal(err)
	}
	uninstalled := handleMcConnectorUninstall(e, ctx, lifecyclePayload(t, map[string]any{
		"endpointId": addedPayload.EndpointID, "confirmToken": token,
	}))
	if !uninstalled.OK {
		t.Fatalf("uninstall: %+v", uninstalled.Error)
	}
	after, err := e.m8expert.ListBoundSkills(ctx, pptID)
	if err != nil {
		t.Fatal(err)
	}
	if containsBoundKey(after, "mcp:playwright") {
		t.Fatalf("uninstall left expert MCP binding: %#v", after)
	}
}

func containsBoundKey(keys []string, want string) bool {
	for _, key := range keys {
		if key == want || m8app.CanonicalDeclaredKey(key) == m8app.CanonicalDeclaredKey(want) {
			return true
		}
	}
	return false
}
