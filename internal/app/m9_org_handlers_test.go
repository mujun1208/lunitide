// T-9.1.3 handler tests: the ten org.* bridge methods run the full
// bootstrap -> bind -> activate -> space/member lifecycle over the real
// 0069 schema, and every org-scoped method fails closed with M9-003 while
// no operator binding exists.
package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/lunitide/lunitide/internal/bridge"
	"github.com/lunitide/lunitide/internal/m9app"
	"github.com/lunitide/lunitide/internal/org"
	sqlitestore "github.com/lunitide/lunitide/internal/storage/sqlite"
	"github.com/lunitide/lunitide/migrations"
)

func newOrgEngine(t *testing.T) *Engine {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	body, err := migrations.Files.ReadFile("0069_m9_org_foundation.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(string(body)); err != nil {
		t.Fatal(err)
	}
	e := NewEngine(nil, "test")
	e.SetM9OrgAdminService(m9app.NewOrgAdminService(
		org.NewService(org.NewGate(sqlitestore.NewOrgStorage(db)), nil),
		m9app.NewFileBindingStore(filepath.Join(t.TempDir(), "binding.json")),
	))
	return e
}

func orgCall(t *testing.T, e *Engine, method bridge.Method, payload any) bridge.Response {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	r := bridge.Request{ID: "01AAAAAAAAAAAAAAAAAAAAAAAA", TraceID: "01BBBBBBBBBBBBBBBBBBBBBBB", Method: string(method), Payload: raw}
	handler, ok := RuntimeHandlers[method]
	if !ok || handler == nil {
		t.Fatalf("method %s missing from RuntimeHandlers", method)
	}
	return handler(e, context.Background(), r)
}

func orgResult[Out any](t *testing.T, e *Engine, method bridge.Method, payload any) Out {
	t.Helper()
	resp := orgCall(t, e, method, payload)
	if !resp.OK {
		t.Fatalf("%s failed: %+v", method, resp.Error)
	}
	raw, err := json.Marshal(resp.Payload)
	if err != nil {
		t.Fatal(err)
	}
	var out Out
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode %s payload: %v", method, err)
	}
	return out
}

type orgStateView struct {
	OrgID string `json:"orgId"`
	State string `json:"state"`
}

func TestOrgAdminHandlersLifecycle(t *testing.T) {
	e := newOrgEngine(t)

	t.Run("unbound operations fail closed with M9-003", func(t *testing.T) {
		for _, method := range []bridge.Method{bridge.MethodOrgSpaceList, bridge.MethodOrgMemberList, bridge.MethodOrgActivate, bridge.MethodOrgSuspend, bridge.MethodOrgSpaceCreate, bridge.MethodOrgMemberInvite} {
			resp := orgCall(t, e, method, map[string]string{})
			if resp.OK || resp.Error == nil || resp.Error.Code != "M9-003" {
				t.Fatalf("%s: want M9-003 fail-closed, got %+v", method, resp.Error)
			}
		}
	})

	t.Run("bootstrap create -> switch -> activate", func(t *testing.T) {
		created := orgResult[orgStateView](t, e, bridge.MethodOrgCreate, map[string]string{"name": "Alpha-Org"})
		if created.State != org.OrgDraft {
			t.Fatalf("create state = %s", created.State)
		}

		switched := orgResult[orgStateView](t, e, bridge.MethodOrgSwitch, map[string]string{"orgId": created.OrgID})
		if switched.OrgID != created.OrgID {
			t.Fatalf("switch drift: %+v", switched)
		}

		activated := orgResult[orgStateView](t, e, bridge.MethodOrgActivate, struct{}{})
		if activated.State != org.OrgActive {
			t.Fatalf("activate state = %s", activated.State)
		}

		summary := orgResult[m9app.SummaryResult](t, e, bridge.MethodOrgSummary, struct{}{})
		if summary.BoundOrgID != created.OrgID || summary.Org == nil || summary.Org.State != org.OrgActive {
			t.Fatalf("summary drift: %+v", summary)
		}
		if len(summary.Orgs) != 1 || summary.Orgs[0].OrgID != created.OrgID {
			t.Fatalf("summary orgs drift: %+v", summary.Orgs)
		}
	})

	t.Run("space and member lifecycle inside the bound org", func(t *testing.T) {
		space := orgResult[struct {
			SpaceID string `json:"spaceId"`
			State   string `json:"state"`
		}](t, e, bridge.MethodOrgSpaceCreate, map[string]string{"name": "core-team"})
		if space.State != "active" {
			t.Fatalf("space state = %s", space.State)
		}

		list := orgResult[struct {
			Spaces []struct {
				SpaceID string `json:"spaceId"`
			} `json:"spaces"`
		}](t, e, bridge.MethodOrgSpaceList, struct{}{})
		if len(list.Spaces) != 1 || list.Spaces[0].SpaceID != space.SpaceID {
			t.Fatalf("space list drift: %+v", list)
		}

		member := orgResult[struct {
			PrincipalID string `json:"principalId"`
			State       string `json:"state"`
		}](t, e, bridge.MethodOrgMemberInvite, map[string]string{"displayName": "alice"})
		if member.State != org.PrincipalActive {
			t.Fatalf("invite state = %s", member.State)
		}

		members := orgResult[struct {
			Members []struct {
				Principal struct {
					PrincipalID string `json:"principalId"`
				} `json:"principal"`
				Bindings []struct {
					BindingID string `json:"bindingId"`
				} `json:"bindings"`
			} `json:"members"`
		}](t, e, bridge.MethodOrgMemberList, struct{}{})
		if len(members.Members) != 1 || members.Members[0].Principal.PrincipalID != member.PrincipalID {
			t.Fatalf("member list drift: %+v", members)
		}

		revoked := orgResult[struct {
			PrincipalID    string `json:"principalId"`
			State          string `json:"state"`
			BindingVersion int    `json:"bindingVersion"`
		}](t, e, bridge.MethodOrgMemberRevoke, map[string]string{"principalId": member.PrincipalID})
		if revoked.State != org.PrincipalRevoked || revoked.BindingVersion != 2 {
			t.Fatalf("revoke drift: %+v", revoked)
		}
	})

	t.Run("suspend refuses further writes with M9-002 and resumes", func(t *testing.T) {
		suspended := orgResult[orgStateView](t, e, bridge.MethodOrgSuspend, struct{}{})
		if suspended.State != org.OrgSuspended {
			t.Fatalf("suspend state = %s", suspended.State)
		}
		resp := orgCall(t, e, bridge.MethodOrgSpaceCreate, map[string]string{"name": "nope"})
		if resp.OK || resp.Error.Code != "M9-002" {
			t.Fatalf("want M9-002 after suspend, got %+v", resp.Error)
		}
		resumed := orgResult[orgStateView](t, e, bridge.MethodOrgActivate, struct{}{})
		if resumed.State != org.OrgActive {
			t.Fatalf("resume state = %s", resumed.State)
		}
	})

	t.Run("switch to unknown org answers M9-001", func(t *testing.T) {
		resp := orgCall(t, e, bridge.MethodOrgSwitch, map[string]string{"orgId": "01AAAAAAAAAAAAAAAAAAAAAAAA"})
		if resp.OK || resp.Error.Code != "M9-001" {
			t.Fatalf("want M9-001, got %+v", resp.Error)
		}
	})
}
