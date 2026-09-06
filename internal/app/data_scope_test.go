package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lunitide/lunitide/internal/domain/asset"
	"github.com/lunitide/lunitide/internal/domain/project"
	"github.com/lunitide/lunitide/internal/m9app"
	"github.com/lunitide/lunitide/internal/org"
	"github.com/oklog/ulid/v2"
)

func TestDataScopePersonalAndOrgKnownIDAndBindingFailure(t *testing.T) {
	ctx := context.Background()
	e, sessionID, store := agentRunEngine(t)
	e.SetAssetStorage(store)
	e.SetDataScopeStore(store)
	bindingPath := filepath.Join(t.TempDir(), "binding.json")
	binding := m9app.NewFileBindingStore(bindingPath)
	admin := m9app.NewOrgAdminService(org.NewService(org.NewGate(store.OrgStorage()), nil), binding)
	e.SetM9OrgAdminService(admin)
	orgA, err := admin.CreateOrg(ctx, "A")
	if err != nil {
		t.Fatal(err)
	}
	orgB, err := admin.CreateOrg(ctx, "B")
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]string{}
	for _, scope := range []string{"", orgA.OrgID, orgB.OrgID} {
		p, err := e.projects.Create(ctx, ulid.Make().String(), "test", map[string]string{"scope": scope}, project.Project{Name: "P", OrgID: scope})
		if err != nil {
			t.Fatal(err)
		}
		ids[scope] = p.ID
	}
	personal, err := store.CreateAssetTemplate(ctx, asset.AssetTemplate{Name: "Personal", TemplateType: asset.TemplateTypeDocument, Status: asset.StatusDraft})
	if err != nil {
		t.Fatal(err)
	}
	scoped, err := store.CreateAssetTemplate(ctx, asset.AssetTemplate{Name: "A", TemplateType: asset.TemplateTypeDocument, Status: asset.StatusDraft, OrgID: orgA.OrgID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = admin.Switch(ctx, orgA.OrgID); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ method, payload string }{
		{"project.update", fmt.Sprintf(`{"id":%q,"version":1,"name":"Changed"}`, ids[""])},
		{"project.update", fmt.Sprintf(`{"id":%q,"version":1,"name":"Changed"}`, ids[orgB.OrgID])},
		{"session.update", fmt.Sprintf(`{"id":%q,"version":1,"title":"Changed","pinned":false}`, sessionID)},
		{"template.enable", fmt.Sprintf(`{"id":%q,"expectedVersion":1}`, personal.ID)},
		{"project.update", fmt.Sprintf(`{"id":%q,"version":1,"name":"Changed"}`, ulid.Make().String())},
	} {
		r := validRequest(tc.method, tc.payload)
		r.IdempotencyKey = ulid.Make().String()
		out := e.Handle(ctx, r)
		if out.OK || out.Error.Code != "DATA_SCOPE_DENIED" {
			t.Fatalf("%s: %+v", tc.method, out.Error)
		}
	}
	for _, scope := range []string{orgA.OrgID, ""} {
		if err = binding.Save(ctx, scope); err != nil {
			t.Fatal(err)
		}
		out := e.Handle(ctx, validRequest("project.list", `{}`))
		if !out.OK {
			t.Fatal(out.Error)
		}
		body, _ := json.Marshal(out.Payload)
		var got struct {
			Items []projectDTO `json:"items"`
		}
		if err = json.Unmarshal(body, &got); err != nil {
			t.Fatal(err)
		}
		if len(got.Items) == 0 {
			t.Fatal("own scope empty")
		}
		for _, p := range got.Items {
			if p.OrgID != scope {
				t.Fatalf("scope %q leaked %q", scope, p.OrgID)
			}
		}
		out = e.Handle(ctx, validRequest("template.list", `{}`))
		if !out.OK {
			t.Fatal(out.Error)
		}
		body, _ = json.Marshal(out.Payload)
		var assets struct {
			Items []assetTemplateDTO `json:"items"`
		}
		_ = json.Unmarshal(body, &assets)
		want := personal.ID
		if scope != "" {
			want = scoped.ID
		}
		if len(assets.Items) != 1 || assets.Items[0].ID != want {
			t.Fatalf("assets %+v", assets)
		}
	}
	if err = os.WriteFile(bindingPath, []byte(`{"orgId":`), 0600); err != nil {
		t.Fatal(err)
	}
	for _, method := range []string{"project.list", "template.list"} {
		out := e.Handle(ctx, validRequest(method, `{}`))
		if out.OK || out.Error.Code != "DATA_SCOPE_UNAVAILABLE" {
			t.Fatalf("corrupt binding %s: %+v", method, out)
		}
	}
}

