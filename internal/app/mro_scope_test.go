package app

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/lunitide/lunitide/internal/m9app"
	"github.com/lunitide/lunitide/internal/mroapp"
	"github.com/lunitide/lunitide/internal/org"
)

func TestMROEngineTrustedScopeReceiptsAndKnownForeignID(t *testing.T) {
	ctx := context.Background()
	e, _, store := agentRunEngine(t)
	e.SetDataScopeStore(store)
	e.SetMROService(mroapp.New(store))
	binding := m9app.NewFileBindingStore(filepath.Join(t.TempDir(), "binding.json"))
	admin := m9app.NewOrgAdminService(org.NewService(org.NewGate(store.OrgStorage()), nil), binding)
	e.SetM9OrgAdminService(admin)
	organization, err := admin.CreateOrg(ctx, "Test organization")
	if err != nil {
		t.Fatal(err)
	}
	payload := `{"toolNo":"same-tool","calibDue":"2099-01-01"}`
	for _, scope := range []string{"", organization.OrgID} {
		if err = binding.Save(ctx, scope); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 2; i++ {
			r := e.Handle(ctx, mroMutationRequest("mro.tool.upsert", payload, "same-request-key"))
			if !r.OK {
				t.Fatalf("scope %q create/replay: %+v", scope, r.Error)
			}
		}
		items, err := e.mro.ListToolViews(mroapp.WithScope(ctx, scope))
		if err != nil || len(items) != 1 {
			t.Fatalf("scope %q duplicated %+v %v", scope, items, err)
		}
		changed := e.Handle(ctx, mroMutationRequest("mro.tool.upsert", `{"toolNo":"changed"}`, "same-request-key"))
		if changed.OK || changed.Error.Code != "MRO_CONFLICT" {
			t.Fatalf("changed payload: %+v", changed)
		}
	}
	personal, err := e.mro.ListToolViews(ctx)
	if err != nil {
		t.Fatal(err)
	}
	foreign := e.Handle(ctx, mroMutationRequest("mro.tool.checkout", fmt.Sprintf(`{"toolId":%q,"holder":"other-org"}`, personal[0].ID), "foreign-tool"))
	if foreign.OK {
		t.Fatal("known foreign tool was checked out")
	}
	listed := e.Handle(ctx, validRequest("mro.tool.list", `{}`))
	if !listed.OK {
		t.Fatal(listed.Error)
	}
	raw, _ := json.Marshal(listed.Payload)
	var body struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	if err = json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Items) != 1 || body.Items[0].ID == personal[0].ID {
		t.Fatalf("foreign row leaked: %s", raw)
	}
	// Renderer ownership claims are rejected by strict decoding, never injected.
	spoof := e.Handle(ctx, mroMutationRequest("mro.tool.upsert", `{"toolNo":"spoof","orgId":""}`, "spoof"))
	if spoof.OK {
		t.Fatal("renderer scope claim accepted")
	}
}
