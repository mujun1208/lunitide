package m8app_test

import (
	"context"
	"testing"

	"github.com/lunitide/lunitide/internal/domain/m8core"
	"github.com/lunitide/lunitide/internal/m8app"
)

// The expert list used to report how large the factory kit is, so an expert the
// user had stripped down to one skill still advertised five. These assert the
// list reports what the expert actually carries, and flags the rows where
// nothing was ever saved instead of reporting a bare zero.
func TestExpertListReportsStoredEquipmentNotTheFactoryKit(t *testing.T) {
	ctx := context.Background()
	store := openSliceStore(t)
	svc := m8app.NewExpertService(store.AgentRuntimeRepository(), "local-user", &m8app.MemoryPersonaStore{})
	svc.SetSkillStore(store)
	created, err := svc.Create(ctx, m8app.CreateInput{
		Source: m8core.ExpertSourceLocal, Frontmatter: fm("count-fixture"), SixSection: sixBody("counts"),
		RequestID: "counts", SkillKeys: []string{"docx-writer", "slide-builder", "mcp:fetch", "brain:codex"},
	})
	if err != nil {
		t.Fatal(err)
	}
	find := func(t *testing.T) m8app.ExpertListItem {
		t.Helper()
		listed, err := svc.List(ctx, m8app.ExpertFilter{})
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range listed.Experts {
			if item.ExpertID == created.ExpertID {
				return item
			}
		}
		t.Fatalf("expert missing from list of %d", len(listed.Experts))
		return m8app.ExpertListItem{}
	}
	row := find(t)
	// Two skills, one MCP, one brain. The brain is not equipment the user picked
	// from either list, so it must not inflate either count.
	if row.BoundSkillCount != 2 || row.BoundMcpCount != 1 {
		t.Fatalf("stored counts: skills=%d mcp=%d, want 2/1", row.BoundSkillCount, row.BoundMcpCount)
	}
	if !row.BoundConfigured {
		t.Fatal("an expert with saved bindings must not be reported as running the default kit")
	}

	// Strip it down to a single skill. The row must follow the user's edit, not
	// keep quoting the kit the template ships with.
	if _, err = svc.ReplaceBoundSkillsVersioned(ctx, created.ExpertID, created.VersionID, []string{"slide-builder"}); err != nil {
		t.Fatal(err)
	}
	if row = find(t); row.BoundSkillCount != 1 || row.BoundMcpCount != 0 {
		t.Fatalf("after strip: skills=%d mcp=%d, want 1/0", row.BoundSkillCount, row.BoundMcpCount)
	}
}

func TestExpertListFallsBackToTheKitWhenNothingWasEverSaved(t *testing.T) {
	ctx := context.Background()
	store := openSliceStore(t)
	svc := m8app.NewExpertService(store.AgentRuntimeRepository(), "local-user", &m8app.MemoryPersonaStore{})
	svc.SetSkillStore(store)
	created, err := svc.Create(ctx, m8app.CreateInput{
		Source: m8core.ExpertSourceLocal, Frontmatter: fm("PPT专家"), SixSection: sixBody("kit floor"), RequestID: "floor",
	})
	if err != nil {
		t.Fatal(err)
	}
	listed, err := svc.List(ctx, m8app.ExpertFilter{})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range listed.Experts {
		if item.ExpertID != created.ExpertID {
			continue
		}
		if item.BoundConfigured {
			t.Fatal("an expert with no saved bindings must be reported as running the default kit")
		}
		// A catalog expert always has a kit, so reporting zero would tell the
		// user it carries nothing when a turn would in fact load the kit.
		if item.BoundSkillCount == 0 && item.BoundMcpCount == 0 {
			t.Fatal("default-kit row reported as carrying nothing")
		}
		return
	}
	t.Fatal("expert missing from list")
}
