package m8app_test

import (
	"context"
	"testing"

	"github.com/lunitide/lunitide/internal/domain/m8core"
	"github.com/lunitide/lunitide/internal/m8app"
)

func TestDetachBoundKeysDoesNotRefloorRemovedKeys(t *testing.T) {
	store := openSliceStore(t)
	svc := m8app.NewExpertService(store.AgentRuntimeRepository(), "local-user", &m8app.MemoryPersonaStore{})
	svc.SetSkillStore(store)
	ctx := context.Background()
	res, err := svc.Create(ctx, m8app.CreateInput{
		Source:         m8core.ExpertSourceLocal,
		Frontmatter:    fm("演示顾问"),
		SixSection:     sixBody("detach-ppt"),
		RequestID:      "req-detach-ppt",
		CatalogItemID:  "ppt-expert",
		CreationOrigin: m8core.ExpertOriginCatalog,
		SkillKeys:      []string{"slide-builder", "mcp:playwright"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.DetachBoundKeys(ctx, []string{"slide-builder", "mcp:playwright"}); err != nil {
		t.Fatal(err)
	}
	got, err := svc.ListBoundSkills(ctx, res.ExpertID)
	if err != nil {
		t.Fatal(err)
	}
	if containsStr(got, "slide-builder") || containsStr(got, "mcp:playwright") {
		t.Fatalf("detach re-applied catalog floor: %#v", got)
	}
}

func TestAttachDeclaredKeysOnlyTouchesCatalogExperts(t *testing.T) {
	store := openSliceStore(t)
	svc := m8app.NewExpertService(store.AgentRuntimeRepository(), "local-user", &m8app.MemoryPersonaStore{})
	svc.SetSkillStore(store)
	ctx := context.Background()
	ppt, err := svc.Create(ctx, m8app.CreateInput{
		Source:         m8core.ExpertSourceLocal,
		Frontmatter:    fm("演示顾问"),
		SixSection:     sixBody("attach-ppt"),
		RequestID:      "req-attach-ppt",
		CatalogItemID:  "ppt-expert",
		CreationOrigin: m8core.ExpertOriginCatalog,
	})
	if err != nil {
		t.Fatal(err)
	}
	custom, err := svc.Create(ctx, m8app.CreateInput{
		Source:      m8core.ExpertSourceLocal,
		Frontmatter: fm("短剧专家"),
		SixSection:  sixBody("attach-custom"),
		RequestID:   "req-attach-custom",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.AttachDeclaredKeys(ctx, []string{"tpl-slide-builder", "mcp:playwright"}); err != nil {
		t.Fatal(err)
	}
	pptKeys, err := svc.ListBoundSkills(ctx, ppt.ExpertID)
	if err != nil {
		t.Fatal(err)
	}
	if !containsStr(pptKeys, "slide-builder") || !containsStr(pptKeys, "mcp:playwright") {
		t.Fatalf("catalog expert missing declared keys: %#v", pptKeys)
	}
	customKeys, err := svc.ListBoundSkills(ctx, custom.ExpertID)
	if err != nil {
		t.Fatal(err)
	}
	if containsStr(customKeys, "slide-builder") || containsStr(customKeys, "mcp:playwright") {
		t.Fatalf("custom expert received guessed keys: %#v", customKeys)
	}
}

func TestApplySkillFloorRespectsBindKeyPresence(t *testing.T) {
	store := openSliceStore(t)
	svc := m8app.NewExpertService(store.AgentRuntimeRepository(), "local-user", &m8app.MemoryPersonaStore{})
	svc.SetSkillStore(store)
	svc.SetBindKeyPresence(func(_ context.Context, key string) bool {
		return key == "slide-builder"
	})
	ctx := context.Background()
	res, err := svc.Create(ctx, m8app.CreateInput{
		Source:         m8core.ExpertSourceLocal,
		Frontmatter:    fm("演示顾问"),
		SixSection:     sixBody("presence-ppt"),
		RequestID:      "req-presence-ppt",
		CatalogItemID:  "ppt-expert",
		CreationOrigin: m8core.ExpertOriginCatalog,
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := svc.ReplaceBoundSkills(ctx, res.ExpertID, []string{})
	if err != nil {
		t.Fatal(err)
	}
	if !containsStr(got, "slide-builder") {
		t.Fatalf("present floor key missing: %#v", got)
	}
	if containsStr(got, "mcp:playwright") {
		t.Fatalf("absent MCP still floored: %#v", got)
	}
}