func TestDataScopeSwitchDoesNotDeadlockNestedFinalization(t *testing.T) {
	e, _, store := agentRunEngine(t)
	e.SetDataScopeStore(store)
	release, err := e.authorizeDataRequest(context.Background(), "chat.persist", json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { unlock := e.beginScopeSwitch(); unlock(); close(done) }()
	// Wait until the queued writer rejects new read leases. This checks the
	// nested finalization path without relying on a scheduling sleep.
	deadline := time.Now().Add(time.Second)
	for {
		nested, err := e.authorizeDataRequest(context.Background(), "chat.persist", json.RawMessage(`{}`))
		if err != nil {
			break
		}
		nested()
		if time.Now().After(deadline) {
			release()
			t.Fatal("switch did not queue")
		}
	}
	release()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("scope switch deadlocked")
	}
}

func TestTemplatePaginationTraversesEveryRowAndBindsFilter(t *testing.T) {
	e, _, store := agentRunEngine(t)
	e.SetAssetStorage(store)
	ctx := context.Background()
	for i := 0; i < 205; i++ {
		_, err := store.CreateAssetTemplate(ctx, asset.AssetTemplate{Name: fmt.Sprintf("Template %03d", i), TemplateType: asset.TemplateTypeDocument, Status: asset.StatusDraft})
		if err != nil {
			t.Fatal(err)
		}
	}
	seen := map[string]bool{}
	cursor := ""
	firstCursor := ""
	for page := 0; page < 3; page++ {
		out := handleTemplateList(e, ctx, validRequest("template.list", fmt.Sprintf(`{"cursor":%q}`, cursor)))
		if !out.OK {
			t.Fatal(out.Error)
		}
		raw, _ := json.Marshal(out.Payload)
		var got struct {
			Items      []assetTemplateDTO `json:"items"`
			NextCursor string             `json:"nextCursor"`
		}
		_ = json.Unmarshal(raw, &got)
		want := 100
		if page == 2 {
			want = 5
		}
		if len(got.Items) != want {
			t.Fatalf("page %d: %d", page, len(got.Items))
		}
		for _, a := range got.Items {
			if seen[a.ID] {
				t.Fatal("duplicate page item")
			}
			seen[a.ID] = true
		}
		cursor = got.NextCursor
		if page == 0 {
			firstCursor = cursor
		}
	}
	if len(seen) != 205 || cursor != "" {
		t.Fatal("lost rows or endless cursor")
	}
	out := handleTemplateList(e, ctx, validRequest("template.list", fmt.Sprintf(`{"cursor":%q,"query":"204"}`, firstCursor)))
	if out.OK {
		t.Fatal("accepted cursor from another filter")
	}
	out = handleTemplateList(e, ctx, validRequest("template.list", `{"query":"Template 000"}`))
	if !out.OK {
		t.Fatal(out.Error)
	}
	raw, _ := json.Marshal(out.Payload)
	var got struct {
		Items []assetTemplateDTO `json:"items"`
	}
	_ = json.Unmarshal(raw, &got)
	if len(got.Items) != 1 || got.Items[0].Name != "Template 000" {
		t.Fatalf("search skipped oldest row: %+v", got)
	}
}
