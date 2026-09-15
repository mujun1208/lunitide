package app

import (
	"context"
	"strings"
	"testing"

	"github.com/lunitide/lunitide/internal/domain/project"
	"github.com/lunitide/lunitide/internal/projectrules"
)

func TestPersonalChatGenerateRequiresRoot(t *testing.T) {
	ctx := context.Background()
	e, svc, created, _ := factoryEngine(t)
	cleared, err := svc.Mutate(ctx, "personal-no-root", "test", "project.update", created.ID, created.Version, map[string]string{"t": "1"}, func(p *project.Project) error {
		p.RootPath = ""
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	resp := handleProjectFactory(e, ctx, validRequest("project.deliverable.generate", `{"projectId":"`+cleared.ID+`","phase":1}`))
	if resp.OK || resp.Error == nil || resp.Error.Code != "PROJECT_ROOT_REQUIRED" {
		t.Fatalf("%#v", resp)
	}
}

func TestProjectFactoryGuidanceSkippedWithoutPhase(t *testing.T) {
	ctx := context.Background()
	e, _, created, root := factoryEngine(t)
	if _, err := projectrules.Materialize(root, projectrules.Input{
		ProjectID: created.ID, DevStandard: "只用 SQLite，禁止 MySQL。", TechStandard: "接口清单是唯一主人。",
	}); err != nil {
		t.Fatal(err)
	}
	if g := e.projectFactoryGuidance(ctx, created.ID, 0); g != "" {
		t.Fatalf("personal/phase0 leaked rules: %s", g)
	}
	if g := e.projectFactoryGuidance(ctx, "", 1); g != "" {
		t.Fatalf("empty projectId leaked rules: %s", g)
	}
	got := e.projectFactoryGuidance(ctx, created.ID, 1)
	if !strings.Contains(got, "只用 SQLite") {
		t.Fatalf("phase session missing rules: %q", got)
	}
}

func TestPersonalChatToolsOmitDeliverableDraft(t *testing.T) {
	e, _, _, _ := factoryEngine(t)
	for _, d := range e.chatTurnToolDefinitions(chatTurnToolBuild{Profile: toolProfileDefault}) {
		if d.Name == "deliverable.draft" {
			t.Fatal("personal chat must not advertise deliverable.draft")
		}
	}
}
