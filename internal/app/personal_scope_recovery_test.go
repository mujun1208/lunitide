package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/lunitide/lunitide/internal/domain/project"
	"github.com/lunitide/lunitide/internal/m9app"
	"github.com/lunitide/lunitide/internal/messageapp"
	"github.com/lunitide/lunitide/internal/org"
	"github.com/oklog/ulid/v2"
)

func TestLegacyPersonalScopeRecoveryPreservesIDsAndOrganizationIsolation(t *testing.T) {
	ctx := context.Background()
	e, sessionID, store := agentRunEngine(t)
	messages, err := messageapp.New(store, store, []byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	e.messages = messages
	e.SetDataScopeStore(store)
	path := filepath.Join(t.TempDir(), "binding.json")
	binding := m9app.NewFileBindingStore(path)
	admin := m9app.NewOrgAdminService(org.NewService(org.NewGate(store.OrgStorage()), nil), binding)
	e.SetM9OrgAdminService(admin)
	if err := m9app.EnsureDefaultOrgBinding(ctx, admin); err != nil {
		t.Fatal(err)
	}
	before, err := admin.Summary(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// Reproduce the released profile: an implicit default org plus legacy
	// personal project/session records, not a post-fix explicit selection.
	raw, _ := json.Marshal(map[string]string{"orgId": before.BoundOrgID})
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	list := func() []projectDTO {
		t.Helper()
		res := e.Handle(ctx, validRequest("project.list", `{}`))
		var page struct {
			Items []projectDTO `json:"items"`
		}
		if !res.OK || decodeResponsePayload(res.Payload, &page) != nil {
			t.Fatalf("project list: %+v", res)
		}
		return page.Items
	}
	if len(list()) != 0 {
		t.Fatal("fixture must reproduce hidden personal projects")
	}
	personal, scoped, err := store.DesktopScopePresence(ctx)
	if err != nil || !personal || scoped {
		t.Fatalf("presence: %v %v %v", personal, scoped, err)
	}
	if err := m9app.RestoreLegacyPersonalBinding(ctx, admin, personal, scoped); err != nil {
		t.Fatal(err)
	}
	if err := m9app.EnsureDefaultOrgBinding(ctx, admin); err != nil {
		t.Fatal(err)
	}
	if len(list()) == 0 {
		t.Fatal("old personal project remains hidden")
	}
	if response := e.Handle(ctx, validRequest("message.list", fmt.Sprintf(`{"sessionId":%q}`, sessionID))); !response.OK {
		t.Fatalf("old history is inaccessible: %+v", response)
	}
	// A new service reading the same file must retain personal selection.
	restarted := m9app.NewOrgAdminService(org.NewService(org.NewGate(store.OrgStorage()), nil), m9app.NewFileBindingStore(path))
	if err := m9app.EnsureDefaultOrgBinding(ctx, restarted); err != nil {
		t.Fatal(err)
	}
	summary, err := restarted.Summary(ctx)
	if err != nil || summary.BoundOrgID != "" {
		t.Fatalf("restart changed personal selection: %+v %v", summary, err)
	}
	other, err := admin.CreateOrg(ctx, "Another organization")
	if err != nil {
		t.Fatal(err)
	}
	p, err := e.projects.Create(ctx, ulid.Make().String(), "test", map[string]string{"orgId": other.OrgID}, project.Project{Name: "Other project", OrgID: other.OrgID})
	if err != nil {
		t.Fatal(err)
	}
	if response := e.Handle(ctx, validRequest("session.list", fmt.Sprintf(`{"projectId":%q}`, p.ID))); response.OK {
		t.Fatal("personal space exposed organization data")
	}
	if _, err := admin.Switch(ctx, other.OrgID); err != nil {
		t.Fatal(err)
	}
	if response := e.Handle(ctx, validRequest("message.list", fmt.Sprintf(`{"sessionId":%q}`, sessionID))); response.OK {
		t.Fatal("organization exposed personal history")
	}
	r := validRequest("org.selectPersonal", `{}`)
	r.IdempotencyKey = "select-personal"
	if response := e.Handle(ctx, r); !response.OK {
		t.Fatalf("select personal: %+v", response)
	}
	if response := e.Handle(ctx, validRequest("message.list", fmt.Sprintf(`{"sessionId":%q}`, sessionID))); !response.OK {
		t.Fatalf("personal history did not return: %+v", response)
	}
}

func TestLegacyPersonalScopeRecoveryKeepsExplicitOrAmbiguousBinding(t *testing.T) {
	for _, scenario := range []string{"explicit-choice", "scoped-data", "multiple-orgs", "custom-org"} {
		t.Run(scenario, func(t *testing.T) {
			ctx := context.Background()
			_, _, store := agentRunEngine(t)
			path := filepath.Join(t.TempDir(), "binding.json")
			binding := m9app.NewFileBindingStore(path)
			admin := m9app.NewOrgAdminService(org.NewService(org.NewGate(store.OrgStorage()), nil), binding)
			if err := m9app.EnsureDefaultOrgBinding(ctx, admin); err != nil {
				t.Fatal(err)
			}
			original, err := admin.Summary(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "multiple-orgs" || scenario == "custom-org" {
				other, err := admin.CreateOrg(ctx, "Custom")
				if err != nil {
					t.Fatal(err)
				}
				if scenario == "custom-org" {
					if _, err := admin.Switch(ctx, other.OrgID); err != nil {
						t.Fatal(err)
					}
					original, _ = admin.Summary(ctx)
				}
			}
			if scenario != "explicit-choice" {
				raw, _ := json.Marshal(map[string]string{"orgId": original.BoundOrgID})
				if err := os.WriteFile(path, raw, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if err := m9app.RestoreLegacyPersonalBinding(ctx, admin, true, scenario == "scoped-data"); err != nil {
				t.Fatal(err)
			}
			after, err := admin.Summary(ctx)
			if err != nil || after.BoundOrgID != original.BoundOrgID {
				t.Fatalf("explicit/ambiguous scope changed: %+v %v", after, err)
			}
		})
	}
}
